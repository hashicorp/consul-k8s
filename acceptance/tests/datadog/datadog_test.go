// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package datadog

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/datadog"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	consulDogstatsDMetricQuery = "consul.memberlist.gossip"
)

// TODO: Refactor test cases into single function that accepts Helm Values, Fixture Name, and Validation Callback
// TestDatadogDogstatsDUnixDomainSocket
// Acceptance test to verify e2e metrics configuration works as expected
// with live datadog API using histogram formatted metric
//
// Method: DogstatsD + Unix Domain Socket.
func TestDatadogDogstatsDUnixDomainSocket(t *testing.T) {
	env := suite.Environment()
	cfg := suite.Config()
	ctx := env.DefaultContext(t)

	if cfg.SkipDataDogTests {
		t.Skipf("skipping this test because -skip-datadog is set")
	}

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

		"global.dualStack.defaultEnabled": cfg.GetDualStack(),
	}

	releaseName := helpers.RandomName()
	datadogOperatorRelease := datadog.OperatorReleaseName

	// Install the consul cluster in the default kubernetes ctx.
	consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)
	consulCluster.Create(t)

	// Deploy Datadog Agent via Datadog Operator and apply dogstatsd overlay
	datadogNamespace := helmValues["global.metrics.datadog.namespace"]
	logger.Log(t, fmt.Sprintf("deploying datadog-operator via helm | namespace: %s | release-name: %s", datadogNamespace, datadogOperatorRelease))
	datadogCluster := datadog.NewDatadogCluster(t, ctx, cfg, datadogOperatorRelease, datadogNamespace, map[string]string{})
	datadogCluster.Create(t)

	logger.Log(t, fmt.Sprintf("applying dogstatd over unix domain sockets kustomization patch to datadog-agent | namespace: %s", datadogNamespace))
	k8s.DeployKustomize(t, ctx.KubectlOptionsForNamespace(datadogNamespace), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/datadog-dogstatsd-uds")
	k8s.WaitForAllPodsToBeReady(t, ctx.KubernetesClient(t), datadogNamespace, "agent.datadoghq.com/component=agent")

	// Retrieve datadog-agent pod name for exec
	podList, err := ctx.KubernetesClient(t).CoreV1().Pods(datadogNamespace).List(context.Background(), metav1.ListOptions{LabelSelector: "agent.datadoghq.com/component=agent"})
	require.NoError(t, err)
	require.Len(t, podList.Items, 1)
	ddAgentName := podList.Items[0].Name

	// Check the dogstats-stats of the local cluster agent to see if consul metrics
	// are being seen by the agent
	logger.Log(t, fmt.Sprintf("retrieving datadog-agent control api auth token from pod %s", ddAgentName))
	bearerToken, err := k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptionsForNamespace(datadogNamespace), "exec", "pod/"+ddAgentName, "-c", "agent", "--", "cat", "/etc/datadog-agent/auth_token")
	require.NoError(t, err)
	// Retry because sometimes the merged metrics server takes a couple hundred milliseconds
	// to start.
	logger.Log(t, fmt.Sprintf("scraping datadog-agent /agent/dogstatsd-stats endpoint for %s | auth-token: %s", consulDogstatsDMetricQuery, bearerToken))
	retry.RunWith(&retry.Counter{Count: 20, Wait: 2 * time.Second}, t, func(r *retry.R) {
		metricsOutput, err := k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptionsForNamespace(datadogNamespace), "exec", "pod/"+ddAgentName, "-c", "agent", "--", "curl", "--silent", "--insecure", "--show-error", "--header", fmt.Sprintf("authorization: Bearer %s", bearerToken), "https://localhost:5001/agent/dogstatsd-stats")
		require.NoError(r, err)
		require.Contains(r, metricsOutput, consulDogstatsDMetricQuery)
	})
}

