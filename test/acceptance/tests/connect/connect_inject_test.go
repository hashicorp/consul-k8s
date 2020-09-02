package connect

import (
	"testing"

	"github.com/hashicorp/consul-helm/test/acceptance/framework"
	"github.com/hashicorp/consul-helm/test/acceptance/helpers"
	"github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
)

const staticClientName = "static-client"

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
	helpers.DeployKustomize(t, ctx.KubectlOptions(), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")
	helpers.DeployKustomize(t, ctx.KubectlOptions(), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-client-inject")

	t.Log("checking that connection is successful")
	helpers.CheckStaticServerConnection(t, ctx.KubectlOptions(), true, staticClientName, "http://localhost:1234")
}

// Test that Connect works in a secure installation,
// with ACLs and TLS enabled.
func TestConnectInjectSecure(t *testing.T) {
	cases := []struct {
		name              string
		enableAutoEncrypt string
	}{
		{
			"without auto-encrypt",
			"false",
		},
		{
			"with auto-encrypt",
			"true",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := suite.Config()
			ctx := suite.Environment().DefaultContext(t)

			helmValues := map[string]string{
				"connectInject.enabled":        "true",
				"global.tls.enabled":           "true",
				"global.tls.enableAutoEncrypt": c.enableAutoEncrypt,
				"global.acls.manageSystemACLs": "true",
			}

			releaseName := helpers.RandomName()
			consulCluster := framework.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)

			consulCluster.Create(t)

			t.Log("creating static-server and static-client deployments")
			helpers.DeployKustomize(t, ctx.KubectlOptions(), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")
			helpers.DeployKustomize(t, ctx.KubectlOptions(), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-client-inject")

			t.Log("checking that the connection is not successful because there's no intention")
			helpers.CheckStaticServerConnection(t, ctx.KubectlOptions(), false, staticClientName, "http://localhost:1234")

			consulClient := consulCluster.SetupConsulClient(t, true)

			t.Log("creating intention")
			_, _, err := consulClient.Connect().IntentionCreate(&api.Intention{
				SourceName:      staticClientName,
				DestinationName: "static-server",
				Action:          api.IntentionActionAllow,
			}, nil)
			require.NoError(t, err)

			t.Log("checking that connection is successful")
			helpers.CheckStaticServerConnection(t, ctx.KubectlOptions(), true, staticClientName, "http://localhost:1234")
		})
	}
}
