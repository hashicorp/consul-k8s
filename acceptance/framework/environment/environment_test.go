// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package environment

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/config"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func TestKubernetesClientConfigTimeout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kubeconfig")
	cfg := clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{
			"cluster": {Server: "https://example.invalid"},
		},
		Contexts: map[string]*clientcmdapi.Context{
			"context": {Cluster: "cluster"},
		},
		CurrentContext: "context",
	}
	require.NoError(t, clientcmd.WriteToFile(cfg, path))
	options := &k8s.KubectlOptions{ConfigPath: path, ContextName: "context"}
	for _, tc := range []struct {
		name      string
		openShift bool
		want      time.Duration
	}{
		{name: "kind unchanged"},
		{name: "OpenShift bounded request", openShift: true, want: 30 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clientConfig := kubernetesClientConfig(t, options, tc.openShift)
			require.Equal(t, tc.want, clientConfig.Timeout)
			require.Equal(t, "https://example.invalid", clientConfig.Host)
		})
	}
}

func TestOpenShiftContextPropagation(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  *config.TestConfig
		want bool
	}{
		{name: "kind", cfg: &config.TestConfig{UseKind: true}},
		{name: "enable OpenShift", cfg: &config.TestConfig{EnableOpenshift: true}, want: true},
		{name: "use OpenShift", cfg: &config.TestConfig{UseOpenshift: true}, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.cfg.EnableMultiCluster = true
			tc.cfg.KubeEnvs = []config.KubeTestConfig{{KubeNamespace: "consul"}, {KubeNamespace: "consul"}}
			env := NewKubernetesEnvironmentFromConfig(tc.cfg)
			require.Len(t, env.contexts, 2)
			for _, ctx := range env.contexts {
				require.Equal(t, tc.want, ctx.openShift)
			}
		})
	}
}
