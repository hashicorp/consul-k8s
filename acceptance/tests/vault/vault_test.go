package vault

import (
	"testing"
	"time"

	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul-k8s/acceptance/framework/vault"
	"github.com/stretchr/testify/require"
)

// Installs Vault, bootstraps it with the kube auth method
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
	time.Sleep(time.Second * 60)
}