// TestDatadogDogstatsDUDP
// Acceptance test to verify e2e metrics configuration works as expected
// with live datadog API using histogram formatted metric
//
// Method: DogstatsD + UDP to Kube Service DNS name on port 8125.
func TestDatadogDogstatsDUDP(t *testing.T) {
	env := suite.Environment()
	cfg := suite.Config()
	ctx := env.DefaultContext(t)

	if cfg.SkipDataDogTests {
		t.Skipf("skipping this test because -skip-datadog is set")
	}

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

		"global.dualStack.defaultEnabled": cfg.GetDualStack(),
	}

	releaseName := helpers.RandomName()
	datadogOperatorRelease := datadog.OperatorReleaseName

	// Install the consul cluster in the default kubernetes ctx.
	consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)
	consulCluster.Create(t)

	// Deploy Datadog Agent via Datadog Operator and apply dogstatsd overlay.
	datadogNamespace := helmValues["global.metrics.datadog.namespace"]
	logger.Log(t, fmt.Sprintf("deploying datadog-operator via helm | namespace: %s | release-name: %s", datadogNamespace, datadogOperatorRelease))
	datadogCluster := datadog.NewDatadogCluster(t, ctx, cfg, datadogOperatorRelease, datadogNamespace, map[string]string{})
	datadogCluster.Create(t)

	logger.Log(t, fmt.Sprintf("applying dogstatd over UDP kustomization patch to datadog-agent | namespace: %s", datadogNamespace))
	k8s.DeployKustomize(t, ctx.KubectlOptionsForNamespace(datadogNamespace), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/datadog-dogstatsd-udp")
	k8s.WaitForAllPodsToBeReady(t, ctx.KubernetesClient(t), datadogNamespace, "agent.datadoghq.com/component=agent")

	// Retrieve datadog-agent pod name for exec
	podList, err := ctx.KubernetesClient(t).CoreV1().Pods(datadogNamespace).List(context.Background(), metav1.ListOptions{LabelSelector: "agent.datadoghq.com/component=agent"})
	require.NoError(t, err)
	require.Len(t, podList.Items, 1)
	ddAgentName := podList.Items[0].Name

	// Check the dogstats-stats of the local cluster agent to see if consul metrics
	// are being seen by the agent
	logger.Log(t, fmt.Sprintf("retrieving datadog-agent control api auth token from pod %s", ddAgentName))
	bearerToken, err := k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptionsForNamespace(datadogNamespace), "exec", "pod/"+ddAgentName, "-c", "agent", "--", "cat", "/etc/datadog-agent/auth_token")
	require.NoError(t, err)
	// Retry because sometimes the merged metrics server takes a couple hundred milliseconds
	// to start.
	logger.Log(t, fmt.Sprintf("scraping datadog-agent /agent/dogstatsd-stats endpoint for %s | auth-token: %s", consulDogstatsDMetricQuery, bearerToken))
	retry.RunWith(&retry.Counter{Count: 20, Wait: 2 * time.Second}, t, func(r *retry.R) {
		metricsOutput, err := k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptionsForNamespace(datadogNamespace), "exec", "pod/"+ddAgentName, "-c", "agent", "--", "curl", "--silent", "--insecure", "--show-error", "--header", fmt.Sprintf("authorization: Bearer %s", bearerToken), "https://localhost:5001/agent/dogstatsd-stats")
		require.NoError(r, err)
		require.Contains(r, metricsOutput, consulDogstatsDMetricQuery)
	})
}

