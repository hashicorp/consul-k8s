// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package vault

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/go-uuid"
	"github.com/hashicorp/go-version"
	"github.com/stretchr/testify/require"

	"github.com/hashicorp/vault/api"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/gruntwork-io/terratest/modules/logger"

	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/vault"
	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
	crlog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)


func TestVault_Partitions(t *testing.T) {
	// 1. Setup & Logging
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
		t.Skipf("skipping: vault secrets backend not supported in %v", cfg.ConsulVersion.String())
	}
	if !cfg.EnableEnterprise || !cfg.EnableMultiCluster {
		t.Skip("skipping: enterprise and multi-cluster required")
	}

	// [FIX] Consolidate KubectlOptions
	// Ensure secondary cluster has the correct ConfigPath.
	// In some environments, the secondary context struct might have an empty ConfigPath
	// even if it shares the file with the primary.
	serverK8sOpts := serverClusterCtx.KubectlOptions(t)
	clientK8sOpts := clientClusterCtx.KubectlOptions(t)

	if clientK8sOpts.ConfigPath == "" {
		t.Logf("Client ConfigPath is empty, inheriting from Server: %s", serverK8sOpts.ConfigPath)
		clientK8sOpts.ConfigPath = serverK8sOpts.ConfigPath
	}

	vaultReleaseName := helpers.RandomName()
	consulReleaseName := helpers.RandomName()

	// -----------------------------------------------------------------------
	// 2. Deploy Vault (Primary)
	// -----------------------------------------------------------------------
	svcType := "LoadBalancer"
	if cfg.UseKind {
		svcType = "NodePort"
	}

	serverClusterVaultHelmValues := map[string]string{
		"server.service.type": svcType,
	}
	if cfg.UseKind {
		serverClusterVaultHelmValues["server.service.nodePort"] = "31000"
	}

	serverClusterVault := vault.NewVaultCluster(t, serverClusterCtx, cfg, vaultReleaseName, serverClusterVaultHelmValues)
	serverClusterVault.Create(t, serverClusterCtx, "")
	
	vaultClient := serverClusterVault.VaultClient(t)

	// [FIX] Service Name Check
	// In Standalone mode (default), the service is named "[Release]-vault".
	// It is only named "[Release]-vault-active" in HA mode.
	var externalVaultAddress string
	if cfg.UseKind {
		externalVaultAddress = serverClusterVault.Address()
	} else {
		// Use serverK8sOpts
		externalVaultAddress = waitForServiceLB(t, serverK8sOpts, vaultReleaseName+"-vault")
		externalVaultAddress = fmt.Sprintf("http://%s:8200", externalVaultAddress)
	}
	logger.Logf(t, "Vault External Address: %s", externalVaultAddress)

	// -----------------------------------------------------------------------
	// 3. Deploy Injector (Secondary)
	// -----------------------------------------------------------------------
	clientClusterVaultHelmValues := map[string]string{
		"server.enabled":             "false",
		"injector.enabled":           "true",
		"injector.externalVaultAddr": externalVaultAddress,
		"injector.authPath":          "auth/kubernetes-" + secondaryPartition,
	}
	secondaryVaultCluster := vault.NewVaultCluster(t, clientClusterCtx, cfg, vaultReleaseName, clientClusterVaultHelmValues)
	secondaryVaultCluster.Create(t, clientClusterCtx, "")

	// -----------------------------------------------------------------------
	// 4. Configure Vault Auth (Fixed for GKE/EKS & Config Path)
	// -----------------------------------------------------------------------
	{
		authMethodRBACName := fmt.Sprintf("%s-vault-auth-method", vaultReleaseName)
		
		// [FIX] Use clientK8sOpts (with valid ConfigPath)
		createVaultAuthRBAC(t, clientClusterCtx, clientNs, authMethodRBACName)

		// [FIX] Load RestConfig using the valid clientK8sOpts
		restConfig, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			&clientcmd.ClientConfigLoadingRules{ExplicitPath: clientK8sOpts.ConfigPath},
			&clientcmd.ConfigOverrides{CurrentContext: clientK8sOpts.ContextName},
		).ClientConfig()
		require.NoError(t, err)

		k8sAuthMethodHost := restConfig.Host
		k8sAuthMethodCA := string(restConfig.CAData)

		logger.Logf(t, "Configuring Vault Auth for secondary: Host=%s", k8sAuthMethodHost)

		authPath := "kubernetes-" + secondaryPartition

		// 1. Enable Auth Method (Idempotent check)
		err = vaultClient.Sys().EnableAuthWithOptions(authPath, &api.EnableAuthOptions{Type: "kubernetes"})
		if err != nil && !strings.Contains(err.Error(), "path is already in use") {
			require.NoError(t, err)
		}

		// 2. Configure with Host AND CA
		_, err = vaultClient.Logical().Write(fmt.Sprintf("auth/%s/config", authPath), map[string]interface{}{
			"kubernetes_host":    k8sAuthMethodHost,
			"kubernetes_ca_cert": k8sAuthMethodCA,
			"token_reviewer_jwt": getServiceAccountToken(t, clientClusterCtx, clientNs, authMethodRBACName),
		})
		require.NoError(t, err)
	}

	// -----------------------------------------------------------------------
	// 5. Configure Secrets & Roles
	// -----------------------------------------------------------------------
	connectCAPolicy := "connect-ca-dc1"

	serverPKIConfig := setupPKI(t, serverClusterVault, ns, consulReleaseName, connectCAPolicy)
	gossipSecret := setupGossip(t, serverClusterVault)
	licenseSecret := setupLicense(t, serverClusterVault, cfg.EnterpriseLicense, cfg.EnableEnterprise)
	bootstrapTokenSecret := setupToken(t, serverClusterVault, "bootstrap")
	partitionTokenSecret := setupToken(t, serverClusterVault, "partition")

	setupPrimaryRoles(t, serverClusterVault, ns, consulReleaseName, serverPKIConfig, gossipSecret, connectCAPolicy, bootstrapTokenSecret, licenseSecret, partitionTokenSecret)

	setupSecondaryRoles(t, serverClusterVault, clientNs, consulReleaseName, secondaryPartition, serverPKIConfig, gossipSecret, partitionTokenSecret)

	// -----------------------------------------------------------------------
	// 6. Deploy Consul Servers (Primary)
	// -----------------------------------------------------------------------
	vaultCASecretName := vault.CASecretName(vaultReleaseName)

	serverHelmValues := map[string]string{
		"global.adminPartitions.enabled":                            "true",
		"global.enableConsulNamespaces":                             "true",
		"global.secretsBackend.vault.enabled":                       "true",
		"global.secretsBackend.vault.consulServerRole":              "server",
		"global.secretsBackend.vault.consulClientRole":              ClientRole,
		"global.secretsBackend.vault.consulCARole":                  serverPKIConfig.RoleName,
		"global.secretsBackend.vault.manageSystemACLsRole":          ManageSystemACLsRole,
		"global.secretsBackend.vault.ca.secretName":                 vaultCASecretName,
		"global.secretsBackend.vault.ca.secretKey":                  "tls.crt",
		"global.secretsBackend.vault.connectCA.address":             serverClusterVault.Address(),
		"global.secretsBackend.vault.connectCA.rootPKIPath":         "connect_root",
		"global.secretsBackend.vault.connectCA.intermediatePKIPath": "dc1/connect_inter",
		"global.acls.manageSystemACLs":                              "true",
		"global.tls.enabled":                                        "true",
		"global.tls.enableAutoEncrypt":                              "true",
		"global.tls.caCert.secretName":                              serverPKIConfig.CAPath,
		"server.exposeGossipAndRPCPorts":                            "true",
		"server.exposeService.enabled":                              "true",
		"server.exposeService.type":                                 svcType,
		"connectInject.certManager.enabled":                         "false",
		"connectInject.webhook.createCert":                          "true",
		"global.gossipEncryption.secretName":                        gossipSecret.Path,
		"global.gossipEncryption.secretKey":                         gossipSecret.Key,
		"global.acls.partitionToken.secretName":                     partitionTokenSecret.Path,
		"global.acls.partitionToken.secretKey":                      partitionTokenSecret.Key,
		"global.acls.bootstrapToken.secretName":                     bootstrapTokenSecret.Path,
		"global.acls.bootstrapToken.secretKey":                      bootstrapTokenSecret.Key,
	}

	if cfg.EnableEnterprise && cfg.EnterpriseLicense != "" {
		serverHelmValues["global.enterpriseLicense.secretName"] = licenseSecret.Path
		serverHelmValues["global.enterpriseLicense.secretKey"] = licenseSecret.Key
	}

	if cfg.UseKind {
		serverHelmValues["server.exposeService.nodePort.https"] = "30000"
		serverHelmValues["meshGateway.service.type"] = "NodePort"
		serverHelmValues["meshGateway.service.nodePort"] = "30100"
	}

	logger.Log(t, "Installing Consul Servers (Primary)")
	consulCluster := consul.NewHelmCluster(t, serverHelmValues, serverClusterCtx, cfg, consulReleaseName)
	consulCluster.Create(t)

	// -----------------------------------------------------------------------
	// 7. Sync Secrets & Get Addresses
	// -----------------------------------------------------------------------
	// [FIX] Use correct K8s Options (clientK8sOpts)
	syncSecret(t, serverClusterCtx,clientClusterCtx, ns, clientNs, vaultCASecretName)

	serverSvcName := fmt.Sprintf("%s-consul-server", consulReleaseName) 
	partitionSvcName := fmt.Sprintf("%s-consul-expose-servers", consulReleaseName)

	var serverSvcAddress, partitionSvcAddress string

	if cfg.UseKind {
		nodeIP, err := k8s.GetNodesE(t, serverK8sOpts)
		require.NoError(t, err)
		if len(nodeIP) > 0 {
			serverSvcAddress = waitForServiceLB(t, serverK8sOpts, serverSvcName)
			partitionSvcAddress = waitForServiceLB(t, serverK8sOpts, partitionSvcName)
		}
	} else {
		serverSvcAddress = waitForServiceLB(t, serverK8sOpts, serverSvcName)
		partitionSvcAddress = waitForServiceLB(t, serverK8sOpts, partitionSvcName)
	}

	// -----------------------------------------------------------------------
	// 8. Deploy Consul Partition (Secondary)
	// -----------------------------------------------------------------------
	// Use clientK8sOpts for loading config
	restConfigClient, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: clientK8sOpts.ConfigPath},
		&clientcmd.ConfigOverrides{CurrentContext: clientK8sOpts.ContextName},
	).ClientConfig()
	require.NoError(t, err)

	clientHelmValues := map[string]string{
		"global.enabled":                 "false",
		"global.adminPartitions.enabled": "true",
		"global.adminPartitions.name":    secondaryPartition,

		"global.secretsBackend.vault.enabled":              "true",
		"global.secretsBackend.vault.address":              externalVaultAddress,
		"global.secretsBackend.vault.consulClientRole":     ClientRole,
		"global.secretsBackend.vault.consulCARole":         serverPKIConfig.RoleName,
		"global.secretsBackend.vault.manageSystemACLsRole": ManageSystemACLsRole,
		"global.secretsBackend.vault.adminPartitionsRole":  "partition-init",
		"global.secretsBackend.vault.ca.secretName":        vaultCASecretName,
		"global.secretsBackend.vault.ca.secretKey":         "tls.crt",

		"global.secretsBackend.vault.consulClientMountPath":    "kubernetes-" + secondaryPartition,
		"global.secretsBackend.vault.adminPartitionsMountPath": "kubernetes-" + secondaryPartition,
		"global.secretsBackend.vault.consulServerMountPath":    "kubernetes-" + secondaryPartition,
		"connectInject.vault.authMethodPath":                   "kubernetes-" + secondaryPartition,

		"global.acls.bootstrapToken.secretName": partitionTokenSecret.Path,
		"global.acls.bootstrapToken.secretKey":  partitionTokenSecret.Key,
		"global.tls.enabled":                    "true",
		"global.tls.enableAutoEncrypt":          "true",
		"global.tls.caCert.secretName":          serverPKIConfig.CAPath,
		"global.gossipEncryption.secretName":    gossipSecret.Path,
		"global.gossipEncryption.secretKey":     gossipSecret.Key,

		"externalServers.enabled":           "true",
		"externalServers.hosts[0]":          serverSvcAddress,
		"externalServers.tlsServerName":     fmt.Sprintf("%s-consul-server", consulReleaseName),
		"externalServers.httpsPort":         "8501",
		"externalServers.k8sAuthMethodHost": restConfigClient.Host,

		"client.enabled":                                "true",
		"client.grpc":                                   "true",
		"client.exposeGossipPorts":                      "true",
		"client.join[0]":                                partitionSvcAddress,
		"connectInject.enabled":                         "true",
		"connectInject.certManager.enabled":             "false",
		"connectInject.webhook.createCert":              "true",
		"connectInject.transparentProxy.defaultEnabled": "true",
	}

	if cfg.UseKind {
		clientHelmValues["externalServers.httpsPort"] = "30000"
		clientHelmValues["meshGateway.service.type"] = "NodePort"
		clientHelmValues["meshGateway.service.nodePort"] = "30100"
	}

	logger.Log(t, "Installing Consul Clients (Secondary Partition)")
	clientConsulCluster := consul.NewHelmCluster(t, clientHelmValues, clientClusterCtx, cfg, consulReleaseName)
	clientConsulCluster.Create(t)

	// -----------------------------------------------------------------------
	// 9. Validation
	// -----------------------------------------------------------------------
	require.Eventually(t, func() bool {
		pods, err := clientClusterCtx.KubernetesClient(t).CoreV1().Pods(clientNs).List(context.Background(), metav1.ListOptions{
			LabelSelector: "app=consul,component=client",
		})
		if err != nil || len(pods.Items) == 0 {
			return false
		}
		for _, p := range pods.Items {
			if p.Status.Phase != corev1.PodRunning {
				return false
			}
		}
		return true
	}, 5*time.Minute, 5*time.Second, "Timeout waiting for secondary consul clients")

	agentPodList, _ := clientClusterCtx.KubernetesClient(t).CoreV1().Pods(clientNs).List(context.Background(), metav1.ListOptions{LabelSelector: "app=consul,component=client"})
	output, err := k8s.RunKubectlAndGetOutputE(t, clientK8sOpts, "logs", agentPodList.Items[0].Name, "consul", "-n", clientNs)
	require.NoError(t, err)
	require.Contains(t, output, "Partition: 'secondary'")
}

