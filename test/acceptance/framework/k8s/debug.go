package k8s

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/gruntwork-io/terratest/modules/k8s"
	terratestLogger "github.com/gruntwork-io/terratest/modules/logger"
	"github.com/hashicorp/consul-helm/test/acceptance/framework/helpers"
	"github.com/hashicorp/consul-helm/test/acceptance/framework/logger"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// WritePodsDebugInfoIfFailed calls kubectl describe and kubectl logs --all-containers
// on pods filtered by the labelSelector and writes it to the debugDirectory.
func WritePodsDebugInfoIfFailed(t *testing.T, kubectlOptions *k8s.KubectlOptions, debugDirectory, labelSelector string) {
	t.Helper()

	if t.Failed() {
		// Create k8s client from kubectl options.
		client := helpers.KubernetesClientFromOptions(t, kubectlOptions)

		contextName := helpers.KubernetesContextFromOptions(t, kubectlOptions)

		// Create a directory for the test.
		testDebugDirectory := filepath.Join(debugDirectory, t.Name(), contextName)
		require.NoError(t, os.MkdirAll(testDebugDirectory, 0755))

		logger.Logf(t, "dumping logs, pod info, and envoy config for %s to %s", labelSelector, testDebugDirectory)

		// Describe and get logs for any pods.
		pods, err := client.CoreV1().Pods(kubectlOptions.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: labelSelector})
		require.NoError(t, err)

		for _, pod := range pods.Items {
			// Get logs for each pod, passing the discard logger to make sure secrets aren't printed to test logs.
			logs, err := RunKubectlAndGetOutputWithLoggerE(t, kubectlOptions, terratestLogger.Discard, "logs", "--all-containers=true", pod.Name)
			if err != nil {
				logs = fmt.Sprintf("Error getting logs: %s: %s", err, logs)
			}

			// Write logs or err to file name <pod.Name>.log
			logFilename := filepath.Join(testDebugDirectory, fmt.Sprintf("%s.log", pod.Name))
			require.NoError(t, ioutil.WriteFile(logFilename, []byte(logs), 0600))

			// Describe pod and write it to a file.
			writeResourceInfoToFile(t, pod.Name, "pod", testDebugDirectory, kubectlOptions)
		}

		// Get envoy configuration from the mesh gateways, if there are any.
		meshGatewayPods, err := client.CoreV1().Pods(kubectlOptions.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: "component=mesh-gateway"})
		require.NoError(t, err)

		for _, mpod := range meshGatewayPods.Items {
			// Get configdump from mesh gateway, passing the discard logger since we only need these logs written to the file (below).
			configDump, err := RunKubectlAndGetOutputWithLoggerE(t, kubectlOptions, terratestLogger.Discard, "exec", mpod.Name, "-c", "consul-sidecar", "--", "curl", "-s", "localhost:19000/config_dump?format=json")
			if err != nil {
				configDump = fmt.Sprintf("Error getting config_dump: %s: %s", err, configDump)
			}
			// Get cluster config from mesh gateway, passing the discard logger since we only need these logs written to the file (below).
			clusters, err := RunKubectlAndGetOutputWithLoggerE(t, kubectlOptions, terratestLogger.Discard, "exec", mpod.Name, "-c", "consul-sidecar", "--", "curl", "-s", "localhost:19000/clusters?format=json")
			if err != nil {
				clusters = fmt.Sprintf("Error getting clusters: %s: %s", err, clusters)
			}

			// Write config/clusters or err to file name <pod.Name>-envoy-[configdump/clusters].json
			configDumpFilename := filepath.Join(testDebugDirectory, fmt.Sprintf("%s-envoy-configdump.json", mpod.Name))
			clustersFilename := filepath.Join(testDebugDirectory, fmt.Sprintf("%s-envoy-clusters.json", mpod.Name))
			require.NoError(t, ioutil.WriteFile(configDumpFilename, []byte(configDump), 0600))
			require.NoError(t, ioutil.WriteFile(clustersFilename, []byte(clusters), 0600))

		}

		// Describe any stateful sets.
		statefulSets, err := client.AppsV1().StatefulSets(kubectlOptions.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: labelSelector})
		if err != nil {
			logger.Log(t, "unable to get statefulsets", "err", err)
		} else {
			for _, statefulSet := range statefulSets.Items {
				// Describe stateful set and write it to a file.
				writeResourceInfoToFile(t, statefulSet.Name, "statefulset", testDebugDirectory, kubectlOptions)
			}
		}

		// Describe any daemonsets.
		daemonsets, err := client.AppsV1().DaemonSets(kubectlOptions.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: labelSelector})
		if err != nil {
			logger.Log(t, "unable to get daemonsets", "err", err)
		} else {
			for _, daemonSet := range daemonsets.Items {
				// Describe daemon set and write it to a file.
				writeResourceInfoToFile(t, daemonSet.Name, "daemonset", testDebugDirectory, kubectlOptions)
			}
		}

		// Describe any deployments.
		deployments, err := client.AppsV1().Deployments(kubectlOptions.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: labelSelector})
		if err != nil {
			logger.Log(t, "unable to get deployments", "err", err)
		} else {
			for _, deployment := range deployments.Items {
				// Describe deployment and write it to a file.
				writeResourceInfoToFile(t, deployment.Name, "deployment", testDebugDirectory, kubectlOptions)
			}
		}
	}
}

// writeResourceInfoToFile takes a Kubernetes resource name, resource type (e.g. pod, deployment, statefulset etc),
// runs 'kubectl describe' with that resource name and type and writes the output of it to a file or errors.
// Note that the resource type has to be compatible with the one you could use with a kubectl describe command,
// e.g. 'daemonset' so that this function can run 'kubectl describe daemonset foo'.
func writeResourceInfoToFile(t *testing.T, resourceName, resourceType, testDebugDirectory string, kubectlOptions *k8s.KubectlOptions) {
	// Describe resource
	desc, err := RunKubectlAndGetOutputWithLoggerE(t, kubectlOptions, terratestLogger.Discard, "describe", resourceType, resourceName)

	// Write resource info or error to file name <resource.Name>-resourceType.txt
	if err != nil {
		desc = fmt.Sprintf("Error describing %s/%s: %s: %s", resourceType, resourceType, err, desc)
	}
	descFilename := filepath.Join(testDebugDirectory, fmt.Sprintf("%s-%s.txt", resourceName, resourceType))
	require.NoError(t, ioutil.WriteFile(descFilename, []byte(desc), 0600))
}
