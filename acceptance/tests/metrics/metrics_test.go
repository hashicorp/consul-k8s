// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package metrics

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/consul/sdk/testutil/retry"

	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const StaticClientName = "static-client"

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
		// Agents have been removed but there could potentially be customers that are still running them. We
		// are using client.enabled to cover that scenario and to make sure agent metrics still works with
		// consul-dataplane.
		"client.enabled": "true",

		"connectInject.enabled": "true",

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
	k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/bases/static-client")

	// Server Metrics

	retry.RunWith(&retry.Counter{Wait: 5 * time.Second, Count: 150}, t, func(r *retry.R) {
		metricsOutput, err := k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "exec", "deploy/"+StaticClientName, "-c", "static-client", "--", "curl", "--silent", "--show-error", fmt.Sprintf("http://%s:8500/v1/agent/metrics?format=prometheus", fmt.Sprintf("%s-consul-server.%s.svc", releaseName, ns)))
		require.NoError(r, err)
		require.Contains(r, metricsOutput, `consul_acl_ResolveToken{quantile="0.5"}`)
	})
	// Client Metrics
	retry.RunWith(&retry.Counter{Wait: 5 * time.Second, Count: 150}, t, func(r *retry.R) {
		metricsOutput, err := k8s.RunKubectlAndGetOutputE(r, ctx.KubectlOptions(t),
			"exec", "deploy/"+StaticClientName,
			"-c", "static-client",
			"--",
			"sh", "-c",
			`if echo "$HOST_IP" | grep -q ':'; then url="http://[$HOST_IP]:8500"; else url="http://$HOST_IP:8500"; fi; curl --silent --show-error "$url/v1/agent/metrics?format=prometheus"`,
		)
		require.NoError(r, err)
		require.Contains(r, metricsOutput, `consul_acl_ResolveToken{quantile="0.5"}`)
	})

	logger.Log(t, "terminating gateway metrics")
	assertGatewayMetricsEnabled(t, ctx, ns, "terminating-gateway", `envoy_cluster_assignment_stale{local_cluster="terminating-gateway",consul_source_service="terminating-gateway"`)

	logger.Log(t, "mesh gateway metrics")
	assertGatewayMetricsEnabled(t, ctx, ns, "mesh-gateway", `envoy_cluster_assignment_stale{local_cluster="mesh-gateway",consul_source_service="mesh-gateway"`)
}

// Test that merged service and envoy metrics are accessible from the
// endpoints that have been exposed on the service.
func TestAppMetrics(t *testing.T) {
	env := suite.Environment()
	cfg := suite.Config()
	ctx := env.DefaultContext(t)
	ns := ctx.KubectlOptions(t).Namespace

	helmValues := map[string]string{
		"global.datacenter":                          "dc1",
		"global.metrics.enabled":                     "true",
		"connectInject.enabled":                      "true",
		"connectInject.metrics.defaultEnableMerging": "true",
	}

	releaseName := helpers.RandomName()

	// Install the consul cluster in the default kubernetes ctx.
	consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)
	consulCluster.Create(t)

	// Deploy service that will emit app and envoy metrics at merged metrics endpoint
	logger.Log(t, "creating static-metrics-app")
	k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/bases/static-metrics-app")

	// Create the static-client deployment so we can use it for in-cluster calls to metrics endpoints.
	// This simulates queries that would be made by a prometheus server that runs externally to the consul
	// components in the cluster.
	logger.Log(t, "creating static-client")
	k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/bases/static-client")

	// Merged App Metrics
	podList, err := ctx.KubernetesClient(t).CoreV1().Pods(ns).List(context.Background(), metav1.ListOptions{LabelSelector: "app=static-metrics-app"})
	require.NoError(t, err)
	require.Len(t, podList.Items, 1)
	podIP := podList.Items[0].Status.PodIP

	// Retry because sometimes the merged metrics server takes a couple hundred milliseconds
	// to start.

	retry.RunWith(&retry.Counter{Wait: 5 * time.Second, Count: 150}, t, func(r *retry.R) {
		metricsOutput, err := k8s.RunKubectlAndGetOutputE(r, ctx.KubectlOptions(r), "exec", "deploy/"+StaticClientName, "-c", "static-client", "--", "curl", "--silent", "--show-error", fmt.Sprintf("http://%s/metrics", net.JoinHostPort(podIP, "20200")))
		require.NoError(r, err)
		// This assertion represents the metrics from the envoy sidecar.
		require.Contains(r, metricsOutput, `envoy_cluster_assignment_stale{local_cluster="server",consul_source_service="server"`)
		// This assertion represents the metrics from the application.
		require.Contains(r, metricsOutput, `service_started_total 1`)
	})
}

