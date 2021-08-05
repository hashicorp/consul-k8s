package sync

import (
	"fmt"
	"testing"
	"time"

	"github.com/hashicorp/consul-helm/test/acceptance/framework/consul"
	"github.com/hashicorp/consul-helm/test/acceptance/framework/helpers"
	"github.com/hashicorp/consul-helm/test/acceptance/framework/k8s"
	"github.com/hashicorp/consul-helm/test/acceptance/framework/logger"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
)

// Test that sync catalog works in both the default installation and
// the secure installation when TLS and ACLs are enabled.
// The test will create a test service and a pod and will
// wait for the service to be synced *to* consul.
func TestSyncCatalog(t *testing.T) {
	cases := []struct {
		name       string
		helmValues map[string]string
		secure     bool
	}{
		{
			"Default installation",
			map[string]string{
				"syncCatalog.enabled": "true",
			},
			false,
		},
		{
			"Secure installation (with TLS and ACLs enabled)",
			map[string]string{
				"syncCatalog.enabled":          "true",
				"global.tls.enabled":           "true",
				"global.acls.manageSystemACLs": "true",
			},
			true,
		},
		{
			"Secure installation (with TLS with auto-encrypt and ACLs enabled)",
			map[string]string{
				"syncCatalog.enabled":          "true",
				"global.tls.enabled":           "true",
				"global.tls.enableAutoEncrypt": "true",
				"global.acls.manageSystemACLs": "true",
			},
			true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := suite.Environment().DefaultContext(t)

			releaseName := helpers.RandomName()
			consulCluster := consul.NewHelmCluster(t, c.helmValues, ctx, suite.Config(), releaseName)

			consulCluster.Create(t)

			logger.Log(t, "creating a static-server with a service")
			k8s.DeployKustomize(t, ctx.KubectlOptions(t), suite.Config().NoCleanupOnFailure, suite.Config().DebugDirectory, "../fixtures/bases/static-server")

			consulClient := consulCluster.SetupConsulClient(t, c.secure)

			logger.Log(t, "checking that the service has been synced to Consul")
			var services map[string][]string
			syncedServiceName := fmt.Sprintf("static-server-%s", ctx.KubectlOptions(t).Namespace)
			counter := &retry.Counter{Count: 10, Wait: 5 * time.Second}
			retry.RunWith(counter, t, func(r *retry.R) {
				var err error
				services, _, err = consulClient.Catalog().Services(nil)
				require.NoError(r, err)
				if _, ok := services[syncedServiceName]; !ok {
					r.Errorf("service '%s' is not in Consul's list of services %s", syncedServiceName, services)
				}
			})

			service, _, err := consulClient.Catalog().Service(syncedServiceName, "", nil)
			require.NoError(t, err)
			require.Equal(t, 1, len(service))
			require.Equal(t, []string{"k8s"}, service[0].ServiceTags)
		})
	}
}
