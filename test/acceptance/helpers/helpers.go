package helpers

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/gruntwork-io/terratest/modules/logger"
	"github.com/gruntwork-io/terratest/modules/random"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
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

	// Wait up to 7m.
	counter := &retry.Counter{Count: 84, Wait: 5 * time.Second}
	retry.RunWith(counter, t, func(r *retry.R) {
		pods, err := client.CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{LabelSelector: podLabelSelector})
		require.NoError(r, err)

		var notReadyPods []string
		for _, pod := range pods.Items {
			if !isReady(pod) {
				notReadyPods = append(notReadyPods, pod.Name)
			}
		}
		if len(notReadyPods) > 0 {
			r.Errorf("%d pods are not ready: %s", len(notReadyPods), strings.Join(notReadyPods, ","))
		}
	})
}

// Deploy creates a Kubernetes deployment by applying configuration stored at filepath,
// sets up a cleanup function and waits for the deployment to become available.
func Deploy(t *testing.T, options *k8s.KubectlOptions, noCleanupOnFailure bool, debugDirectory string, filepath string) {
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
		WritePodsDebugInfoIfFailed(t, options, debugDirectory, labelMapToString(deployment.GetLabels()))
		KubectlDelete(t, options, filepath)
	})

	RunKubectl(t, options, "wait", "--for=condition=available", fmt.Sprintf("deploy/%s", deployment.Name))
}

// DeployKustomize creates a Kubernetes deployment by applying the kustomize directory stored at kustomizeDir,
// sets up a cleanup function and waits for the deployment to become available.
func DeployKustomize(t *testing.T, options *k8s.KubectlOptions, noCleanupOnFailure bool, debugDirectory string, kustomizeDir string) {
	t.Helper()

	KubectlApplyK(t, options, kustomizeDir)

	output, err := RunKubectlAndGetOutputE(t, options, "kustomize", kustomizeDir)
	require.NoError(t, err)

	deployment := v1.Deployment{}
	err = yaml.NewYAMLOrJSONDecoder(strings.NewReader(output), 1024).Decode(&deployment)
	require.NoError(t, err)

	Cleanup(t, noCleanupOnFailure, func() {
		// Note: this delete command won't wait for pods to be fully terminated.
		// This shouldn't cause any test pollution because the underlying
		// objects are deployments, and so when other tests create these
		// they should have different pod names.
		WritePodsDebugInfoIfFailed(t, options, debugDirectory, labelMapToString(deployment.GetLabels()))
		KubectlDeleteK(t, options, kustomizeDir)
	})

	RunKubectl(t, options, "wait", "--for=condition=available", "--timeout=1m", fmt.Sprintf("deploy/%s", deployment.Name))
}

// CheckStaticServerConnection execs into a pod of the deployment given by deploymentName
// and runs a curl command with the provided curlArgs.
// This function assumes that the connection is made to the static-server and expects the output
// to be "hello world" in a case of success.
// If expectSuccess is true, it will expect connection to succeed,
// otherwise it will expect failure due to intentions.
func CheckStaticServerConnection(
	t *testing.T,
	options *k8s.KubectlOptions,
	expectSuccess bool,
	deploymentName string,
	failureMessage string,
	curlArgs ...string,
) {
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
			require.Contains(r, output, failureMessage)
		}
	})
}

// CheckStaticServerConnectionSuccessful is just like CheckStaticServerConnection
// but it always expects a successful connection.
func CheckStaticServerConnectionSuccessful(t *testing.T, options *k8s.KubectlOptions, deploymentName string, curlArgs ...string) {
	CheckStaticServerConnection(t, options, true, deploymentName, "", curlArgs...)
}

