// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package vault

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul-k8s/acceptance/framework/vault"
	"github.com/hashicorp/go-uuid"
	"github.com/hashicorp/go-version"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	crlog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func TestVault_Partitions(t *testing.T) {
	// Set up logger for controller-runtime used by the Vault Helm chart hooks.
	crlog.SetLogger(zap.New(zap.UseDevMode(true)))

	env := suite.Environment()
	cfg := suite.Config()
	serverClusterCtx := env.DefaultContext(t)
	clientClusterCtx := env.Context(t, 1)
	ns := serverClusterCtx.KubectlOptions(t).Namespace
	clientNs := clientClusterCtx.KubectlOptions(t).Namespace

	const secondaryPartition = "secondary"

	ver, err := version.NewVersion("1.12.0")
	require.NoError(t, err)
	if cfg.ConsulVersion != nil && cfg.ConsulVersion.LessThan(ver) {
		t.Skipf("skipping this test because vault secrets backend is not supported in version %v", cfg.ConsulVersion.String())
	}
	if !cfg.EnableEnterprise {
		t.Skipf("skipping this test because -enable-enterprise is not set")
	}
	if !cfg.EnableMultiCluster {
		t.Skipf("skipping this test because -enable-multi-cluster is not set")
	}
	vaultReleaseName := helpers.RandomName()
	consulReleaseName := helpers.RandomName()

	// In the primary cluster, we will expose Vault server as a Load balancer
	// or a NodePort service so that the secondary can connect to it.
	serverClusterVaultHelmValues := map[string]string{
		"server.service.type": "LoadBalancer",
	}
	if cfg.UseKind {
		serverClusterVaultHelmValues["server.service.type"] = "NodePort"
		serverClusterVaultHelmValues["server.service.nodePort"] = "31000"
	}
	serverClusterVault := vault.NewVaultCluster(t, serverClusterCtx, cfg, vaultReleaseName, serverClusterVaultHelmValues)
	serverClusterVault.Create(t, serverClusterCtx, "")

	externalVaultAddress := vaultAddress(t, cfg, serverClusterCtx, vaultReleaseName)

	// In the secondary cluster, we will only deploy the agent injector and provide
	// it with the primary's Vault address. We also want to configure the injector with
	// a different k8s auth method path since the secondary cluster will need its own auth method.
	clientClusterVaultHelmValues := map[string]string{
		"server.enabled":             "false",
		"injector.externalVaultAddr": externalVaultAddress,
		"injector.authPath":          "auth/kubernetes-" + secondaryPartition,
	}

	secondaryVaultCluster := vault.NewVaultCluster(t, clientClusterCtx, cfg, vaultReleaseName, clientClusterVaultHelmValues)
	secondaryVaultCluster.Create(t, clientClusterCtx, "")

	vaultClient := serverClusterVault.VaultClient(t)

	// Configure Vault Kubernetes auth method for the secondary cluster.
	{
		// Create auth method service account and ClusterRoleBinding.
		authMethodRBACName := fmt.Sprintf("%s-vault-auth-method", vaultReleaseName)

		_, err = clientClusterCtx.KubernetesClient(t).RbacV1().ClusterRoleBindings().Create(context.Background(), &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: authMethodRBACName,
			},
			Subjects: []rbacv1.Subject{{Kind: rbacv1.ServiceAccountKind, Name: authMethodRBACName, Namespace: clientNs}},
			RoleRef:  rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Name: "system:auth-delegator", Kind: "ClusterRole"},
		}, metav1.CreateOptions{})
		require.NoError(t, err)

		// Create service account for the auth method in the secondary cluster.
		svcAcct, err := clientClusterCtx.KubernetesClient(t).CoreV1().ServiceAccounts(clientNs).Create(context.Background(), &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name: authMethodRBACName,
			},
		}, metav1.CreateOptions{})
		require.NoError(t, err)

		if len(svcAcct.Secrets) == 0 {
			_, err = clientClusterCtx.KubernetesClient(t).CoreV1().Secrets(clientNs).Create(context.Background(), &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:        authMethodRBACName,
					Annotations: map[string]string{corev1.ServiceAccountNameKey: authMethodRBACName},
				},
				Type: corev1.SecretTypeServiceAccountToken,
			}, metav1.CreateOptions{})
			require.NoError(t, err)

			require.Eventually(t, func() bool {
				s, err := clientClusterCtx.KubernetesClient(t).CoreV1().Secrets(clientNs).Get(context.Background(), authMethodRBACName, metav1.GetOptions{})
				if err != nil {
					return false
				}
				return len(s.Data["token"]) > 0
			}, 10*time.Second, 1*time.Second, "Timeout waiting for service account token to be populated")
		}
		t.Cleanup(func() {
			clientClusterCtx.KubernetesClient(t).RbacV1().ClusterRoleBindings().Delete(context.Background(), authMethodRBACName, metav1.DeleteOptions{})
			clientClusterCtx.KubernetesClient(t).CoreV1().ServiceAccounts(clientNs).Delete(context.Background(), authMethodRBACName, metav1.DeleteOptions{})
		})

		k8sAuthMethodHost := k8s.KubernetesAPIServerHost(t, cfg, clientClusterCtx)
		secondaryVaultCluster.ConfigureAuthMethod(t, vaultClient, "kubernetes-"+secondaryPartition, k8sAuthMethodHost, authMethodRBACName, clientNs)
	}

	// -------------------------
	// PKI
	// -------------------------
	connectCAPolicy := "connect-ca-dc1"
	connectCARootPath := "connect_root"
	connectCAIntermediatePath := "dc1/connect_inter"
	vault.CreateConnectCARootAndIntermediatePKIPolicy(t, vaultClient, connectCAPolicy, connectCARootPath, connectCAIntermediatePath)

	// Configure Server PKI
	serverPKIConfig := &vault.PKIAndAuthRoleConfiguration{
		BaseURL:             "pki",
		PolicyName:          "consul-ca-policy",
		RoleName:            "consul-ca-role",
		KubernetesNamespace: ns,
		DataCenter:          "dc1",
		ServiceAccountName:  fmt.Sprintf("%s-consul-%s", consulReleaseName, "server"),
		// AllowedSubdomain limits the certificate to the Service Name only
		AllowedSubdomain: fmt.Sprintf("%s-consul-%s", consulReleaseName, "server"),
		MaxTTL:           "1h",
		AuthMethodPath:   KubernetesAuthMethodPath,
	}
	serverPKIConfig.ConfigurePKIAndAuthRole(t, vaultClient)

	// -------------------------
	// KV2 secrets
	// -------------------------
	gossipKey, err := vault.GenerateGossipSecret()
	require.NoError(t, err)
	gossipSecret := &vault.KV2Secret{
		Path:       "consul/data/secret/gossip",
		Key:        "gossip",
		Value:      gossipKey,
		PolicyName: "gossip",
	}
	gossipSecret.SaveSecretAndAddReadPolicy(t, vaultClient)

	licenseSecret := &vault.KV2Secret{
		Path: "consul/data/secret/license",
		Key:  "license",
		// Check for empty license to avoid saving empty secret
		Value:      cfg.EnterpriseLicense,
		PolicyName: "license",
	}
	if cfg.EnableEnterprise && cfg.EnterpriseLicense != "" {
		licenseSecret.SaveSecretAndAddReadPolicy(t, vaultClient)
	}

	bootstrapToken, err := uuid.GenerateUUID()
	require.NoError(t, err)
	bootstrapTokenSecret := &vault.KV2Secret{
		Path:       "consul/data/secret/bootstrap",
		Key:        "token",
		Value:      bootstrapToken,
		PolicyName: "bootstrap",
	}
	bootstrapTokenSecret.SaveSecretAndAddReadPolicy(t, vaultClient)

	partitionToken, err := uuid.GenerateUUID()
	require.NoError(t, err)
	partitionTokenSecret := &vault.KV2Secret{
		Path:       "consul/data/secret/partition",
		Key:        "token",
		Value:      partitionToken,
		PolicyName: "partition",
	}
	partitionTokenSecret.SaveSecretAndAddReadPolicy(t, vaultClient)

	// -------------------------------------------
	// Additional Auth Roles in Primary Datacenter
	// -------------------------------------------
	serverPolicies := fmt.Sprintf("%s,%s,%s,%s", gossipSecret.PolicyName, connectCAPolicy, serverPKIConfig.PolicyName, bootstrapTokenSecret.PolicyName)
	if cfg.EnableEnterprise && cfg.EnterpriseLicense != "" {
		serverPolicies += fmt.Sprintf(",%s", licenseSecret.PolicyName)
	}

	consulServerRole := "server"
	srvAuthRoleConfig := &vault.KubernetesAuthRoleConfiguration{
		ServiceAccountName:  serverPKIConfig.ServiceAccountName,
		KubernetesNamespace: ns,
		AuthMethodPath:      KubernetesAuthMethodPath,
		RoleName:            consulServerRole,
		PolicyNames:         serverPolicies,
	}
	srvAuthRoleConfig.ConfigureK8SAuthRole(t, vaultClient)

	consulClientRole := ClientRole
	consulClientServiceAccountName := fmt.Sprintf("%s-consul-%s", consulReleaseName, ClientRole)
	clientAuthRoleConfig := &vault.KubernetesAuthRoleConfiguration{
		ServiceAccountName:  consulClientServiceAccountName,
		KubernetesNamespace: ns,
		AuthMethodPath:      KubernetesAuthMethodPath,
		RoleName:            consulClientRole,
		PolicyNames:         gossipSecret.PolicyName,
	}
	clientAuthRoleConfig.ConfigureK8SAuthRole(t, vaultClient)

	manageSystemACLsRole := ManageSystemACLsRole
	manageSystemACLsServiceAccountName := fmt.Sprintf("%s-consul-%s", consulReleaseName, ManageSystemACLsRole)
	aclAuthRoleConfig := &vault.KubernetesAuthRoleConfiguration{
		ServiceAccountName:  manageSystemACLsServiceAccountName,
		KubernetesNamespace: ns,
		AuthMethodPath:      KubernetesAuthMethodPath,
		RoleName:            manageSystemACLsRole,
		PolicyNames:         fmt.Sprintf("%s,%s", bootstrapTokenSecret.PolicyName, partitionTokenSecret.PolicyName),
	}
	aclAuthRoleConfig.ConfigureK8SAuthRole(t, vaultClient)

	srvCAAuthRoleConfig := &vault.KubernetesAuthRoleConfiguration{
		ServiceAccountName:  "*",
		KubernetesNamespace: ns,
		AuthMethodPath:      KubernetesAuthMethodPath,
		RoleName:            serverPKIConfig.RoleName,
		PolicyNames:         serverPKIConfig.PolicyName,
	}
	srvCAAuthRoleConfig.ConfigureK8SAuthRole(t, vaultClient)

	// ---------------------------------------------
	// Additional Auth Roles in Secondary Datacenter
	// ---------------------------------------------

	clientAuthRoleConfigSecondary := &vault.KubernetesAuthRoleConfiguration{
		ServiceAccountName:  consulClientServiceAccountName,
		KubernetesNamespace: clientNs,
		AuthMethodPath:      fmt.Sprintf("kubernetes-%s", secondaryPartition),
		RoleName:            consulClientRole,
		PolicyNames:         gossipSecret.PolicyName,
	}
	clientAuthRoleConfigSecondary.ConfigureK8SAuthRole(t, vaultClient)

	aclAuthRoleConfigSecondary := &vault.KubernetesAuthRoleConfiguration{
		ServiceAccountName:  manageSystemACLsServiceAccountName,
		KubernetesNamespace: clientNs,
		AuthMethodPath:      fmt.Sprintf("kubernetes-%s", secondaryPartition),
		RoleName:            manageSystemACLsRole,
		PolicyNames:         partitionTokenSecret.PolicyName,
	}
	aclAuthRoleConfigSecondary.ConfigureK8SAuthRole(t, vaultClient)

	adminPartitionsRole := "partition-init"
	partitionInitServiceAccountName := fmt.Sprintf("%s-consul-%s", consulReleaseName, "partition-init")
	prtAuthRoleConfigSecondary := &vault.KubernetesAuthRoleConfiguration{
		ServiceAccountName:  partitionInitServiceAccountName,
		KubernetesNamespace: clientNs,
		AuthMethodPath:      fmt.Sprintf("kubernetes-%s", secondaryPartition),
		RoleName:            adminPartitionsRole,
		PolicyNames:         partitionTokenSecret.PolicyName,
	}
	prtAuthRoleConfigSecondary.ConfigureK8SAuthRole(t, vaultClient)

	srvCAAuthRoleConfigSecondary := &vault.KubernetesAuthRoleConfiguration{
		ServiceAccountName:  "*",
		KubernetesNamespace: clientNs,
		AuthMethodPath:      fmt.Sprintf("kubernetes-%s", secondaryPartition),
		RoleName:            serverPKIConfig.RoleName,
		PolicyNames:         serverPKIConfig.PolicyName,
	}
	srvCAAuthRoleConfigSecondary.ConfigureK8SAuthRole(t, vaultClient)

	secondaryServerPKIConfig := &vault.PKIAndAuthRoleConfiguration{
    BaseURL:             "pki-secondary",
    PolicyName:          serverPKIConfig.PolicyName,
    RoleName:            serverPKIConfig.RoleName,
    KubernetesNamespace: clientNs,
    DataCenter:          "dc1", 
    ServiceAccountName:  "*",    // secondary accepts all SAs
    AllowedSubdomain:    serverPKIConfig.AllowedSubdomain,
    MaxTTL:              serverPKIConfig.MaxTTL,
    AuthMethodPath:      fmt.Sprintf("kubernetes-%s", secondaryPartition),
}

