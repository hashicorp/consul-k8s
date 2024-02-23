package datadog

import (
	"encoding/json"
	"fmt"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/datadog"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/stretchr/testify/require"
	"testing"
)

const (
	maxDatadogAPIRetryAttempts   = 20
	consulDogstatsDMetricQuery   = "consul.memberlist.gossip.50percentile"
	consulIntegrationMetricQuery = "consul.memberlist.gossip.quantile"
)

// TestDatadogDogstatsDUnixDomainSocket
// Acceptance test to verify e2e metrics configuration works as expected
// with live datadog API using histogram formatted metric
//
// Method: DogstatsD + Unix Domain Socket
func TestDatadogDogstatsDUnixDomainSocket(t *testing.T) {
	env := suite.Environment()
	cfg := suite.Config()
	ctx := env.DefaultContext(t)

	acceptanceTestingTags := "acceptance_test:unix_domain_sockets"
	helmValues := map[string]string{
		"global.datacenter":                                    "dc1",
		"global.metrics.enabled":                               "true",
		"global.metrics.enableAgentMetrics":                    "true",
		"global.metrics.disableAgentHostName":                  "true",
		"global.metrics.enableHostMetrics":                     "true",
		"global.metrics.datadog.enabled":                       "true",
		"global.metrics.datadog.namespace":                     "datadog",
		"global.metrics.datadog.dogstatsd.enabled":             "true",
		"global.metrics.datadog.dogstatsd.socketTransportType": "UDS",
		"global.metrics.datadog.dogstatsd.dogstatsdTags[0]":    "source:consul",
		"global.metrics.datadog.dogstatsd.dogstatsdTags[1]":    "consul_service:consul-server",
		"global.metrics.datadog.dogstatsd.dogstatsdTags[2]":    acceptanceTestingTags,
	}

	datadogOperatorHelmValues := map[string]string{
		"replicaCount":     "1",
		"image.tag":        datadog.DefaultHelmChartVersion,
		"image.repository": "gcr.io/datadoghq/operator",
	}

	releaseName := helpers.RandomName()
	datadogOperatorRelease := datadog.DatadogOperatorReleaseName

	// Install the consul cluster in the default kubernetes ctx.
	consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)
	consulCluster.Create(t)

	// Deploy Datadog Agent via Datadog Operator and apply dogstatsd overlay
	datadogNamespace := helmValues["global.metrics.datadog.namespace"]
	logger.Log(t, fmt.Sprintf("deploying datadog-operator via helm | namespace: %s | release-name: %s", datadogNamespace, datadogOperatorRelease))
	datadogCluster := datadog.NewDatadogCluster(t, ctx, cfg, datadogOperatorRelease, datadogNamespace, datadogOperatorHelmValues)
	datadogCluster.Create(t)

	logger.Log(t, fmt.Sprintf("applying dogstatd over unix domain sockets kustomization patch to datadog-agent | namespace: %s", datadogNamespace))
	k8s.DeployKustomize(t, ctx.KubectlOptionsForNamespace(datadogNamespace), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/datadog-dogstatsd-uds")
	k8s.WaitForAllPodsToBeReady(t, ctx.KubernetesClient(t), datadogNamespace, "agent.datadoghq.com/component=agent")

	datadogAPIClient := datadogCluster.DatadogClient(t)
	response, fullResponse, err := datadog.ApiWithRetry(t, datadogAPIClient, datadog.MetricTimeSeriesQuery, acceptanceTestingTags, consulDogstatsDMetricQuery, maxDatadogAPIRetryAttempts)
	if err != nil {
		content, _ := json.MarshalIndent(response.QueryResponse, "", "   ")
		fullContent, _ := json.MarshalIndent(fullResponse, "", "    ")
		logger.Logf(t, "Error when querying /v1/query endpoint for %s: %v", consulDogstatsDMetricQuery, err)
		logger.Logf(t, "Response: %v", string(content))
		logger.Logf(t, "Full Response: %v", string(fullContent))
	}
	content, _ := json.Marshal(response.QueryResponse)
	require.Contains(t, string(content), consulDogstatsDMetricQuery)
}

