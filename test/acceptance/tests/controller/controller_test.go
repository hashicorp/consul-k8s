package controller

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/hashicorp/consul-helm/test/acceptance/framework/consul"
	"github.com/hashicorp/consul-helm/test/acceptance/framework/helpers"
	"github.com/hashicorp/consul-helm/test/acceptance/framework/k8s"
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

	// The name of a service intention in consul is
	// the name of the destination service and is not
	// the same as the kube name of the resource.
	const IntentionName = "svc1"

	for _, c := range cases {
		name := fmt.Sprintf("secure: %t; auto-encrypt: %t", c.secure, c.autoEncrypt)
		t.Run(name, func(t *testing.T) {
			ctx := suite.Environment().DefaultContext(t)

			helmValues := map[string]string{
				"controller.enabled":    "true",
				"connectInject.enabled": "true",
				// todo: remove when 1.9.0 is released.
				"global.image": "hashicorpdev/consul",

				"global.tls.enabled":           strconv.FormatBool(c.secure),
				"global.tls.enableAutoEncrypt": strconv.FormatBool(c.autoEncrypt),
				"global.acls.manageSystemACLs": strconv.FormatBool(c.secure),
			}

			releaseName := helpers.RandomName()
			consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)

			consulCluster.Create(t)
			consulClient := consulCluster.SetupConsulClient(t, c.secure)

			// Test creation.
			{
				t.Log("creating custom resources")
				retry.Run(t, func(r *retry.R) {
					// Retry the kubectl apply because we've seen sporadic
					// "connection refused" errors where the mutating webhook
					// endpoint fails initially.
					out, err := k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "apply", "-f", "../fixtures/crds")
					require.NoError(r, err, out)
					helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
						// Ignore errors here because if the test ran as expected
						// the custom resources will have been deleted.
						k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "delete", "-f", "../fixtures/crds")
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

					// service-splitter
					entry, _, err = consulClient.ConfigEntries().Get(api.ServiceSplitter, "splitter", nil)
					require.NoError(r, err)
					svcSplitterEntry, ok := entry.(*api.ServiceSplitterConfigEntry)
					require.True(r, ok, "could not cast to ServiceSplitterConfigEntry")
					require.Equal(r, float32(100), svcSplitterEntry.Splits[0].Weight)

					// service-intentions
					entry, _, err = consulClient.ConfigEntries().Get(api.ServiceIntentions, IntentionName, nil)
					require.NoError(r, err)
					svcIntentionsEntry, ok := entry.(*api.ServiceIntentionsConfigEntry)
					require.True(r, ok, "could not cast to ServiceIntentionsConfigEntry")
					require.Equal(r, api.IntentionActionAllow, svcIntentionsEntry.Sources[0].Action)
					require.Equal(r, api.IntentionActionAllow, svcIntentionsEntry.Sources[1].Permissions[0].Action)
				})
			}

			// Test updates.
			{
				t.Log("patching service-defaults custom resource")
				patchProtocol := "tcp"
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "patch", "servicedefaults", "defaults", "-p", fmt.Sprintf(`{"spec":{"protocol":"%s"}}`, patchProtocol), "--type=merge")

				t.Log("patching service-resolver custom resource")
				patchRedirectSvc := "baz"
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "patch", "serviceresolver", "resolver", "-p", fmt.Sprintf(`{"spec":{"redirect":{"service": "%s"}}}`, patchRedirectSvc), "--type=merge")

				t.Log("patching proxy-defaults custom resource")
				patchMeshGatewayMode := "remote"
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "patch", "proxydefaults", "global", "-p", fmt.Sprintf(`{"spec":{"meshGateway":{"mode": "%s"}}}`, patchMeshGatewayMode), "--type=merge")

				t.Log("patching service-router custom resource")
				patchPathPrefix := "/baz"
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "patch", "servicerouter", "router", "-p", fmt.Sprintf(`{"spec":{"routes":[{"match":{"http":{"pathPrefix":"%s"}}}]}}`, patchPathPrefix), "--type=merge")

				t.Log("patching service-splitter custom resource")
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "patch", "servicesplitter", "splitter", "-p", `{"spec": {"splits": [{"weight": 50}, {"weight": 50, "service": "other-splitter"}]}}`, "--type=merge")

				t.Log("patching service-intentions custom resource")
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "patch", "serviceintentions", "intentions", "-p", `{"spec": {"sources": [{"name": "svc2", "action": "deny"}, {"name": "svc3", "permissions": [{"action": "deny", "http": {"pathExact": "/foo", "methods": ["GET", "PUT"]}}]}]}}`, "--type=merge")

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

					// service-splitter
					entry, _, err = consulClient.ConfigEntries().Get(api.ServiceSplitter, "splitter", nil)
					require.NoError(r, err)
					svcSplitter, ok := entry.(*api.ServiceSplitterConfigEntry)
					require.True(r, ok, "could not cast to ServiceSplitterConfigEntry")
					require.Equal(r, float32(50), svcSplitter.Splits[0].Weight)
					require.Equal(r, float32(50), svcSplitter.Splits[1].Weight)
					require.Equal(r, "other-splitter", svcSplitter.Splits[1].Service)

					// service-intentions
					entry, _, err = consulClient.ConfigEntries().Get(api.ServiceIntentions, IntentionName, nil)
					require.NoError(r, err)
					svcIntentions, ok := entry.(*api.ServiceIntentionsConfigEntry)
					require.True(r, ok, "could not cast to ServiceIntentionsConfigEntry")
					require.Equal(r, api.IntentionActionDeny, svcIntentions.Sources[0].Action)
					require.Equal(r, api.IntentionActionDeny, svcIntentions.Sources[1].Permissions[0].Action)
				})
			}

			// Test a delete.
			{
				t.Log("deleting service-defaults custom resource")
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "delete", "servicedefaults", "defaults")

				t.Log("deleting service-resolver custom resource")
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "delete", "serviceresolver", "resolver")

				t.Log("deleting proxy-defaults custom resource")
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "delete", "proxydefaults", "global")

				t.Log("deleting service-router custom resource")
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "delete", "servicerouter", "router")

				t.Log("deleting service-splitter custom resource")
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "delete", "servicesplitter", "splitter")

				t.Log("deleting service-intentions custom resource")
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "delete", "serviceintentions", "intentions")

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
					require.Error(r, err)
					require.Contains(r, err.Error(), "404 (Config entry not found")

					// service-router
					_, _, err = consulClient.ConfigEntries().Get(api.ServiceRouter, "router", nil)
					require.Error(r, err)
					require.Contains(r, err.Error(), "404 (Config entry not found")

					// service-splitter
					_, _, err = consulClient.ConfigEntries().Get(api.ServiceSplitter, "splitter", nil)
					require.Error(r, err)
					require.Contains(r, err.Error(), "404 (Config entry not found")

					// service-intentions
					_, _, err = consulClient.ConfigEntries().Get(api.ServiceIntentions, IntentionName, nil)
					require.Error(r, err)
					require.Contains(r, err.Error(), "404 (Config entry not found")
				})
			}
		})
	}
}
