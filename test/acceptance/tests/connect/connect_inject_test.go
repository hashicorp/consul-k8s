package connect

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/hashicorp/consul-helm/test/acceptance/framework"
	"github.com/hashicorp/consul-helm/test/acceptance/helpers"
	"github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
)

const staticClientName = "static-client"
const staticServerName = "static-server"

// Test that Connect works in a default and a secure installation
func TestConnectInject(t *testing.T) {
	cases := []struct {
		secure      bool
		autoEncrypt bool
	}{
		{false, false},
		{true, false},
		{true, true},
	}

	for _, c := range cases {
		name := fmt.Sprintf("secure: %t; auto-encrypt: %t", c.secure, c.autoEncrypt)
		t.Run(name, func(t *testing.T) {
			cfg := suite.Config()
			ctx := suite.Environment().DefaultContext(t)

			helmValues := map[string]string{
				"connectInject.enabled":        "true",
				"global.tls.enabled":           strconv.FormatBool(c.secure),
				"global.tls.enableAutoEncrypt": strconv.FormatBool(c.autoEncrypt),
				"global.acls.manageSystemACLs": strconv.FormatBool(c.secure),
			}

			releaseName := helpers.RandomName()
			consulCluster := framework.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)

			consulCluster.Create(t)

			t.Log("creating static-server and static-client deployments")
			helpers.DeployKustomize(t, ctx.KubectlOptions(), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")
			helpers.DeployKustomize(t, ctx.KubectlOptions(), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-client-inject")

			if c.secure {
				t.Log("checking that the connection is not successful because there's no intention")
				helpers.CheckStaticServerConnection(t, ctx.KubectlOptions(), false, staticClientName, "http://localhost:1234")

				consulClient := consulCluster.SetupConsulClient(t, true)

				t.Log("creating intention")
				_, _, err := consulClient.Connect().IntentionCreate(&api.Intention{
					SourceName:      staticClientName,
					DestinationName: staticServerName,
					Action:          api.IntentionActionAllow,
				}, nil)
				require.NoError(t, err)
			}

			t.Log("checking that connection is successful")
			helpers.CheckStaticServerConnection(t, ctx.KubectlOptions(), true, staticClientName, "http://localhost:1234")
		})
	}
}
