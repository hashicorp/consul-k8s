package connect

import (
	"strconv"
	"testing"

	terratestk8s "github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/hashicorp/consul-helm/test/acceptance/framework/consul"
	"github.com/hashicorp/consul-helm/test/acceptance/framework/helpers"
	"github.com/hashicorp/consul-helm/test/acceptance/framework/k8s"
	"github.com/hashicorp/consul-helm/test/acceptance/framework/logger"
	"github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
)

const staticServerNamespace = "ns1"
const staticClientNamespace = "ns2"

// Test that Connect works with Consul Enterprise namespaces.
// These tests currently only test non-secure and secure without auto-encrypt installations
// because in the case of namespaces there isn't a significant distinction in code between auto-encrypt
// and non-auto-encrypt secure installations, so testing just one is enough.
func TestConnectInjectNamespaces(t *testing.T) {
	cfg := suite.Config()
	if !cfg.EnableEnterprise {
		t.Skipf("skipping this test because -enable-enterprise is not set")
	}

	cases := []struct {
		name                 string
		destinationNamespace string
		mirrorK8S            bool
		secure               bool
	}{
		{
			"single destination namespace (non-default)",
			staticServerNamespace,
			false,
			false,
		},
		{
			"single destination namespace (non-default); secure",
			staticServerNamespace,
			false,
			true,
		},
		{
			"mirror k8s namespaces",
			staticServerNamespace,
			true,
			false,
		},
		{
			"mirror k8s namespaces; secure",
			staticServerNamespace,
			true,
			true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := suite.Environment().DefaultContext(t)

			helmValues := map[string]string{
				"global.enableConsulNamespaces": "true",
				"connectInject.enabled":         "true",
				// When mirroringK8S is set, this setting is ignored.
				"connectInject.consulNamespaces.consulDestinationNamespace": c.destinationNamespace,
				"connectInject.consulNamespaces.mirroringK8S":               strconv.FormatBool(c.mirrorK8S),

				"global.acls.manageSystemACLs": strconv.FormatBool(c.secure),
				"global.tls.enabled":           strconv.FormatBool(c.secure),
			}

			releaseName := helpers.RandomName()
			consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)

			consulCluster.Create(t)

			staticServerOpts := &terratestk8s.KubectlOptions{
				ContextName: ctx.KubectlOptions(t).ContextName,
				ConfigPath:  ctx.KubectlOptions(t).ConfigPath,
				Namespace:   staticServerNamespace,
			}
			staticClientOpts := &terratestk8s.KubectlOptions{
				ContextName: ctx.KubectlOptions(t).ContextName,
				ConfigPath:  ctx.KubectlOptions(t).ConfigPath,
				Namespace:   staticClientNamespace,
			}

			logger.Logf(t, "creating namespaces %s and %s", staticServerNamespace, staticClientNamespace)
			k8s.RunKubectl(t, ctx.KubectlOptions(t), "create", "ns", staticServerNamespace)
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "delete", "ns", staticServerNamespace)
			})

			k8s.RunKubectl(t, ctx.KubectlOptions(t), "create", "ns", staticClientNamespace)
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "delete", "ns", staticClientNamespace)
			})

			logger.Log(t, "creating static-server and static-client deployments")
			k8s.DeployKustomize(t, staticServerOpts, cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")
			k8s.DeployKustomize(t, staticClientOpts, cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-client-namespaces")

			consulClient := consulCluster.SetupConsulClient(t, c.secure)

			// Make sure that services are registered in the correct namespace.
			// If mirroring is enabled, we expect services to be registered in the
			// Consul namespace with the same name as their source
			// Kubernetes namespace.
			// If a single destination namespace is set, we expect all services
			// to be registered in that destination Consul namespace.
			serverQueryOpts := &api.QueryOptions{Namespace: staticServerNamespace}
			clientQueryOpts := &api.QueryOptions{Namespace: staticClientNamespace}

			if !c.mirrorK8S {
				serverQueryOpts = &api.QueryOptions{Namespace: c.destinationNamespace}
				clientQueryOpts = &api.QueryOptions{Namespace: c.destinationNamespace}
			}
			services, _, err := consulClient.Catalog().Service(staticServerName, "", serverQueryOpts)
			require.NoError(t, err)
			require.Len(t, services, 1)

			services, _, err = consulClient.Catalog().Service(staticClientName, "", clientQueryOpts)
			require.NoError(t, err)
			require.Len(t, services, 1)

			if c.secure {
				logger.Log(t, "checking that the connection is not successful because there's no intention")
				k8s.CheckStaticServerConnectionFailing(t, staticClientOpts, staticClientName, "http://localhost:1234")

				intention := &api.Intention{
					SourceName:      staticClientName,
					SourceNS:        staticClientNamespace,
					DestinationName: staticServerName,
					DestinationNS:   staticServerNamespace,
					Action:          api.IntentionActionAllow,
				}

				// Set the destination namespace to be the same
				// unless mirrorK8S is true.
				if !c.mirrorK8S {
					intention.SourceNS = c.destinationNamespace
					intention.DestinationNS = c.destinationNamespace
				}

				logger.Log(t, "creating intention")
				_, _, err := consulClient.Connect().IntentionCreate(intention, nil)
				require.NoError(t, err)
			}

			logger.Log(t, "checking that connection is successful")
			k8s.CheckStaticServerConnectionSuccessful(t, staticClientOpts, staticClientName, "http://localhost:1234")
		})
	}
}
