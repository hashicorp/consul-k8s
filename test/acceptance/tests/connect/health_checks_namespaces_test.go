package connect

import (
	"strconv"
	"testing"

	"github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/hashicorp/consul-helm/test/acceptance/framework"
	"github.com/hashicorp/consul-helm/test/acceptance/helpers"
	"github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
)

// Test that health checks work with Consul Enterprise namespaces.
// Deploy with a passing health check. Test that the service is accessible over the mesh.
// Update the container with readiness probe so that it fails. Test that the service is inaccessible over the mesh.
// These tests currently only test non-secure and secure without auto-encrypt installations
// because in the case of namespaces there isn't a significant distinction in code between auto-encrypt
// and non-auto-encrypt secure installations, so testing just one is enough.
func TestHealthCheckNamespaces(t *testing.T) {
	cfg := suite.Config()
	if !cfg.EnableEnterprise {
		t.Skipf("skipping this test because -enable-enterprise is not set")
	}

	cases := []struct {
		Name                 string
		DestinationNamespace string
		MirrorK8S            bool
		Secure               bool
	}{
		{
			Name:                 "single destination namespace (non-default)",
			DestinationNamespace: staticServerNamespace,
			MirrorK8S:            false,
			Secure:               false,
		},
		{
			Name:                 "single destination namespace (non-default); secure",
			DestinationNamespace: staticServerNamespace,
			MirrorK8S:            false,
			Secure:               true,
		},
		{
			Name:                 "mirror k8s namespaces",
			DestinationNamespace: staticServerNamespace,
			MirrorK8S:            true,
			Secure:               false,
		},
		{
			Name:                 "mirror k8s namespaces; secure",
			DestinationNamespace: staticServerNamespace,
			MirrorK8S:            true,
			Secure:               true,
		},
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			ctx := suite.Environment().DefaultContext(t)
			cfg := suite.Config()

			helmValues := map[string]string{
				"global.enableConsulNamespaces":      "true",
				"connectInject.enabled":              "true",
				"connectInject.healthChecks.enabled": "true",
				// When mirroringK8S is set, this setting is ignored.
				"connectInject.consulNamespaces.consulDestinationNamespace": c.DestinationNamespace,
				"connectInject.consulNamespaces.mirroringK8S":               strconv.FormatBool(c.MirrorK8S),
				"global.acls.manageSystemACLs":                              strconv.FormatBool(c.Secure),
				"global.tls.enabled":                                        strconv.FormatBool(c.Secure),
			}

			releaseName := helpers.RandomName()
			consulCluster := framework.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)
			consulCluster.Create(t)

			staticServerOpts := &k8s.KubectlOptions{
				ContextName: ctx.KubectlOptions(t).ContextName,
				ConfigPath:  ctx.KubectlOptions(t).ConfigPath,
				Namespace:   staticServerNamespace,
			}
			staticClientOpts := &k8s.KubectlOptions{
				ContextName: ctx.KubectlOptions(t).ContextName,
				ConfigPath:  ctx.KubectlOptions(t).ConfigPath,
				Namespace:   staticClientNamespace,
			}
			t.Logf("creating namespaces %s and %s", staticServerNamespace, staticClientNamespace)
			helpers.RunKubectl(t, ctx.KubectlOptions(t), "create", "ns", staticServerNamespace)
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
				helpers.RunKubectl(t, ctx.KubectlOptions(t), "delete", "ns", staticServerNamespace)
			})
			helpers.RunKubectl(t, ctx.KubectlOptions(t), "create", "ns", staticClientNamespace)
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
				helpers.RunKubectl(t, ctx.KubectlOptions(t), "delete", "ns", staticClientNamespace)
			})

			t.Log("creating static-server and static-client deployments")
			helpers.DeployKustomize(t, staticServerOpts, cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-server-hc")
			helpers.DeployKustomize(t, staticClientOpts, cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-client-namespaces")

			// If ACLs are enabled we must create an intention.
			if c.Secure {
				consulClient := consulCluster.SetupConsulClient(t, true)

				intention := &api.Intention{
					SourceName:      staticClientName,
					SourceNS:        staticClientNamespace,
					DestinationName: staticServerName,
					DestinationNS:   staticServerNamespace,
					Action:          api.IntentionActionAllow,
				}
				// Set the destination namespace to be the same
				// unless mirrorK8S is true.
				if !c.MirrorK8S {
					intention.SourceNS = c.DestinationNamespace
					intention.DestinationNS = c.DestinationNamespace
				}
				t.Log("creating intention")
				_, _, err := consulClient.Connect().IntentionCreate(intention, nil)
				require.NoError(t, err)
			}
			t.Log("checking that connection is successful")
			helpers.CheckStaticServerConnectionSuccessful(t, staticClientOpts, staticClientName, "http://localhost:1234")

			// Now create the file so that the readiness probe of the static-server pod fails.
			helpers.RunKubectl(t, staticServerOpts, "exec", "deploy/"+staticServerName, "--", "touch", "/tmp/unhealthy")

			// The readiness probe should take a moment to be reflected in Consul, CheckStaticServerConnection will retry
			// until Consul marks the service instance unavailable for mesh traffic, causing the connection to fail.
			// We are expecting a "connection reset by peer" error because in a case of health checks,
			// there will be no healthy proxy host to connect to. That's why we can't assert that we receive an empty reply
			// from server, which is the case when a connection is unsuccessful due to intentions in other tests.
			t.Log("checking that connection is unsuccessful")
			helpers.CheckStaticServerConnectionMultipleFailureMessages(
				t,
				staticClientOpts,
				false,
				staticClientName,
				[]string{"curl: (56) Recv failure: Connection reset by peer", "curl: (52) Empty reply from server"},
				"http://localhost:1234")
		})
	}
}
