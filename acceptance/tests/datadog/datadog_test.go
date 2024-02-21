package datadog

import (
	"encoding/json"
	"fmt"
	"github.com/DataDog/datadog-api-client-go/v2/api/datadogV1"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/datadog"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/stretchr/testify/require"
	"testing"
)

// Test that prometheus metrics, when enabled, are accessible from the
// endpoints that have been exposed on the server, client and gateways.
func TestDatadogDogstatsDUnixDomainSocket(t *testing.T) {
	env := suite.Environment()
	cfg := suite.Config()
	ctx := env.DefaultContext(t)
	// ns := ctx.KubectlOptions(t).Namespace

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
	//k8s.DeployKustomize(t, ctx.KubectlOptionsForNamespace(datadogNamespace), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/bases/datadog-operator")
	//k8s.WaitForAllPodsToBeReady(t, ctx.KubernetesClient(t), datadogNamespace, "app.kubernetes.io/name=datadog-operator")

	logger.Log(t, fmt.Sprintf("deploying datadog-agent | namespace: %s", datadogNamespace))
	k8s.DeployKustomize(t, ctx.KubectlOptionsForNamespace(datadogNamespace), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/bases/datadog")
	k8s.WaitForAllPodsToBeReady(t, ctx.KubernetesClient(t), datadogNamespace, "agent.datadoghq.com/component=agent")

	logger.Log(t, fmt.Sprintf("applying dogstatd over unix domain sockets patch to datadog-agent | namespace: %s", datadogNamespace))
	k8s.DeployKustomize(t, ctx.KubectlOptionsForNamespace(datadogNamespace), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/datadog-dogstatsd-uds")
	k8s.WaitForAllPodsToBeReady(t, ctx.KubernetesClient(t), datadogNamespace, "agent.datadoghq.com/component=agent")

	datadogAPIClient := datadogCluster.DatadogClient(t)
	api := datadogV1.NewMetricsApi(datadogAPIClient.ApiClient)

	response, fullResponse, err := api.ListMetrics(datadogAPIClient.Ctx, "consul.acl")
	if err != nil {
		logger.Logf(t, "Error when calling MetricsApi.ListMetris: %v", err)
		logger.Logf(t, "Full Response: %v", fullResponse)
	}
	content, _ := json.MarshalIndent(response, "", " ")
	logger.Logf(t, "Full Response: %v", string(content))
	require.Contains(t, string(content), `consul.acl.ResolveToken.50percentile`)
}