// TestDatadogDogstatsDUDP
// Acceptance test to verify e2e metrics configuration works as expected
// with live datadog API using histogram formatted metric
//
// Method: DogstatsD + UDP to Kube Service DNS name on port 8125
func TestDatadogDogstatsDUDP(t *testing.T) {
	env := suite.Environment()
	cfg := suite.Config()
	ctx := env.DefaultContext(t)

	acceptanceTestingTags := "acceptance_test:dogstatsd_udp"
	helmValues := map[string]string{
		"global.datacenter":                                    "dc1",
		"global.metrics.enabled":                               "true",
		"global.metrics.enableAgentMetrics":                    "true",
		"global.metrics.disableAgentHostName":                  "true",
		"global.metrics.enableHostMetrics":                     "true",
		"global.metrics.datadog.enabled":                       "true",
		"global.metrics.datadog.namespace":                     "datadog",
		"global.metrics.datadog.dogstatsd.enabled":             "true",
		"global.metrics.datadog.dogstatsd.socketTransportType": "UDP",
		"global.metrics.datadog.dogstatsd.dogstatsdAddr":       "datadog-agent.datadog.svc.cluster.local",
		"global.metrics.datadog.dogstatsd.dogstatsdTags[0]":    "source:consul",
		"global.metrics.datadog.dogstatsd.dogstatsdTags[1]":    "consul_service:consul-server",
		"global.metrics.datadog.dogstatsd.dogstatsdTags[2]":    acceptanceTestingTags,
	}

	datadogOperatorHelmValues := map[string]string{
		"replicaCount":     "1",
		"image.tag":        datadog.DefaultHelmChartVersion,
		"image.repository": "gcr.io/datadoghq/operator",
	}

	releaseName := helpers.RandomName()
	datadogOperatorRelease := datadog.DatadogOperatorReleaseName

	// Install the consul cluster in the default kubernetes ctx.
	consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)
	consulCluster.Create(t)

	// Deploy Datadog Agent via Datadog Operator and apply dogstatsd overlay
	datadogNamespace := helmValues["global.metrics.datadog.namespace"]
	logger.Log(t, fmt.Sprintf("deploying datadog-operator via helm | namespace: %s | release-name: %s", datadogNamespace, datadogOperatorRelease))
	datadogCluster := datadog.NewDatadogCluster(t, ctx, cfg, datadogOperatorRelease, datadogNamespace, datadogOperatorHelmValues)
	datadogCluster.Create(t)

	logger.Log(t, fmt.Sprintf("applying dogstatd over UDP kustomization patch to datadog-agent | namespace: %s", datadogNamespace))
	k8s.DeployKustomize(t, ctx.KubectlOptionsForNamespace(datadogNamespace), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/datadog-dogstatsd-udp")
	k8s.WaitForAllPodsToBeReady(t, ctx.KubernetesClient(t), datadogNamespace, "agent.datadoghq.com/component=agent")

	datadogAPIClient := datadogCluster.DatadogClient(t)
	response, fullResponse, err := datadog.ApiWithRetry(t, datadogAPIClient, datadog.MetricTimeSeriesQuery, acceptanceTestingTags, consulDogstatsDMetricQuery, maxDatadogAPIRetryAttempts)
	if err != nil {
		content, _ := json.MarshalIndent(response.QueryResponse, "", "   ")
		fullContent, _ := json.MarshalIndent(fullResponse, "", "    ")
		logger.Logf(t, "Error when querying /v1/query endpoint for %s: %v", consulDogstatsDMetricQuery, err)
		logger.Logf(t, "Response: %v", string(content))
		logger.Logf(t, "Full Response: %v", string(fullContent))
	}
	content, _ := json.MarshalIndent(response.QueryResponse, "", "   ")
	logger.Logf(t, "Response: %v", string(content))
	require.Contains(t, string(content), consulDogstatsDMetricQuery)
}

// TestDatadogConsulChecks
// Acceptance test to verify e2e metrics configuration works as expected
// with live datadog API using histogram formatted metric
//
// Method: Consul Integrated Datadog Checks
func TestDatadogConsulChecks(t *testing.T) {
	env := suite.Environment()
	cfg := suite.Config()
	ctx := env.DefaultContext(t)

	helmValues := map[string]string{
		"global.datacenter":                   "dc1",
		"global.metrics.enabled":              "true",
		"global.metrics.enableAgentMetrics":   "true",
		"global.metrics.disableAgentHostName": "true",
		"global.metrics.enableHostMetrics":    "true",
		"server.replicas":                     "3", // Network Latency Checks need > 1 node
		"server.affinity":                     "null",
		"global.metrics.datadog.enabled":      "true",
		"global.metrics.datadog.namespace":    "datadog",
	}

	datadogOperatorHelmValues := map[string]string{
		"replicaCount":     "1",
		"image.tag":        datadog.DefaultHelmChartVersion,
		"image.repository": "gcr.io/datadoghq/operator",
	}

	releaseName := helpers.RandomName()
	datadogOperatorRelease := datadog.DatadogOperatorReleaseName

	acceptanceTestingTags := fmt.Sprintf("kube_stateful_set:%s-consul-server", releaseName)
	// Install the consul cluster in the default kubernetes ctx.
	consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)
	consulCluster.Create(t)

	// Deploy Datadog Agent via Datadog Operator and apply dogstatsd overlay
	datadogNamespace := helmValues["global.metrics.datadog.namespace"]
	logger.Log(t, fmt.Sprintf("deploying datadog-operator via helm | namespace: %s | release-name: %s", datadogNamespace, datadogOperatorRelease))
	datadogCluster := datadog.NewDatadogCluster(t, ctx, cfg, datadogOperatorRelease, datadogNamespace, datadogOperatorHelmValues)
	datadogCluster.Create(t)

	logger.Log(t, fmt.Sprintf("applying datadog consul integration patch to datadog-agent | namespace: %s", datadogNamespace))
	k8s.DeployKustomize(t, ctx.KubectlOptionsForNamespace(datadogNamespace), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/bases/datadog")
	k8s.WaitForAllPodsToBeReady(t, ctx.KubernetesClient(t), datadogNamespace, "agent.datadoghq.com/component=agent")

	datadogAPIClient := datadogCluster.DatadogClient(t)
	response, fullResponse, err := datadog.ApiWithRetry(t, datadogAPIClient, datadog.MetricTimeSeriesQuery, acceptanceTestingTags, consulIntegrationMetricQuery, maxDatadogAPIRetryAttempts)
	if err != nil {
		content, _ := json.MarshalIndent(response.QueryResponse, "", "   ")
		fullContent, _ := json.MarshalIndent(fullResponse, "", "    ")
		logger.Logf(t, "Error when querying /v1/query endpoint for %s: %v", consulIntegrationMetricQuery, err)
		logger.Logf(t, "Response: %v", string(content))
		logger.Logf(t, "Full Response: %v", string(fullContent))
	}
	content, _ := json.MarshalIndent(response.QueryResponse, "", "   ")
	logger.Logf(t, "Response: %v", string(content))
	require.Contains(t, string(content), consulIntegrationMetricQuery)
}