secondaryServerPKIConfig.ConfigurePKIAndAuthRole(t, vaultClient)

	vaultCASecretName := vault.CASecretName(vaultReleaseName)

	commonHelmValues := map[string]string{
		"global.adminPartitions.enabled":                   "true",
		"global.enableConsulNamespaces":                    "true",
		"connectInject.enabled":                            "true",
		"connectInject.replicas":                           "1",
		"global.secretsBackend.vault.enabled":              "true",
		"global.secretsBackend.vault.consulClientRole":     consulClientRole,
		"global.secretsBackend.vault.consulCARole":         serverPKIConfig.RoleName,
		"global.secretsBackend.vault.manageSystemACLsRole": manageSystemACLsRole,
		"global.secretsBackend.vault.ca.secretName":        vaultCASecretName,
		"global.secretsBackend.vault.ca.secretKey":         "tls.crt",
		"global.acls.manageSystemACLs":                     "true",
		"global.tls.enabled":                               "true",
		"global.tls.enableAutoEncrypt":                     "true",
		"connectInject.certManager.enabled":                "false",
        "connectInject.webhook.createCert":                 "true",
		"global.tls.caCert.secretName":                     serverPKIConfig.CAPath,
		"global.gossipEncryption.secretName":               gossipSecret.Path,
		"global.gossipEncryption.secretKey":                gossipSecret.Key,
		"global.enterpriseLicense.secretName":              licenseSecret.Path,
		"global.enterpriseLicense.secretKey":               licenseSecret.Key,
	}

