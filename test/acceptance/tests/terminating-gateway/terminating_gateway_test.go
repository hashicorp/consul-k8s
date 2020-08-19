package terminatinggateway

import (
	"testing"

	"github.com/hashicorp/consul-helm/test/acceptance/framework"
	"github.com/hashicorp/consul-helm/test/acceptance/helpers"
	"github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
)

// Test that terminating gateways work in a default installation.
func TestTerminatingGateway(t *testing.T) {
	ctx := suite.Environment().DefaultContext(t)
	cfg := suite.Config()

	helmValues := map[string]string{
		"connectInject.enabled":                    "true",
		"terminatingGateways.enabled":              "true",
		"terminatingGateways.gateways[0].name":     "terminating-gateway",
		"terminatingGateways.gateways[0].replicas": "1",
	}

	t.Log("creating consul cluster")
	releaseName := helpers.RandomName()
	consulCluster := framework.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)
	consulCluster.Create(t)

	// Deploy a static-server that will play the role of an external service
	t.Log("creating static-server deployment")
	helpers.Deploy(t, ctx.KubectlOptions(), cfg.NoCleanupOnFailure, "fixtures/static-server.yaml")

	// Once the cluster is up, register the external service, then create the config entry.
	consulClient := consulCluster.SetupConsulClient(t, false)

	// Register the external service
	t.Log("registering the external service")
	_, err := consulClient.Catalog().Register(&api.CatalogRegistration{
		Node:     "legacy_node",
		Address:  "static-server",
		NodeMeta: map[string]string{"external-node": "true", "external-probe": "true"},
		Service: &api.AgentService{
			ID:      "static-server",
			Service: "static-server",
			Port:    80,
		},
	}, &api.WriteOptions{})
	require.NoError(t, err)

	// Create the config entry for the terminating gateway
	t.Log("creating config entry")
	created, _, err := consulClient.ConfigEntries().Set(&api.TerminatingGatewayConfigEntry{
		Kind:     api.TerminatingGateway,
		Name:     "terminating-gateway",
		Services: []api.LinkedService{{Name: "static-server"}},
	}, nil)
	require.NoError(t, err)
	require.True(t, created, "config entry failed")

	// Deploy the static client
	t.Log("deploying static client")
	helpers.Deploy(t, ctx.KubectlOptions(), cfg.NoCleanupOnFailure, "fixtures/static-client.yaml")

	// Test that we can make a call to the terminating gateway
	t.Log("trying calls to terminating gateway")
	helpers.CheckStaticServerConnection(t,
		ctx.KubectlOptions(),
		"static-client",
		true,
		"http://localhost:1234")
}