// CheckStaticServerConnectionSuccessful is just like CheckStaticServerConnection
// but it always expects a failing connection with error "Empty reply from server."
func CheckStaticServerConnectionFailing(t *testing.T, options *k8s.KubectlOptions, deploymentName string, curlArgs ...string) {
	CheckStaticServerConnection(t, options, false, deploymentName, "curl: (52) Empty reply from server", curlArgs...)
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

// WritePodsDebugInfoIfFailed calls kubectl describe and kubectl logs --all-containers
// on pods filtered by the labelSelector and writes it to the debugDirectory.
func WritePodsDebugInfoIfFailed(t *testing.T, kubectlOptions *k8s.KubectlOptions, debugDirectory, labelSelector string) {
	t.Helper()

	if t.Failed() {
		// Create k8s client from kubectl options
		client := KubernetesClientFromOptions(t, kubectlOptions)

		contextName := KubernetesContextFromOptions(t, kubectlOptions)

		// Create a directory for the test
		testDebugDirectory := filepath.Join(debugDirectory, t.Name(), contextName)
		require.NoError(t, os.MkdirAll(testDebugDirectory, 0755))

		t.Logf("dumping logs and pod info for %s to %s", labelSelector, testDebugDirectory)
		pods, err := client.CoreV1().Pods(kubectlOptions.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: labelSelector})
		require.NoError(t, err)

		for _, pod := range pods.Items {
			// Get logs for each pod, passing the discard logger to make sure secrets aren't printed to test logs.
			logs, err := RunKubectlAndGetOutputWithLoggerE(t, kubectlOptions, logger.Discard, "logs", "--all-containers=true", pod.Name)

			// Write logs or err to file name <pod.Name>.log
			logFilename := filepath.Join(testDebugDirectory, fmt.Sprintf("%s.log", pod.Name))
			if err != nil {
				logs = fmt.Sprintf("Error getting logs: %s: %s", err, logs)
			}
			require.NoError(t, ioutil.WriteFile(logFilename, []byte(logs), 0600))

			// Describe pod and write it to a file.
			writeResourceInfoToFile(t, pod.Name, "pod", testDebugDirectory, kubectlOptions)
		}

		// Describe any stateful sets.
		statefulSets, err := client.AppsV1().StatefulSets(kubectlOptions.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: labelSelector})
		for _, statefulSet := range statefulSets.Items {
			// Describe stateful set and write it to a file.
			writeResourceInfoToFile(t, statefulSet.Name, "statefulset", testDebugDirectory, kubectlOptions)
		}

		// Describe any daemonsets.
		daemonsets, err := client.AppsV1().DaemonSets(kubectlOptions.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: labelSelector})
		for _, daemonSet := range daemonsets.Items {
			// Describe daemon set and write it to a file.
			writeResourceInfoToFile(t, daemonSet.Name, "daemonset", testDebugDirectory, kubectlOptions)
		}

		// Describe any deployments.
		deployments, err := client.AppsV1().Deployments(kubectlOptions.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: labelSelector})
		for _, deployment := range deployments.Items {
			// Describe deployment and write it to a file.
			writeResourceInfoToFile(t, deployment.Name, "deployment", testDebugDirectory, kubectlOptions)
		}
	}
}

// writeResourceInfoToFile takes a Kubernetes resource name, resource type (e.g. pod, deployment, statefulset etc),
// runs 'kubectl describe' with that resource name and type and writes the output of it to a file or errors.
// Note that the resource type has to be compatible with the one you could use with a kubectl describe command,
// e.g. 'daemonset' so that this function can run 'kubectl describe daemonset foo'.
func writeResourceInfoToFile(t *testing.T, resourceName, resourceType, testDebugDirectory string, kubectlOptions *k8s.KubectlOptions) {
	// Describe resource
	desc, err := RunKubectlAndGetOutputWithLoggerE(t, kubectlOptions, logger.Discard, "describe", resourceType, resourceName)

	// Write resource info or error to file name <resource.Name>-resourceType.txt
	if err != nil {
		desc = fmt.Sprintf("Error describing %s/%s: %s: %s", resourceType, resourceType, err, desc)
	}
	descFilename := filepath.Join(testDebugDirectory, fmt.Sprintf("%s-%s.txt", resourceName, resourceType))
	require.NoError(t, ioutil.WriteFile(descFilename, []byte(desc), 0600))
}

// KubernetesClientFromOptions takes KubectlOptions and returns Kubernetes API client.
func KubernetesClientFromOptions(t *testing.T, options *k8s.KubectlOptions) kubernetes.Interface {
	configPath, err := options.GetConfigPath(t)
	require.NoError(t, err)

	config, err := k8s.LoadApiClientConfigE(configPath, options.ContextName)
	require.NoError(t, err)

	client, err := kubernetes.NewForConfig(config)
	require.NoError(t, err)

	return client
}

// KubernetesContextFromOptions returns the Kubernetes context from options.
// If context is explicitly set in options, it returns that context.
// Otherwise, it returns the current context.
func KubernetesContextFromOptions(t *testing.T, options *k8s.KubectlOptions) string {
	t.Helper()

	// First, check if context set in options and return that
	if options.ContextName != "" {
		return options.ContextName
	}

	// Otherwise, get current context from config
	configPath, err := options.GetConfigPath(t)
	require.NoError(t, err)

	rawConfig, err := k8s.LoadConfigFromPath(configPath).RawConfig()
	require.NoError(t, err)

	return rawConfig.CurrentContext
}

// labelMapToString takes a label map[string]string
// and returns the string-ified version of, e.g app=foo,env=dev.
func labelMapToString(labelMap map[string]string) string {
	var labels []string
	for k, v := range labelMap {
		labels = append(labels, fmt.Sprintf("%s=%s", k, v))
	}

	return strings.Join(labels, ",")
}

// isReady returns true if pod is ready.
func isReady(pod corev1.Pod) bool {
	if len(pod.Status.ContainerStatuses) == 0 {
		return false
	}

	for _, contStatus := range pod.Status.InitContainerStatuses {
		if !contStatus.Ready {
			return false
		}
	}
	for _, contStatus := range pod.Status.ContainerStatuses {
		if !contStatus.Ready {
			return false
		}
	}
	return true
}
