package controller

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/hashicorp/consul-helm/test/acceptance/framework"
	"github.com/hashicorp/consul-helm/test/acceptance/helpers"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
)

func TestController(t *testing.T) {
	cfg := suite.Config()

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
			ctx := suite.Environment().DefaultContext(t)

			helmValues := map[string]string{
				"controller.enabled":    "true",
				"connectInject.enabled": "true",

				"global.tls.enabled":           strconv.FormatBool(c.secure),
				"global.tls.enableAutoEncrypt": strconv.FormatBool(c.autoEncrypt),
				"global.acls.manageSystemACLs": strconv.FormatBool(c.secure),
			}

			releaseName := helpers.RandomName()
			consulCluster := framework.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)

			consulCluster.Create(t)
			consulClient := consulCluster.SetupConsulClient(t, c.secure)

			// Test creation.
			{
				t.Log("creating custom resources")
				retry.Run(t, func(r *retry.R) {
					// Retry the kubectl apply because we've seen sporadic
					// "connection refused" errors where the mutating webhook
					// endpoint fails initially.
					out, err := helpers.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "apply", "-f", "../fixtures/crds")
					require.NoError(r, err, out)
					helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
						// Ignore errors here because if the test ran as expected
						// the custom resources will have been deleted.
						helpers.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "delete", "-f", "../fixtures/crds")
					})
				})

				// On startup, the controller can take upwards of 1m to perform
				// leader election so we may need to wait a long time for
				// the reconcile loop to run (hence the 1m timeout here).
				counter := &retry.Counter{Count: 60, Wait: 1 * time.Second}
				retry.RunWith(counter, t, func(r *retry.R) {
					// service-defaults
					entry, _, err := consulClient.ConfigEntries().Get(api.ServiceDefaults, "defaults", nil)
					require.NoError(r, err)
					svcDefaultEntry, ok := entry.(*api.ServiceConfigEntry)
					require.True(r, ok, "could not cast to ServiceConfigEntry")
					require.Equal(r, "http", svcDefaultEntry.Protocol)

					// service-resolver
					entry, _, err = consulClient.ConfigEntries().Get(api.ServiceResolver, "resolver", nil)
					require.NoError(r, err)
					svcResolverEntry, ok := entry.(*api.ServiceResolverConfigEntry)
					require.True(r, ok, "could not cast to ServiceResolverConfigEntry")
					require.Equal(r, "bar", svcResolverEntry.Redirect.Service)

					// proxy-defaults
					entry, _, err = consulClient.ConfigEntries().Get(api.ProxyDefaults, "global", nil)
					require.NoError(r, err)
					proxyDefaultEntry, ok := entry.(*api.ProxyConfigEntry)
					require.True(r, ok, "could not cast to ProxyConfigEntry")
					require.Equal(r, api.MeshGatewayModeLocal, proxyDefaultEntry.MeshGateway.Mode)

					// service-router
					entry, _, err = consulClient.ConfigEntries().Get(api.ServiceRouter, "router", nil)
					require.NoError(r, err)
					svcRouterEntry, ok := entry.(*api.ServiceRouterConfigEntry)
					require.True(r, ok, "could not cast to ServiceRouterConfigEntry")
					require.Equal(r, "/foo", svcRouterEntry.Routes[0].Match.HTTP.PathPrefix)
				})
			}

			// Test updates.
			{
				t.Log("patching service-defaults custom resource")
				patchProtocol := "tcp"
				helpers.RunKubectl(t, ctx.KubectlOptions(t), "patch", "servicedefaults", "defaults", "-p", fmt.Sprintf(`{"spec":{"protocol":"%s"}}`, patchProtocol), "--type=merge")

				t.Log("patching service-resolver custom resource")
				patchRedirectSvc := "baz"
				helpers.RunKubectl(t, ctx.KubectlOptions(t), "patch", "serviceresolver", "resolver", "-p", fmt.Sprintf(`{"spec":{"redirect":{"service": "%s"}}}`, patchRedirectSvc), "--type=merge")

				t.Log("patching proxy-defaults custom resource")
				patchMeshGatewayMode := "remote"
				helpers.RunKubectl(t, ctx.KubectlOptions(t), "patch", "proxydefaults", "global", "-p", fmt.Sprintf(`{"spec":{"meshGateway":{"mode": "%s"}}}`, patchMeshGatewayMode), "--type=merge")

				t.Log("patching service-router custom resource")
				patchPathPrefix := "/baz"
				helpers.RunKubectl(t, ctx.KubectlOptions(t), "patch", "servicerouter", "router", "-p", fmt.Sprintf(`{"spec":{"routes":[{"match":{"http":{"pathPrefix":"%s"}}}]}}`, patchPathPrefix), "--type=merge")

				counter := &retry.Counter{Count: 10, Wait: 500 * time.Millisecond}
				retry.RunWith(counter, t, func(r *retry.R) {
					// service-defaults
					entry, _, err := consulClient.ConfigEntries().Get(api.ServiceDefaults, "defaults", nil)
					require.NoError(r, err)
					svcDefaultEntry, ok := entry.(*api.ServiceConfigEntry)
					require.True(r, ok, "could not cast to ServiceConfigEntry")
					require.Equal(r, patchProtocol, svcDefaultEntry.Protocol)

					// service-resolver
					entry, _, err = consulClient.ConfigEntries().Get(api.ServiceResolver, "resolver", nil)
					require.NoError(r, err)
					svcResolverEntry, ok := entry.(*api.ServiceResolverConfigEntry)
					require.True(r, ok, "could not cast to ServiceResolverConfigEntry")
					require.Equal(r, patchRedirectSvc, svcResolverEntry.Redirect.Service)

					// proxy-defaults
					entry, _, err = consulClient.ConfigEntries().Get(api.ProxyDefaults, "global", nil)
					require.NoError(r, err)
					proxyDefaultsEntry, ok := entry.(*api.ProxyConfigEntry)
					require.True(r, ok, "could not cast to ProxyConfigEntry")
					require.Equal(r, api.MeshGatewayModeRemote, proxyDefaultsEntry.MeshGateway.Mode)

					// service-router
					entry, _, err = consulClient.ConfigEntries().Get(api.ServiceRouter, "router", nil)
					require.NoError(r, err)
					svcRouterEntry, ok := entry.(*api.ServiceRouterConfigEntry)
					require.True(r, ok, "could not cast to ServiceRouterConfigEntry")
					require.Equal(r, patchPathPrefix, svcRouterEntry.Routes[0].Match.HTTP.PathPrefix)
				})
			}

			// Test a delete.
			{
				t.Log("deleting service-defaults custom resource")
				helpers.RunKubectl(t, ctx.KubectlOptions(t), "delete", "servicedefaults", "defaults")

				t.Log("deleting service-resolver custom resource")
				helpers.RunKubectl(t, ctx.KubectlOptions(t), "delete", "serviceresolver", "resolver")

				t.Log("deleting proxy-defaults custom resource")
				helpers.RunKubectl(t, ctx.KubectlOptions(t), "delete", "proxydefaults", "global")

				t.Log("deleting service-router custom resource")
				helpers.RunKubectl(t, ctx.KubectlOptions(t), "delete", "servicerouter", "router")

				counter := &retry.Counter{Count: 10, Wait: 500 * time.Millisecond}
				retry.RunWith(counter, t, func(r *retry.R) {
					// service-defaults
					_, _, err := consulClient.ConfigEntries().Get(api.ServiceDefaults, "defaults", nil)
					require.Error(r, err)
					require.Contains(r, err.Error(), "404 (Config entry not found")

					// service-resolver
					_, _, err = consulClient.ConfigEntries().Get(api.ServiceResolver, "resolver", nil)
					require.Error(r, err)
					require.Contains(r, err.Error(), "404 (Config entry not found")

					// proxy-defaults
					_, _, err = consulClient.ConfigEntries().Get(api.ProxyDefaults, "global", nil)

					// service-router
					_, _, err = consulClient.ConfigEntries().Get(api.ServiceRouter, "router", nil)
					require.Error(r, err)
					require.Contains(r, err.Error(), "404 (Config entry not found")
				})
			}
		})
	}
}
