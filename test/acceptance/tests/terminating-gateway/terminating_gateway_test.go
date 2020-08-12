package connect

import (
	"testing"

	"github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/hashicorp/consul-helm/test/acceptance/framework"
	"github.com/hashicorp/consul-helm/test/acceptance/helpers"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Test that terminating gateways work in a default installation.
func TestTerminatingGateway(t *testing.T) {
	env := suite.Environment()
	helmValues := map[string]string{
		"connectInject.enabled":                    "true",
		"terminatingGateways.enabled":              "true",
		"terminatingGateways.gateways[0].name":     "terminating-gateway",
		"terminatingGateways.gateways[0].replicas": "1",
	}

	t.Log("creating consul cluster")
	releaseName := helpers.RandomName()
	consulCluster := framework.NewHelmCluster(t, helmValues, env.DefaultContext(t), suite.Config(), releaseName)
	consulCluster.Create(t)

	// Once the cluster is up register the external service, then create the config entry.
	consulClient := consulCluster.SetupConsulClient(t, false)

	// Register the external service
	t.Log("registering the external service")
	_, err := consulClient.Catalog().Register(&api.CatalogRegistration{
		Node: "legacy_node",
		//ID:       "example-http",
		Address:  "example.com",
		NodeMeta: map[string]string{"external-node": "true", "external-probe": "true"},
		Service: &api.AgentService{
			ID:      "example-http",
			Service: "example-http",
			Port:    80,
		},
	}, &api.WriteOptions{})
	require.NoError(t, err)

	// Create the config entry for the terminating gateway
	t.Log("creating config entry")
	created, _, err := consulClient.ConfigEntries().Set(&api.TerminatingGatewayConfigEntry{
		Kind:     api.TerminatingGateway,
		Name:     "terminating-gateway",
		Services: []api.LinkedService{{Name: "example-http"}},
	}, nil)
	require.NoError(t, err)
	require.True(t, created, "config entry failed")

	k8sClient := env.DefaultContext(t).KubernetesClient(t)
	k8sOptions := env.DefaultContext(t).KubectlOptions()

	// Deploy the static client
	t.Log("deploying static client")
	deployStaticClient(t, suite.Config(), env.DefaultContext(t).KubectlOptions())

	// Test that we can make a call to the terminating gateway
	t.Log("trying calls to terminating gateway")
	checkConnection(t, k8sOptions, k8sClient)
}

// checkConnection checks if static-client can connect to the external service through the terminating gateway.
func checkConnection(t *testing.T, options *k8s.KubectlOptions, client kubernetes.Interface) {
	pods, err := client.CoreV1().Pods(options.Namespace).List(metav1.ListOptions{LabelSelector: "app=static-client"})
	require.NoError(t, err)
	require.Len(t, pods.Items, 1)
	retry.Run(t, func(r *retry.R) {
		output, err := helpers.RunKubectlAndGetOutputE(t, options, "exec", pods.Items[0].Name, "--",
			"curl", "-vvvs", "-H", "Host: example.com", "http://localhost:1234/")
		require.NoError(r, err)
		require.Contains(r, output, "Example Domain")
	})
}

func deployStaticClient(t *testing.T, cfg *framework.TestConfig, options *k8s.KubectlOptions) {
	helpers.KubectlApply(t, options, "fixtures/static-client.yaml")

	helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
		helpers.KubectlDelete(t, options, "fixtures/static-client.yaml")
	})
	helpers.RunKubectl(t, options, "wait", "--for=condition=available", "deploy/static-client")
}