// Test that a single container exposing metrics on more than one port has all
// of those ports scraped and merged, via the service-metrics-endpoints
// annotation.
func TestAppMetricsMultiplePorts(t *testing.T) {
	env := suite.Environment()
	cfg := suite.Config()
	ctx := env.DefaultContext(t)
	ns := ctx.KubectlOptions(t).Namespace

	helmValues := map[string]string{
		"global.datacenter":                          "dc1",
		"global.metrics.enabled":                     "true",
		"connectInject.enabled":                      "true",
		"connectInject.metrics.defaultEnableMerging": "true",
	}

	releaseName := helpers.RandomName()

	// Install the consul cluster in the default kubernetes ctx.
	consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)
	consulCluster.Create(t)

	// Deploy a service whose single container serves metrics on two ports.
	logger.Log(t, "creating static-multiport-metrics-app")
	k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/bases/static-multiport-metrics-app")

	// Create the static-client deployment so we can use it for in-cluster calls to metrics endpoints.
	logger.Log(t, "creating static-client")
	k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/bases/static-client")

	podList, err := ctx.KubernetesClient(t).CoreV1().Pods(ns).List(context.Background(), metav1.ListOptions{LabelSelector: "app=static-multiport-metrics-app"})
	require.NoError(t, err)
	require.Len(t, podList.Items, 1)
	podIP := podList.Items[0].Status.PodIP

	// The injected sidecar should have been given one
	// -telemetry-prom-service-metrics-url flag per configured endpoint. The
	// host is matched loosely because it is 127.0.0.1 or ::1 depending on the
	// cluster's address family.
	serviceMetricsURLs := consulDataplaneServiceMetricsURLs(t, podList.Items[0])
	require.Len(t, serviceMetricsURLs, 2, "expected one flag per configured metrics endpoint, got %v", serviceMetricsURLs)
	require.Contains(t, serviceMetricsURLs[0], ":8080/metrics")
	require.Contains(t, serviceMetricsURLs[1], ":9090/alt-metrics")

	// Retry because sometimes the merged metrics server takes a couple hundred milliseconds
	// to start.
	retry.RunWith(&retry.Counter{Wait: 5 * time.Second, Count: 150}, t, func(r *retry.R) {
		metricsOutput, err := k8s.RunKubectlAndGetOutputE(r, ctx.KubectlOptions(r), "exec", "deploy/"+StaticClientName, "-c", "static-client", "--", "curl", "--silent", "--show-error", fmt.Sprintf("http://%s/metrics", net.JoinHostPort(podIP, "20200")))
		require.NoError(r, err)
		// This assertion represents the metrics from the envoy sidecar.
		require.Contains(r, metricsOutput, `envoy_cluster_assignment_stale{local_cluster="multiport-metrics",consul_source_service="multiport-metrics"`)
		// These assertions represent the metrics from both of the
		// application's ports. The two ports serve disjoint metric families, so
		// seeing both proves that more than one port was scraped and merged.
		require.Contains(r, metricsOutput, `app_http_requests_total{port="8080",code="200"} 1027`)
		require.Contains(r, metricsOutput, `app_build_info{port="8080",version="1.4.2"} 1`)
		require.Contains(r, metricsOutput, `worker_jobs_processed_total{port="9090",queue="default"} 88`)
		require.Contains(r, metricsOutput, `worker_last_success_timestamp_seconds{port="9090"} 1757000000`)
	})
}