// -----------------------------------------------------------------------
// HELPER FUNCTIONS
// -----------------------------------------------------------------------

func setupPKI(t *testing.T, cluster *vault.VaultCluster, ns, consulReleaseName, connectCAPolicy string) *vault.PKIAndAuthRoleConfiguration {
	client := cluster.VaultClient(t)
	vault.CreateConnectCARootAndIntermediatePKIPolicy(t, client, connectCAPolicy, "connect_root", "dc1/connect_inter")

	config := &vault.PKIAndAuthRoleConfiguration{
		BaseURL:             "pki",
		PolicyName:          "consul-ca-policy",
		RoleName:            "consul-ca-role",
		KubernetesNamespace: ns,
		DataCenter:          "dc1",
		ServiceAccountName:  fmt.Sprintf("%s-consul-server", consulReleaseName),
		AllowedSubdomain:    fmt.Sprintf("%s-consul-server", consulReleaseName),
		MaxTTL:              "1h",
		AuthMethodPath:      KubernetesAuthMethodPath,
	}
	config.ConfigurePKIAndAuthRole(t, client)
	return config
}

func setupGossip(t *testing.T, cluster *vault.VaultCluster) *vault.KV2Secret {
	client := cluster.VaultClient(t)
	gossipKey, err := vault.GenerateGossipSecret()
	require.NoError(t, err)
	s := &vault.KV2Secret{
		Path:       "consul/data/secret/gossip",
		Key:        "gossip",
		Value:      gossipKey,
		PolicyName: "gossip",
	}
	s.SaveSecretAndAddReadPolicy(t, client)
	return s
}

