package connectinject

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestContainerSidecarCommand(t *testing.T) {
	cases := []struct {
		name                     string
		extraEnvoyOpts           string
		expectedContainerCommand []string
	}{
		{
			name:           "no extra options provided",
			extraEnvoyOpts: "",
			expectedContainerCommand: []string{
				"envoy", "--max-obj-name-len", "256",
				"--config-path", "/consul/connect-inject/envoy-bootstrap.yaml",
			},
		},
		{
			name:           "extra log-level option",
			extraEnvoyOpts: "--log-level debug",
			expectedContainerCommand: []string{
				"envoy", "--max-obj-name-len", "256",
				"--config-path", "/consul/connect-inject/envoy-bootstrap.yaml",
				"--log-level", "debug",
			},
		},
		{
			name:           "extraEnvoyOpts with quotes inside",
			extraEnvoyOpts: "--log-level debug --admin-address-path \"/tmp/consul/foo bar\"",
			expectedContainerCommand: []string{
				"envoy", "--max-obj-name-len", "256",
				"--config-path", "/consul/connect-inject/envoy-bootstrap.yaml",
				"--log-level", "debug",
				"--admin-address-path", "\"/tmp/consul/foo bar\"",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := Handler{
				ImageConsul:    "hashicorp/consul:latest",
				ImageEnvoy:     "hashicorp/consul-k8s:latest",
				ExtraEnvoyOpts: tc.extraEnvoyOpts,
			}

			c, err := h.containerSidecar(nil)
			require.NoError(t, err)
			require.Equal(t, tc.expectedContainerCommand, c.Command)
		})
	}
}