// Test that when one of a container's metrics ports is down, the merged metrics
// server still serves envoy metrics and the metrics from the remaining healthy
// port. A failing service-metrics endpoint must not take down the whole merged
// scrape.
//
// Both ports are exercised as the failing one. Killing only the last configured
// endpoint would still pass against a dataplane that honours a single
// -telemetry-prom-service-metrics-url flag and silently drops the rest, so the
// case where the *first* endpoint dies is what actually proves each configured
// endpoint is scraped independently.
func TestAppMetricsMultiplePorts_PortDown(t *testing.T) {
	const (
		metricsA1 = `app_http_requests_total{port="8080",code="200"} 1027`
		metricsA2 = `app_build_info{port="8080",version="1.4.2"} 1`
		metricsB1 = `worker_jobs_processed_total{port="9090",queue="default"} 88`
		metricsB2 = `worker_last_success_timestamp_seconds{port="9090"} 1757000000`
	)

	cases := []struct {
		name string
		// killPattern matches the httpd process serving the downed port.
		killPattern string
		// present must still appear in the merged output, absent must not.
		present []string
		absent  []string
	}{
		{
			name: "second port down",
			// Anchored with ^ so it matches only the httpd child (argv starts
			// with "httpd"), not the parent "/bin/sh -c" whose script text also
			// contains this substring. Killing the shell would take down PID 1
			// and restart the whole pod.
			killPattern: "^httpd -f -p 9090",
			present:     []string{metricsA1, metricsA2},
			absent:      []string{metricsB1, metricsB2},
		},
		{
			name:        "first port down",
			killPattern: "^httpd -f -p 8080",
			present:     []string{metricsB1, metricsB2},
			absent:      []string{metricsA1, metricsA2},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := suite.Environment()
			cfg := suite.Config()
			ctx := env.DefaultContext(t)
			ns := ctx.KubectlOptions(t).Namespace

			helmValues := map[string]string{
				"global.datacenter":                          "dc1",
				"global.metrics.enabled":                     "true",
				"connectInject.enabled":                      "true",
				"connectInject.metrics.defaultEnableMerging": "true",
			}

			releaseName := helpers.RandomName()

			// Install the consul cluster in the default kubernetes ctx.
			consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)
			consulCluster.Create(t)

			// Deploy a service whose single container serves metrics on two ports.
			logger.Log(t, "creating static-multiport-metrics-app")
			k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/bases/static-multiport-metrics-app")

			// Create the static-client deployment so we can use it for in-cluster calls to metrics endpoints.
			logger.Log(t, "creating static-client")
			k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/bases/static-client")

			podList, err := ctx.KubernetesClient(t).CoreV1().Pods(ns).List(context.Background(), metav1.ListOptions{LabelSelector: "app=static-multiport-metrics-app"})
			require.NoError(t, err)
			require.Len(t, podList.Items, 1)
			podIP := podList.Items[0].Status.PodIP

			// Bring down one metrics port by killing the httpd process serving
			// it, leaving the other port and envoy untouched. The container
			// keeps running because the other httpd is still alive.
			logger.Logf(t, "killing the httpd matching %q", c.killPattern)
			k8s.RunKubectl(t, ctx.KubectlOptions(t), "exec", "deploy/static-multiport-metrics-app", "-c", "static-multiport-metrics-app", "--", "pkill", "-f", c.killPattern)

			// Retry because sometimes the merged metrics server takes a couple hundred milliseconds
			// to start.
			retry.RunWith(&retry.Counter{Wait: 5 * time.Second, Count: 150}, t, func(r *retry.R) {
				metricsOutput, err := k8s.RunKubectlAndGetOutputE(r, ctx.KubectlOptions(r), "exec", "deploy/"+StaticClientName, "-c", "static-client", "--", "curl", "--silent", "--show-error", fmt.Sprintf("http://%s/metrics", net.JoinHostPort(podIP, "20200")))
				require.NoError(r, err)
				// Envoy sidecar metrics are still merged in.
				require.Contains(r, metricsOutput, `envoy_cluster_assignment_stale{local_cluster="multiport-metrics",consul_source_service="multiport-metrics"`)
				// The healthy port is still scraped and merged.
				for _, expected := range c.present {
					require.Contains(r, metricsOutput, expected)
				}
				// The downed port contributes no metrics, but its failure does
				// not prevent the rest of the merged output from being served.
				for _, unexpected := range c.absent {
					require.NotContains(r, metricsOutput, unexpected)
				}
			})
		})
	}
}

// consulDataplaneServiceMetricsURLs returns the values of every
// -telemetry-prom-service-metrics-url flag passed to the consul-dataplane
// sidecar injected into the given pod, in the order they appear.
func consulDataplaneServiceMetricsURLs(t *testing.T, pod corev1.Pod) []string {
	t.Helper()

	const flagPrefix = "-telemetry-prom-service-metrics-url="

	var container *corev1.Container
	for i, c := range pod.Spec.Containers {
		if c.Name == "consul-dataplane" {
			container = &pod.Spec.Containers[i]
			break
		}
	}
	require.NotNil(t, container, "no consul-dataplane container found in pod %s", pod.Name)

	var urls []string
	for _, arg := range container.Args {
		if strings.HasPrefix(arg, flagPrefix) {
			urls = append(urls, strings.TrimPrefix(arg, flagPrefix))
		}
	}
	return urls
}

func assertGatewayMetricsEnabled(t *testing.T, ctx environment.TestContext, ns, label, metricsAssertion string) {
	pods, err := ctx.KubernetesClient(t).CoreV1().Pods(ns).List(context.Background(), metav1.ListOptions{LabelSelector: fmt.Sprintf("component=%s", label)})
	require.NoError(t, err)
	for _, pod := range pods.Items {
		podIP := pod.Status.PodIP
		retry.RunWith(&retry.Counter{Wait: 5 * time.Second, Count: 150}, t, func(r *retry.R) {
			metricsOutput, err := k8s.RunKubectlAndGetOutputE(r, ctx.KubectlOptions(r), "exec", "deploy/"+StaticClientName, "-c", "static-client", "--", "curl", "--silent", "--show-error", fmt.Sprintf("http://%s/metrics", net.JoinHostPort(podIP, "20200")))
			require.NoError(r, err)
			require.Contains(r, metricsOutput, metricsAssertion)
		})
	}
}
