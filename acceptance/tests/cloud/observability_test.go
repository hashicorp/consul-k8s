// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package cloud

import (
	"strings"
	"testing"
	"time"

	terratestk8s "github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/serf/testutil/retry"
	"github.com/stretchr/testify/require"
)

var (
	resourceSecretName     = "resource-sec-name"
	resourceSecretKey      = "resource-sec-key"
	resourceSecretKeyValue = "organization/11eb1a35-aac0-f7c7-8fe1-0242ac110008/project/11eb1a35-ab64-d576-8fe1-0242ac110008/hashicorp.consul.global-network-manager.cluster/TEST"

	clientIDSecretName     = "clientid-sec-name"
	clientIDSecretKey      = "clientid-sec-key"
	clientIDSecretKeyValue = "clientid"

	clientSecretName     = "client-sec-name"
	clientSecretKey      = "client-sec-key"
	clientSecretKeyValue = "client-secret"

	apiHostSecretName     = "apihost-sec-name"
	apiHostSecretKey      = "apihost-sec-key"
	apiHostSecretKeyValue = "fake-server:443"

	authUrlSecretName     = "authurl-sec-name"
	authUrlSecretKey      = "authurl-sec-key"
	authUrlSecretKeyValue = "https://fake-server:443"

	scadaAddressSecretName     = "scadaaddress-sec-name"
	scadaAddressSecretKey      = "scadaaddress-sec-key"
	scadaAddressSecretKeyValue = "fake-server:443"
)

