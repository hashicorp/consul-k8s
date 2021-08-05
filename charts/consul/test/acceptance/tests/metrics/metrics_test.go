package metrics

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/consul-helm/test/acceptance/framework/consul"
	"github.com/hashicorp/consul-helm/test/acceptance/framework/environment"
	"github.com/hashicorp/consul-helm/test/acceptance/framework/helpers"
	"github.com/hashicorp/consul-helm/test/acceptance/framework/k8s"
	"github.com/hashicorp/consul-helm/test/acceptance/framework/logger"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const staticClientName = "static-client"

// Test that prometheus metrics, when enabled, are accessible from the
// endpoints that have been exposed on the server, client and gateways.
func TestComponentMetrics(t *testing.T) {
	env := suite.Environment()
	cfg := suite.Config()
	ctx := env.DefaultContext(t)
	ns := ctx.KubectlOptions(t).Namespace

	helmValues := map[string]string{
		"global.datacenter":                 "dc1",
		"global.metrics.enabled":            "true",
		"global.metrics.enableAgentMetrics": "true",

		"connectInject.enabled": "true",
		"controller.enabled":    "true",

		"meshGateway.enabled":      "true",
		"meshGateway.replicas":     "1",
		"meshGateway.service.type": "ClusterIP",

		"ingressGateways.enabled":              "true",
		"ingressGateways.gateways[0].name":     "ingress-gateway",
		"ingressGateways.gateways[0].replicas": "1",

		"terminatingGateways.enabled":              "true",
		"terminatingGateways.gateways[0].name":     "terminating-gateway",
		"terminatingGateways.gateways[0].replicas": "1",

		// Reduce CPU resource requests because tests were running into CPU scheduling
		// limits and because we're not really testing performance.
		"controller.resources.requests.cpu":                   "50m",
		"ingressGateways.defaults.resources.requests.cpu":     "50m",
		"terminatingGateways.defaults.resources.requests.cpu": "50m",
		"meshGateway.resources.requests.cpu":                  "50m",
	}

	releaseName := helpers.RandomName()

	// Install the consul cluster in the default kubernetes ctx.
	consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)
	consulCluster.Create(t)

	// Create the static-client deployment so we can use it for in-cluster calls to metrics endpoints.
	// This simulates queries that would be made by a prometheus server that runs externally to the consul
	// components in the cluster.
	logger.Log(t, "creating static-client")
	k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/bases/static-client")

	// Server Metrics
	metricsOutput, err := k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "exec", "deploy/"+staticClientName, "--", "curl", "--silent", "--show-error", fmt.Sprintf("http://%s:8500/v1/agent/metrics?format=prometheus", fmt.Sprintf("%s-consul-server.%s.svc", releaseName, ns)))
	require.NoError(t, err)
	require.Contains(t, metricsOutput, `consul_acl_ResolveToken{quantile="0.5"}`)

	// Client Metrics
	metricsOutput, err = k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "exec", "deploy/"+staticClientName, "--", "sh", "-c", "curl --silent --show-error http://$HOST_IP:8500/v1/agent/metrics?format=prometheus")
	require.NoError(t, err)
	require.Contains(t, metricsOutput, `consul_acl_ResolveToken{quantile="0.5"}`)

	// Ingress Gateway Metrics
	assertGatewayMetricsEnabled(t, ctx, ns, "ingress-gateway", `envoy_cluster_assignment_stale{local_cluster="ingress-gateway",consul_source_service="ingress-gateway",consul_source_namespace="default",consul_source_datacenter="dc1",envoy_cluster_name="local_agent"} 0`)

	// Terminating Gateway Metrics
	assertGatewayMetricsEnabled(t, ctx, ns, "terminating-gateway", `envoy_cluster_assignment_stale{local_cluster="terminating-gateway",consul_source_service="terminating-gateway",consul_source_namespace="default",consul_source_datacenter="dc1",envoy_cluster_name="local_agent"} 0`)

	// Mesh Gateway Metrics
	assertGatewayMetricsEnabled(t, ctx, ns, "mesh-gateway", `envoy_cluster_assignment_stale{local_cluster="mesh-gateway",consul_source_service="mesh-gateway",consul_source_namespace="default",consul_source_datacenter="dc1",envoy_cluster_name="local_agent"} 0`)
}

// Test that merged service and envoy metrics are accessible from the
// endpoints that have been exposed on the service.
func TestAppMetrics(t *testing.T) {
	env := suite.Environment()
	cfg := suite.Config()
	ctx := env.DefaultContext(t)
	ns := ctx.KubectlOptions(t).Namespace

	helmValues := map[string]string{
		"global.datacenter":      "dc1",
		"global.metrics.enabled": "true",

		"connectInject.enabled":                      "true",
		"connectInject.metrics.defaultEnableMerging": "true",
	}

	releaseName := helpers.RandomName()

	// Install the consul cluster in the default kubernetes ctx.
	consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)
	consulCluster.Create(t)

	// Deploy service that will emit app and envoy metrics at merged metrics endpoint
	logger.Log(t, "creating static-metrics-app")
	k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/bases/static-metrics-app")

	// Create the static-client deployment so we can use it for in-cluster calls to metrics endpoints.
	// This simulates queries that would be made by a prometheus server that runs externally to the consul
	// components in the cluster.
	logger.Log(t, "creating static-client")
	k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/bases/static-client")

	// Merged App Metrics
	podList, err := ctx.KubernetesClient(t).CoreV1().Pods(ns).List(context.Background(), metav1.ListOptions{LabelSelector: "app=static-metrics-app"})
	require.NoError(t, err)
	require.Len(t, podList.Items, 1)
	podIP := podList.Items[0].Status.PodIP
	metricsOutput, err := k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "exec", "deploy/"+staticClientName, "--", "curl", "--silent", "--show-error", fmt.Sprintf("http://%s:20200/metrics", podIP))
	require.NoError(t, err)
	// This assertion represents the metrics from the envoy sidecar.
	require.Contains(t, metricsOutput, `envoy_cluster_assignment_stale{local_cluster="server",consul_source_service="server",consul_source_namespace="default",consul_source_datacenter="dc1",envoy_cluster_name="local_agent"} 0`)
	// This assertion represents the metrics from the application.
	require.Contains(t, metricsOutput, `service_started_total 1`)
}

func assertGatewayMetricsEnabled(t *testing.T, ctx environment.TestContext, ns, label, metricsAssertion string) {
	pods, err := ctx.KubernetesClient(t).CoreV1().Pods(ns).List(context.Background(), metav1.ListOptions{LabelSelector: fmt.Sprintf("component=%s", label)})
	require.NoError(t, err)
	for _, pod := range pods.Items {
		podIP := pod.Status.PodIP
		metricsOutput, err := k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "exec", "deploy/"+staticClientName, "--", "curl", "--silent", "--show-error", fmt.Sprintf("http://%s:20200/metrics", podIP))
		require.NoError(t, err)
		require.Contains(t, metricsOutput, metricsAssertion)
	}
}
