package connectinject

import (
	"os"
	"testing"

	"github.com/hashicorp/consul/api"
	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"
)

func TestRun_FlagValidation(t *testing.T) {
	cases := []struct {
		flags  []string
		expErr string
	}{
		{
			flags:  []string{},
			expErr: "-consul-k8s-image must be set",
		},
		{
			flags:  []string{"-consul-k8s-image", "foo"},
			expErr: "-consul-image must be set",
		},
		{
			flags:  []string{"-consul-k8s-image", "foo", "-consul-image", "foo"},
			expErr: "-envoy-image must be set",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-envoy-image", "envoy:1.16.0",
				"-log-level", "invalid"},
			expErr: "unknown log level \"invalid\": unrecognized level: \"invalid\"",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-envoy-image", "envoy:1.16.0",
				"-enable-central-config", "true"},
			expErr: "-enable-central-config is no longer supported",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-envoy-image", "envoy:1.16.0",
				"-default-protocol", "http"},
			expErr: "-default-protocol is no longer supported",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-envoy-image", "envoy:1.16.0",
				"-ca-file", "bar"},
			expErr: "error reading Consul's CA cert file \"bar\"",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-envoy-image", "envoy:1.16.0",
				"-enable-partitions", "true"},
			expErr: "-partition-name must set if -enable-partitions is set to 'true'",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-envoy-image", "envoy:1.16.0",
				"-partition", "default"},
			expErr: "-enable-partitions must be set to 'true' if -partition-name is set",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-envoy-image", "envoy:1.16.0",
				"-default-sidecar-proxy-cpu-limit=unparseable"},
			expErr: "-default-sidecar-proxy-cpu-limit is invalid",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-envoy-image", "envoy:1.16.0",
				"-default-sidecar-proxy-cpu-request=unparseable"},
			expErr: "-default-sidecar-proxy-cpu-request is invalid",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-envoy-image", "envoy:1.16.0",
				"-default-sidecar-proxy-memory-limit=unparseable"},
			expErr: "-default-sidecar-proxy-memory-limit is invalid",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-envoy-image", "envoy:1.16.0",
				"-default-sidecar-proxy-memory-request=unparseable"},
			expErr: "-default-sidecar-proxy-memory-request is invalid",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-envoy-image", "envoy:1.16.0",
				"-default-sidecar-proxy-memory-request=50Mi",
				"-default-sidecar-proxy-memory-limit=25Mi",
			},
			expErr: "request must be <= limit: -default-sidecar-proxy-memory-request value of \"50Mi\" is greater than the -default-sidecar-proxy-memory-limit value of \"25Mi\"",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-envoy-image", "envoy:1.16.0",
				"-default-sidecar-proxy-cpu-request=50m",
				"-default-sidecar-proxy-cpu-limit=25m",
			},
			expErr: "request must be <= limit: -default-sidecar-proxy-cpu-request value of \"50m\" is greater than the -default-sidecar-proxy-cpu-limit value of \"25m\"",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-envoy-image", "envoy:1.16.0",
				"-init-container-cpu-limit=unparseable"},
			expErr: "-init-container-cpu-limit 'unparseable' is invalid",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-envoy-image", "envoy:1.16.0",
				"-init-container-cpu-request=unparseable"},
			expErr: "-init-container-cpu-request 'unparseable' is invalid",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-envoy-image", "envoy:1.16.0",
				"-init-container-memory-limit=unparseable"},
			expErr: "-init-container-memory-limit 'unparseable' is invalid",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-envoy-image", "envoy:1.16.0",
				"-init-container-memory-request=unparseable"},
			expErr: "-init-container-memory-request 'unparseable' is invalid",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-envoy-image", "envoy:1.16.0",
				"-init-container-memory-request=50Mi",
				"-init-container-memory-limit=25Mi",
			},
			expErr: "request must be <= limit: -init-container-memory-request value of \"50Mi\" is greater than the -init-container-memory-limit value of \"25Mi\"",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-envoy-image", "envoy:1.16.0",
				"-init-container-cpu-request=50m",
				"-init-container-cpu-limit=25m",
			},
			expErr: "request must be <= limit: -init-container-cpu-request value of \"50m\" is greater than the -init-container-cpu-limit value of \"25m\"",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-envoy-image", "envoy:1.16.0",
				"-consul-sidecar-cpu-limit=unparseable"},
			expErr: "-consul-sidecar-cpu-limit 'unparseable' is invalid",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-envoy-image", "envoy:1.16.0",
				"-consul-sidecar-cpu-request=unparseable"},
			expErr: "-consul-sidecar-cpu-request 'unparseable' is invalid",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-envoy-image", "envoy:1.16.0",
				"-consul-sidecar-memory-limit=unparseable"},
			expErr: "-consul-sidecar-memory-limit 'unparseable' is invalid",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-envoy-image", "envoy:1.16.0",
				"-consul-sidecar-memory-request=unparseable"},
			expErr: "-consul-sidecar-memory-request 'unparseable' is invalid",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-envoy-image", "envoy:1.16.0",
				"-consul-sidecar-memory-request=50Mi",
				"-consul-sidecar-memory-limit=25Mi",
			},
			expErr: "request must be <= limit: -consul-sidecar-memory-request value of \"50Mi\" is greater than the -consul-sidecar-memory-limit value of \"25Mi\"",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-envoy-image", "envoy:1.16.0",
				"-consul-sidecar-cpu-request=50m",
				"-consul-sidecar-cpu-limit=25m",
			},
			expErr: "request must be <= limit: -consul-sidecar-cpu-request value of \"50m\" is greater than the -consul-sidecar-cpu-limit value of \"25m\"",
		},
		{
			flags: []string{"-consul-k8s-image", "hashicorp/consul-k8s", "-consul-image", "foo", "-envoy-image", "envoy:1.16.0",
				"-http-addr=http://0.0.0.0:9999",
				"-listen", "999999"},
			expErr: "missing port in address: 999999",
		},
		{
			flags: []string{"-consul-k8s-image", "hashicorp/consul-k8s", "-consul-image", "foo", "-envoy-image", "envoy:1.16.0",
				"-http-addr=http://0.0.0.0:9999",
				"-listen", ":foobar"},
			expErr: "unable to parse port string: strconv.Atoi: parsing \"foobar\": invalid syntax",
		},
	}

	for _, c := range cases {
		t.Run(c.expErr, func(t *testing.T) {
			k8sClient := fake.NewSimpleClientset()
			ui := cli.NewMockUi()
			cmd := Command{
				UI:        ui,
				clientset: k8sClient,
			}
			code := cmd.Run(c.flags)
			require.Equal(t, 1, code)
			require.Contains(t, ui.ErrorWriter.String(), c.expErr)
		})
	}
}