func setupToken(t *testing.T, cluster *vault.VaultCluster, name string) *vault.KV2Secret {
	client := cluster.VaultClient(t)
	token, err := uuid.GenerateUUID()
	require.NoError(t, err)
	s := &vault.KV2Secret{
		Path:       fmt.Sprintf("consul/data/secret/%s", name),
		Key:        "token",
		Value:      token,
		PolicyName: name,
	}
	s.SaveSecretAndAddReadPolicy(t, client)
	return s
}

func setupLicense(t *testing.T, cluster *vault.VaultCluster, license string, enabled bool) *vault.KV2Secret {
	client := cluster.VaultClient(t)
	s := &vault.KV2Secret{
		Path:       "consul/data/secret/license",
		Key:        "license",
		Value:      license,
		PolicyName: "license",
	}
	if enabled && license != "" {
		s.SaveSecretAndAddReadPolicy(t, client)
	}
	return s
}

func setupPrimaryRoles(t *testing.T, cluster *vault.VaultCluster, ns, consulReleaseName string,
	pki *vault.PKIAndAuthRoleConfiguration, gossip *vault.KV2Secret, connectPol string,
	bootToken *vault.KV2Secret, license *vault.KV2Secret, partToken *vault.KV2Secret) {

	client := cluster.VaultClient(t)
	serverPolicies := fmt.Sprintf("%s,%s,%s,%s", gossip.PolicyName, connectPol, pki.PolicyName, bootToken.PolicyName)
	if license.Value != "" {
		serverPolicies += fmt.Sprintf(",%s", license.PolicyName)
	}

	(&vault.KubernetesAuthRoleConfiguration{
		ServiceAccountName:  pki.ServiceAccountName,
		KubernetesNamespace: ns,
		AuthMethodPath:      KubernetesAuthMethodPath,
		RoleName:            "server",
		PolicyNames:         serverPolicies,
	}).ConfigureK8SAuthRole(t, client)

	(&vault.KubernetesAuthRoleConfiguration{
		ServiceAccountName:  fmt.Sprintf("%s-consul-%s", consulReleaseName, ClientRole),
		KubernetesNamespace: ns,
		AuthMethodPath:      KubernetesAuthMethodPath,
		RoleName:            ClientRole,
		PolicyNames:         gossip.PolicyName,
	}).ConfigureK8SAuthRole(t, client)

	(&vault.KubernetesAuthRoleConfiguration{
		ServiceAccountName:  fmt.Sprintf("%s-consul-%s", consulReleaseName, ManageSystemACLsRole),
		KubernetesNamespace: ns,
		AuthMethodPath:      KubernetesAuthMethodPath,
		RoleName:            ManageSystemACLsRole,
		PolicyNames:         fmt.Sprintf("%s,%s", bootToken.PolicyName, partToken.PolicyName),
	}).ConfigureK8SAuthRole(t, client)

	(&vault.KubernetesAuthRoleConfiguration{
		ServiceAccountName:  "*",
		KubernetesNamespace: ns,
		AuthMethodPath:      KubernetesAuthMethodPath,
		RoleName:            pki.RoleName,
		PolicyNames:         pki.PolicyName,
	}).ConfigureK8SAuthRole(t, client)
}

