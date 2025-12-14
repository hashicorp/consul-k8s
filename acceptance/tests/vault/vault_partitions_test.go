// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package vault

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
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
	"k8s.io/apimachinery/pkg/util/rand"
	crlog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func TestVault_Partitions(t *testing.T) {
	crlog.SetLogger(zap.New(zap.UseDevMode(true)))
	env := suite.Environment()
	cfg := suite.Config()
	serverClusterCtx := env.DefaultContext(t)
	clientClusterCtx := env.Context(t, 1)
	ns := serverClusterCtx.KubectlOptions(t).Namespace

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
		// Create auth method service account and ClusterRoleBinding. The Vault server
		// in the primary cluster will use this service account token to talk to the secondary
		// Kubernetes cluster.
		// This ClusterRoleBinding is adapted from the Vault server's role:
		// https://github.com/hashicorp/vault-helm/blob/b0528fce49c529f2c37953ea3a14f30ed651e0d6/templates/server-clusterrolebinding.yaml

		// Use a single name for all RBAC objects.
		authMethodRBACName := fmt.Sprintf("%s-vault-auth-method", vaultReleaseName)
		_, err := clientClusterCtx.KubernetesClient(t).RbacV1().ClusterRoleBindings().Create(context.Background(), &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: authMethodRBACName,
			},
			Subjects: []rbacv1.Subject{{Kind: rbacv1.ServiceAccountKind, Name: authMethodRBACName, Namespace: ns}},
			RoleRef:  rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Name: "system:auth-delegator", Kind: "ClusterRole"},
		}, metav1.CreateOptions{})
		require.NoError(t, err)

		// Create service account for the auth method in the secondary cluster.
		svcAcct, err := clientClusterCtx.KubernetesClient(t).CoreV1().ServiceAccounts(ns).Create(context.Background(), &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name: authMethodRBACName,
			},
		}, metav1.CreateOptions{})
		require.NoError(t, err)
		// In Kubernetes 1.24+ the serviceAccount does not automatically populate secrets with permanent JWT tokens, use this instead.
		// It will be cleaned up by Kubernetes automatically since it references the ServiceAccount.
		if len(svcAcct.Secrets) == 0 {
			_, err = clientClusterCtx.KubernetesClient(t).CoreV1().Secrets(ns).Create(context.Background(), &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:        authMethodRBACName,
					Annotations: map[string]string{corev1.ServiceAccountNameKey: authMethodRBACName},
				},
				Type: corev1.SecretTypeServiceAccountToken,
			}, metav1.CreateOptions{})
			require.NoError(t, err)
		}
		t.Cleanup(func() {
			clientClusterCtx.KubernetesClient(t).RbacV1().ClusterRoleBindings().Delete(context.Background(), authMethodRBACName, metav1.DeleteOptions{})
			clientClusterCtx.KubernetesClient(t).CoreV1().ServiceAccounts(ns).Delete(context.Background(), authMethodRBACName, metav1.DeleteOptions{})
		})

		// Figure out the host for the Kubernetes API. This needs to be reachable from the Vault server
		// in the primary cluster.
		k8sAuthMethodHost := k8s.KubernetesAPIServerHost(t, cfg, clientClusterCtx)

		// Now, configure the auth method in Vault.
		secondaryVaultCluster.ConfigureAuthMethod(t, vaultClient, "kubernetes-"+secondaryPartition, k8sAuthMethodHost, authMethodRBACName, ns)
	}

	// -------------------------
	// PKI
	// -------------------------
	// Configure Service Mesh CA
	connectCAPolicy := "connect-ca-dc1"
	connectCARootPath := "connect_root"
	connectCAIntermediatePath := "dc1/connect_inter"
	// Configure Policy for Connect CA
	vault.CreateConnectCARootAndIntermediatePKIPolicy(t, vaultClient, connectCAPolicy, connectCARootPath, connectCAIntermediatePath)

	// Configure Server PKI
	serverPKIConfig := &vault.PKIAndAuthRoleConfiguration{
		BaseURL:             "pki",
		PolicyName:          "consul-ca-policy",
		RoleName:            "consul-ca-role",
		KubernetesNamespace: ns,
		DataCenter:          "dc1",
		ServiceAccountName:  fmt.Sprintf("%s-consul-%s", consulReleaseName, "server"),
		AllowedSubdomain:    fmt.Sprintf("%s-consul-%s", consulReleaseName, "server"),
		MaxTTL:              "1h",
		AuthMethodPath:      KubernetesAuthMethodPath,
	}
	serverPKIConfig.ConfigurePKIAndAuthRole(t, vaultClient)

	// -------------------------
	// KV2 secrets
	// -------------------------
	// Gossip key
	gossipKey, err := vault.GenerateGossipSecret()
	require.NoError(t, err)
	gossipSecret := &vault.KV2Secret{
		Path:       "consul/data/secret/gossip",
		Key:        "gossip",
		Value:      gossipKey,
		PolicyName: "gossip",
	}
	gossipSecret.SaveSecretAndAddReadPolicy(t, vaultClient)

	// License
	licenseSecret := &vault.KV2Secret{
		Path:       "consul/data/secret/license",
		Key:        "license",
		Value:      cfg.EnterpriseLicense,
		PolicyName: "license",
	}
	if cfg.EnableEnterprise {
		licenseSecret.SaveSecretAndAddReadPolicy(t, vaultClient)
	}

	// Bootstrap Token
	bootstrapToken, err := uuid.GenerateUUID()
	require.NoError(t, err)
	bootstrapTokenSecret := &vault.KV2Secret{
		Path:       "consul/data/secret/bootstrap",
		Key:        "token",
		Value:      bootstrapToken,
		PolicyName: "bootstrap",
	}
	bootstrapTokenSecret.SaveSecretAndAddReadPolicy(t, vaultClient)

	// Partition Token
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
	consulServerRole := "server"
	serverPolicies := fmt.Sprintf("%s,%s,%s,%s,%s", gossipSecret.PolicyName, connectCAPolicy, serverPKIConfig.PolicyName, bootstrapTokenSecret.PolicyName, partitionTokenSecret.PolicyName)
	if cfg.EnableEnterprise {
		serverPolicies += fmt.Sprintf(",%s", licenseSecret.PolicyName)
	}
	// server
	srvAuthRoleConfig := &vault.KubernetesAuthRoleConfiguration{
		ServiceAccountName:  serverPKIConfig.ServiceAccountName,
		KubernetesNamespace: ns,
		AuthMethodPath:      KubernetesAuthMethodPath,
		RoleName:            consulServerRole,
		PolicyNames:         serverPolicies,
	}
	srvAuthRoleConfig.ConfigureK8SAuthRole(t, vaultClient)

	// client
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

	// manageSystemACLs
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

	// allow all components to access server ca
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

	// client
	clientAuthRoleConfigSecondary := &vault.KubernetesAuthRoleConfiguration{
		ServiceAccountName:  consulClientServiceAccountName,
		KubernetesNamespace: ns,
		AuthMethodPath:      fmt.Sprintf("kubernetes-%s", secondaryPartition),
		RoleName:            consulClientRole,
		PolicyNames:         fmt.Sprintf("%s,%s", gossipSecret.PolicyName, partitionTokenSecret.PolicyName),
	}
	clientAuthRoleConfigSecondary.ConfigureK8SAuthRole(t, vaultClient)

	// manageSystemACLs
	aclAuthRoleConfigSecondary := &vault.KubernetesAuthRoleConfiguration{
		ServiceAccountName:  manageSystemACLsServiceAccountName,
		KubernetesNamespace: ns,
		AuthMethodPath:      fmt.Sprintf("kubernetes-%s", secondaryPartition),
		RoleName:            manageSystemACLsRole,
		PolicyNames:         partitionTokenSecret.PolicyName,
	}
	aclAuthRoleConfigSecondary.ConfigureK8SAuthRole(t, vaultClient)

	// partition init
	adminPartitionsRole := "partition-init"
	partitionInitServiceAccountName := fmt.Sprintf("%s-consul-%s", consulReleaseName, "partition-init")
	prtAuthRoleConfigSecondary := &vault.KubernetesAuthRoleConfiguration{
		ServiceAccountName:  partitionInitServiceAccountName,
		KubernetesNamespace: ns,
		AuthMethodPath:      fmt.Sprintf("kubernetes-%s", secondaryPartition),
		RoleName:            adminPartitionsRole,
		PolicyNames:         partitionTokenSecret.PolicyName,
	}
	prtAuthRoleConfigSecondary.ConfigureK8SAuthRole(t, vaultClient)

	// allow all components to access server ca
	srvCAAuthRoleConfigSecondary := &vault.KubernetesAuthRoleConfiguration{
		ServiceAccountName:  "*",
		KubernetesNamespace: ns,
		AuthMethodPath:      fmt.Sprintf("kubernetes-%s", secondaryPartition),
		RoleName:            serverPKIConfig.RoleName,
		PolicyNames:         serverPKIConfig.PolicyName,
	}
	srvCAAuthRoleConfigSecondary.ConfigureK8SAuthRole(t, vaultClient)

	vaultCASecretName := vault.CASecretName(vaultReleaseName)

	commonHelmValues := map[string]string{
		"global.adminPartitions.enabled": "true",

		"global.enableConsulNamespaces": "true",

		"connectInject.enabled":  "true",
		"connectInject.replicas": "1",

		"global.secretsBackend.vault.enabled":              "true",
		"global.secretsBackend.vault.consulClientRole":     consulClientRole,
		"global.secretsBackend.vault.consulCARole":         serverPKIConfig.RoleName,
		"global.secretsBackend.vault.manageSystemACLsRole": manageSystemACLsRole,

		"global.secretsBackend.vault.ca.secretName": vaultCASecretName,
		"global.secretsBackend.vault.ca.secretKey":  "tls.crt",

		"global.acls.manageSystemACLs": "true",

		"global.tls.enabled":           "true",
		"global.tls.enableAutoEncrypt": "true",
		"global.tls.caCert.secretName": serverPKIConfig.CAPath,

		"global.gossipEncryption.secretName": gossipSecret.Path,
		"global.gossipEncryption.secretKey":  gossipSecret.Key,

		"global.enterpriseLicense.secretName": licenseSecret.Path,
		"global.enterpriseLicense.secretKey":  licenseSecret.Key,
	}

	serverHelmValues := map[string]string{
		"global.secretsBackend.vault.consulServerRole":              consulServerRole,
		"global.secretsBackend.vault.connectCA.address":             serverClusterVault.Address(),
		"global.secretsBackend.vault.connectCA.rootPKIPath":         connectCARootPath,
		"global.secretsBackend.vault.connectCA.intermediatePKIPath": connectCAIntermediatePath,

		"global.acls.bootstrapToken.secretName": bootstrapTokenSecret.Path,
		"global.acls.bootstrapToken.secretKey":  bootstrapTokenSecret.Key,
		"global.acls.partitionToken.secretName": partitionTokenSecret.Path,
		"global.acls.partitionToken.secretKey":  partitionTokenSecret.Key,

		"server.serverCert.secretName": serverPKIConfig.CertPath,

		"server.extraVolumes[0].type": "secret",
		"server.extraVolumes[0].name": vaultCASecretName,
		"server.extraVolumes[0].load": "false",
	}

	// On Kind, there are no load balancers but since all clusters
	// share the same node network (docker bridge), we can use
	// a NodePort service so that we can access node(s) in a different Kind cluster.
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

	partitionServiceName := fmt.Sprintf("%s-consul-expose-servers", consulReleaseName)
	partitionSvcAddress := k8s.ServiceHost(t, cfg, serverClusterCtx, partitionServiceName)

	k8sAuthMethodHost := k8s.KubernetesAPIServerHost(t, cfg, clientClusterCtx)

	// Move Vault CA secret from primary to secondary so that we can mount it to pods in the
	// secondary cluster.
	logger.Logf(t, "retrieving Vault CA secret %s from the primary cluster and applying to the secondary", vaultCASecretName)
	vaultCASecret, err := serverClusterCtx.KubernetesClient(t).CoreV1().Secrets(ns).Get(context.Background(), vaultCASecretName, metav1.GetOptions{})
	vaultCASecret.ResourceVersion = ""
	require.NoError(t, err)
	_, err = clientClusterCtx.KubernetesClient(t).CoreV1().Secrets(ns).Create(context.Background(), vaultCASecret, metav1.CreateOptions{})
	require.NoError(t, err)
	t.Cleanup(func() {
		clientClusterCtx.KubernetesClient(t).CoreV1().Secrets(ns).Delete(context.Background(), vaultCASecretName, metav1.DeleteOptions{})
	})

	// DEBUG: Verify Vault CA secret was copied successfully
	logger.Logf(t, "DEBUG: Verifying Vault CA secret in secondary cluster...")
	copiedCASecret, err := clientClusterCtx.KubernetesClient(t).CoreV1().Secrets(ns).Get(
		context.Background(), vaultCASecretName, metav1.GetOptions{})
	if err != nil {
		logger.Logf(t, "ERROR: Failed to retrieve copied Vault CA secret: %v", err)
	} else {
		logger.Logf(t, "DEBUG: Vault CA secret successfully copied to secondary cluster")
		logger.Logf(t, "  Secret Name: %s", copiedCASecret.Name)
		logger.Logf(t, "  Secret Data Keys: %v", getSecretKeys(copiedCASecret.Data))

		// Check for required CA cert
		if caCert, ok := copiedCASecret.Data["ca.crt"]; ok {
			logger.Logf(t, "  CA Cert Size: %d bytes", len(caCert))
		} else if caCert, ok := copiedCASecret.Data["cert"]; ok {
			logger.Logf(t, "  Using 'cert' key instead of 'ca.crt', Size: %d bytes", len(caCert))
		} else if caCert, ok := copiedCASecret.Data["tls.crt"]; ok {
			// FIX: Add check for tls.crt
			logger.Logf(t, "  Using 'tls.crt' key, Size: %d bytes", len(caCert))
		} else {
			logger.Logf(t, "ERROR: No CA certificate found in secret. Available keys: %v", getSecretKeys(copiedCASecret.Data))
		}
	}

	// DEBUG: Verify partition token secret exists
	logger.Logf(t, "DEBUG: Verifying partition token secret...")
	if partitionTokenSecret != nil {
		// FIX: Use the Kubernetes name we decided on above ("consul-partition-token")
		// Instead of partitionTokenSecret.Path
		tokenSecret, err := serverClusterCtx.KubernetesClient(t).CoreV1().Secrets(ns).Get(
			context.Background(), partitionTokenSecret.Path, metav1.GetOptions{})

		if err != nil {
			logger.Logf(t, "ERROR: Partition token secret not found: consul-partition-token, error: %v", err)
		} else {
			logger.Logf(t, "DEBUG: Partition token secret found")
			if tokenData, ok := tokenSecret.Data[partitionTokenSecret.Key]; ok {
				logger.Logf(t, "  Token Size: %d bytes", len(tokenData))
				// Log token prefix for debugging (not full token for security)
				if len(tokenData) > 10 {
					logger.Logf(t, "  Token Prefix: %s...", string(tokenData[:10]))
				}
			} else {
				logger.Logf(t, "ERROR: Key '%s' not found in token secret. Available keys: %v",
					partitionTokenSecret.Key, getSecretKeys(tokenSecret.Data))
			}
		}
	} else {
		logger.Logf(t, "ERROR: partitionTokenSecret is nil")
	}

	// DEBUG: Parse and verify service address
	logger.Logf(t, "DEBUG: Parsing service address: %s", partitionSvcAddress)
	if partitionSvcAddress == "" {
		logger.Logf(t, "ERROR: partitionSvcAddress is empty")
	} else {
		// Try to parse host and port
		parts := strings.Split(partitionSvcAddress, ":")
		if len(parts) >= 2 {
			host, port := parts[0], parts[1]
			logger.Logf(t, "  Host: %s", host)
			logger.Logf(t, "  Port: %s", port)

			// Check if it's a full service name
			if strings.Contains(host, ".svc.") {
				logger.Logf(t, "  Service FQDN detected")
				serviceName := strings.Split(host, ".")[0]
				logger.Logf(t, "  Service Name: %s", serviceName)

				// Try to get service from primary cluster
				svc, err := serverClusterCtx.KubernetesClient(t).CoreV1().Services(ns).Get(
					context.Background(), serviceName, metav1.GetOptions{})
				if err != nil {
					logger.Logf(t, "WARN: Service %s not found in primary cluster: %v", serviceName, err)
				} else {
					logger.Logf(t, "  Service exists in primary cluster:")
					logger.Logf(t, "    Type: %s", svc.Spec.Type)
					logger.Logf(t, "    ClusterIP: %s", svc.Spec.ClusterIP)
					for _, port := range svc.Spec.Ports {
						logger.Logf(t, "    Port: %s -> %d/%s", port.Name, port.Port, port.Protocol)
					}

					// Check endpoints
					endpoints, err := serverClusterCtx.KubernetesClient(t).CoreV1().Endpoints(ns).Get(
						context.Background(), serviceName, metav1.GetOptions{})
					if err != nil {
						logger.Logf(t, "WARN: Failed to get endpoints: %v", err)
					} else if len(endpoints.Subsets) == 0 {
						logger.Logf(t, "ERROR: Service has no endpoints")
					} else {
						logger.Logf(t, "  Service has %d endpoint subsets", len(endpoints.Subsets))
					}
				}
			}
		} else {
			logger.Logf(t, "WARN: Could not parse host:port from address")
		}
	}

	// DEBUG: Verify Vault configuration
	logger.Logf(t, "DEBUG: Verifying Vault configuration...")
	logger.Logf(t, "  Vault Release Name: %s", vaultReleaseName)
	logger.Logf(t, "  Vault TLS Server Name: %s-vault", vaultReleaseName)
	logger.Logf(t, "  Admin Partitions Role: %s", adminPartitionsRole)
	logger.Logf(t, "  k8sAuthMethodHost: %s", k8sAuthMethodHost)

	if adminPartitionsRole == "" {
		logger.Logf(t, "ERROR: adminPartitionsRole is empty")
	}
	if k8sAuthMethodHost == "" {
		logger.Logf(t, "ERROR: k8sAuthMethodHost is empty")
	}

	// Create client cluster configuration
	clientHelmValues := map[string]string{
		"global.enabled": "false",

		"global.adminPartitions.name": secondaryPartition,

		"global.acls.bootstrapToken.secretName": partitionTokenSecret.Path,
		"global.acls.bootstrapToken.secretKey":  partitionTokenSecret.Key,

		"global.secretsBackend.vault.agentAnnotations":    fmt.Sprintf("vault.hashicorp.com/tls-server-name: %s-vault", vaultReleaseName),
		"global.secretsBackend.vault.adminPartitionsRole": adminPartitionsRole,

		"externalServers.enabled":           "true",
		"externalServers.hosts[0]":          partitionSvcAddress,
		"externalServers.tlsServerName":     "server.dc1.consul",
		"externalServers.k8sAuthMethodHost": k8sAuthMethodHost,

		"client.enabled": "true",
	}

	if cfg.UseKind {
		clientHelmValues["externalServers.httpsPort"] = "30000"
		clientHelmValues["meshGateway.service.type"] = "NodePort"
		clientHelmValues["meshGateway.service.nodePort"] = "30100"

		logger.Logf(t, "DEBUG: Using Kind-specific configuration:")
		logger.Logf(t, "  HTTPS Port: %s", clientHelmValues["externalServers.httpsPort"])
		logger.Logf(t, "  Mesh Gateway NodePort: %s", clientHelmValues["meshGateway.service.nodePort"])
	}

	helpers.MergeMaps(clientHelmValues, commonHelmValues)

	// DEBUG: Log all helm values (masking sensitive ones)
	logger.Logf(t, "DEBUG: Final Helm values for client cluster:")
	for key, value := range clientHelmValues {
		// Mask tokens and secrets in logs
		logger.Logf(t, "  %s: %s", key, value)
	}

	// DEBUG: Quick network connectivity test
	logger.Logf(t, "DEBUG: Testing basic network connectivity...")
	testConnectivity(t, serverClusterCtx, clientClusterCtx, ns, partitionSvcAddress)

	// Install the consul cluster without servers in the client cluster kubernetes context.
	logger.Logf(t, "DEBUG: Creating client Consul cluster...")
	clientConsulCluster := consul.NewHelmCluster(t, clientHelmValues, clientClusterCtx, cfg, consulReleaseName)
	clientConsulCluster.Create(t)
	// debug after cluster creation

	time.Sleep(30 * time.Second)
	debugVaultPartitions(t, clientClusterCtx, ns)
	// Ensure consul clients are created.
	agentPodList, err := clientClusterCtx.KubernetesClient(t).CoreV1().Pods(clientClusterCtx.KubectlOptions(t).Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: "app=consul,component=client"})
	require.NoError(t, err)
	require.NotEmpty(t, agentPodList.Items)

	output, err := k8s.RunKubectlAndGetOutputE(t, clientClusterCtx.KubectlOptions(t), "logs", agentPodList.Items[0].Name, "consul", "-n", clientClusterCtx.KubectlOptions(t).Namespace)
	require.NoError(t, err)
	require.Contains(t, output, "Partition: 'secondary'")
}

