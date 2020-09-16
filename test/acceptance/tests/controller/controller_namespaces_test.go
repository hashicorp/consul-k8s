package controller

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/consul-helm/test/acceptance/framework"
	"github.com/hashicorp/consul-helm/test/acceptance/helpers"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
)

const (
	KubeNS       = "ns1"
	ConsulDestNS = "from-k8s"
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
			consulCluster := framework.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)

			consulCluster.Create(t)

			t.Logf("creating namespace %q", KubeNS)
			out, err := helpers.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(), "create", "ns", KubeNS)
			if err != nil && !strings.Contains(out, "(AlreadyExists)") {
				require.NoError(t, err)
			}
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
				helpers.RunKubectl(t, ctx.KubectlOptions(), "delete", "ns", KubeNS)
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
			consulClient := consulCluster.SetupConsulClient(t, c.secure)

			// Test creation.
			{
				t.Log("creating service-defaults CRD")
				retry.Run(t, func(r *retry.R) {
					// Retry the kubectl apply because we've seen sporadic
					// "connection refused" errors where the mutating webhook
					// endpoint fails initially.
					out, err := helpers.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(), "apply", "-n", KubeNS, "-f", "../fixtures/crds")
					require.NoError(r, err, out)
					// NOTE: No need to clean up because the namespace will be deleted.
				})

				// On startup, the controller can take upwards of 6s to perform
				// leader election so we may need to wait a long time for
				// the reconcile loop to run (hence the 20s timeout here).
				counter := &retry.Counter{Count: 20, Wait: 1 * time.Second}
				retry.RunWith(counter, t, func(r *retry.R) {
					entry, _, err := consulClient.ConfigEntries().Get(api.ServiceDefaults, "foo", queryOpts)
					require.NoError(r, err, "ns: %s", queryOpts.Namespace)

					svcDefaultEntry, ok := entry.(*api.ServiceConfigEntry)
					require.True(r, ok, "could not cast to ServiceConfigEntry")
					require.Equal(r, "http", svcDefaultEntry.Protocol)
				})
			}

			// Test an update.
			{
				t.Log("patching service-defaults CRD")
				helpers.RunKubectl(t, ctx.KubectlOptions(), "patch", "-n", KubeNS, "servicedefaults", "foo", "-p", `{"spec":{"protocol":"tcp"}}`, "--type=merge")

				counter := &retry.Counter{Count: 10, Wait: 500 * time.Millisecond}
				retry.RunWith(counter, t, func(r *retry.R) {
					entry, _, err := consulClient.ConfigEntries().Get(api.ServiceDefaults, "foo", queryOpts)
					require.NoError(r, err, "ns: %s", queryOpts.Namespace)

					svcDefaultEntry, ok := entry.(*api.ServiceConfigEntry)
					require.True(r, ok, "could not cast to ServiceConfigEntry")
					require.Equal(r, "tcp", svcDefaultEntry.Protocol)
				})
			}

			// Test a delete.
			{
				t.Log("deleting service-defaults CRD")
				helpers.RunKubectl(t, ctx.KubectlOptions(), "delete", "-n", KubeNS, "servicedefaults", "foo")

				counter := &retry.Counter{Count: 10, Wait: 500 * time.Millisecond}
				retry.RunWith(counter, t, func(r *retry.R) {
					_, _, err := consulClient.ConfigEntries().Get(api.ServiceDefaults, "foo", queryOpts)
					require.Error(r, err)
					require.Contains(r, err.Error(), "404 (Config entry not found")
				})
			}
		})
	}
}
