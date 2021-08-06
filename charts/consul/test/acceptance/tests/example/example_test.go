// Rename package to your test package.
package example

import (
	"context"
	"testing"

	"github.com/hashicorp/consul-helm/test/acceptance/framework/consul"
	"github.com/hashicorp/consul-helm/test/acceptance/framework/helpers"
	"github.com/hashicorp/consul-helm/test/acceptance/framework/k8s"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestExample(t *testing.T) {
	// Get test configuration.
	cfg := suite.Config()

	// Get the default context.
	ctx := suite.Environment().DefaultContext(t)

	// Create Helm values for the Helm install.
	helmValues := map[string]string{
		"exampleFeature.enabled": "true",
	}

	// Generate a random name for this test.
	releaseName := helpers.RandomName()

	// Create a new Consul cluster object.
	consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)

	// Create the Consul cluster with Helm.
	consulCluster.Create(t)

	// Make test assertions.

	// To run kubectl commands, you need to get KubectlOptions from the test context.
	// There are a number of kubectl commands available in the helpers/kubectl.go file.
	// For example, to call 'kubectl apply' from the test write the following:
	k8s.KubectlApply(t, ctx.KubectlOptions(t), "path/to/config")

	// Clean up any Kubernetes resources you have created
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
		k8s.KubectlDelete(t, ctx.KubectlOptions(t), "path/to/config")
	})

	// Similarly, you can obtain Kubernetes client from your test context.
	// You can use it to, for example, read all services in a namespace:
	k8sClient := ctx.KubernetesClient(t)
	services, err := k8sClient.CoreV1().Services(ctx.KubectlOptions(t).Namespace).List(context.Background(), metav1.ListOptions{})
	require.NoError(t, err)
	require.NotNil(t, services.Items)

	// To make Consul API calls, you can get the Consul client from the consulCluster object,
	// indicating whether the client needs to be secure or not (i.e. whether TLS and ACLs are enabled on the Consul cluster):
	consulClient := consulCluster.SetupConsulClient(t, true)
	consulServices, _, err := consulClient.Catalog().Services(nil)
	require.NoError(t, err)
	require.NotNil(t, consulServices)
}
