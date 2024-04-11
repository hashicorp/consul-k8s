// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package sync

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"

	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
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
			require.Len(t, service, 1)
			require.Equal(t, []string{"k8s"}, service[0].ServiceTags)
			filter := fmt.Sprintf("ServiceID == %q", service[0].ServiceID)
			healthChecks, _, err := consulClient.Health().Checks(syncedServiceName, &api.QueryOptions{Filter: filter})
			require.NoError(t, err)
			require.Len(t, healthChecks, 1)
			require.Equal(t, api.HealthPassing, healthChecks[0].Status)
		})
	}
}

// Test that sync catalog works in both the default installation and
// the secure installation when TLS and ACLs are enabled with an Ingress resource.
// The test will create a test service and a pod and will
// wait for the service to be synced *to* consul.
func TestSyncCatalogWithIngress(t *testing.T) {
	t.Skip("TODO(fails): NET-8594")

	cfg := suite.Config()
	if cfg.EnableCNI {
		t.Skipf("skipping because -enable-cni is set and sync catalog is already tested with regular tproxy")
	}
	if !cfg.UseEKS {
		t.Skipf("skipping because -use-eks is not set and the ingress test only runs on EKS")
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
				"syncCatalog.ingress.enabled":  "true",
				"global.tls.enabled":           strconv.FormatBool(c.secure),
				"global.acls.manageSystemACLs": strconv.FormatBool(c.secure),
			}

			releaseName := helpers.RandomName()
			consulCluster := consul.NewHelmCluster(t, helmValues, ctx, suite.Config(), releaseName)

			logger.Log(t, "creating ingress resource")
			retry.Run(t, func(r *retry.R) {
				// Retry the kubectl apply because we've seen sporadic
				// "connection refused" errors where the mutating webhook
				// endpoint fails initially.
				out, err := k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "apply", "-k", "../fixtures/bases/ingress")
				require.NoError(r, err, out)
				helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
					// Ignore errors here because if the test ran as expected
					// the custom resources will have been deleted.
					k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "delete", "-k", "../fixtures/bases/ingress")
				})
			})

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
			require.Len(t, service, 1)
			require.Equal(t, "test.acceptance.com", service[0].Address)
			require.Equal(t, []string{"k8s"}, service[0].ServiceTags)
			filter := fmt.Sprintf("ServiceID == %q", service[0].ServiceID)
			healthChecks, _, err := consulClient.Health().Checks(syncedServiceName, &api.QueryOptions{Filter: filter})
			require.NoError(t, err)
			require.Len(t, healthChecks, 1)
			require.Equal(t, api.HealthPassing, healthChecks[0].Status)
		})
	}
}