func setupSecondaryRoles(t *testing.T, cluster *vault.VaultCluster, ns, consulReleaseName, partition string,
	pki *vault.PKIAndAuthRoleConfiguration, gossip *vault.KV2Secret, partToken *vault.KV2Secret) {

	client := cluster.VaultClient(t)
	authPath := "kubernetes-" + partition

	(&vault.KubernetesAuthRoleConfiguration{
		ServiceAccountName:  fmt.Sprintf("%s-consul-%s", consulReleaseName, ClientRole),
		KubernetesNamespace: ns,
		AuthMethodPath:      authPath,
		RoleName:            ClientRole,
		PolicyNames:         gossip.PolicyName,
	}).ConfigureK8SAuthRole(t, client)

	(&vault.KubernetesAuthRoleConfiguration{
		ServiceAccountName:  fmt.Sprintf("%s-consul-%s", consulReleaseName, ManageSystemACLsRole),
		KubernetesNamespace: ns,
		AuthMethodPath:      authPath,
		RoleName:            ManageSystemACLsRole,
		PolicyNames:         partToken.PolicyName,
	}).ConfigureK8SAuthRole(t, client)

	(&vault.KubernetesAuthRoleConfiguration{
		ServiceAccountName:  fmt.Sprintf("%s-consul-%s", consulReleaseName, "partition-init"),
		KubernetesNamespace: ns,
		AuthMethodPath:      authPath,
		RoleName:            "partition-init",
		PolicyNames:         partToken.PolicyName,
	}).ConfigureK8SAuthRole(t, client)

	(&vault.PKIAndAuthRoleConfiguration{
		BaseURL:             pki.BaseURL,
		PolicyName:          pki.PolicyName,
		RoleName:            pki.RoleName,
		KubernetesNamespace: ns,
		DataCenter:          "dc1",
		ServiceAccountName:  "*",
		AllowedSubdomain:    pki.AllowedSubdomain,
		MaxTTL:              pki.MaxTTL,
		AuthMethodPath:      authPath,
	}).ConfigurePKIAndAuthRole(t, client)
}

