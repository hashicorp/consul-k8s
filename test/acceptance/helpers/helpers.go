package helpers

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/gruntwork-io/terratest/modules/random"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes"
)

// RandomName generates a random string with a 'test-' prefix.
func RandomName() string {
	return fmt.Sprintf("test-%s", strings.ToLower(random.UniqueId()))
}

// WaitForAllPodsToBeReady waits until all pods with the provided podLabelSelector
// are in the ready status. It checks every 5 seconds for a total of 20 tries.
// If there is at least one container in a pod that isn't ready after that,
// it fails the test.
func WaitForAllPodsToBeReady(t *testing.T, client kubernetes.Interface, namespace, podLabelSelector string) {
	t.Helper()

	t.Log("Waiting for pods to be ready.")

	// Wait up to 3m.
	counter := &retry.Counter{Count: 36, Wait: 5 * time.Second}
	retry.RunWith(counter, t, func(r *retry.R) {
		pods, err := client.CoreV1().Pods(namespace).List(metav1.ListOptions{LabelSelector: podLabelSelector})
		require.NoError(r, err)
		var numNotReadyContainers int
		var totalNumContainers int
		for _, pod := range pods.Items {
			if len(pod.Status.ContainerStatuses) == 0 {
				r.Errorf("pod %s is pending", pod.Name)
			}
			for _, contStatus := range pod.Status.InitContainerStatuses {
				totalNumContainers++
				if !contStatus.Ready {
					numNotReadyContainers++
				}
			}
			for _, contStatus := range pod.Status.ContainerStatuses {
				totalNumContainers++
				if !contStatus.Ready {
					numNotReadyContainers++
				}
			}
		}
		if numNotReadyContainers != 0 {
			r.Errorf("%d out of %d containers are ready", totalNumContainers-numNotReadyContainers, totalNumContainers)
		}
	})
}

// Deploy creates a Kubernetes deployment by applying configuration stored at filepath,
// sets up a cleanup function and waits for the deployment to become available.
func Deploy(t *testing.T, options *k8s.KubectlOptions, noCleanupOnFailure bool, filepath string) {
	t.Helper()

	KubectlApply(t, options, filepath)

	file, err := os.Open(filepath)
	require.NoError(t, err)

	deployment := v1.Deployment{}
	err = yaml.NewYAMLOrJSONDecoder(file, 1024).Decode(&deployment)
	require.NoError(t, err)

	Cleanup(t, noCleanupOnFailure, func() {
		// Note: this delete command won't wait for pods to be fully terminated.
		// This shouldn't cause any test pollution because the underlying
		// objects are deployments, and so when other tests create these
		// they should have different pod names.
		KubectlDelete(t, options, filepath)
	})

	RunKubectl(t, options, "wait", "--for=condition=available", fmt.Sprintf("deploy/%s", deployment.Name))
}

// CheckStaticServerConnection execs into a pod of the deployment given by deploymentName
// and runs a curl command with the provided curlArgs.
// This function assumes that the connection is made to the static-server and expects the output
// to be "hello world" in a case of success.
// If expectSuccess is true, it will expect connection to succeed,
// otherwise it will expect failure due to intentions.
func CheckStaticServerConnection(t *testing.T, options *k8s.KubectlOptions, expectSuccess bool, deploymentName string, curlArgs ...string) {
	t.Helper()

	retrier := &retry.Timer{Timeout: 20 * time.Second, Wait: 500 * time.Millisecond}

	args := []string{"exec", "deploy/" + deploymentName, "-c", deploymentName, "--", "curl", "-vvvsSf"}
	args = append(args, curlArgs...)

	retry.RunWith(retrier, t, func(r *retry.R) {
		output, err := RunKubectlAndGetOutputE(t, options, args...)
		if expectSuccess {
			require.NoError(r, err)
			require.Contains(r, output, "hello world")
		} else {
			require.Error(r, err)
			require.Contains(r, output, "curl: (52) Empty reply from server")
		}
	})
}

// Sets up a goroutine that will wait for interrupt signals
// and call cleanup function when it catches it.
func SetupInterruptHandler(cleanup func()) {
	c := make(chan os.Signal)
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
	// Always clean up when an interrupt signal is caught.
	SetupInterruptHandler(cleanup)

	// If noCleanupOnFailure is set, don't clean up resources if tests fail.
	// We need to wrap the cleanup function because t that is passed in to this function
	// might not have the information on whether the test has failed yet.
	wrappedCleanupFunc := func() {
		if !(noCleanupOnFailure && t.Failed()) {
			t.Logf("cleaning up resources for %s", t.Name())
			cleanup()
		} else {
			t.Log("skipping resource cleanup")
		}
	}

	t.Cleanup(wrappedCleanupFunc)
}
