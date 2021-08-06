// +build enterprise

package consulsidecar

import (
	"os"
	"testing"
	"time"

	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/require"
)

// Test that we register the services with namespaces.
func TestRun_ServicesRegistration_Namespaces(t *testing.T) {
	t.Parallel()
	tmpDir, configFile := createServicesTmpFile(t, servicesRegistrationWithNamespaces)
	defer os.RemoveAll(tmpDir)

	a, err := testutil.NewTestServerConfigT(t, nil)
	require.NoError(t, err)
	defer a.Stop()

	ui := cli.NewMockUi()
	cmd := Command{
		UI: ui,
	}

	// Run async because we need to kill it when the test is over.
	exitChan := runCommandAsynchronously(&cmd, []string{
		"-http-addr", a.HTTPAddr,
		"-service-config", configFile,
		"-sync-period", "100ms",
	})
	defer stopCommand(t, &cmd, exitChan)

	client, err := api.NewClient(&api.Config{
		Address: a.HTTPAddr,
	})
	require.NoError(t, err)

	// create necessary namespaces first
	_, _, err = client.Namespaces().Create(&api.Namespace{Name: "namespace"}, nil)
	require.NoError(t, err)

	timer := &retry.Timer{Timeout: 1 * time.Second, Wait: 100 * time.Millisecond}
	retry.RunWith(timer, t, func(r *retry.R) {
		svc, _, err := client.Agent().Service("service-id", &api.QueryOptions{Namespace: "namespace"})
		require.NoError(r, err)
		require.Equal(r, 80, svc.Port)
		require.Equal(r, "namespace", svc.Namespace)

		svcProxy, _, err := client.Agent().Service("service-id-sidecar-proxy", &api.QueryOptions{Namespace: "namespace"})
		require.NoError(r, err)
		require.Equal(r, 2000, svcProxy.Port)
		require.Equal(r, svcProxy.Namespace, "namespace")
		require.Len(r, svcProxy.Proxy.Upstreams, 1)
		require.Equal(r, svcProxy.Proxy.Upstreams[0].DestinationNamespace, "dest-namespace")
	})
}

const servicesRegistrationWithNamespaces = `
services {
	id   = "service-id"
	name = "service"
	port = 80
	namespace = "namespace"
}
services {
	id   = "service-id-sidecar-proxy"
	name = "service-sidecar-proxy"
	namespace = "namespace"
	port = 2000
	kind = "connect-proxy"
	proxy {
		destination_service_name = "service"
		destination_service_id = "service-id"
		local_service_port = 80
		upstreams {
			destination_type = "service"
			destination_name = "dest-name"
			destination_namespace = "dest-namespace"
			local_bind_port = 1234
		}
	}
}`
