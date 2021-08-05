package controller

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/consul-helm/test/acceptance/framework/consul"
	"github.com/hashicorp/consul-helm/test/acceptance/framework/helpers"
	"github.com/hashicorp/consul-helm/test/acceptance/framework/k8s"
	"github.com/hashicorp/consul-helm/test/acceptance/framework/logger"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
)

const (
	KubeNS                 = "ns1"
	ConsulDestNS           = "from-k8s"
	DefaultConsulNamespace = "default"

	// The name of a service intention in consul is
	// the name of the destination service and is not
	// the same as the kube name of the resource.
	IntentionName = "svc1"
)

// Test that the controller works with Consul Enterprise namespaces.
// These tests currently only test non-secure and secure without auto-encrypt installations
// because in the case of namespaces there isn't a significant distinction in code between auto-encrypt
// and non-auto-encrypt secure installations, so testing just one is enough.
func TestControllerNamespaces(t *testing.T) {
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
			ConsulDestNS,
			false,
			false,
		},
		{
			"single destination namespace (non-default); secure",
			ConsulDestNS,
			false,
			true,
		},
		{
			"mirror k8s namespaces",
			KubeNS,
			true,
			false,
		},
		{
			"mirror k8s namespaces; secure",
			KubeNS,
			true,
			true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := suite.Environment().DefaultContext(t)

			helmValues := map[string]string{
				"global.enableConsulNamespaces": "true",
				"controller.enabled":            "true",
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

			logger.Logf(t, "creating namespace %q", KubeNS)
			out, err := k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "create", "ns", KubeNS)
			if err != nil && !strings.Contains(out, "(AlreadyExists)") {
				require.NoError(t, err)
			}
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "delete", "ns", KubeNS)
			})

			// Make sure that config entries are created in the correct namespace.
			// If mirroring is enabled, we expect config entries to be created in the
			// Consul namespace with the same name as their source
			// Kubernetes namespace.
			// If a single destination namespace is set, we expect all config entries
			// to be created in that destination Consul namespace.
			queryOpts := &api.QueryOptions{Namespace: KubeNS}
			if !c.mirrorK8S {
				queryOpts = &api.QueryOptions{Namespace: c.destinationNamespace}
			}
			defaultOpts := &api.QueryOptions{
				Namespace: DefaultConsulNamespace,
			}
			consulClient := consulCluster.SetupConsulClient(t, c.secure)

			// Test creation.
			{
				logger.Log(t, "creating custom resources")
				retry.Run(t, func(r *retry.R) {
					// Retry the kubectl apply because we've seen sporadic
					// "connection refused" errors where the mutating webhook
					// endpoint fails initially.
					out, err := k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "apply", "-n", KubeNS, "-f", "../fixtures/crds")
					require.NoError(r, err, out)
					// NOTE: No need to clean up because the namespace will be deleted.
				})

				// On startup, the controller can take upwards of 1m to perform
				// leader election so we may need to wait a long time for
				// the reconcile loop to run (hence the 1m timeout here).
				counter := &retry.Counter{Count: 60, Wait: 1 * time.Second}
				retry.RunWith(counter, t, func(r *retry.R) {
					// service-defaults
					entry, _, err := consulClient.ConfigEntries().Get(api.ServiceDefaults, "defaults", queryOpts)
					require.NoError(r, err)
					svcDefaultEntry, ok := entry.(*api.ServiceConfigEntry)
					require.True(r, ok, "could not cast to ServiceConfigEntry")
					require.Equal(r, "http", svcDefaultEntry.Protocol)

					// service-resolver
					entry, _, err = consulClient.ConfigEntries().Get(api.ServiceResolver, "resolver", queryOpts)
					require.NoError(r, err)
					svcResolverEntry, ok := entry.(*api.ServiceResolverConfigEntry)
					require.True(r, ok, "could not cast to ServiceResolverConfigEntry")
					require.Equal(r, "bar", svcResolverEntry.Redirect.Service)

					// proxy-defaults
					entry, _, err = consulClient.ConfigEntries().Get(api.ProxyDefaults, "global", defaultOpts)
					require.NoError(r, err)
					proxyDefaultEntry, ok := entry.(*api.ProxyConfigEntry)
					require.True(r, ok, "could not cast to ProxyConfigEntry")
					require.Equal(r, api.MeshGatewayModeLocal, proxyDefaultEntry.MeshGateway.Mode)

					// mesh
					entry, _, err = consulClient.ConfigEntries().Get(api.MeshConfig, "mesh", defaultOpts)
					require.NoError(r, err)
					meshEntry, ok := entry.(*api.MeshConfigEntry)
					require.True(r, ok, "could not cast to MeshConfigEntry")
					require.True(r, meshEntry.TransparentProxy.MeshDestinationsOnly)

					// service-router
					entry, _, err = consulClient.ConfigEntries().Get(api.ServiceRouter, "router", queryOpts)
					require.NoError(r, err)
					svcRouterEntry, ok := entry.(*api.ServiceRouterConfigEntry)
					require.True(r, ok, "could not cast to ServiceRouterConfigEntry")
					require.Equal(r, "/foo", svcRouterEntry.Routes[0].Match.HTTP.PathPrefix)

					// service-splitter
					entry, _, err = consulClient.ConfigEntries().Get(api.ServiceSplitter, "splitter", queryOpts)
					require.NoError(r, err)
					svcSplitterEntry, ok := entry.(*api.ServiceSplitterConfigEntry)
					require.True(r, ok, "could not cast to ServiceSplitterConfigEntry")
					require.Equal(r, float32(100), svcSplitterEntry.Splits[0].Weight)

					// service-intentions
					entry, _, err = consulClient.ConfigEntries().Get(api.ServiceIntentions, IntentionName, queryOpts)
					require.NoError(r, err)
					svcIntentions, ok := entry.(*api.ServiceIntentionsConfigEntry)
					require.True(r, ok, "could not cast to ServiceSplitterConfigEntry")
					require.Equal(r, api.IntentionActionAllow, svcIntentions.Sources[0].Action)

					// ingress-gateway
					entry, _, err = consulClient.ConfigEntries().Get(api.IngressGateway, "ingress-gateway", queryOpts)
					require.NoError(r, err)
					ingressGatewayEntry, ok := entry.(*api.IngressGatewayConfigEntry)
					require.True(r, ok, "could not cast to IngressGatewayConfigEntry")
					require.Len(r, ingressGatewayEntry.Listeners, 1)
					require.Equal(r, "tcp", ingressGatewayEntry.Listeners[0].Protocol)
					require.Equal(r, 8080, ingressGatewayEntry.Listeners[0].Port)
					require.Len(r, ingressGatewayEntry.Listeners[0].Services, 1)
					require.Equal(r, "foo", ingressGatewayEntry.Listeners[0].Services[0].Name)

					// terminating-gateway
					entry, _, err = consulClient.ConfigEntries().Get(api.TerminatingGateway, "terminating-gateway", queryOpts)
					require.NoError(r, err)
					terminatingGatewayEntry, ok := entry.(*api.TerminatingGatewayConfigEntry)
					require.True(r, ok, "could not cast to TerminatingGatewayConfigEntry")
					require.Len(r, terminatingGatewayEntry.Services, 1)
					require.Equal(r, "name", terminatingGatewayEntry.Services[0].Name)
					require.Equal(r, "caFile", terminatingGatewayEntry.Services[0].CAFile)
					require.Equal(r, "certFile", terminatingGatewayEntry.Services[0].CertFile)
					require.Equal(r, "keyFile", terminatingGatewayEntry.Services[0].KeyFile)
					require.Equal(r, "sni", terminatingGatewayEntry.Services[0].SNI)
				})
			}

			// Test updates.
			{
				logger.Log(t, "patching service-defaults custom resource")
				patchProtocol := "tcp"
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "patch", "-n", KubeNS, "servicedefaults", "defaults", "-p", fmt.Sprintf(`{"spec":{"protocol":"%s"}}`, patchProtocol), "--type=merge")

				logger.Log(t, "patching service-resolver custom resource")
				patchRedirectSvc := "baz"
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "patch", "-n", KubeNS, "serviceresolver", "resolver", "-p", fmt.Sprintf(`{"spec":{"redirect":{"service": "%s"}}}`, patchRedirectSvc), "--type=merge")

				logger.Log(t, "patching proxy-defaults custom resource")
				patchMeshGatewayMode := "remote"
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "patch", "-n", KubeNS, "proxydefaults", "global", "-p", fmt.Sprintf(`{"spec":{"meshGateway":{"mode": "%s"}}}`, patchMeshGatewayMode), "--type=merge")

				logger.Log(t, "patching mesh custom resource")
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "patch", "-n", KubeNS, "mesh", "mesh", "-p", fmt.Sprintf(`{"spec":{"transparentProxy":{"meshDestinationsOnly": %t}}}`, false), "--type=merge")

				logger.Log(t, "patching service-router custom resource")
				patchPathPrefix := "/baz"
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "patch", "-n", KubeNS, "servicerouter", "router", "-p", fmt.Sprintf(`{"spec":{"routes":[{"match":{"http":{"pathPrefix":"%s"}}}]}}`, patchPathPrefix), "--type=merge")

				logger.Log(t, "patching service-splitter custom resource")
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "patch", "-n", KubeNS, "servicesplitter", "splitter", "-p", `{"spec": {"splits": [{"weight": 50}, {"weight": 50, "service": "other-splitter"}]}}`, "--type=merge")

				logger.Log(t, "patching service-intentions custom resource")
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "patch", "-n", KubeNS, "serviceintentions", "intentions", "-p", `{"spec": {"sources": [{"name": "svc2", "action": "deny"}]}}`, "--type=merge")

				logger.Log(t, "patching ingress-gateway custom resource")
				patchPort := 9090
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "patch", "-n", KubeNS, "ingressgateway", "ingress-gateway", "-p", fmt.Sprintf(`{"spec": {"listeners": [{"port": %d, "protocol": "tcp", "services": [{"name": "foo"}]}]}}`, patchPort), "--type=merge")

				logger.Log(t, "patching terminating-gateway custom resource")
				patchSNI := "patch-sni"
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "patch", "-n", KubeNS, "terminatinggateway", "terminating-gateway", "-p", fmt.Sprintf(`{"spec": {"services": [{"name":"name","caFile":"caFile","certFile":"certFile","keyFile":"keyFile","sni":"%s"}]}}`, patchSNI), "--type=merge")

				counter := &retry.Counter{Count: 10, Wait: 500 * time.Millisecond}
				retry.RunWith(counter, t, func(r *retry.R) {
					// service-defaults
					entry, _, err := consulClient.ConfigEntries().Get(api.ServiceDefaults, "defaults", queryOpts)
					require.NoError(r, err)
					svcDefaultEntry, ok := entry.(*api.ServiceConfigEntry)
					require.True(r, ok, "could not cast to ServiceConfigEntry")
					require.Equal(r, patchProtocol, svcDefaultEntry.Protocol)

					// service-resolver
					entry, _, err = consulClient.ConfigEntries().Get(api.ServiceResolver, "resolver", queryOpts)
					require.NoError(r, err)
					svcResolverEntry, ok := entry.(*api.ServiceResolverConfigEntry)
					require.True(r, ok, "could not cast to ServiceResolverConfigEntry")
					require.Equal(r, patchRedirectSvc, svcResolverEntry.Redirect.Service)

					// proxy-defaults
					entry, _, err = consulClient.ConfigEntries().Get(api.ProxyDefaults, "global", defaultOpts)
					require.NoError(r, err)
					proxyDefaultsEntry, ok := entry.(*api.ProxyConfigEntry)
					require.True(r, ok, "could not cast to ProxyConfigEntry")
					require.Equal(r, api.MeshGatewayModeRemote, proxyDefaultsEntry.MeshGateway.Mode)

					// mesh
					entry, _, err = consulClient.ConfigEntries().Get(api.MeshConfig, "mesh", defaultOpts)
					require.NoError(r, err)
					meshEntry, ok := entry.(*api.MeshConfigEntry)
					require.True(r, ok, "could not cast to MeshConfigEntry")
					require.False(r, meshEntry.TransparentProxy.MeshDestinationsOnly)

					// service-router
					entry, _, err = consulClient.ConfigEntries().Get(api.ServiceRouter, "router", queryOpts)
					require.NoError(r, err)
					svcRouterEntry, ok := entry.(*api.ServiceRouterConfigEntry)
					require.True(r, ok, "could not cast to ServiceRouterConfigEntry")
					require.Equal(r, patchPathPrefix, svcRouterEntry.Routes[0].Match.HTTP.PathPrefix)

					// service-splitter
					entry, _, err = consulClient.ConfigEntries().Get(api.ServiceSplitter, "splitter", queryOpts)
					require.NoError(r, err)
					svcSplitter, ok := entry.(*api.ServiceSplitterConfigEntry)
					require.True(r, ok, "could not cast to ServiceSplitterConfigEntry")
					require.Equal(r, float32(50), svcSplitter.Splits[0].Weight)
					require.Equal(r, float32(50), svcSplitter.Splits[1].Weight)
					require.Equal(r, "other-splitter", svcSplitter.Splits[1].Service)

					// service-intentions
					entry, _, err = consulClient.ConfigEntries().Get(api.ServiceIntentions, IntentionName, queryOpts)
					require.NoError(r, err)
					svcIntentions, ok := entry.(*api.ServiceIntentionsConfigEntry)
					require.True(r, ok, "could not cast to ServiceIntentionsConfigEntry")
					require.Equal(r, api.IntentionActionDeny, svcIntentions.Sources[0].Action)

					// ingress-gateway
					entry, _, err = consulClient.ConfigEntries().Get(api.IngressGateway, "ingress-gateway", queryOpts)
					require.NoError(r, err)
					ingressGatewayEntry, ok := entry.(*api.IngressGatewayConfigEntry)
					require.True(r, ok, "could not cast to IngressGatewayConfigEntry")
					require.Equal(r, patchPort, ingressGatewayEntry.Listeners[0].Port)

					// terminating-gateway
					entry, _, err = consulClient.ConfigEntries().Get(api.TerminatingGateway, "terminating-gateway", queryOpts)
					require.NoError(r, err)
					terminatingGatewayEntry, ok := entry.(*api.TerminatingGatewayConfigEntry)
					require.True(r, ok, "could not cast to TerminatingGatewayConfigEntry")
					require.Equal(r, patchSNI, terminatingGatewayEntry.Services[0].SNI)
				})
			}

			// Test a delete.
			{
				logger.Log(t, "deleting service-defaults custom resource")
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "delete", "-n", KubeNS, "servicedefaults", "defaults")

				logger.Log(t, "deleting service-resolver custom resource")
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "delete", "-n", KubeNS, "serviceresolver", "resolver")

				logger.Log(t, "deleting proxy-defaults custom resource")
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "delete", "-n", KubeNS, "proxydefaults", "global")

				logger.Log(t, "deleting mesh custom resource")
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "delete", "-n", KubeNS, "mesh", "mesh")

				logger.Log(t, "deleting service-router custom resource")
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "delete", "-n", KubeNS, "servicerouter", "router")

				logger.Log(t, "deleting service-splitter custom resource")
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "delete", "-n", KubeNS, "servicesplitter", "splitter")

				logger.Log(t, "deleting service-intentions custom resource")
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "delete", "-n", KubeNS, "serviceintentions", "intentions")

				logger.Log(t, "deleting ingress-gateway custom resource")
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "delete", "-n", KubeNS, "ingressgateway", "ingress-gateway")

				logger.Log(t, "deleting terminating-gateway custom resource")
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "delete", "-n", KubeNS, "terminatinggateway", "terminating-gateway")

				counter := &retry.Counter{Count: 10, Wait: 500 * time.Millisecond}
				retry.RunWith(counter, t, func(r *retry.R) {
					// service-defaults
					_, _, err := consulClient.ConfigEntries().Get(api.ServiceDefaults, "defaults", queryOpts)
					require.Error(r, err)
					require.Contains(r, err.Error(), "404 (Config entry not found")

					// service-resolver
					_, _, err = consulClient.ConfigEntries().Get(api.ServiceResolver, "resolver", queryOpts)
					require.Error(r, err)
					require.Contains(r, err.Error(), "404 (Config entry not found")

					// proxy-defaults
					_, _, err = consulClient.ConfigEntries().Get(api.ProxyDefaults, "global", defaultOpts)
					require.Error(r, err)
					require.Contains(r, err.Error(), "404 (Config entry not found")

					// mesh
					_, _, err = consulClient.ConfigEntries().Get(api.MeshConfig, "mesh", defaultOpts)
					require.Error(r, err)
					require.Contains(r, err.Error(), "404 (Config entry not found")

					// service-router
					_, _, err = consulClient.ConfigEntries().Get(api.ServiceRouter, "router", queryOpts)
					require.Error(r, err)
					require.Contains(r, err.Error(), "404 (Config entry not found")

					// service-splitter
					_, _, err = consulClient.ConfigEntries().Get(api.ServiceSplitter, "splitter", queryOpts)
					require.Error(r, err)
					require.Contains(r, err.Error(), "404 (Config entry not found")

					// service-intentions
					_, _, err = consulClient.ConfigEntries().Get(api.ServiceIntentions, IntentionName, queryOpts)
					require.Error(r, err)
					require.Contains(r, err.Error(), "404 (Config entry not found")

					// ingress-gateway
					_, _, err = consulClient.ConfigEntries().Get(api.IngressGateway, "ingress-gateway", queryOpts)
					require.Error(r, err)
					require.Contains(r, err.Error(), "404 (Config entry not found")

					// terminating-gateway
					_, _, err = consulClient.ConfigEntries().Get(api.IngressGateway, "terminating-gateway", queryOpts)
					require.Error(r, err)
					require.Contains(r, err.Error(), "404 (Config entry not found")
				})
			}
		})
	}
}