func waitForServiceLB(t *testing.T, ctx *k8s.KubectlOptions, serviceName string) string {
	var addr string
	require.Eventually(t, func() bool {
		svc, err := k8s.GetServiceE(t, ctx, serviceName)
		if err != nil {
			return false
		}

		if len(svc.Status.LoadBalancer.Ingress) > 0 {
			if svc.Status.LoadBalancer.Ingress[0].Hostname != "" {
				addr = svc.Status.LoadBalancer.Ingress[0].Hostname
				return true
			}
			if svc.Status.LoadBalancer.Ingress[0].IP != "" {
				addr = svc.Status.LoadBalancer.Ingress[0].IP
				return true
			}
		}

		if svc.Spec.Type == corev1.ServiceTypeNodePort {
			nodes, _ := k8s.GetNodesE(t, ctx)
			if len(nodes) > 0 {
				addr = "localhost"
				return true
			}
		}

		return false
	}, 10*time.Minute, 5*time.Second, "Waiting for LoadBalancer IP/Hostname")
	return addr
}

func createVaultAuthRBAC(t *testing.T, ctx environment.TestContext, namespace, name string) {
    client := ctx.KubernetesClient(t)

    _, err := client.RbacV1().ClusterRoleBindings().Create(context.Background(), &rbacv1.ClusterRoleBinding{
        ObjectMeta: metav1.ObjectMeta{Name: name},
        Subjects: []rbacv1.Subject{
            {
                Kind:      rbacv1.ServiceAccountKind,
                Name:      name,
                Namespace: namespace,
            },
        },
        RoleRef: rbacv1.RoleRef{
            APIGroup: "rbac.authorization.k8s.io",
            Kind:     "ClusterRole",
            Name:     "system:auth-delegator",
        },
    }, metav1.CreateOptions{})
    if err != nil && !strings.Contains(err.Error(), "already exists") {
        require.NoError(t, err)
    }

    svcAcct, err := client.CoreV1().ServiceAccounts(namespace).Create(
        context.Background(),
        &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: name}},
        metav1.CreateOptions{},
    )
    if err != nil && !strings.Contains(err.Error(), "already exists") {
        require.NoError(t, err)
    }

    if svcAcct != nil && len(svcAcct.Secrets) == 0 {
        _, err = client.CoreV1().Secrets(namespace).Create(
            context.Background(),
            &corev1.Secret{
                ObjectMeta: metav1.ObjectMeta{
                    Name:        name,
                    Annotations: map[string]string{corev1.ServiceAccountNameKey: name},
                },
                Type: corev1.SecretTypeServiceAccountToken,
            },
            metav1.CreateOptions{},
        )
        if err != nil && !strings.Contains(err.Error(), "already exists") {
            require.NoError(t, err)
        }
    }
}



