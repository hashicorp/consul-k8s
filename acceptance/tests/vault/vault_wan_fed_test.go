package vault

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/consul-k8s/acceptance/framework/config"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul-k8s/acceptance/framework/vault"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/go-uuid"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Test that WAN federation via Mesh gateways works with Vault
// as the secrets backend, testing all possible credentials that can be used for WAN federation.
// This test deploys a Vault cluster with a server in the primary k8s cluster and exposes it to the
// secondary cluster via a Kubernetes service. We then only need to deploy Vault agent injector
// in the secondary that will treat the Vault server in the primary as an external server.
func TestVault_WANFederationViaGateways(t *testing.T) {
	cfg := suite.Config()
	if !cfg.EnableMultiCluster {
		t.Skipf("skipping this test because -enable-multi-cluster is not set")
	}
	primaryCtx := suite.Environment().DefaultContext(t)
	secondaryCtx := suite.Environment().Context(t, environment.SecondaryContextName)

	ns := primaryCtx.KubectlOptions(t).Namespace

	vaultReleaseName := helpers.RandomName()
	consulReleaseName := helpers.RandomName()

	// In the primary cluster, we will expose Vault server as a Load balancer
	// or a NodePort service so that the secondary can connect to it.
	primaryVaultHelmValues := map[string]string{
		"server.service.type": "LoadBalancer",
	}
	if cfg.UseKind {
		primaryVaultHelmValues["server.service.type"] = "NodePort"
		primaryVaultHelmValues["server.service.nodePort"] = "31000"
	}

	primaryVaultCluster := vault.NewVaultCluster(t, primaryCtx, cfg, vaultReleaseName, primaryVaultHelmValues)
	primaryVaultCluster.Create(t, primaryCtx, "")

	externalVaultAddress := vaultAddress(t, cfg, primaryCtx, vaultReleaseName)

	// In the secondary cluster, we will only deploy the agent injector and provide
	// it with the primary's Vault address. We also want to configure the injector with
	// a different k8s auth method path since the secondary cluster will need its own auth method.
	secondaryVaultHelmValues := map[string]string{
		"server.enabled":             "false",
		"injector.externalVaultAddr": externalVaultAddress,
		"injector.authPath":          "auth/kubernetes-dc2",
	}

	secondaryVaultCluster := vault.NewVaultCluster(t, secondaryCtx, cfg, vaultReleaseName, secondaryVaultHelmValues)
	secondaryVaultCluster.Create(t, secondaryCtx, "")

	vaultClient := primaryVaultCluster.VaultClient(t)

	secondaryAuthMethodName := "kubernetes-dc2"

	// Configure Vault Kubernetes auth method for the secondary datacenter.
	{
		// Create auth method service account and ClusterRoleBinding. The Vault server
		// in the primary cluster will use this service account token to talk to the secondary
		// Kubernetes cluster.
		// This ClusterRoleBinding is adapted from the Vault server's role:
		// https://github.com/hashicorp/vault-helm/blob/b0528fce49c529f2c37953ea3a14f30ed651e0d6/templates/server-clusterrolebinding.yaml

		// Use a single name for all RBAC objects.
		authMethodRBACName := fmt.Sprintf("%s-vault-auth-method", vaultReleaseName)
		_, err := secondaryCtx.KubernetesClient(t).RbacV1().ClusterRoleBindings().Create(context.Background(), &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: authMethodRBACName,
			},
			Subjects: []rbacv1.Subject{{Kind: rbacv1.ServiceAccountKind, Name: authMethodRBACName, Namespace: ns}},
			RoleRef:  rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Name: "system:auth-delegator", Kind: "ClusterRole"},
		}, metav1.CreateOptions{})
		require.NoError(t, err)

		// Create service account for the auth method in the secondary cluster.
		_, err = secondaryCtx.KubernetesClient(t).CoreV1().ServiceAccounts(ns).Create(context.Background(), &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name: authMethodRBACName,
			},
		}, metav1.CreateOptions{})
		require.NoError(t, err)
		t.Cleanup(func() {
			secondaryCtx.KubernetesClient(t).RbacV1().ClusterRoleBindings().Delete(context.Background(), authMethodRBACName, metav1.DeleteOptions{})
			secondaryCtx.KubernetesClient(t).CoreV1().ServiceAccounts(ns).Delete(context.Background(), authMethodRBACName, metav1.DeleteOptions{})
		})

		// Figure out the host for the Kubernetes API. This needs to be reachable from the Vault server
		// in the primary cluster.
		k8sAuthMethodHost := k8s.KubernetesAPIServerHost(t, cfg, secondaryCtx)

		// Now, configure the auth method in Vault.
		secondaryVaultCluster.ConfigureAuthMethod(t, vaultClient, secondaryAuthMethodName, k8sAuthMethodHost, authMethodRBACName, ns)
	}
	// -------------------------
	// PKI
	// -------------------------
	// dc1
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
		AuthMethodPath:      "kubernetes",
	}
	vault.ConfigurePKIAndAuthRole(t, vaultClient, serverPKIConfig)

	// dc2
	// Configure Service Mesh CA
	connectCAPolicySecondary := "connect-ca-dc2"
	connectCARootPathSecondary := "connect_root"
	connectCAIntermediatePathSecondary := "dc2/connect_inter"
	// Configure Policy for Connect CA
	vault.CreateConnectCARootAndIntermediatePKIPolicy(t, vaultClient, connectCAPolicySecondary, connectCARootPathSecondary, connectCAIntermediatePathSecondary)

	// Configure Server PKI
	serverPKIConfigSecondary := &vault.PKIAndAuthRoleConfiguration{
		BaseURL:             "pki",
		PolicyName:          "consul-ca-policy-dc2",
		RoleName:            "consul-ca-role-dc2",
		KubernetesNamespace: ns,
		DataCenter:          "dc2",
		ServiceAccountName:  fmt.Sprintf("%s-consul-%s", consulReleaseName, "server"),
		AllowedSubdomain:    fmt.Sprintf("%s-consul-%s", consulReleaseName, "server"),
		MaxTTL:              "1h",
		AuthMethodPath:      secondaryAuthMethodName,
		SkipPKIMount:        true,
	}
	vault.ConfigurePKIAndAuthRole(t, vaultClient, serverPKIConfigSecondary)

	// -------------------------
	// KV2 secrets
	// -------------------------
	// Gossip key
	gossipKey, err := vault.GenerateGossipSecret()
	require.NoError(t, err)
	gossipSecret := &vault.SaveVaultSecretConfiguration{
		Path:       "consul/data/secret/gossip",
		Key:        "gossip",
		Value:      gossipKey,
		PolicyName: "gossip",
	}
	vault.SaveSecret(t, vaultClient, gossipSecret)

	// License
	licenseSecret := &vault.SaveVaultSecretConfiguration{
		Path:       "consul/data/secret/license",
		Key:        "license",
		Value:      cfg.EnterpriseLicense,
		PolicyName: "license",
	}
	if cfg.EnableEnterprise {
		vault.SaveSecret(t, vaultClient, licenseSecret)
	}

	// Bootstrap Token
	bootstrapToken, err := uuid.GenerateUUID()
	require.NoError(t, err)
	bootstrapTokenSecret := &vault.SaveVaultSecretConfiguration{
		Path:       "consul/data/secret/bootstrap",
		Key:        "token",
		Value:      bootstrapToken,
		PolicyName: "bootstrap",
	}
	vault.SaveSecret(t, vaultClient, bootstrapTokenSecret)

	// Replication Token
	replicationToken, err := uuid.GenerateUUID()
	require.NoError(t, err)
	replicationTokenSecret := &vault.SaveVaultSecretConfiguration{
		Path:       "consul/data/secret/replication",
		Key:        "token",
		Value:      replicationToken,
		PolicyName: "replication",
	}
	vault.SaveSecret(t, vaultClient, replicationTokenSecret)

	commonServerPolicies := "gossip"
	if cfg.EnableEnterprise {
		commonServerPolicies += ",license"
	}

	// --------------------------------------------
	// Additional Auth Roles for Primary Datacenter
	// --------------------------------------------
	// server
	serverPolicies := fmt.Sprintf("%s,%s,%s,%s", commonServerPolicies, connectCAPolicy, serverPKIConfig.PolicyName, bootstrapTokenSecret.PolicyName)
	if cfg.EnableEnterprise {
		serverPolicies += fmt.Sprintf(",%s", licenseSecret.PolicyName)
	}
	consulServerRole := "server"
	vault.ConfigureK8SAuthRole(t, vaultClient, &vault.KubernetesAuthRoleConfiguration{
		ServiceAccountName:  serverPKIConfig.ServiceAccountName,
		KubernetesNamespace: ns,
		AuthMethodPath:      "kubernetes",
		RoleName:            consulServerRole,
		PolicyNames:         serverPolicies,
	})

	// client
	consulClientRole := "client"
	consulClientServiceAccountName := fmt.Sprintf("%s-consul-%s", consulReleaseName, "client")
	vault.ConfigureK8SAuthRole(t, vaultClient, &vault.KubernetesAuthRoleConfiguration{
		ServiceAccountName:  consulClientServiceAccountName,
		KubernetesNamespace: ns,
		AuthMethodPath:      "kubernetes",
		RoleName:            consulClientRole,
		PolicyNames:         gossipSecret.PolicyName,
	})

	// manageSystemACLs
	manageSystemACLsRole := "server-acl-init"
	manageSystemACLsServiceAccountName := fmt.Sprintf("%s-consul-%s", consulReleaseName, "server-acl-init")
	vault.ConfigureK8SAuthRole(t, vaultClient, &vault.KubernetesAuthRoleConfiguration{
		ServiceAccountName:  manageSystemACLsServiceAccountName,
		KubernetesNamespace: ns,
		AuthMethodPath:      "kubernetes",
		RoleName:            manageSystemACLsRole,
		PolicyNames:         fmt.Sprintf("%s,%s", bootstrapTokenSecret.PolicyName, replicationTokenSecret.PolicyName),
	})

	// allow all components to access server ca
	vault.ConfigureK8SAuthRole(t, vaultClient, &vault.KubernetesAuthRoleConfiguration{
		ServiceAccountName:  "*",
		KubernetesNamespace: ns,
		AuthMethodPath:      "kubernetes",
		RoleName:            serverPKIConfig.RoleName,
		PolicyNames:         serverPKIConfig.PolicyName,
	})

	// --------------------------------------------
	// Additional Auth Roles for Secondary Datacenter
	// --------------------------------------------
	// server
	secondaryServerPolicies := fmt.Sprintf("%s,%s,%s,%s", commonServerPolicies, connectCAPolicySecondary, serverPKIConfigSecondary.PolicyName, replicationTokenSecret.PolicyName)
	vault.ConfigureK8SAuthRole(t, vaultClient, &vault.KubernetesAuthRoleConfiguration{
		ServiceAccountName:  serverPKIConfig.ServiceAccountName,
		KubernetesNamespace: ns,
		AuthMethodPath:      secondaryAuthMethodName,
		RoleName:            consulServerRole,
		PolicyNames:         secondaryServerPolicies,
	})

	// client
	vault.ConfigureK8SAuthRole(t, vaultClient, &vault.KubernetesAuthRoleConfiguration{
		ServiceAccountName:  consulClientServiceAccountName,
		KubernetesNamespace: ns,
		AuthMethodPath:      secondaryAuthMethodName,
		RoleName:            consulClientRole,
		PolicyNames:         gossipSecret.PolicyName,
	})

	// manageSystemACLs
	vault.ConfigureK8SAuthRole(t, vaultClient, &vault.KubernetesAuthRoleConfiguration{
		ServiceAccountName:  manageSystemACLsServiceAccountName,
		KubernetesNamespace: ns,
		AuthMethodPath:      secondaryAuthMethodName,
		RoleName:            manageSystemACLsRole,
		PolicyNames:         replicationTokenSecret.PolicyName,
	})

	// allow all components to access server ca
	vault.ConfigureK8SAuthRole(t, vaultClient, &vault.KubernetesAuthRoleConfiguration{
		ServiceAccountName:  "*",
		KubernetesNamespace: ns,
		AuthMethodPath:      secondaryAuthMethodName,
		RoleName:            serverPKIConfig.RoleName,
		PolicyNames:         serverPKIConfig.PolicyName,
	})

	// // Move Vault CA secret from primary to secondary so that we can mount it to pods in the
	// // secondary cluster.
	vaultCASecretName := vault.CASecretName(vaultReleaseName)
	logger.Logf(t, "retrieving Vault CA secret %s from the primary cluster and applying to the secondary", vaultCASecretName)
	vaultCASecret, err := primaryCtx.KubernetesClient(t).CoreV1().Secrets(ns).Get(context.Background(), vaultCASecretName, metav1.GetOptions{})
	vaultCASecret.ResourceVersion = ""
	require.NoError(t, err)
	_, err = secondaryCtx.KubernetesClient(t).CoreV1().Secrets(ns).Create(context.Background(), vaultCASecret, metav1.CreateOptions{})
	require.NoError(t, err)
	t.Cleanup(func() {
		secondaryCtx.KubernetesClient(t).CoreV1().Secrets(ns).Delete(context.Background(), vaultCASecretName, metav1.DeleteOptions{})
	})

	primaryConsulHelmValues := map[string]string{
		"global.datacenter": "dc1",

		"global.federation.enabled": "true",

		// TLS config.
		"global.tls.enabled":           "true",
		"global.tls.enableAutoEncrypt": "true",
		"global.tls.caCert.secretName": serverPKIConfig.CAPath,
		"server.serverCert.secretName": serverPKIConfig.CertPath,

		// Gossip config.
		"global.gossipEncryption.secretName": gossipSecret.Path,
		"global.gossipEncryption.secretKey":  gossipSecret.Key,

		// ACL config.
		"global.acls.manageSystemACLs":            "true",
		"global.acls.bootstrapToken.secretName":   bootstrapTokenSecret.Path,
		"global.acls.bootstrapToken.secretKey":    bootstrapTokenSecret.Key,
		"global.acls.createReplicationToken":      "true",
		"global.acls.replicationToken.secretName": replicationTokenSecret.Path,
		"global.acls.replicationToken.secretKey":  replicationTokenSecret.Key,

		// Mesh config.
		"connectInject.enabled": "true",
		"controller.enabled":    "true",
		"meshGateway.enabled":   "true",
		"meshGateway.replicas":  "1",

		// Server config.
		"server.extraVolumes[0].type": "secret",
		"server.extraVolumes[0].name": vaultCASecretName,
		"server.extraVolumes[0].load": "false",

		// Vault config.
		"global.secretsBackend.vault.enabled":                       "true",
		"global.secretsBackend.vault.consulServerRole":              consulServerRole,
		"global.secretsBackend.vault.consulClientRole":              consulClientRole,
		"global.secretsBackend.vault.consulCARole":                  serverPKIConfig.RoleName,
		"global.secretsBackend.vault.manageSystemACLsRole":          manageSystemACLsRole,
		"global.secretsBackend.vault.ca.secretName":                 vaultCASecretName,
		"global.secretsBackend.vault.ca.secretKey":                  "tls.crt",
		"global.secretsBackend.vault.connectCA.address":             primaryVaultCluster.Address(),
		"global.secretsBackend.vault.connectCA.rootPKIPath":         connectCARootPath,
		"global.secretsBackend.vault.connectCA.intermediatePKIPath": connectCAIntermediatePath,
	}

	if cfg.EnableEnterprise {
		primaryConsulHelmValues["global.enterpriseLicense.secretName"] = licenseSecret.Path
		primaryConsulHelmValues["global.enterpriseLicense.secretKey"] = licenseSecret.Key
	}

	if cfg.UseKind {
		primaryConsulHelmValues["meshGateway.service.type"] = "NodePort"
		primaryConsulHelmValues["meshGateway.service.nodePort"] = "30000"
	}

	primaryConsulCluster := consul.NewHelmCluster(t, primaryConsulHelmValues, primaryCtx, cfg, consulReleaseName)
	primaryConsulCluster.Create(t)

	var k8sAuthMethodHost string
	// When running on kind, the kube API address in kubeconfig will have a localhost address
	// which will not work from inside the container. That's why we need to use the endpoints address instead
	// which will point the node IP.
	if cfg.UseKind {
		// The Kubernetes AuthMethod host is read from the endpoints for the Kubernetes service.
		kubernetesEndpoint, err := secondaryCtx.KubernetesClient(t).CoreV1().Endpoints("default").Get(context.Background(), "kubernetes", metav1.GetOptions{})
		require.NoError(t, err)
		k8sAuthMethodHost = fmt.Sprintf("%s:%d", kubernetesEndpoint.Subsets[0].Addresses[0].IP, kubernetesEndpoint.Subsets[0].Ports[0].Port)
	} else {
		k8sAuthMethodHost = k8s.KubernetesAPIServerHostFromOptions(t, secondaryCtx.KubectlOptions(t))
	}

	// Get the address of the mesh gateway.
	primaryMeshGWAddress := meshGatewayAddress(t, cfg, primaryCtx, consulReleaseName)
	secondaryConsulHelmValues := map[string]string{
		"global.datacenter": "dc2",

		"global.federation.enabled":            "true",
		"global.federation.k8sAuthMethodHost":  k8sAuthMethodHost,
		"global.federation.primaryDatacenter":  "dc1",
		"global.federation.primaryGateways[0]": primaryMeshGWAddress,

		// TLS config.
		"global.tls.enabled":           "true",
		"global.tls.enableAutoEncrypt": "true",
		"global.tls.caCert.secretName": serverPKIConfigSecondary.CAPath,
		"server.serverCert.secretName": serverPKIConfigSecondary.CertPath,

		// Gossip config.
		"global.gossipEncryption.secretName": gossipSecret.Path,
		"global.gossipEncryption.secretKey":  gossipSecret.Key,

		// ACL config.
		"global.acls.manageSystemACLs":            "true",
		"global.acls.replicationToken.secretName": replicationTokenSecret.Path,
		"global.acls.replicationToken.secretKey":  replicationTokenSecret.Key,

		// Mesh config.
		"connectInject.enabled": "true",
		"meshGateway.enabled":   "true",
		"meshGateway.replicas":  "1",

		// Server config.
		"server.extraVolumes[0].type": "secret",
		"server.extraVolumes[0].name": vaultCASecretName,
		"server.extraVolumes[0].load": "false",

		// Vault config.
		"global.secretsBackend.vault.enabled":                       "true",
		"global.secretsBackend.vault.consulServerRole":              consulServerRole,
		"global.secretsBackend.vault.consulClientRole":              consulClientRole,
		"global.secretsBackend.vault.consulCARole":                  serverPKIConfig.RoleName,
		"global.secretsBackend.vault.manageSystemACLsRole":          manageSystemACLsRole,
		"global.secretsBackend.vault.ca.secretName":                 vaultCASecretName,
		"global.secretsBackend.vault.ca.secretKey":                  "tls.crt",
		"global.secretsBackend.vault.agentAnnotations":              fmt.Sprintf("vault.hashicorp.com/tls-server-name: %s-vault", vaultReleaseName),
		"global.secretsBackend.vault.connectCA.address":             externalVaultAddress,
		"global.secretsBackend.vault.connectCA.authMethodPath":      secondaryAuthMethodName,
		"global.secretsBackend.vault.connectCA.rootPKIPath":         connectCARootPathSecondary,
		"global.secretsBackend.vault.connectCA.intermediatePKIPath": connectCAIntermediatePathSecondary,
		"global.secretsBackend.vault.connectCA.additionalConfig":    fmt.Sprintf(`"{"connect": [{"ca_config": [{"tls_server_name": "%s-vault"}]}]}"`, vaultReleaseName),
	}

	if cfg.EnableEnterprise {
		secondaryConsulHelmValues["global.enterpriseLicense.secretName"] = licenseSecret.Path
		secondaryConsulHelmValues["global.enterpriseLicense.secretKey"] = licenseSecret.Key
	}

	if cfg.UseKind {
		secondaryConsulHelmValues["meshGateway.service.type"] = "NodePort"
		secondaryConsulHelmValues["meshGateway.service.nodePort"] = "30000"
	}

	// Install the secondary consul cluster in the secondary kubernetes context.
	secondaryConsulCluster := consul.NewHelmCluster(t, secondaryConsulHelmValues, secondaryCtx, cfg, consulReleaseName)
	secondaryConsulCluster.Create(t)

	// Verify federation between servers.
	logger.Log(t, "verifying federation was successful")
	primaryConsulCluster.ACLToken = bootstrapToken
	primaryClient, _ := primaryConsulCluster.SetupConsulClient(t, true)
	secondaryConsulCluster.ACLToken = replicationToken
	secondaryClient, _ := secondaryConsulCluster.SetupConsulClient(t, true)
	helpers.VerifyFederation(t, primaryClient, secondaryClient, consulReleaseName, true)

	// Create a ProxyDefaults resource to configure services to use the mesh
	// gateways.
	logger.Log(t, "creating proxy-defaults config")
	kustomizeDir := "../fixtures/bases/mesh-gateway"
	k8s.KubectlApplyK(t, primaryCtx.KubectlOptions(t), kustomizeDir)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
		k8s.KubectlDeleteK(t, primaryCtx.KubectlOptions(t), kustomizeDir)
	})

	// Check that we can connect services over the mesh gateways.
	logger.Log(t, "creating static-server in dc2")
	k8s.DeployKustomize(t, secondaryCtx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")

	logger.Log(t, "creating static-client in dc1")
	k8s.DeployKustomize(t, primaryCtx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-client-multi-dc")

	logger.Log(t, "creating intention")
	_, _, err = primaryClient.ConfigEntries().Set(&api.ServiceIntentionsConfigEntry{
		Kind: api.ServiceIntentions,
		Name: "static-server",
		Sources: []*api.SourceIntention{
			{
				Name:   "static-client",
				Action: api.IntentionActionAllow,
			},
		},
	}, nil)
	require.NoError(t, err)

	logger.Log(t, "checking that connection is successful")
	k8s.CheckStaticServerConnectionSuccessful(t, primaryCtx.KubectlOptions(t), staticClientName, "http://localhost:1234")
}

// vaultAddress returns Vault's server URL depending on test configuration.
func vaultAddress(t *testing.T, cfg *config.TestConfig, ctx environment.TestContext, vaultReleaseName string) string {
	vaultHost := k8s.ServiceHost(t, cfg, ctx, fmt.Sprintf("%s-vault", vaultReleaseName))
	if cfg.UseKind {
		return fmt.Sprintf("https://%s:31000", vaultHost)
	}
	return fmt.Sprintf("https://%s:8200", vaultHost)
}

// meshGatewayAddress returns a full address of the mesh gateway depending on configuration.
func meshGatewayAddress(t *testing.T, cfg *config.TestConfig, ctx environment.TestContext, consulReleaseName string) string {
	primaryMeshGWHost := k8s.ServiceHost(t, cfg, ctx, fmt.Sprintf("%s-consul-mesh-gateway", consulReleaseName))
	if cfg.UseKind {
		return fmt.Sprintf("%s:%d", primaryMeshGWHost, 30000)
	} else {
		return fmt.Sprintf("%s:%d", primaryMeshGWHost, 443)
	}
}
