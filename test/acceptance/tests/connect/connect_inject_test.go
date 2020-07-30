package connect

import (
	"testing"
	"time"

	"github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/hashicorp/consul-helm/test/acceptance/framework"
	"github.com/hashicorp/consul-helm/test/acceptance/helpers"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Test that Connect works in a default installation
func TestConnectInjectDefault(t *testing.T) {
	env := suite.Environment()

	helmValues := map[string]string{
		"connectInject.enabled": "true",
	}

	releaseName := helpers.RandomName()
	consulCluster := framework.NewHelmCluster(t, helmValues, env.DefaultContext(t), suite.Config(), releaseName)

	consulCluster.Create(t)

	t.Log("creating static-server and static-client deployments")
	createServerAndClient(t, suite.Config(), env.DefaultContext(t).KubectlOptions())

	t.Log("checking that connection is successful")
	checkConnection(t, env.DefaultContext(t).KubectlOptions(), env.DefaultContext(t).KubernetesClient(t), true)
}

// Test that Connect works in a secure installation,
// with ACLs and TLS enabled
func TestConnectInjectSecure(t *testing.T) {
	env := suite.Environment()

	helmValues := map[string]string{
		"connectInject.enabled":        "true",
		"global.tls.enabled":           "true",
		"global.acls.manageSystemACLs": "true",
	}

	releaseName := helpers.RandomName()
	consulCluster := framework.NewHelmCluster(t, helmValues, env.DefaultContext(t), suite.Config(), releaseName)

	consulCluster.Create(t)

	t.Log("creating static-server and static-client deployments")
	createServerAndClient(t, suite.Config(), env.DefaultContext(t).KubectlOptions())

	t.Log("checking that the connection is not successful because there's no intention")
	checkConnection(t, env.DefaultContext(t).KubectlOptions(), env.DefaultContext(t).KubernetesClient(t), false)

	consulClient := consulCluster.SetupConsulClient(t, true)

	t.Log("creating intention")
	_, _, err := consulClient.Connect().IntentionCreate(&api.Intention{
		SourceName:      "static-client",
		DestinationName: "static-server",
		Action:          api.IntentionActionAllow,
	}, nil)
	require.NoError(t, err)

	t.Log("checking that connection is successful")
	checkConnection(t, env.DefaultContext(t).KubectlOptions(), env.DefaultContext(t).KubernetesClient(t), true)
}

// createServerAndClient sets up static-server and static-client
// deployments that will be talking to each other over Connect.
func createServerAndClient(t *testing.T, cfg *framework.TestConfig, options *k8s.KubectlOptions) {
	helpers.KubectlApply(t, options, "fixtures/static-server.yaml")
	helpers.KubectlApply(t, options, "fixtures/static-client.yaml")

	helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
		// Note: this delete command won't wait for pods to be fully terminated.
		// This shouldn't cause any test pollution because the underlying
		// objects are deployments, and so when other tests create these
		// they should have different pod names.
		helpers.KubectlDelete(t, options, "fixtures/static-server.yaml")
		helpers.KubectlDelete(t, options, "fixtures/static-client.yaml")
	})

	// Wait for both deployments
	helpers.RunKubectl(t, options, "wait", "--for=condition=available", "deploy/static-server")
	helpers.RunKubectl(t, options, "wait", "--for=condition=available", "deploy/static-client")
}

// checkConnection checks if static-client can talk to static-server.
// If expectSuccess is true, it will expect connection to succeed,
// otherwise it will expect failure due to intentions.
func checkConnection(t *testing.T, options *k8s.KubectlOptions, client kubernetes.Interface, expectSuccess bool) {
	pods, err := client.CoreV1().Pods(options.Namespace).List(metav1.ListOptions{LabelSelector: "app=static-client"})
	require.NoError(t, err)
	require.Len(t, pods.Items, 1)

	retrier := &retry.Timer{
		Timeout: 20 * time.Second,
		Wait:    500 * time.Millisecond,
	}
	retry.RunWith(retrier, t, func(r *retry.R) {
		output, err := helpers.RunKubectlAndGetOutputE(t, options, "exec", pods.Items[0].Name, "-c", "static-client", "--", "curl", "-vvvsSf", "http://127.0.0.1:1234/")
		if expectSuccess {
			require.NoError(r, err)
			require.Contains(r, output, "hello world")
		} else {
			require.Error(r, err)
			require.Contains(r, output, "503 Service Unavailable")
		}
	})
}
