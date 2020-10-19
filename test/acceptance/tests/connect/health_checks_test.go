package connect

import (
	"fmt"
	"github.com/hashicorp/consul-helm/test/acceptance/framework"
	"github.com/hashicorp/consul-helm/test/acceptance/helpers"
	"strconv"
	"testing"
)

// Test that health checks work in a default installation and a secure installation with TLS/auto-encrypt permutations.
// Deploy with a passing health check.
// Test that the service is accessible over the mesh.
// Update the container with readiness probe so that it fails.
// Test that the service is inaccessible over the mesh.
func TestHealthChecks(t *testing.T) {
	cases := []struct {
		secure      bool
		autoEncrypt bool
	}{
		{
			false,
			false,
		},
		{
			true,
			false,
		},
		{
			true,
			true,
		},
	}

	for _, c := range cases {
		name := fmt.Sprintf("secure: %t, auto-encrypt: %t", c.secure, c.autoEncrypt)
		t.Run(name, func(t *testing.T) {
			ctx := suite.Environment().DefaultContext(t)
			cfg := suite.Config()

			helmValues := map[string]string{
				"connectInject.enabled":              "true",
				"connectInject.healthChecks.enabled": "true",
				"global.tls.enabled":                 strconv.FormatBool(c.secure),
				"global.tls.autoEncrypt":             strconv.FormatBool(c.autoEncrypt),
			}

			releaseName := helpers.RandomName()
			consulCluster := framework.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)
			consulCluster.Create(t)

			t.Log("creating static-server and static-client deployments")
			helpers.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-server-hc")
			helpers.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-client-inject")
			// TODO: it would be nice to add a codepath which makes a connection to the agent where staticServer is running
			// so that it can fetch the healthcheck and its status and assert on this. Right now the health check status
			// is implied by the traffic passing or not.
			t.Log("checking that connection is successful")
			helpers.CheckStaticServerConnection(t, ctx.KubectlOptions(t), true, staticClientName, "http://localhost:1234")

			// Now create the file so that the readiness probe of the static-server pod fails.
			helpers.RunKubectl(t, ctx.KubectlOptions(t), "exec", "-it", "deploy/"+staticServerName, "--", "touch", "/tmp/unhealthy")

			// The readiness probe should take a moment to be reflected in Consul, CheckStaticServerConnection will retry
			// until Consul marks the service instance unavailable for mesh traffic, causing the connection to fail.
			t.Log("checking that connection is unsuccessful")
			helpers.CheckStaticServerConnection(t, ctx.KubectlOptions(t), false, staticClientName, "http://localhost:1234")
		})
	}
}
