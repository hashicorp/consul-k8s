package connectinject

import (
	"testing"

	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"
)

func TestRun_FlagValidation(t *testing.T) {
	mandatoryResourceFlags := []string{
		"-init-copy-container-memory-limit=125M",
		"-init-copy-container-memory-request=25M",
		"-init-copy-container-cpu-limit=50m",
		"-init-copy-container-cpu-request=50m",
		"-lifecycle-sidecar-memory-limit=25Mi",
		"-lifecycle-sidecar-memory-request=25Mi",
		"-lifecycle-sidecar-cpu-limit=20m",
		"-lifecycle-sidecar-cpu-request=20m",
	}

	cases := []struct {
		name   string
		flags  []string
		expErr string
	}{
		{
			//flags:  []string{} + mandatoryResourceFlags,
			flags:  mandatoryResourceFlags,
			expErr: "-consul-k8s-image must be set",
		},
		{
			flags:  append([]string{"-consul-k8s-image", "foo", "-ca-file", "bar"},  mandatoryResourceFlags...),
			expErr: "Error reading Consul's CA cert file \"bar\"",
		},
		{
			flags:  append([]string{"-consul-k8s-image", "foo", "-default-sidecar-proxy-cpu-limit=unparseable"}, mandatoryResourceFlags...),
			expErr: "-default-sidecar-proxy-cpu-limit is invalid",
		},
		{
			flags:  append([]string{"-consul-k8s-image", "foo", "-default-sidecar-proxy-cpu-request=unparseable"}, mandatoryResourceFlags...),
			expErr: "-default-sidecar-proxy-cpu-request is invalid",
		},
		{
			flags:  append([]string{"-consul-k8s-image", "foo", "-default-sidecar-proxy-memory-limit=unparseable"}, mandatoryResourceFlags...),
			expErr: "-default-sidecar-proxy-memory-limit is invalid",
		},
		{
			flags:  append([]string{"-consul-k8s-image", "foo", "-default-sidecar-proxy-memory-request=unparseable"}, mandatoryResourceFlags...),
			expErr: "-default-sidecar-proxy-memory-request is invalid",
		},
		{
			flags: append([]string{"-consul-k8s-image", "foo",
				"-default-sidecar-proxy-memory-request=50Mi",
				"-default-sidecar-proxy-memory-limit=25Mi",
			}, mandatoryResourceFlags...),
			expErr: "request must be <= limit: -default-sidecar-proxy-memory-request value of \"50Mi\" is greater than the -default-sidecar-proxy-memory-limit value of \"25Mi\"",
		},
		{
			flags: append([]string{"-consul-k8s-image", "foo",
				"-default-sidecar-proxy-cpu-request=50m",
				"-default-sidecar-proxy-cpu-limit=25m",
			}, mandatoryResourceFlags...),
			expErr: "request must be <= limit: -default-sidecar-proxy-cpu-request value of \"50m\" is greater than the -default-sidecar-proxy-cpu-limit value of \"25m\"",
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