func getServiceAccountToken(t *testing.T, ctx environment.TestContext, namespace, name string) string {
	var token string
	client := ctx.KubernetesClient(t)
	
	require.Eventually(t, func() bool {
		s, err := client.CoreV1().Secrets(namespace).Get(context.Background(), name, metav1.GetOptions{})
		if err != nil {
			return false
		}
		if len(s.Data["token"]) > 0 {
			token = string(s.Data["token"])
			return true
		}
		return false
	}, 1*time.Minute, 1*time.Second, "Waiting for SA token")
	return token
}

func syncSecret(t *testing.T, srcCtx, dstCtx environment.TestContext, srcNs, dstNs, secretName string) {
    srcClient := srcCtx.KubernetesClient(t)
    dstClient := dstCtx.KubernetesClient(t)

    s, err := srcClient.CoreV1().Secrets(srcNs).Get(context.Background(), secretName, metav1.GetOptions{})
    require.NoError(t, err)

    // Clear cluster-specific metadata and set target namespace
    s.ResourceVersion = ""
    s.Namespace = dstNs
    s.ObjectMeta = metav1.ObjectMeta{
        Name: s.Name,
        Labels: s.Labels,
        Annotations: s.Annotations,
    }

    _, err = dstClient.CoreV1().Secrets(dstNs).Create(context.Background(), s, metav1.CreateOptions{})
    if err != nil && !strings.Contains(err.Error(), "already exists") {
        require.NoError(t, err)
    }
}