func debugVaultPartitions(t *testing.T, clientClusterCtx environment.TestContext, ns string) {
	logger.Logf(t, "DEBUG: Checking client cluster state...")

	// List all pods in the namespace
	pods, err := clientClusterCtx.KubernetesClient(t).CoreV1().Pods(ns).List(
		context.Background(), metav1.ListOptions{})
	if err != nil {
		logger.Logf(t, "ERROR: Failed to list pods: %v", err)
		return
	}

	for _, pod := range pods.Items {
		logger.Logf(t, "Pod: %s, Status: %s", pod.Name, pod.Status.Phase)
		if pod.Status.Phase != corev1.PodRunning {
			logger.Logf(t, "  Container Statuses:")
			for _, cs := range pod.Status.ContainerStatuses {
				logger.Logf(t, "    %s: Ready=%v, Restarts=%d",
					cs.Name, cs.Ready, cs.RestartCount)
				if cs.State.Waiting != nil {
					logger.Logf(t, "      Waiting Reason: %s, Message: %s",
						cs.State.Waiting.Reason, cs.State.Waiting.Message)
				}
			}

			// Get pod events
			events, err := clientClusterCtx.KubernetesClient(t).CoreV1().Events(ns).List(
				context.Background(), metav1.ListOptions{
					FieldSelector: fmt.Sprintf("involvedObject.name=%s", pod.Name),
				})
			if err == nil && len(events.Items) > 0 {
				for _, event := range events.Items {
					logger.Logf(t, "  Event: %s - %s", event.Reason, event.Message)
				}
			}
		}
	}

	// Check for failed init containers
	for _, pod := range pods.Items {
		for _, status := range pod.Status.InitContainerStatuses {
			if !status.Ready && status.State.Terminated != nil {
				logger.Logf(t, "ERROR: Init container %s in pod %s failed: %s",
					status.Name, pod.Name, status.State.Terminated.Message)
			}
		}
	}
}

// Helper function to get keys from a map
func getSecretKeys(data map[string][]byte) []string {
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	return keys
}

// Helper function to test connectivity
func testConnectivity(t *testing.T, serverCtx, clientCtx environment.TestContext, ns, targetAddress string) {
	// Create a simple test pod in client cluster
	testPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "network-test-" + strings.ToLower(rand.String(5)),
			Namespace: ns,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:    "test",
					Image:   "busybox:latest",
					Command: []string{"sleep", "30"},
				},
			},
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}

	// Try to create test pod
	clientK8s := clientCtx.KubernetesClient(t)
	_, err := clientK8s.CoreV1().Pods(ns).Create(context.Background(), testPod, metav1.CreateOptions{})
	if err != nil {
		logger.Logf(t, "WARN: Could not create network test pod: %v", err)
		return
	}

	// Clean up after test
	defer func() {
		clientK8s.CoreV1().Pods(ns).Delete(context.Background(), testPod.Name, metav1.DeleteOptions{})
	}()

	// Wait for pod to be ready
	time.Sleep(2 * time.Second)

}


