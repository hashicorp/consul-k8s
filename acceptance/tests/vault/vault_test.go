package vault

import (
	"fmt"
	"testing"

	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul-k8s/acceptance/framework/vault"
	"github.com/stretchr/testify/require"
)

const (
	gossipKey = "3R7oLrdpkk2V0Y7yHLizyxXeS2RtaVuy07DkU15Lhws="
)

// Installs Vault, bootstraps it with the Kube Auth Method
// and then validates that the KV2 secrets engine is online
// and the Kube Auth Method is enabled.
func TestVault_Create(t *testing.T) {
	cfg := suite.Config()
	ctx := suite.Environment().DefaultContext(t)

	vaultReleaseName := helpers.RandomName()
	vaultCluster := vault.NewVaultCluster(t, nil, ctx, cfg, vaultReleaseName)
	vaultCluster.Create(t, ctx)
	logger.Log(t, "Finished Installing and Bootstrapping")

	vaultClient := vaultCluster.VaultClient(t)

	// Write to the KV2 engine succeeds.
	logger.Log(t, "Creating a KV2 Secret")
	params := map[string]interface{}{
		"data": map[string]interface{}{
			"foo": "bar",
		},
	}
	_, err := vaultClient.Logical().Write("consul/data/secret/test", params)
	require.NoError(t, err)

	// Validate that the Auth Method exists.
	authList, err := vaultClient.Sys().ListAuth()
	require.NoError(t, err)
	logger.Log(t, "Auth List: ", authList)
	require.NotNil(t, authList["kubernetes/"])
}

// Installs Vault, bootstraps it with secrets, policies, and Kube Auth Method
// then creates a gossip encryption secret and uses this to bootstrap Consul.
func TestVault_BootstrapConsulGossipEncryptionKey(t *testing.T) {
	cfg := suite.Config()
	ctx := suite.Environment().DefaultContext(t)

	consulReleaseName := helpers.RandomName()
	vaultReleaseName := helpers.RandomName()
	consulClientServiceAccountName := fmt.Sprintf("%s-consul-client", consulReleaseName)
	consulServerServiceAccountName := fmt.Sprintf("%s-consul-server", consulReleaseName)

	vaultCluster := vault.NewVaultCluster(t, nil, ctx, cfg, vaultReleaseName)
	vaultCluster.Create(t, ctx)
	// Vault is now installed in the cluster.

	// Now fetch the Vault client so we can create the policies and secrets.
	vaultClient := vaultCluster.VaultClient(t)

	// Create the Vault Policy for the gossip key.
	logger.Log(t, "Creating the gossip policy")
	rules := `
path "consul/data/secret/gossip" {
  capabilities = ["read"]
}`
	err := vaultClient.Sys().PutPolicy("consul-gossip", rules)
	require.NoError(t, err)

	// Create the Auth Roles for consul-server + consul-client.
	logger.Log(t, "Creating the gossip auth roles")
	params := map[string]interface{}{
		"bound_service_account_names":      consulClientServiceAccountName,
		"bound_service_account_namespaces": "default",
		"policies":                         "consul-gossip",
		"ttl":                              "24h",
	}
	_, err = vaultClient.Logical().Write("auth/kubernetes/role/consul-client", params)
	require.NoError(t, err)

	params = map[string]interface{}{
		"bound_service_account_names":      consulServerServiceAccountName,
		"bound_service_account_namespaces": "default",
		"policies":                         "consul-gossip",
		"ttl":                              "24h",
	}
	_, err = vaultClient.Logical().Write("auth/kubernetes/role/consul-server", params)
	require.NoError(t, err)

	// Create the gossip key.
	logger.Log(t, "Creating the gossip secret")
	params = map[string]interface{}{
		"data": map[string]interface{}{
			"gossip": gossipKey,
		},
	}
	_, err = vaultClient.Logical().Write("consul/data/secret/gossip", params)
	require.NoError(t, err)

	consulHelmValues := map[string]string{
		"server.enabled":  "true",
		"server.replicas": "1",

		"connectInject.enabled": "true",

		"global.secretsBackend.vault.enabled":          "true",
		"global.secretsBackend.vault.consulServerRole": "consul-server",
		"global.secretsBackend.vault.consulclientRole": "consul-client",

		"global.acls.manageSystemACLs":       "true",
		"global.tls.enabled":                 "true",
		"global.gossipEncryption.secretName": "consul/data/secret/gossip",
		"global.gossipEncryption.secretKey":  ".Data.data.gossip",
	}
	logger.Log(t, "Installing Consul")
	consulCluster := consul.NewHelmCluster(t, consulHelmValues, ctx, cfg, consulReleaseName)
	consulCluster.Create(t)

	// Validate that the gossip encryption key is set correctly.
	logger.Log(t, "Validating the gossip key has been set correctly.")
	consulClient := consulCluster.SetupConsulClient(t, true)
	keys, err := consulClient.Operator().KeyringList(nil)
	require.NoError(t, err)
	// we use keys[0] because KeyringList returns a list of keyrings for each dc, in this case there is only 1 dc.
	require.Equal(t, 1, keys[0].PrimaryKeys[gossipKey])
}