// TestDatadogConsulChecks
// Acceptance test to verify e2e metrics configuration works as expected
// with live datadog API using histogram formatted metric
//
// Method: Consul Integrated Datadog Checks.
func TestDatadogConsulChecks(t *testing.T) {
	env := suite.Environment()
	cfg := suite.Config()
	ctx := env.DefaultContext(t)

	if cfg.SkipDataDogTests {
		t.Skipf("skipping this test because -skip-datadog is set")
	}

	helmValues := map[string]string{
		"global.datacenter":                   "dc1",
		"global.metrics.enabled":              "true",
		"global.metrics.enableAgentMetrics":   "true",
		"global.metrics.disableAgentHostName": "true",
		"global.metrics.enableHostMetrics":    "true",
		"global.metrics.datadog.enabled":      "true",
		"global.metrics.datadog.namespace":    "datadog",

		"global.dualStack.defaultEnabled": cfg.GetDualStack(),
	}

	releaseName := helpers.RandomName()
	datadogOperatorRelease := datadog.OperatorReleaseName

	// Install the consul cluster in the default kubernetes ctx.
	consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)
	consulCluster.Create(t)

	// Deploy Datadog Agent via Datadog Operator and apply dogstatsd overlay.
	datadogNamespace := helmValues["global.metrics.datadog.namespace"]
	logger.Log(t, fmt.Sprintf("deploying datadog-operator via helm | namespace: %s | release-name: %s", datadogNamespace, datadogOperatorRelease))
	datadogCluster := datadog.NewDatadogCluster(t, ctx, cfg, datadogOperatorRelease, datadogNamespace, map[string]string{})
	datadogCluster.Create(t)

	logger.Log(t, fmt.Sprintf("applying datadog consul integration patch to datadog-agent | namespace: %s", datadogNamespace))
	k8s.DeployKustomize(t, ctx.KubectlOptionsForNamespace(datadogNamespace), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/bases/datadog")
	k8s.WaitForAllPodsToBeReady(t, ctx.KubernetesClient(t), datadogNamespace, "agent.datadoghq.com/component=agent")

	// Retrieve datadog-agent pod name for exec
	podList, err := ctx.KubernetesClient(t).CoreV1().Pods(datadogNamespace).List(context.Background(), metav1.ListOptions{LabelSelector: "agent.datadoghq.com/component=agent"})
	require.NoError(t, err)
	require.Len(t, podList.Items, 1)
	ddAgentName := podList.Items[0].Name

	// Check the dogstats-stats of the local cluster agent to see if consul metrics
	// are being seen by the agent
	logger.Log(t, fmt.Sprintf("retrieving datadog-agent control api auth token from pod %s", ddAgentName))
	bearerToken, err := k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptionsForNamespace(datadogNamespace), "exec", "pod/"+ddAgentName, "-c", "agent", "--", "cat", "/etc/datadog-agent/auth_token")
	// Retry because sometimes the merged metrics server takes a couple hundred milliseconds
	// to start.
	logger.Log(t, fmt.Sprintf("scraping datadog-agent /agent/status endpoint | auth-token: %s", bearerToken))
	var metricsOutput string
	retry.RunWith(&retry.Counter{Count: 20, Wait: 2 * time.Second}, t, func(r *retry.R) {
		metricsOutput, err = k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptionsForNamespace(datadogNamespace), "exec", "pod/"+ddAgentName, "-c", "agent", "--", "curl", "--silent", "--insecure", "--show-error", "--header", fmt.Sprintf("authorization: Bearer %s", bearerToken), "https://localhost:5001/agent/status")
		require.NoError(r, err)
	})
	var root Root
	err = json.Unmarshal([]byte(metricsOutput), &root)
	require.NoError(t, err)
	for _, check := range root.RunnerStats.Checks.Consul {
		require.Equal(t, ``, check.LastError)
	}
}

