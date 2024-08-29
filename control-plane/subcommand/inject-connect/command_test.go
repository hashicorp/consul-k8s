// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package connectinject

import (
	"testing"

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
			expErr: "-consul-dataplane-image must be set",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-consul-dataplane-image", "consul-dataplane:1.14.0",
				"-log-level", "invalid"},
			expErr: "unknown log level \"invalid\": unrecognized level: \"invalid\"",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-consul-dataplane-image", "consul-dataplane:1.14.0",
				"-ca-cert-file", "bar"},
			expErr: "error reading Consul's CA cert file \"bar\"",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-consul-dataplane-image", "consul-dataplane:1.14.0",
				"-enable-partitions", "true"},
			expErr: "-partition must set if -enable-partitions is set to 'true'",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-consul-dataplane-image", "consul-dataplane:1.14.0",
				"-partition", "default"},
			expErr: "-enable-partitions must be set to 'true' if -partition is set",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-consul-dataplane-image", "consul-dataplane:1.14.0",
				"-default-sidecar-proxy-cpu-limit=unparseable"},
			expErr: "-default-sidecar-proxy-cpu-limit is invalid",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-consul-dataplane-image", "consul-dataplane:1.14.0",
				"-default-sidecar-proxy-cpu-request=unparseable"},
			expErr: "-default-sidecar-proxy-cpu-request is invalid",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-consul-dataplane-image", "consul-dataplane:1.14.0",
				"-default-sidecar-proxy-memory-limit=unparseable"},
			expErr: "-default-sidecar-proxy-memory-limit is invalid",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-consul-dataplane-image", "consul-dataplane:1.14.0",
				"-default-sidecar-proxy-memory-request=unparseable"},
			expErr: "-default-sidecar-proxy-memory-request is invalid",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-consul-dataplane-image", "consul-dataplane:1.14.0",
				"-default-sidecar-proxy-memory-request=50Mi",
				"-default-sidecar-proxy-memory-limit=25Mi",
			},
			expErr: "request must be <= limit: -default-sidecar-proxy-memory-request value of \"50Mi\" is greater than the -default-sidecar-proxy-memory-limit value of \"25Mi\"",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-consul-dataplane-image", "consul-dataplane:1.14.0",
				"-default-sidecar-proxy-cpu-request=50m",
				"-default-sidecar-proxy-cpu-limit=25m",
			},
			expErr: "request must be <= limit: -default-sidecar-proxy-cpu-request value of \"50m\" is greater than the -default-sidecar-proxy-cpu-limit value of \"25m\"",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-consul-dataplane-image", "consul-dataplane:1.14.0",
				"-init-container-cpu-limit=unparseable"},
			expErr: "-init-container-cpu-limit 'unparseable' is invalid",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-consul-dataplane-image", "consul-dataplane:1.14.0",
				"-init-container-cpu-request=unparseable"},
			expErr: "-init-container-cpu-request 'unparseable' is invalid",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-consul-dataplane-image", "consul-dataplane:1.14.0",
				"-init-container-memory-limit=unparseable"},
			expErr: "-init-container-memory-limit 'unparseable' is invalid",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-consul-dataplane-image", "consul-dataplane:1.14.0",
				"-init-container-memory-request=unparseable"},
			expErr: "-init-container-memory-request 'unparseable' is invalid",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-consul-dataplane-image", "consul-dataplane:1.14.0",
				"-init-container-memory-request=50Mi",
				"-init-container-memory-limit=25Mi",
			},
			expErr: "request must be <= limit: -init-container-memory-request value of \"50Mi\" is greater than the -init-container-memory-limit value of \"25Mi\"",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-consul-dataplane-image", "consul-dataplane:1.14.0",
				"-init-container-cpu-request=50m",
				"-init-container-cpu-limit=25m",
			},
			expErr: "request must be <= limit: -init-container-cpu-request value of \"50m\" is greater than the -init-container-cpu-limit value of \"25m\"",
		},
		{
			flags: []string{"-consul-k8s-image", "hashicorp/consul-k8s", "-consul-image", "foo", "-consul-dataplane-image", "consul-dataplane:1.14.0",
				"-listen", "999999"},
			expErr: "missing port in address: 999999",
		},
		{
			flags: []string{"-consul-k8s-image", "hashicorp/consul-k8s", "-consul-image", "foo", "-consul-dataplane-image", "consul-dataplane:1.14.0",
				"-listen", ":foobar"},
			expErr: "unable to parse port string: strconv.Atoi: parsing \"foobar\": invalid syntax",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-consul-dataplane-image", "consul-dataplane:1.14.0",
				"-default-envoy-proxy-concurrency=-42",
			},
			expErr: "-default-envoy-proxy-concurrency must be >= 0 if set",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-consul-dataplane-image", "consul-dataplane:1.14.0",
				"-global-image-pull-policy", "garbage",
			},
			expErr: "-global-image-pull-policy must be `IfNotPresent`, `Always`, `Never`, or `` ",
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
}
