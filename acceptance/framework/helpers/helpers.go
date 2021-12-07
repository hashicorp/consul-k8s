package helpers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gruntwork-io/terratest/modules/helm"

	terratestk8s "github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/gruntwork-io/terratest/modules/random"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// RandomName generates a random string with a 'test-' prefix.
func RandomName() string {
	return fmt.Sprintf("test-%s", strings.ToLower(random.UniqueId()))
}

// CheckForPriorInstallations checks if there is an existing Helm release
// for this Helm chart already installed. If there is, it fails the tests.
func CheckForPriorInstallations(t *testing.T, client kubernetes.Interface, options *helm.Options, chartName, labelSelector string) {
	t.Helper()

	var helmListOutput string
	// Check if there's an existing cluster and fail if there is one.
	// We may need to retry since this is the first command run once the Kube
	// cluster is created and sometimes the API server returns errors.
	retry.RunWith(&retry.Counter{Wait: 1 * time.Second, Count: 3}, t, func(r *retry.R) {
		var err error
		// NOTE: It's okay to pass in `t` to RunHelmCommandAndGetOutputE despite being in a retry
		// because we're using RunHelmCommandAndGetOutputE (not RunHelmCommandAndGetOutput) so the `t` won't
		// get used to fail the test, just for logging.
		helmListOutput, err = helm.RunHelmCommandAndGetOutputE(t, options, "list", "--output", "json")
		require.NoError(r, err)
	})

	var installedReleases []map[string]string

	err := json.Unmarshal([]byte(helmListOutput), &installedReleases)
	require.NoError(t, err, "unmarshalling %q", helmListOutput)

	for _, r := range installedReleases {
		require.NotContains(t, r["chart"], chartName, fmt.Sprintf("detected an existing installation of %s %s, release name: %s", chartName, r["chart"], r["name"]))
	}

	// Wait for all pods in the "default" namespace to exit. A previous
	// release may not be listed by Helm but its pods may still be terminating.
	retry.RunWith(&retry.Counter{Wait: 1 * time.Second, Count: 60}, t, func(r *retry.R) {
		pods, err := client.CoreV1().Pods(options.KubectlOptions.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: labelSelector})
		require.NoError(r, err)
		if len(pods.Items) > 0 {
			var podNames []string
			for _, p := range pods.Items {
				podNames = append(podNames, p.Name)
			}
			r.Errorf("pods from previous installation still running: %s", strings.Join(podNames, ", "))
		}
	})
}

// WaitForAllPodsToBeReady waits until all pods with the provided podLabelSelector
// are in the ready status. It checks every 5 seconds for a total of 20 tries.
// If there is at least one container in a pod that isn't ready after that,
// it fails the test.
func WaitForAllPodsToBeReady(t *testing.T, client kubernetes.Interface, namespace, podLabelSelector string) {
	t.Helper()

	logger.Logf(t, "Waiting for pods with label %q to be ready.", podLabelSelector)

	// Wait up to 10m.
	// On Azure, volume provisioning can sometimes take close to 5 min,
	// so we need to give a bit more time for pods to become healthy.
	counter := &retry.Counter{Count: 600, Wait: 1 * time.Second}
	retry.RunWith(counter, t, func(r *retry.R) {
		pods, err := client.CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{LabelSelector: podLabelSelector})
		require.NoError(r, err)
		require.NotEmpty(r, pods.Items)

		var notReadyPods []string
		for _, pod := range pods.Items {
			if !IsReady(pod) {
				notReadyPods = append(notReadyPods, pod.Name)
			}
		}
		if len(notReadyPods) > 0 {
			r.Errorf("%d pods are not ready: %s", len(notReadyPods), strings.Join(notReadyPods, ","))
		}
	})
	logger.Log(t, "Finished waiting for pods to be ready.")
}

// SetupInterruptHandler sets up a goroutine that will wait for interrupt signals
// and call cleanup function when it catches it.
func SetupInterruptHandler(cleanup func()) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		fmt.Println("\r- Ctrl+C pressed in Terminal. Cleaning up resources.")
		cleanup()
		os.Exit(1)
	}()
}

// Cleanup will both register a cleanup function with t
// and SetupInterruptHandler to make sure resources get cleaned up
// if an interrupt signal is caught.
func Cleanup(t *testing.T, noCleanupOnFailure bool, cleanup func()) {
	t.Helper()

	// Always clean up when an interrupt signal is caught.
	SetupInterruptHandler(cleanup)

	// If noCleanupOnFailure is set, don't clean up resources if tests fail.
	// We need to wrap the cleanup function because t that is passed in to this function
	// might not have the information on whether the test has failed yet.
	wrappedCleanupFunc := func() {
		if !(noCleanupOnFailure && t.Failed()) {
			logger.Logf(t, "cleaning up resources for %s", t.Name())
			cleanup()
		} else {
			logger.Log(t, "skipping resource cleanup")
		}
	}

	t.Cleanup(wrappedCleanupFunc)
}

// KubernetesClientFromOptions takes KubectlOptions and returns Kubernetes API client.
func KubernetesClientFromOptions(t *testing.T, options *terratestk8s.KubectlOptions) kubernetes.Interface {
	configPath, err := options.GetConfigPath(t)
	require.NoError(t, err)

	config, err := terratestk8s.LoadApiClientConfigE(configPath, options.ContextName)
	require.NoError(t, err)

	client, err := kubernetes.NewForConfig(config)
	require.NoError(t, err)

	return client
}

// KubernetesContextFromOptions returns the Kubernetes context from options.
// If context is explicitly set in options, it returns that context.
// Otherwise, it returns the current context.
func KubernetesContextFromOptions(t *testing.T, options *terratestk8s.KubectlOptions) string {
	t.Helper()

	// First, check if context set in options and return that
	if options.ContextName != "" {
		return options.ContextName
	}

	// Otherwise, get current context from config
	configPath, err := options.GetConfigPath(t)
	require.NoError(t, err)

	rawConfig, err := terratestk8s.LoadConfigFromPath(configPath).RawConfig()
	require.NoError(t, err)

	return rawConfig.CurrentContext
}

// IsReady returns true if pod is ready.
func IsReady(pod corev1.Pod) bool {
	if pod.Status.Phase == corev1.PodPending {
		return false
	}

	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady {
			if cond.Status == corev1.ConditionTrue {
				return true
			} else {
				return false
			}
		}
	}

	return false
}
