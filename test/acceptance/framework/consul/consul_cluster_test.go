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
	helmValues := map[string]string{
		"global.image":           "test-image",
		"feature.enabled":        "true",
		"server.bootstrapExpect": "3",
		"server.replicas":        "3",
	}
	cluster := NewHelmCluster(t, helmValues, &ctx{}, &config.TestConfig{ConsulImage: "test-config-image"}, "test")

	require.Equal(t, cluster.(*HelmCluster).helmOptions.SetValues, helmValues)
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