// TestDatadogOpenmetrics
// Acceptance test to verify e2e metrics configuration works as expected
// with live datadog API using histogram formatted metric
//
// Method: Datadog Openmetrics Prometheus Metrics Collection.
func TestDatadogOpenmetrics(t *testing.T) {
	env := suite.Environment()
	cfg := suite.Config()
	ctx := env.DefaultContext(t)

	if cfg.SkipDataDogTests {
		t.Skipf("skipping this test because -skip-datadog is set")
	}

	helmValues := map[string]string{
		"global.datacenter":                                    "dc1",
		"global.metrics.enabled":                               "true",
		"global.metrics.enableAgentMetrics":                    "true",
		"global.metrics.disableAgentHostName":                  "true",
		"global.metrics.enableHostMetrics":                     "true",
		"global.metrics.datadog.enabled":                       "true",
		"global.metrics.datadog.namespace":                     "datadog",
		"global.metrics.datadog.openMetricsPrometheus.enabled": "true",

		"global.dualStack.defaultEnabled": cfg.GetDualStack(),
	}

	releaseName := helpers.RandomName()
	datadogOperatorRelease := datadog.OperatorReleaseName

	// Install the consul cluster in the default kubernetes ctx.
	consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)
	consulCluster.Create(t)

	// Deploy Datadog Agent via Datadog Operator and apply dogstatsd overlay
	datadogNamespace := helmValues["global.metrics.datadog.namespace"]
	logger.Log(t, fmt.Sprintf("deploying datadog-operator via helm | namespace: %s | release-name: %s", datadogNamespace, datadogOperatorRelease))
	datadogCluster := datadog.NewDatadogCluster(t, ctx, cfg, datadogOperatorRelease, datadogNamespace, map[string]string{})
	datadogCluster.Create(t)

	logger.Log(t, fmt.Sprintf("applying datadog openmetrics patch to datadog-agent | namespace: %s", datadogNamespace))
	k8s.DeployKustomize(t, ctx.KubectlOptionsForNamespace(datadogNamespace), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/datadog-openmetrics")
	k8s.WaitForAllPodsToBeReady(t, ctx.KubernetesClient(t), datadogNamespace, "agent.datadoghq.com/component=agent")

	// Retrieve datadog-agent pod name for exec
	podList, err := ctx.KubernetesClient(t).CoreV1().Pods(datadogNamespace).List(context.Background(), metav1.ListOptions{LabelSelector: "agent.datadoghq.com/component=agent"})
	require.NoError(t, err)
	require.Len(t, podList.Items, 1)
	ddAgentName := podList.Items[0].Name

	// Check the dogstats-stats of the local cluster agent to see if consul metrics
	// are being seen by the agent
	logger.Log(t, fmt.Sprintf("retrieving datadog-agent control api auth token from pod %s", ddAgentName))
	bearerToken, err := k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptionsForNamespace(datadogNamespace), "exec", "pod/"+ddAgentName, "-c", "agent", "--", "cat", "/etc/datadog-agent/auth_token")
	// Retry because sometimes the merged metrics server takes a couple hundred milliseconds
	// to start.
	logger.Log(t, fmt.Sprintf("scraping datadog-agent /agent/status endpoint | auth-token: %s", bearerToken))
	var metricsOutput string
	retry.RunWith(&retry.Counter{Count: 20, Wait: 2 * time.Second}, t, func(r *retry.R) {
		metricsOutput, err = k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptionsForNamespace(datadogNamespace), "exec", "pod/"+ddAgentName, "-c", "agent", "--", "curl", "--silent", "--insecure", "--show-error", "--header", fmt.Sprintf("authorization: Bearer %s", bearerToken), "https://localhost:5001/agent/status")
		require.NoError(r, err)
	})
	var root Root
	err = json.Unmarshal([]byte(metricsOutput), &root)
	require.NoError(t, err)
	for _, check := range root.RunnerStats.Checks.Openmetrics {
		if strings.Contains(check.CheckID, "consul") {
			require.Equal(t, ``, check.LastError)
			break
		}
		continue
	}
}

// TestDatadogOTLPCollection
// Acceptance test to verify e2e metrics configuration works as expected
// with live datadog API using histogram formatted metric
//
// Method: Datadog otlp metrics collection via consul-telemetry collector using dd-agent gRPC receiver.
//func TestDatadogOTLPCollection(t *testing.T) {
//	env := suite.Environment()
//	cfg := suite.Config()
//	ctx := env.DefaultContext(t)
//	// ns := ctx.KubectlOptions(t).Namespace
//
//	helmValues := map[string]string{
//		"global.datacenter":                    "dc1",
//		"global.metrics.enabled":               "true",
//		"global.metrics.enableAgentMetrics":    "true",
//		"global.metrics.disableAgentHostName":  "true",
//		"global.metrics.enableHostMetrics":     "true",
//		"global.metrics.datadog.enabled":       "true",
//		"global.metrics.datadog.namespace":     "datadog",
//		"global.metrics.datadog.otlp.enabled":  "true",
//		"global.metrics.datadog.otlp.protocol": "http",
//		"telemetryCollector.enabled":           "true",
//	}
//
//	datadogOperatorHelmValues := map[string]string{
//		"replicaCount":     "1",
//		"image.tag":        datadog.DefaultHelmChartVersion,
//		"image.repository": "gcr.io/datadoghq/operator",
//	}
//
//	releaseName := helpers.RandomName()
//	datadogOperatorRelease := datadog.OperatorReleaseName
//
//	// Install the consul cluster in the default kubernetes ctx.
//	consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)
//	consulCluster.Create(t)
//
//	// Deploy Datadog Agent via Datadog Operator and apply dogstatsd overlay
//	datadogNamespace := helmValues["global.metrics.datadog.namespace"]
//	logger.Log(t, fmt.Sprintf("deploying datadog-operator via helm | namespace: %s | release-name: %s", datadogNamespace, datadogOperatorRelease))
//	datadogCluster := datadog.NewDatadogCluster(t, ctx, cfg, datadogOperatorRelease, datadogNamespace, datadogOperatorHelmValues)
//	datadogCluster.Create(t)
//
//	logger.Log(t, fmt.Sprintf("applying datadog otlp HTTP endpoint collector patch to datadog-agent | namespace: %s", datadogNamespace))
//	k8s.DeployKustomize(t, ctx.KubectlOptionsForNamespace(datadogNamespace), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/datadog-otlp")
//	k8s.WaitForAllPodsToBeReady(t, ctx.KubernetesClient(t), datadogNamespace, "agent.datadoghq.com/component=agent")
//
//	// Retrieve datadog-agent pod name for exec
//	podList, err := ctx.KubernetesClient(t).CoreV1().Pods(datadogNamespace).List(context.Background(), metav1.ListOptions{LabelSelector: "agent.datadoghq.com/component=agent"})
//	require.NoError(t, err)
//	require.Len(t, podList.Items, 1)
//	ddAgentName := podList.Items[0].Name
//
//	// Check the dogstats-stats of the local cluster agent to see if consul metrics
//	// are being seen by the agent
//	bearerToken, err := k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptionsForNamespace(datadogNamespace), "exec", "pod/"+ddAgentName, "-c", "agent", "--", "cat /etc/datadog-agent/auth_token")
//	metricsOutput, err := k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptionsForNamespace(datadogNamespace), "exec", "pod/"+ddAgentName, "-c", "agent", "--", "curl", "--silent", "--insecure", "--show-error", "--header", fmt.Sprintf("authorization: Bearer %s", bearerToken), "https://localhost:5001/agent/dogstatsd-stats")
//	require.NoError(t, err)
//	require.Contains(t, metricsOutput, consulOTLPMetricQuery)
//}