func TestObservabilityCloud(t *testing.T) {
	ctx := suite.Environment().DefaultContext(t)

	kubectlOptions := ctx.KubectlOptions(t)
	ns := kubectlOptions.Namespace
	k8sClient := environment.KubernetesClientFromOptions(t, kubectlOptions)

	cfg := suite.Config()

	if cfg.HCPResourceID != "" {
		resourceSecretKeyValue = cfg.HCPResourceID
	}
	consul.CreateK8sSecret(t, k8sClient, cfg, ns, resourceSecretName, resourceSecretKey, resourceSecretKeyValue)
	consul.CreateK8sSecret(t, k8sClient, cfg, ns, clientIDSecretName, clientIDSecretKey, clientIDSecretKeyValue)
	consul.CreateK8sSecret(t, k8sClient, cfg, ns, clientSecretName, clientSecretKey, clientSecretKeyValue)
	consul.CreateK8sSecret(t, k8sClient, cfg, ns, apiHostSecretName, apiHostSecretKey, apiHostSecretKeyValue)
	consul.CreateK8sSecret(t, k8sClient, cfg, ns, authUrlSecretName, authUrlSecretKey, authUrlSecretKeyValue)
	consul.CreateK8sSecret(t, k8sClient, cfg, ns, scadaAddressSecretName, scadaAddressSecretKey, scadaAddressSecretKeyValue)

	k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/bases/cloud/hcp-mock")
	podName, err := k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "get", "pod", "-l", "app=fake-server", "-o", `jsonpath="{.items[0].metadata.name}"`)
	podName = strings.ReplaceAll(podName, "\"", "")
	if err != nil {
		logger.Log(t, "error finding pod name")
		return
	}
	logger.Log(t, "fake-server pod name:"+podName)
	localPort := terratestk8s.GetAvailablePort(t)
	tunnel := terratestk8s.NewTunnelWithLogger(
		ctx.KubectlOptions(t),
		terratestk8s.ResourceTypePod,
		podName,
		localPort,
		443,
		logger.TestLogger{})
	defer tunnel.Close()
	// Retry creating the port forward since it can fail occasionally.
	retry.RunWith(&retry.Counter{Wait: 5 * time.Second, Count: 60}, t, func(r *retry.R) {
		// NOTE: It's okay to pass in `t` to ForwardPortE despite being in a retry
		// because we're using ForwardPortE (not ForwardPort) so the `t` won't
		// get used to fail the test, just for logging.
		require.NoError(r, tunnel.ForwardPortE(t))
	})

	fsClient := newfakeServerClient(tunnel.Endpoint())
	logger.Log(t, "fake-server addr:"+tunnel.Endpoint())
	consulToken, err := fsClient.requestToken(tunnel.Endpoint())
	if err != nil {
		logger.Log(t, "error finding consul token")
		return
	}

	logger.Log(t, "consul test token :"+consulToken)

	releaseName := helpers.RandomName()

	helmValues := map[string]string{
		"global.imagePullPolicy":             "IfNotPresent",
		"global.cloud.enabled":               "true",
		"global.cloud.resourceId.secretName": resourceSecretName,
		"global.cloud.resourceId.secretKey":  resourceSecretKey,

		"global.cloud.clientId.secretName": clientIDSecretName,
		"global.cloud.clientId.secretKey":  clientIDSecretKey,

		"global.cloud.clientSecret.secretName": clientSecretName,
		"global.cloud.clientSecret.secretKey":  clientSecretKey,

		"global.cloud.apiHost.secretName": apiHostSecretName,
		"global.cloud.apiHost.secretKey":  apiHostSecretKey,

		"global.cloud.authUrl.secretName": authUrlSecretName,
		"global.cloud.authUrl.secretKey":  authUrlSecretKey,

		"global.cloud.scadaAddress.secretName": scadaAddressSecretName,
		"global.cloud.scadaAddress.secretKey":  scadaAddressSecretKey,
		"connectInject.default":                "true",

		// TODO: Follow up with this bug
		"global.acls.manageSystemACLs":         "false",
		"global.gossipEncryption.autoGenerate": "false",
		"global.tls.enabled":                   "true",
		"global.tls.enableAutoEncrypt":         "true",
		// TODO: Take this out

		"telemetryCollector.enabled":                   "true",
		"telemetryCollector.image":                     "hashicorp/consul-telemetry-collector:0.0.1",
		"telemetryCollector.cloud.clientId.secretName": clientIDSecretName,
		"telemetryCollector.cloud.clientId.secretKey":  clientIDSecretKey,

		"telemetryCollector.cloud.clientSecret.secretName": clientSecretName,
		"telemetryCollector.cloud.clientSecret.secretKey":  clientSecretKey,

		"telemetryCollector.extraEnvironmentVars.HCP_API_TLS":       "insecure",
		"telemetryCollector.extraEnvironmentVars.HCP_AUTH_TLS":      "insecure",
		"telemetryCollector.extraEnvironmentVars.HCP_SCADA_TLS":     "insecure",
		"telemetryCollector.extraEnvironmentVars.OTLP_EXPORTER_TLS": "insecure",

		"server.extraEnvironmentVars.HCP_API_TLS":   "insecure",
		"server.extraEnvironmentVars.HCP_AUTH_TLS":  "insecure",
		"server.extraEnvironmentVars.HCP_SCADA_TLS": "insecure",
	}
	if cfg.ConsulImage != "" {
		helmValues["global.image"] = cfg.ConsulImage
	}

	consulCluster := consul.NewHelmCluster(t, helmValues, suite.Environment().DefaultContext(t), suite.Config(), releaseName)
	consulCluster.Create(t)

	logger.Log(t, "creating static-server deployment")

	k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/bases/static-server")
	t.Log("Finished deployment. Validating expected conditions now")

	// Consul Telemetry Collector Test
	// Metrics Export Test - Validate that metrics are exported to the gateway from the Consul-Telemetry-Collector.
	retry.RunWith(&retry.Timer{Timeout: 1 * time.Minute, Wait: 10 * time.Second}, t, func(r *retry.R) {
		validationPayload := &validationBody{
			Path:                 validationPathCollector,
			ExpectedLabelKeys:    []string{"service_name", "service_instance_id"},
			ExpectedMetricName:   "otelcol_receiver_accepted_metric_points",
			DisallowedMetricName: "server.memory_heap_size",
		}
		require.NoError(r, fsClient.validateMetrics(validationPayload))
	})

	// Consul Core (Server Metrics) Tests
	// Metrics Export Test - Validate that metrics are exported to the gateway from Consul core servers.
	retry.RunWith(&retry.Timer{Timeout: 3 * time.Minute, Wait: 1 * time.Minute}, t, func(r *retry.R) {
		validationPayload := &validationBody{
			Path:                 validationPathConsul,
			ExpectedLabelKeys:    []string{"node_id", "node_name"},
			ExpectedMetricName:   "consul.rpc",
			DisallowedMetricName: "consul.state",
		}
		require.NoError(r, fsClient.validateMetrics(validationPayload))
	})

	// Refresh Test - New Configuration
	// 1. Add a new label and new filter that was previously disallowed via modify telemetry config endpoint.
	refreshTime := time.Now()
	modifyPayload := &modifyTelemetryConfigBody{
		Filters: []string{"consul.state"},
		Labels:  map[string]string{"new_label": "testLabel"},
	}
	require.NoError(t, fsClient.modifyTelemetryConfig(modifyPayload))

	// 2. Verify refresh has an effect on the exported metrics.
	// Consul server metrics exported every 1 minute (https://github.com/hashicorp/consul/blob/9776c10efb4472f196b47f88bc0db58b1bfa12ef/agent/hcp/telemetry/otel_sink.go#L27)
	// Try to obtain exported metrics with refreshed changes for max 5 minutes.
	retry.RunWith(&retry.Timer{Timeout: 3 * time.Minute, Wait: 1 * time.Minute}, t, func(r *retry.R) {
		validationPayload := &validationBody{
			Path:                 validationPathConsul,
			ExpectedLabelKeys:    []string{"node_id", "node_name", "new_label"},
			ExpectedMetricName:   "consul.state.services",
			DisallowedMetricName: "consul.fsm",
			// Add 10 seconds (2 * periodic refresh interval in tests) to allow a periodic refresh from Consul side to take place.
			FilterRecordsSince: refreshTime.Add(2 * 5 * time.Second).UnixNano(),
		}
		require.NoError(r, fsClient.validateMetrics(validationPayload))
	})

	// Refresh Test - Disable metrics
	// 1. Disable metrics via modify telemetry config endpoint
	refreshTime = time.Now()
	modifyDisablePayload := &modifyTelemetryConfigBody{
		Filters:  []string{"consul.state"},
		Labels:   map[string]string{"new_label": "testLabel"},
		Disabled: true,
	}
	require.NoError(t, fsClient.modifyTelemetryConfig(modifyDisablePayload))

	// 2. Verify refresh has turned metrics off
	// Consul server metrics exported every 1 minute (https://github.com/hashicorp/consul/blob/9776c10efb4472f196b47f88bc0db58b1bfa12ef/agent/hcp/telemetry/otel_sink.go#L27)
	// Try to obtain exported metrics with refreshed changes for max 5 minutes.
	retry.RunWith(&retry.Timer{Timeout: 5 * time.Minute, Wait: 1 * time.Minute}, t, func(r *retry.R) {
		validationPayload := &validationBody{
			Path:               validationPathConsul,
			MetricsDisabled:    true,
			FilterRecordsSince: refreshTime.Add(2 * 5 * time.Second).UnixNano(),
		}
		require.NoError(r, fsClient.validateMetrics(validationPayload))
	})
}
