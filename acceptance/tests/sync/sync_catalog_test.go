package sync

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
)

// Test that sync catalog works in both the default installation and
// the secure installation when TLS and ACLs are enabled.
// The test will create a test service and a pod and will
// wait for the service to be synced *to* consul.
func TestSyncCatalog(t *testing.T) {
	cfg := suite.Config()
	if cfg.EnableCNI {
		t.Skipf("skipping because -enable-cni is set and sync catalog is already tested with regular tproxy")
	}

	cases := map[string]struct {
		secure bool
	}{
		"non-secure": {secure: false},
		"secure":     {secure: true},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := suite.Environment().DefaultContext(t)
			helmValues := map[string]string{
				"syncCatalog.enabled":          "true",
				"global.tls.enabled":           strconv.FormatBool(c.secure),
				"global.acls.manageSystemACLs": strconv.FormatBool(c.secure),
			}

			releaseName := helpers.RandomName()
			consulCluster := consul.NewHelmCluster(t, helmValues, ctx, suite.Config(), releaseName)

			consulCluster.Create(t)

			logger.Log(t, "creating a static-server with a service")
			k8s.DeployKustomize(t, ctx.KubectlOptions(t), suite.Config().NoCleanupOnFailure, suite.Config().DebugDirectory, "../fixtures/bases/static-server")

			consulClient, _ := consulCluster.SetupConsulClient(t, c.secure)

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
