package connectinject

import (
	"testing"

	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"
)

func TestRun_FlagValidation(t *testing.T) {
	cases := []struct {
		name   string
		flags  []string
		expErr string
	}{
		{
			flags:  []string{},
			expErr: "-consul-k8s-image must be set",
		},
		{
			flags:  []string{"-consul-k8s-image", "foo", "-ca-file", "bar"},
			expErr: "Error reading Consul's CA cert file \"bar\"",
		},
		{
			flags:  []string{"-consul-k8s-image", "foo", "-default-sidecar-proxy-cpu-limit=unparseable"},
			expErr: "-default-sidecar-proxy-cpu-limit is invalid",
		},
		{
			flags:  []string{"-consul-k8s-image", "foo", "-default-sidecar-proxy-cpu-request=unparseable"},
			expErr: "-default-sidecar-proxy-cpu-request is invalid",
		},
		{
			flags:  []string{"-consul-k8s-image", "foo", "-default-sidecar-proxy-memory-limit=unparseable"},
			expErr: "-default-sidecar-proxy-memory-limit is invalid",
		},
		{
			flags:  []string{"-consul-k8s-image", "foo", "-default-sidecar-proxy-memory-request=unparseable"},
			expErr: "-default-sidecar-proxy-memory-request is invalid",
		},
		{
			flags: []string{"-consul-k8s-image", "foo",
				"-default-sidecar-proxy-memory-request=50Mi",
				"-default-sidecar-proxy-memory-limit=25Mi",
			},
			expErr: "request must be <= limit: -default-sidecar-proxy-memory-request value of \"50Mi\" is greater than the -default-sidecar-proxy-memory-limit value of \"25Mi\"",
		},
		{
			flags: []string{"-consul-k8s-image", "foo",
				"-default-sidecar-proxy-cpu-request=50m",
				"-default-sidecar-proxy-cpu-limit=25m",
			},
			expErr: "request must be <= limit: -default-sidecar-proxy-cpu-request value of \"50m\" is greater than the -default-sidecar-proxy-cpu-limit value of \"25m\"",
		},
		{
			flags:  []string{"-consul-k8s-image", "foo", "-init-container-cpu-limit=unparseable"},
			expErr: "-init-container-cpu-limit 'unparseable' is invalid",
		},
		{
			flags:  []string{"-consul-k8s-image", "foo", "-init-container-cpu-request=unparseable"},
			expErr: "-init-container-cpu-request 'unparseable' is invalid",
		},
		{
			flags:  []string{"-consul-k8s-image", "foo", "-init-container-memory-limit=unparseable"},
			expErr: "-init-container-memory-limit 'unparseable' is invalid",
		},
		{
			flags:  []string{"-consul-k8s-image", "foo", "-init-container-memory-request=unparseable"},
			expErr: "-init-container-memory-request 'unparseable' is invalid",
		},
		{
			flags: []string{"-consul-k8s-image", "foo",
				"-init-container-memory-request=50Mi",
				"-init-container-memory-limit=25Mi",
			},
			expErr: "request must be <= limit: -init-container-memory-request value of \"50Mi\" is greater than the -init-container-memory-limit value of \"25Mi\"",
		},
		{
			flags: []string{"-consul-k8s-image", "foo",
				"-init-container-cpu-request=50m",
				"-init-container-cpu-limit=25m",
			},
			expErr: "request must be <= limit: -init-container-cpu-request value of \"50m\" is greater than the -init-container-cpu-limit value of \"25m\"",
		},
		{
			flags:  []string{"-consul-k8s-image", "foo", "-lifecycle-sidecar-cpu-limit=unparseable"},
			expErr: "-lifecycle-sidecar-cpu-limit 'unparseable' is invalid",
		},
		{
			flags:  []string{"-consul-k8s-image", "foo", "-lifecycle-sidecar-cpu-request=unparseable"},
			expErr: "-lifecycle-sidecar-cpu-request 'unparseable' is invalid",
		},
		{
			flags:  []string{"-consul-k8s-image", "foo", "-lifecycle-sidecar-memory-limit=unparseable"},
			expErr: "-lifecycle-sidecar-memory-limit 'unparseable' is invalid",
		},
		{
			flags:  []string{"-consul-k8s-image", "foo", "-lifecycle-sidecar-memory-request=unparseable"},
			expErr: "-lifecycle-sidecar-memory-request 'unparseable' is invalid",
		},
		{
			flags: []string{"-consul-k8s-image", "foo",
				"-lifecycle-sidecar-memory-request=50Mi",
				"-lifecycle-sidecar-memory-limit=25Mi",
			},
			expErr: "request must be <= limit: -lifecycle-sidecar-memory-request value of \"50Mi\" is greater than the -lifecycle-sidecar-memory-limit value of \"25Mi\"",
		},
		{
			flags: []string{"-consul-k8s-image", "foo",
				"-lifecycle-sidecar-cpu-request=50m",
				"-lifecycle-sidecar-cpu-limit=25m",
			},
			expErr: "request must be <= limit: -lifecycle-sidecar-cpu-request value of \"50m\" is greater than the -lifecycle-sidecar-cpu-limit value of \"25m\"",
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

	// Lifecycle sidecar container defaults
	require.Equal(t, cmd.flagLifecycleSidecarCPURequest, "20m")
	require.Equal(t, cmd.flagLifecycleSidecarCPULimit, "20m")
	require.Equal(t, cmd.flagLifecycleSidecarMemoryRequest, "25Mi")
	require.Equal(t, cmd.flagLifecycleSidecarMemoryLimit, "50Mi")
}
