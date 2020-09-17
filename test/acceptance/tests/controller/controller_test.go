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
				t.Log("creating CRDs")
				retry.Run(t, func(r *retry.R) {
					// Retry the kubectl apply because we've seen sporadic
					// "connection refused" errors where the mutating webhook
					// endpoint fails initially.
					out, err := helpers.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(), "apply", "-f", "../fixtures/crds")
					require.NoError(r, err, out)
					helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
						// Ignore errors here because if the test ran as expected
						// the custom resource will have been deleted.
						helpers.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(), "delete", "-f", "../fixtures/crds")
					})
				})

				// On startup, the controller can take upwards of 6s to perform
				// leader election so we may need to wait a long time for
				// the reconcile loop to run (hence the 20s timeout here).
				counter := &retry.Counter{Count: 20, Wait: 1 * time.Second}
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
				})
			}

			// Test updates.
			{
				t.Log("patching service-defaults CRD")
				patchProtocol := "tcp"
				helpers.RunKubectl(t, ctx.KubectlOptions(), "patch", "servicedefaults", "defaults", "-p", fmt.Sprintf(`{"spec":{"protocol":"%s"}}`, patchProtocol), "--type=merge")

				t.Log("patching service-resolver CRD")
				patchRedirectSvc := "baz"
				helpers.RunKubectl(t, ctx.KubectlOptions(), "patch", "serviceresolver", "resolver", "-p", fmt.Sprintf(`{"spec":{"redirect":{"service": "%s"}}}`, patchRedirectSvc), "--type=merge")

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
				})
			}

			// Test a delete.
			{
				t.Log("deleting service-defaults CRD")
				helpers.RunKubectl(t, ctx.KubectlOptions(), "delete", "servicedefaults", "defaults")

				t.Log("deleting service-resolver CRD")
				helpers.RunKubectl(t, ctx.KubectlOptions(), "delete", "serviceresolver", "resolver")

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
				})
			}
		})
	}
}