type ConsulCheck struct {
	AverageExecutionTime int    `json:"AverageExecutionTime"`
	CheckConfigSource    string `json:"CheckConfigSource"`
	CheckID              string `json:"CheckID"`
	CheckName            string `json:"CheckName"`
	CheckVersion         string `json:"CheckVersion"`
	Events               int    `json:"Events"`
	ExecutionTimes       []int  `json:"ExecutionTimes"`
	LastError            string `json:"LastError"`
	LastExecutionTime    int    `json:"LastExecutionTime"`
	LastSuccessDate      int    `json:"LastSuccessDate"`
	MetricSamples        int    `json:"MetricSamples"`
	ServiceChecks        int    `json:"ServiceChecks"`
	TotalErrors          int    `json:"TotalErrors"`
	TotalEvents          int    `json:"TotalEvents"`
	TotalMetricSamples   int    `json:"TotalMetricSamples"`
	TotalRuns            int    `json:"TotalRuns"`
	TotalServiceChecks   int    `json:"TotalServiceChecks"`
	TotalWarnings        int    `json:"TotalWarnings"`
	UpdateTimestamp      int    `json:"UpdateTimestamp"`
}

type OpenmetricsCheck struct {
	AverageExecutionTime     int                    `json:"AverageExecutionTime"`
	CheckConfigSource        string                 `json:"CheckConfigSource"`
	CheckID                  string                 `json:"CheckID"`
	CheckName                string                 `json:"CheckName"`
	CheckVersion             string                 `json:"CheckVersion"`
	Events                   int                    `json:"Events"`
	ExecutionTimes           []int                  `json:"ExecutionTimes"`
	LastError                string                 `json:"LastError"`
	LastExecutionTime        int                    `json:"LastExecutionTime"`
	LastSuccessDate          int64                  `json:"LastSuccessDate"`
	MetricSamples            int                    `json:"MetricSamples"`
	ServiceChecks            int                    `json:"ServiceChecks"`
	TotalErrors              int                    `json:"TotalErrors"`
	TotalEventPlatformEvents map[string]interface{} `json:"TotalEventPlatformEvents"`
	TotalEvents              int                    `json:"TotalEvents"`
	TotalHistogramBuckets    int                    `json:"TotalHistogramBuckets"`
	TotalMetricSamples       int                    `json:"TotalMetricSamples"`
	TotalRuns                int                    `json:"TotalRuns"`
	TotalServiceChecks       int                    `json:"TotalServiceChecks"`
	TotalWarnings            int                    `json:"TotalWarnings"`
	UpdateTimestamp          int64                  `json:"UpdateTimestamp"`
}

type Checks struct {
	Consul      map[string]ConsulCheck      `json:"consul"`
	Openmetrics map[string]OpenmetricsCheck `json:"openmetrics"`
}

type RunnerStats struct {
	Checks Checks `json:"Checks"`
}

type Root struct {
	RunnerStats RunnerStats `json:"runnerStats"`
}
