package datadog

import (
	"fmt"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

const (
	StaticClientName = "static-client"
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

	releaseName := helpers.RandomName()

	// Install the consul cluster in the default kubernetes ctx.
	consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)
	consulCluster.Create(t)

	// Deploy Datadog Agent via Datadog Operator and apply dogstatsd overlay
	datadogNamespace := helmValues["global.metrics.datadog.namespace"]
	logger.Log(t, fmt.Sprintf("deploying datadog-agent using operator | namespace: %s", datadogNamespace))
	k8s.DeployKustomize(t, ctx.KubectlOptionsForNamespace(datadogNamespace), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/bases/datadog-operator")
	k8s.WaitForAllPodsToBeReady(t, ctx.KubernetesClient(t), datadogNamespace, "app.kubernetes.io/name=datadog-operator")

	k8s.DeployKustomize(t, ctx.KubectlOptionsForNamespace(datadogNamespace), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/bases/datadog")
	k8s.WaitForAllPodsToBeReady(t, ctx.KubernetesClient(t), datadogNamespace, "agent.datadoghq.com/component=agent")

	k8s.DeployKustomize(t, ctx.KubectlOptionsForNamespace(datadogNamespace), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/datadog-dogstatsd-uds")
	k8s.WaitForAllPodsToBeReady(t, ctx.KubernetesClient(t), datadogNamespace, "agent.datadoghq.com/component=agent")

	// Create the static-client deployment so we can use it for in-cluster calls to metrics endpoints.
	// This simulates queries that would be made by a prometheus server that runs externally to the consul
	// components in the cluster.
	logger.Log(t, "creating static-client")
	k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/bases/static-client")
	k8s.WaitForAllPodsToBeReady(t, ctx.KubernetesClient(t), ctx.KubectlOptions(t).Namespace, "app=static-client")
	// Server Metrics
	searchQuery := "?q=consul.acl"
	retry.RunWith(&retry.Counter{Count: 30, Wait: 10 * time.Second}, t, func(r *retry.R) {
		metricsOutput, err := k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "exec", "deploy/"+StaticClientName, "-c", "static-client", "--", "curl", "--silent", "--show-error", fmt.Sprintf("--header \"DD-API-KEY: %s\"", cfg.DatadogAPIKey), fmt.Sprintf("--header \"DD-APP-KEY: %s\"", cfg.DatadogAppKey), fmt.Sprintf("https://api.datadoghq.com/api/v1/search%s", searchQuery))
		require.NoError(t, err)
		require.Contains(t, metricsOutput, `consul.acl.ResolveToken.50percentile`)
	})
}
