// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package k8s

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/gruntwork-io/terratest/modules/k8s"
	terratestLogger "github.com/gruntwork-io/terratest/modules/logger"
	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul-k8s/acceptance/framework/portforward"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"                                        // NEW import
	gatewayclientset "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned" // NEW import
)

// WritePodsDebugInfoIfFailed calls kubectl describe and kubectl logs --all-containers
// on pods filtered by the labelSelector and writes it to the debugDirectory.
func WritePodsDebugInfoIfFailed(t *testing.T, kubectlOptions *k8s.KubectlOptions, debugDirectory, labelSelector string) {
	t.Helper()

	if t.Failed() {
		// Create k8s client from kubectl options.
		client := environment.KubernetesClientFromOptions(t, kubectlOptions)

		// NEW: Create a separate clientset for the Gateway API resources.
		config, err := clientcmd.BuildConfigFromFlags("", kubectlOptions.ConfigPath)
		require.NoError(t, err)
		gatewayClient, err := gatewayclientset.NewForConfig(config)
		require.NoError(t, err)

		contextName := environment.KubernetesContextFromOptions(t, kubectlOptions)

		// Create a directory for the test, first remove special characters from test name
		reg, err := regexp.Compile("[^A-Za-z0-9/_-]+")
		if err != nil {
			logger.Log(t, "unable to generate regex for test name special character replacement", "err", err)
		}
		tn := reg.ReplaceAllString(t.Name(), "_")

		testDebugDirectory := filepath.Join(debugDirectory, tn, contextName)
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
			require.NoError(t, os.WriteFile(logFilename, []byte(logs), 0600))

			if pod.Status.ContainerStatuses != nil {
				for _, status := range pod.Status.InitContainerStatuses {
					// If this init container restarted, get logs from previous instance.
					if status.RestartCount > 0 {
						prevInitLogs, err := RunKubectlAndGetOutputWithLoggerE(t, kubectlOptions, terratestLogger.Discard, "logs", "-c", status.Name, "--previous", pod.Name)
						if err != nil {
							prevInitLogs = fmt.Sprintf("Error getting logs: %s: %s", err, prevInitLogs)
						}

						// Write previous init container logs or err to file name <pod.Name>-<container.Name>-init-previous.log
						prevInitLogFilename := filepath.Join(testDebugDirectory, fmt.Sprintf("%s-%s-init-previous.log", pod.Name, status.Name))
						require.NoError(t, os.WriteFile(prevInitLogFilename, []byte(prevInitLogs), 0600))
					}
				}
			}

			// Describe pod and write it to a file.
			writeResourceInfoToFile(t, pod.Name, "pod", testDebugDirectory, kubectlOptions)

			// Check if the pod is connect-injected, and if so, dump envoy config information.
			_, isServiceMeshPod := pod.Annotations[constants.KeyInjectStatus]
			_, isGatewayPod := pod.Annotations[constants.AnnotationGatewayKind]
			if isServiceMeshPod || isGatewayPod {
				localPort := portforward.CreateTunnelToResourcePort(t, pod.Name, 19000, kubectlOptions, terratestLogger.Discard)

				configDumpResp, err := http.DefaultClient.Get(fmt.Sprintf("http://%s/config_dump?format=json", localPort))
				var configDump string
				if err != nil {
					configDump = fmt.Sprintf("Error getting config_dump: %s: %s", err, configDump)
				} else {
					configDumpRespBytes, err := io.ReadAll(configDumpResp.Body)
					require.NoError(t, err)
					configDump = string(configDumpRespBytes)
				}

				clustersResp, err := http.DefaultClient.Get(fmt.Sprintf("http://%s/clusters?format=json", localPort))
				var clusters string
				if err != nil {
					clusters = fmt.Sprintf("Error getting clusters: %s: %s", err, clusters)
				} else {
					clustersRespBytes, err := io.ReadAll(clustersResp.Body)
					require.NoError(t, err)
					clusters = string(clustersRespBytes)
				}

				// NEW: Add a call to the Consul agent's catalog services endpoint.
				// This is very useful for debugging service discovery and peering issues.
				catalogServicesResp, err := http.DefaultClient.Get(fmt.Sprintf("http://%s/v1/catalog/services", localPort))
				var catalogServices string
				if err != nil {
					catalogServices = fmt.Sprintf("Error getting /v1/catalog/services: %s", err)
				} else {
					catalogServicesRespBytes, err := io.ReadAll(catalogServicesResp.Body)
					require.NoError(t, err)
					catalogServices = string(catalogServicesRespBytes)
				}

				// Write config/clusters or err to file name <pod.Name>-envoy-[configdump/clusters].json
				configDumpFilename := filepath.Join(testDebugDirectory, fmt.Sprintf("%s-envoy-configdump.json", pod.Name))
				clustersFilename := filepath.Join(testDebugDirectory, fmt.Sprintf("%s-envoy-clusters.json", pod.Name))
				catalogServicesFilename := filepath.Join(testDebugDirectory, fmt.Sprintf("%s-consul-catalog-services.json", pod.Name)) // NEW
				require.NoError(t, os.WriteFile(configDumpFilename, []byte(configDump), 0600))
				require.NoError(t, os.WriteFile(clustersFilename, []byte(clusters), 0600))
				require.NoError(t, os.WriteFile(catalogServicesFilename, []byte(catalogServices), 0600)) // NEW
			}
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

		// Describe any replicasets.
		replicasets, err := client.AppsV1().ReplicaSets(kubectlOptions.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: labelSelector})
		if err != nil {
			logger.Log(t, "unable to get replicasets", "err", err)
		} else {
			for _, replicaset := range replicasets.Items {
				// Describe replicaset and write it to a file.
				writeResourceInfoToFile(t, replicaset.Name, "replicaset", testDebugDirectory, kubectlOptions)
			}
		}

		// Describe any services.
		services, err := client.CoreV1().Services(kubectlOptions.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: labelSelector})
		if err != nil {
			logger.Log(t, "unable to get services", "err", err)
		} else {
			for _, service := range services.Items {
				// Describe service and write it to a file.
				writeResourceInfoToFile(t, service.Name, "service", testDebugDirectory, kubectlOptions)
			}
		}

		// Describe any persistent volume claims in the namespace.
		// for consul server/storage debugging
		// This is useful for debugging storage issues, as the describe output includes events.
		pvcs, err := client.CoreV1().PersistentVolumeClaims(kubectlOptions.Namespace).List(context.Background(), metav1.ListOptions{})
		if err != nil {
			logger.Log(t, "unable to get persistentvolumeclaims", "err", err)
		} else {
			for _, pvc := range pvcs.Items {
				// Describe pvc and write it to a file.
				writeResourceInfoToFile(t, pvc.Name, "pvc", testDebugDirectory, kubectlOptions)
			}
		}

		// Describe any endpoints.
		endpoints, err := client.CoreV1().Endpoints(kubectlOptions.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: labelSelector})
		if err != nil {
			logger.Log(t, "unable to get endpoints", "err", err)
		} else {
			for _, endpoint := range endpoints.Items {
				// Describe endpoint and write it to a file.
				writeResourceInfoToFile(t, endpoint.Name, "endpoints", testDebugDirectory, kubectlOptions)
			}
		}

		// Get YAML spec for any endpoints.
		endpointsList, err := client.CoreV1().Endpoints(kubectlOptions.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: labelSelector})
		if err != nil {
			logger.Log(t, "unable to get endpoints", "err", err)
		} else {
			for _, endpoint := range endpointsList.Items {
				endpointYAML, err := k8s.RunKubectlAndGetOutputE(t, kubectlOptions, "get", "endpoints", endpoint.Name, "-o", "yaml")
				if err != nil {
					endpointYAML = fmt.Sprintf("Error getting endpoints YAML: %s: %s", err, endpointYAML)
				}

				// Write endpoints YAML or err to file name <endpoint.Name>-endpoints.yaml
				endpointYAMLFilename := filepath.Join(testDebugDirectory, fmt.Sprintf("%s-endpoints.yaml", endpoint.Name))
				require.NoError(t, os.WriteFile(endpointYAMLFilename, []byte(endpointYAML), 0600))
			}
		}

		// can we add here logs and resource info for apiGatway too?
		// NEW: Add specific debugging for API Gateway resources.
		logger.Log(t, "dumping API Gateway resource info")
		gwcList, err := gatewayClient.GatewayV1beta1().GatewayClasses().List(context.Background(), metav1.ListOptions{})
		if err != nil {
			logger.Log(t, "unable to list GatewayClasses", "err", err)
		} else {
			for _, gwc := range gwcList.Items {
				writeResourceInfoToFile(t, gwc.Name, "gatewayclass", testDebugDirectory, kubectlOptions)
				writeResourceYAMLToFile(t, gwc.Name, "gatewayclass", testDebugDirectory, kubectlOptions)
			}
		}

		gwList, err := gatewayClient.GatewayV1beta1().Gateways(kubectlOptions.Namespace).List(context.Background(), metav1.ListOptions{})
		if err != nil {
			logger.Log(t, "unable to list Gateways", "err", err)
		} else {
			for _, gw := range gwList.Items {
				writeResourceInfoToFile(t, gw.Name, "gateway", testDebugDirectory, kubectlOptions)
				writeResourceYAMLToFile(t, gw.Name, "gateway", testDebugDirectory, kubectlOptions)
			}
		}

		httpRouteList, err := gatewayClient.GatewayV1beta1().HTTPRoutes(kubectlOptions.Namespace).List(context.Background(), metav1.ListOptions{})
		if err != nil {
			logger.Log(t, "unable to list HTTPRoutes", "err", err)
		} else {
			for _, route := range httpRouteList.Items {
				writeResourceInfoToFile(t, route.Name, "httproute", testDebugDirectory, kubectlOptions)
				writeResourceYAMLToFile(t, route.Name, "httproute", testDebugDirectory, kubectlOptions)
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
	require.NoError(t, os.WriteFile(descFilename, []byte(desc), 0600))
}

// NEW: Add a helper function to get the YAML for a resource.
// writeResourceYAMLToFile takes a Kubernetes resource name and type, runs 'kubectl get -o yaml'
// and writes the output to a file.
func writeResourceYAMLToFile(t *testing.T, resourceName, resourceType, testDebugDirectory string, kubectlOptions *k8s.KubectlOptions) {
	t.Helper()
	yaml, err := k8s.RunKubectlAndGetOutputE(t, kubectlOptions, "get", resourceType, resourceName, "-o", "yaml")
	if err != nil {
		yaml = fmt.Sprintf("Error getting YAML for %s/%s: %s: %s", resourceType, resourceName, err, yaml)
	}
	yamlFilename := filepath.Join(testDebugDirectory, fmt.Sprintf("%s-%s.yaml", resourceName, resourceType))
	require.NoError(t, os.WriteFile(yamlFilename, []byte(yaml), 0600))
}