serverHelmValues := map[string]string{
    "global.secretsBackend.vault.consulServerRole":              consulServerRole,
    "global.secretsBackend.vault.connectCA.address":             serverClusterVault.Address(),
    "global.secretsBackend.vault.connectCA.rootPKIPath":         connectCARootPath,
    "global.secretsBackend.vault.connectCA.intermediatePKIPath": connectCAIntermediatePath,
    "global.acls.bootstrapToken.secretName":                     bootstrapTokenSecret.Path,
    "global.acls.bootstrapToken.secretKey":                      bootstrapTokenSecret.Key,
    "global.acls.partitionToken.secretName":                     partitionTokenSecret.Path,
    "global.acls.partitionToken.secretKey":                      partitionTokenSecret.Key,
    "server.exposeGossipAndRPCPorts":                            "true",
    "server.serverCert.secretName":                              serverPKIConfig.CertPath,
    "server.extraVolumes[0].type":                               "secret",
    "server.extraVolumes[0].name":                               vaultCASecretName,
    "server.extraVolumes[0].load":                               "false",
    "server.exposeService.enabled":                              "true",
    "server.exposeService.type":                                 "LoadBalancer",

    // ADD THESE TWO LINES
    "connectInject.certManager.enabled": "false",
    "connectInject.webhook.createCert":  "true",
	"global.secretsBackend.vault.consulCAMountPath": "pki",
}

	if cfg.UseKind {
		serverHelmValues["meshGateway.service.type"] = "NodePort"
		serverHelmValues["meshGateway.service.nodePort"] = "30100"
		serverHelmValues["server.exposeService.type"] = "NodePort"
		serverHelmValues["server.exposeService.nodePort.https"] = "30000"
	}

	helpers.MergeMaps(serverHelmValues, commonHelmValues)

	logger.Log(t, "Installing Consul")
	consulCluster := consul.NewHelmCluster(t, serverHelmValues, serverClusterCtx, cfg, consulReleaseName)
	consulCluster.Create(t)

	// Retrieve both the Partition (Gossip) Service and the Server (HTTPS) Service
	partitionServiceName := fmt.Sprintf("%s-consul-expose-servers", consulReleaseName)
	partitionSvcAddress := k8s.ServiceHost(t, cfg, serverClusterCtx, partitionServiceName)

	serverServiceName := fmt.Sprintf("%s-consul-server", consulReleaseName)
	serverSvcAddress := k8s.ServiceHost(t, cfg, serverClusterCtx, serverServiceName)

	k8sAuthMethodHost := k8s.KubernetesAPIServerHost(t, cfg, clientClusterCtx)

	logger.Logf(t, "retrieving Vault CA secret %s from the primary cluster and applying to the secondary", vaultCASecretName)
	vaultCASecret, err := serverClusterCtx.KubernetesClient(t).CoreV1().Secrets(ns).Get(context.Background(), vaultCASecretName, metav1.GetOptions{})
	require.NoError(t, err)
	vaultCASecret.ResourceVersion = ""

	_, err = clientClusterCtx.KubernetesClient(t).CoreV1().Secrets(clientNs).Create(context.Background(), vaultCASecret, metav1.CreateOptions{})
	require.NoError(t, err)

	t.Cleanup(func() {
		clientClusterCtx.KubernetesClient(t).CoreV1().Secrets(clientNs).Delete(context.Background(), vaultCASecretName, metav1.DeleteOptions{})
	})

	// Create client cluster.
	clientHelmValues := map[string]string{
		"global.enabled": "false",

		"global.adminPartitions.name": secondaryPartition,

		"global.acls.bootstrapToken.secretName": partitionTokenSecret.Path,
		"global.acls.bootstrapToken.secretKey":  partitionTokenSecret.Key,
		"global.secretsBackend.vault.agentAnnotations":    fmt.Sprintf("vault.hashicorp.com/tls-server-name: %s-vault", vaultReleaseName),
		"global.secretsBackend.vault.adminPartitionsRole": adminPartitionsRole,

		// ------------------------------------------------------------------
		// CRITICAL FIX: Explicitly set the Auth Mount Path for the client
		// ------------------------------------------------------------------
		// The client pods default to "kubernetes", but we set up "kubernetes-secondary" in Vault.
		"global.secretsBackend.vault.consulClientMountPath": "kubernetes-" + secondaryPartition,

		// The admin partitions init job also needs to know which auth mount to use.
		"global.secretsBackend.vault.adminPartitionsMountPath": "kubernetes-" + secondaryPartition,

		// Ensure the Consul Agents and Init containers know exactly where Vault is
		"global.secretsBackend.vault.address": externalVaultAddress,

		"externalServers.enabled":           "true",
		"externalServers.hosts[0]":          serverSvcAddress,
		"externalServers.tlsServerName":     fmt.Sprintf("%s-consul-server", consulReleaseName),
		"externalServers.k8sAuthMethodHost": k8sAuthMethodHost,
		"externalServers.httpsPort":         "8501",

		"client.enabled":           "true",
		"client.grpc":              "true",
		"client.dataPlane":         "false",
		"client.exposeGossipPorts": "true",
		"client.join[0]":           partitionSvcAddress,

		"connectInject.transparentProxy.defaultEnabled": "true",
		"global.secretsBackend.vault.consulCAMountPath": "pki-secondary",

		// Ensure sidecar injector also knows the correct auth path if sidecars are injected
		 "connectInject.vault.pkiMountPath": "pki-secondary",

	}

	if cfg.UseKind {
		clientHelmValues["externalServers.httpsPort"] = "30000"
		clientHelmValues["meshGateway.service.type"] = "NodePort"
		clientHelmValues["meshGateway.service.nodePort"] = "30100"
	}

	helpers.MergeMaps(clientHelmValues, commonHelmValues)

	// Install the consul cluster without servers in the client cluster kubernetes context.
	clientConsulCluster := consul.NewHelmCluster(t, clientHelmValues, clientClusterCtx, cfg, consulReleaseName)
	clientConsulCluster.Create(t)

	// Ensure consul clients are created.
	// Use a broader LabelSelector to debug if specific labels are missing
	agentPodList, err := clientClusterCtx.KubernetesClient(t).CoreV1().Pods(clientNs).List(context.Background(), metav1.ListOptions{LabelSelector: "app=consul,component=client"})
	require.NoError(t, err)

	// ------------------------------------------------------------------
	// DEBUGGING BLOCK: Inspect pods if list is empty
	// ------------------------------------------------------------------
	if len(agentPodList.Items) == 0 {
		t.Logf("!!! DEBUG: agentPodList is empty. Listing ALL pods in namespace %s to debug...", clientNs)
		allPods, err := clientClusterCtx.KubernetesClient(t).CoreV1().Pods(clientNs).List(context.Background(), metav1.ListOptions{})
		if err == nil {
			for _, p := range allPods.Items {
				t.Logf("Found Pod: Name=%s, Status=%s, Labels=%v", p.Name, p.Status.Phase, p.Labels)
				// If pod is stuck in Init, print InitContainerStatuses
				for _, ic := range p.Status.InitContainerStatuses {
					t.Logf("  InitContainer %s: State=%v", ic.Name, ic.State)
					if ic.State.Terminated != nil && ic.State.Terminated.ExitCode != 0 {
						// Attempt to fetch logs for failed init container
						logs, _ := k8s.RunKubectlAndGetOutputE(t, clientClusterCtx.KubectlOptions(t), "logs", p.Name, "-c", ic.Name, "-n", clientNs)
						t.Logf("  Logs for %s:\n%s", ic.Name, logs)
					}
				}
			}
		} else {
			t.Logf("!!! DEBUG: Failed to list pods: %v", err)
		}

		// Check Events
		events, _ := k8s.RunKubectlAndGetOutputE(t, clientClusterCtx.KubectlOptions(t), "get", "events", "-n", clientNs, "--sort-by=.lastTimestamp")
		t.Logf("!!! DEBUG: Events in namespace:\n%s", events)
	}

	require.NotEmpty(t, agentPodList.Items, "Consul Client pods should be present in the secondary cluster")

	output, err := k8s.RunKubectlAndGetOutputE(t, clientClusterCtx.KubectlOptions(t), "logs", agentPodList.Items[0].Name, "consul", "-n", clientNs)
	require.NoError(t, err)
	require.Contains(t, output, "Partition: 'secondary'")
}
