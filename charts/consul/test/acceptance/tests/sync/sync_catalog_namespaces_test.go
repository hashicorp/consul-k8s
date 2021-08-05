package sync

import (
	"strconv"
	"testing"
	"time"

	terratestk8s "github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/hashicorp/consul-helm/test/acceptance/framework/consul"
	"github.com/hashicorp/consul-helm/test/acceptance/framework/helpers"
	"github.com/hashicorp/consul-helm/test/acceptance/framework/k8s"
	"github.com/hashicorp/consul-helm/test/acceptance/framework/logger"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
)

const staticServerNamespace = "sync"
const staticServerService = "static-server"

// Test that sync catalog can sync services to consul namespaces,
// using both single namespace and mirroringK8S settings.
// These tests currently only test non-secure and secure without auto-encrypt installations
// because in the case of namespaces there isn't a significant distinction in code between auto-encrypt
// and non-auto-encrypt secure installations, so testing just one is enough.
func TestSyncCatalogNamespaces(t *testing.T) {
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
				"syncCatalog.enabled":           "true",
				// When mirroringK8S is set, this setting is ignored.
				"syncCatalog.consulNamespaces.consulDestinationNamespace": c.destinationNamespace,
				"syncCatalog.consulNamespaces.mirroringK8S":               strconv.FormatBool(c.mirrorK8S),
				"syncCatalog.addK8SNamespaceSuffix":                       "false",

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

			logger.Logf(t, "creating namespace %s", staticServerNamespace)
			k8s.RunKubectl(t, ctx.KubectlOptions(t), "create", "ns", staticServerNamespace)
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "delete", "ns", staticServerNamespace)
			})

			logger.Log(t, "creating a static-server with a service")
			k8s.DeployKustomize(t, staticServerOpts, cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/bases/static-server")

			consulClient := consulCluster.SetupConsulClient(t, c.secure)

			logger.Log(t, "checking that the service has been synced to Consul")
			var services map[string][]string
			counter := &retry.Counter{Count: 10, Wait: 5 * time.Second}

			consulNamespace := c.destinationNamespace
			if c.mirrorK8S {
				consulNamespace = staticServerNamespace
			}

			retry.RunWith(counter, t, func(r *retry.R) {
				var err error
				services, _, err = consulClient.Catalog().Services(&api.QueryOptions{Namespace: consulNamespace})
				require.NoError(r, err)
				if _, ok := services[staticServerService]; !ok {
					r.Errorf("service '%s' is not in Consul's list of services %s", staticServerService, services)
				}
			})

			service, _, err := consulClient.Catalog().Service(staticServerService, "", &api.QueryOptions{Namespace: consulNamespace})
			require.NoError(t, err)
			require.Equal(t, 1, len(service))
			require.Equal(t, []string{"k8s"}, service[0].ServiceTags)
		})
	}
}