// TestDatadogOpenmetrics
// Acceptance test to verify e2e metrics configuration works as expected
// with live datadog API using histogram formatted metric
//
// Method: Datadog Openmetrics Prometheus Metrics Collection
func TestDatadogOpenmetrics(t *testing.T) {
	env := suite.Environment()
	cfg := suite.Config()
	ctx := env.DefaultContext(t)
	ns := ctx.KubectlOptions(t).Namespace
	consulOpenmetricsQuery := fmt.Sprintf("%s.consul_memberlist_gossip.quantile", ns)

	helmValues := map[string]string{
		"global.datacenter":                                    "dc1",
		"global.metrics.enabled":                               "true",
		"global.metrics.enableAgentMetrics":                    "true",
		"global.metrics.disableAgentHostName":                  "true",
		"global.metrics.enableHostMetrics":                     "true",
		"global.metrics.datadog.enabled":                       "true",
		"global.metrics.datadog.namespace":                     "datadog",
		"global.metrics.datadog.openMetricsPrometheus.enabled": "true",
	}

	datadogOperatorHelmValues := map[string]string{
		"replicaCount":     "1",
		"image.tag":        datadog.DefaultHelmChartVersion,
		"image.repository": "gcr.io/datadoghq/operator",
	}

	releaseName := helpers.RandomName()
	datadogOperatorRelease := datadog.DatadogOperatorReleaseName

	acceptanceTestingTags := fmt.Sprintf("kube_stateful_set:%s-consul-server", releaseName)
	// Install the consul cluster in the default kubernetes ctx.
	consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)
	consulCluster.Create(t)

	// Deploy Datadog Agent via Datadog Operator and apply dogstatsd overlay
	datadogNamespace := helmValues["global.metrics.datadog.namespace"]
	logger.Log(t, fmt.Sprintf("deploying datadog-operator via helm | namespace: %s | release-name: %s", datadogNamespace, datadogOperatorRelease))
	datadogCluster := datadog.NewDatadogCluster(t, ctx, cfg, datadogOperatorRelease, datadogNamespace, datadogOperatorHelmValues)
	datadogCluster.Create(t)

	logger.Log(t, fmt.Sprintf("applying datadog openmetrics patch to datadog-agent | namespace: %s", datadogNamespace))
	k8s.DeployKustomize(t, ctx.KubectlOptionsForNamespace(datadogNamespace), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/datadog-openmetrics")
	k8s.WaitForAllPodsToBeReady(t, ctx.KubernetesClient(t), datadogNamespace, "agent.datadoghq.com/component=agent")

	datadogAPIClient := datadogCluster.DatadogClient(t)
	response, fullResponse, err := datadog.ApiWithRetry(t, datadogAPIClient, datadog.MetricTimeSeriesQuery, acceptanceTestingTags, consulOpenmetricsQuery, maxDatadogAPIRetryAttempts)
	if err != nil {
		content, _ := json.MarshalIndent(response.QueryResponse, "", "   ")
		fullContent, _ := json.MarshalIndent(fullResponse, "", "    ")
		logger.Logf(t, "Error when querying /v1/query endpoint for %s: %v", consulOpenmetricsQuery, err)
		logger.Logf(t, "Response: %v", string(content))
		logger.Logf(t, "Full Response: %v", string(fullContent))
	}
	content, _ := json.MarshalIndent(response.QueryResponse, "", "   ")
	logger.Logf(t, "Response: %v", string(content))
	require.Contains(t, string(content), consulOpenmetricsQuery)
}