func TestRun_ResourceLimitDefaults(t *testing.T) {
	cmd := Command{}
	cmd.init()

	// Init container defaults
	require.Equal(t, cmd.flagInitContainerCPURequest, "50m")
	require.Equal(t, cmd.flagInitContainerCPULimit, "50m")
	require.Equal(t, cmd.flagInitContainerMemoryRequest, "25Mi")
	require.Equal(t, cmd.flagInitContainerMemoryLimit, "150Mi")

	// Consul sidecar container defaults
	require.Equal(t, cmd.flagConsulSidecarCPURequest, "20m")
	require.Equal(t, cmd.flagConsulSidecarCPULimit, "20m")
	require.Equal(t, cmd.flagConsulSidecarMemoryRequest, "25Mi")
	require.Equal(t, cmd.flagConsulSidecarMemoryLimit, "50Mi")
}

func TestRun_ValidationConsulHTTPAddr(t *testing.T) {
	k8sClient := fake.NewSimpleClientset()
	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		clientset: k8sClient,
	}
	flags := []string{"-consul-k8s-image", "hashicorp/consul-k8s", "-consul-image", "foo", "-envoy-image", "envoy:1.16.0"}

	os.Setenv(api.HTTPAddrEnvName, "%")
	code := cmd.Run(flags)
	os.Unsetenv(api.HTTPAddrEnvName)

	require.Equal(t, 1, code)
	require.Contains(t, ui.ErrorWriter.String(), "error parsing consul address \"http://%\": parse \"http://%\": invalid URL escape \"%")
}
