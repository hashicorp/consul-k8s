package consul

import (
	"testing"

	"github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/hashicorp/consul-helm/test/acceptance/framework/config"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
)

// Test that if TestConfig has values that need to be provided
// to a helm install, it will respect the helmValues over
// the values from TestConfig.
func TestNewHelmCluster(t *testing.T) {
	tests := []struct {
		name       string
		helmValues map[string]string
		want       map[string]string
	}{
		{
			name:       "defaults are used when no helmValues are set",
			helmValues: map[string]string{},
			want: map[string]string{
				"global.image":                                  "test-config-image",
				"server.bootstrapExpect":                        "1",
				"server.replicas":                               "1",
				"connectInject.envoyExtraArgs":                  "--log-level debug",
				"connectInject.logLevel":                        "debug",
				"connectInject.transparentProxy.defaultEnabled": "false",
				"dns.enabled":                                   "false",
			},
		},
		{
			name: "when using helmValues, defaults are overridden",
			helmValues: map[string]string{
				"global.image":                                  "test-image",
				"server.bootstrapExpect":                        "3",
				"server.replicas":                               "3",
				"connectInject.envoyExtraArgs":                  "--foo",
				"connectInject.logLevel":                        "debug",
				"connectInject.transparentProxy.defaultEnabled": "true",
				"dns.enabled":                                   "true",
				"feature.enabled":                               "true",
			},
			want: map[string]string{
				"global.image":                                  "test-image",
				"server.bootstrapExpect":                        "3",
				"server.replicas":                               "3",
				"connectInject.envoyExtraArgs":                  "--foo",
				"connectInject.logLevel":                        "debug",
				"connectInject.transparentProxy.defaultEnabled": "true",
				"dns.enabled":                                   "true",
				"feature.enabled":                               "true",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cluster := NewHelmCluster(t, tt.helmValues, &ctx{}, &config.TestConfig{ConsulImage: "test-config-image"}, "test")
			require.Equal(t, cluster.(*HelmCluster).helmOptions.SetValues, tt.want)
		})
	}
}

type ctx struct{}

func (c *ctx) Name() string {
	return ""
}

func (c *ctx) KubectlOptions(_ *testing.T) *k8s.KubectlOptions {
	return &k8s.KubectlOptions{}
}
func (c *ctx) KubernetesClient(_ *testing.T) kubernetes.Interface {
	return fake.NewSimpleClientset()
}
