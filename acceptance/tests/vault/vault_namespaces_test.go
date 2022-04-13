package vault

import (
	"fmt"
	"os"
	"testing"

	terratestLogger "github.com/gruntwork-io/terratest/modules/logger"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul-k8s/acceptance/framework/vault"
	"github.com/stretchr/testify/require"
)

// TestVault installs Vault, configures a Vault namespace, and then bootstraps it
// with secrets, policies, and Kube Auth Method.
// It then configures Consul to use vault as the backend and checks that it works
//with the vault namespace.
func TestVault_VaultNamespace(t *testing.T) {
	cfg := suite.Config()
	ctx := suite.Environment().DefaultContext(t)
	ns := ctx.KubectlOptions(t).Namespace
	vaultNamespacePath := "admin"
	consulReleaseName := helpers.RandomName()
	vaultReleaseName := helpers.RandomName()

	k8sClient := environment.KubernetesClientFromOptions(t, ctx.KubectlOptions(t))
	vaultLicenseSecretName := fmt.Sprintf("%s-enterprise-license", vaultReleaseName)
	vaultLicenseSecretKey := "license"

	vaultEnterpriseLicense := os.Getenv("VAULT_ENT_LICENSE")

	logger.Log(t, "Creating secret for Vault license")
	consul.CreateK8sSecret(t, k8sClient, cfg, ns, vaultLicenseSecretName, vaultLicenseSecretKey, vaultEnterpriseLicense)
	vaultHelmvalues := map[string]string{
		"server.image.repository":             "hashicorp/vault-enterprise",
		"server.image.tag":                    "1.9.4-ent",
		"server.enterpriseLicense.secretName": vaultLicenseSecretName,
		"server.enterpriseLicense.secretKey":  vaultLicenseSecretKey,
	}
	vaultCluster := vault.NewVaultCluster(t, ctx, cfg, vaultReleaseName, vaultHelmvalues)
	vaultCluster.Create(t, ctx, vaultNamespacePath)
	// Vault is now installed in the cluster.

	// Now fetch the Vault client so we can create the policies and secrets.
	vaultClient := vaultCluster.VaultClient(t)

	gossipKey := vault.ConfigureGossipVaultSecret(t, vaultClient)

	vault.CreateConnectCAPolicy(t, vaultClient, "dc1")
	if cfg.EnableEnterprise {
		vault.ConfigureEnterpriseLicenseVaultSecret(t, vaultClient, cfg)
	}

	bootstrapToken := vault.ConfigureACLTokenVaultSecret(t, vaultClient, "bootstrap")

	serverPolicies := "gossip,connect-ca-dc1,server-cert-dc1,bootstrap-token"
	if cfg.EnableEnterprise {
		serverPolicies += ",license"
	}
	vault.ConfigureKubernetesAuthRole(t, vaultClient, consulReleaseName, ns, "kubernetes", "server", serverPolicies)
	vault.ConfigureKubernetesAuthRole(t, vaultClient, consulReleaseName, ns, "kubernetes", "client", "gossip")
	vault.ConfigureKubernetesAuthRole(t, vaultClient, consulReleaseName, ns, "kubernetes", "server-acl-init", "bootstrap-token")
	vault.ConfigureConsulCAKubernetesAuthRole(t, vaultClient, ns, "kubernetes")

	vault.ConfigurePKICA(t, vaultClient)
	certPath := vault.ConfigurePKICertificates(t, vaultClient, consulReleaseName, ns, "dc1")

	vaultCASecret := vault.CASecretName(vaultReleaseName)

	consulHelmValues := map[string]string{
		"server.extraVolumes[0].type": "secret",
		"server.extraVolumes[0].name": vaultCASecret,
		"server.extraVolumes[0].load": "false",

		"connectInject.enabled":  "true",
		"connectInject.replicas": "1",
		"controller.enabled":     "true",

		"global.secretsBackend.vault.enabled":              "true",
		"global.secretsBackend.vault.consulServerRole":     "server",
		"global.secretsBackend.vault.consulClientRole":     "client",
		"global.secretsBackend.vault.consulCARole":         "consul-ca",
		"global.secretsBackend.vault.manageSystemACLsRole": "server-acl-init",

		"global.secretsBackend.vault.ca.secretName": vaultCASecret,
		"global.secretsBackend.vault.ca.secretKey":  "tls.crt",

		"global.secretsBackend.vault.connectCA.address":             vaultCluster.Address(),
		"global.secretsBackend.vault.connectCA.rootPKIPath":         "connect_root",
		"global.secretsBackend.vault.connectCA.intermediatePKIPath": "dc1/connect_inter",
		"global.secretsBackend.vault.connectCA.additionalConfig":    fmt.Sprintf(`"{\"connect\": [{ \"ca_config\": [{ \"namespace\": \"%s\"}]}]}"`, vaultNamespacePath),

		"global.secretsBackend.vault.agentAnnotations": fmt.Sprintf("\"vault.hashicorp.com/namespace\": \"%s\"", vaultNamespacePath),

		"global.acls.manageSystemACLs":          "true",
		"global.acls.bootstrapToken.secretName": "consul/data/secret/bootstrap",
		"global.acls.bootstrapToken.secretKey":  "token",
		"global.tls.enabled":                    "true",
		"global.gossipEncryption.secretName":    "consul/data/secret/gossip",
		"global.gossipEncryption.secretKey":     "gossip",

		"ingressGateways.enabled":               "true",
		"ingressGateways.defaults.replicas":     "1",
		"terminatingGateways.enabled":           "true",
		"terminatingGateways.defaults.replicas": "1",

		"server.serverCert.secretName": certPath,
		"global.tls.caCert.secretName": "pki/cert/ca",
		"global.tls.enableAutoEncrypt": "true",

		// For sync catalog, it is sufficient to check that the deployment is running and ready
		// because we only care that get-auto-encrypt-client-ca init container was able
		// to talk to the Consul server using the CA from Vault. For this reason,
		// we don't need any services to be synced in either direction.
		"syncCatalog.enabled":  "true",
		"syncCatalog.toConsul": "false",
		"syncCatalog.toK8S":    "false",
	}

	if cfg.EnableEnterprise {
		consulHelmValues["global.enterpriseLicense.secretName"] = "consul/data/secret/license"
		consulHelmValues["global.enterpriseLicense.secretKey"] = "license"
	}

	logger.Log(t, "Installing Consul")
	consulCluster := consul.NewHelmCluster(t, consulHelmValues, ctx, cfg, consulReleaseName)
	consulCluster.Create(t)

	// Validate that the gossip encryption key is set correctly.
	logger.Log(t, "Validating the gossip key has been set correctly.")
	consulCluster.ACLToken = bootstrapToken
	consulClient := consulCluster.SetupConsulClient(t, true)
	keys, err := consulClient.Operator().KeyringList(nil)
	require.NoError(t, err)
	// There are two identical keys for LAN and WAN since there is only 1 dc.
	require.Len(t, keys, 2)
	require.Equal(t, 1, keys[0].PrimaryKeys[gossipKey])

	// Confirm that the Vault Connect CA has been bootstrapped correctly.
	caConfig, _, err := consulClient.Connect().CAGetConfig(nil)
	require.NoError(t, err)
	require.Equal(t, caConfig.Provider, "vault")

	// Validate that consul sever is running correctly and the consul members command works
	logger.Log(t, "Confirming that we can run Consul commands when exec'ing into server container")
	membersOutput, err := k8s.RunKubectlAndGetOutputWithLoggerE(t, ctx.KubectlOptions(t), terratestLogger.Discard, "exec", fmt.Sprintf("%s-consul-server-0", consulReleaseName), "-c", "consul", "--", "sh", "-c", fmt.Sprintf("CONSUL_HTTP_TOKEN=%s consul members", bootstrapToken))
	logger.Logf(t, "Members: \n%s", membersOutput)
	require.NoError(t, err)
	require.Contains(t, membersOutput, fmt.Sprintf("%s-consul-server-0", consulReleaseName))

	if cfg.EnableEnterprise {
		// Validate that the enterprise license is set correctly.
		logger.Log(t, "Validating the enterprise license has been set correctly.")
		license, licenseErr := consulClient.Operator().LicenseGet(nil)
		require.NoError(t, licenseErr)
		require.True(t, license.Valid)
	}

	// Deploy two services and check that they can talk to each other.
	logger.Log(t, "creating static-server and static-client deployments")
	k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")
	if cfg.EnableTransparentProxy {
		k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-client-tproxy")
	} else {
		k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-client-inject")
	}
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
		k8s.KubectlDeleteK(t, ctx.KubectlOptions(t), "../fixtures/bases/intention")
	})
	k8s.KubectlApplyK(t, ctx.KubectlOptions(t), "../fixtures/bases/intention")

	logger.Log(t, "checking that connection is successful")
	if cfg.EnableTransparentProxy {
		k8s.CheckStaticServerConnectionSuccessful(t, ctx.KubectlOptions(t), staticClientName, "http://static-server")
	} else {
		k8s.CheckStaticServerConnectionSuccessful(t, ctx.KubectlOptions(t), staticClientName, "http://localhost:1234")
	}
}
