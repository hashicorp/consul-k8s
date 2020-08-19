package connect

import (
	"testing"

	"github.com/hashicorp/consul-helm/test/acceptance/framework"
	"github.com/hashicorp/consul-helm/test/acceptance/helpers"
	"github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
)

// Test that Connect works in a default installation
func TestConnectInjectDefault(t *testing.T) {
	cfg := suite.Config()
	ctx := suite.Environment().DefaultContext(t)

	helmValues := map[string]string{
		"connectInject.enabled": "true",
	}

	releaseName := helpers.RandomName()
	consulCluster := framework.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)

	consulCluster.Create(t)

	t.Log("creating static-server and static-client deployments")
	helpers.Deploy(t, ctx.KubectlOptions(), cfg.NoCleanupOnFailure, "fixtures/static-server.yaml")
	helpers.Deploy(t, ctx.KubectlOptions(), cfg.NoCleanupOnFailure, "fixtures/static-client.yaml")

	t.Log("checking that connection is successful")
	helpers.CheckConnection(t, ctx.KubectlOptions(), "static-client", true, "http://localhost:1234")
}

// Test that Connect works in a secure installation,
// with ACLs and TLS enabled.
func TestConnectInjectSecure(t *testing.T) {
	cfg := suite.Config()
	ctx := suite.Environment().DefaultContext(t)

	helmValues := map[string]string{
		"connectInject.enabled":        "true",
		"global.tls.enabled":           "true",
		"global.acls.manageSystemACLs": "true",
	}

	releaseName := helpers.RandomName()
	consulCluster := framework.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)

	consulCluster.Create(t)

	t.Log("creating static-server and static-client deployments")
	helpers.Deploy(t, ctx.KubectlOptions(), cfg.NoCleanupOnFailure, "fixtures/static-server.yaml")
	helpers.Deploy(t, ctx.KubectlOptions(), cfg.NoCleanupOnFailure, "fixtures/static-client.yaml")

	t.Log("checking that the connection is not successful because there's no intention")
	helpers.CheckConnection(t, ctx.KubectlOptions(), "static-client", false, "http://localhost:1234")

	consulClient := consulCluster.SetupConsulClient(t, true)

	t.Log("creating intention")
	_, _, err := consulClient.Connect().IntentionCreate(&api.Intention{
		SourceName:      "static-client",
		DestinationName: "static-server",
		Action:          api.IntentionActionAllow,
	}, nil)
	require.NoError(t, err)

	t.Log("checking that connection is successful")
	helpers.CheckConnection(t, ctx.KubectlOptions(), "static-client", true, "http://localhost:1234")
}
