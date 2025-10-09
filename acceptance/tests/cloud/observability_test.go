// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package cloud

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	terratestk8s "github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul/api"
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

	bootstrapTokenSecretName = "bootstrap-token"
	bootstrapTokenSecretKey  = "token"
	bootstrapToken           = uuid.NewString()
)

func TestObservabilityCloud(t *testing.T) {
	cfg := suite.Config()

	if cfg.HCPResourceID != "" {
		resourceSecretKeyValue = cfg.HCPResourceID
	}

	cases := []struct {
		name                      string
		validateCloudInteractions bool
		enableConsulNamespaces    bool
		mirroringK8S              bool
		adminPartitionsEnabled    bool
		secure                    bool
	}{
		{
			name:                      "default namespace and partition",
			validateCloudInteractions: true,
		},
		{
			name:   "default namespace and partition; secure",
			secure: true,
		},
		{
			name:                   "namespace mirroring; secure",
			enableConsulNamespaces: true,
			mirroringK8S:           true,
			secure:                 true,
		},
		{
			name:                   "admin partitions; secure",
			enableConsulNamespaces: true,
			mirroringK8S:           true,
			adminPartitionsEnabled: true,
			secure:                 true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := suite.Environment().DefaultContext(t)

			if c.enableConsulNamespaces && !cfg.EnableEnterprise {
				t.Skip("skipping this test because -enable-enterprise is not set")
			}

			options := &terratestk8s.KubectlOptions{
				ContextName: ctx.KubectlOptions(t).ContextName,
				ConfigPath:  ctx.KubectlOptions(t).ConfigPath,
				Namespace:   ctx.KubectlOptions(t).Namespace,
			}
			ns := options.Namespace

			k8sClient := environment.KubernetesClientFromOptions(t, options)

			// Create cloud and telemetryCollector secrets.
			consul.CreateK8sSecret(t, k8sClient, cfg, ns, resourceSecretName, resourceSecretKey, resourceSecretKeyValue)
			consul.CreateK8sSecret(t, k8sClient, cfg, ns, clientIDSecretName, clientIDSecretKey, clientIDSecretKeyValue)
			consul.CreateK8sSecret(t, k8sClient, cfg, ns, clientSecretName, clientSecretKey, clientSecretKeyValue)
			consul.CreateK8sSecret(t, k8sClient, cfg, ns, apiHostSecretName, apiHostSecretKey, apiHostSecretKeyValue)
			consul.CreateK8sSecret(t, k8sClient, cfg, ns, authUrlSecretName, authUrlSecretKey, authUrlSecretKeyValue)
			consul.CreateK8sSecret(t, k8sClient, cfg, ns, scadaAddressSecretName, scadaAddressSecretKey, scadaAddressSecretKeyValue)
			consul.CreateK8sSecret(t, k8sClient, cfg, ns, bootstrapTokenSecretName, bootstrapTokenSecretKey, bootstrapToken)

			k8s.DeployKustomize(t, options, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/bases/cloud/hcp-mock")
			podName, err := k8s.RunKubectlAndGetOutputE(t, options, "get", "pod", "-l", "app=fake-server", "-o", `jsonpath="{.items[0].metadata.name}"`)
			podName = strings.ReplaceAll(podName, "\"", "")
			if err != nil {
				logger.Log(t, "error finding pod name")
				return
			}
			logger.Log(t, "fake-server pod name:"+podName)
			localPort := terratestk8s.GetAvailablePort(t)
			tunnel := terratestk8s.NewTunnelWithLogger(
				options,
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
			consulToken, err := fsClient.requestToken()
			if err != nil {
				logger.Log(t, "error finding consul token")
				return
			}

			logger.Log(t, "consul test token :"+consulToken)

			releaseName := helpers.RandomName()

			helmValues := map[string]string{
				"global.imagePullPolicy": "IfNotPresent",

				"global.acls.manageSystemACLs":   fmt.Sprint(c.secure),
				"global.tls.enabled":             fmt.Sprint(c.secure),
				"global.adminPartitions.enabled": fmt.Sprint(c.adminPartitionsEnabled),

				"global.enableConsulNamespaces":               fmt.Sprint(c.enableConsulNamespaces),
				"connectInject.enabled":                       "true",
				"connectInject.consulNamespaces.mirroringK8S": fmt.Sprint(c.mirroringK8S),

				// TODO this doesn't appear to work because we just deploy to default using kubectl options from context.
				// https://github.com/hashicorp/consul-k8s/blob/74097fe7b3023105ca755b45da9c72c716547f46/acceptance/framework/consul/helm_cluster.go#L107
				// "connectInject.consulNamespaces.consulDestinationNamespace": c.destinationNamespace,

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

				"telemetryCollector.enabled":                   "true",
				"telemetryCollector.image":                     cfg.ConsulCollectorImage,
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

				"global.dualStack.defaultEnabled": cfg.GetDualStack(),
			}
			if cfg.ConsulImage != "" {
				helmValues["global.image"] = cfg.ConsulImage
			}
			if c.secure {
				helmValues["global.acls.bootstrapToken.secretName"] = bootstrapTokenSecretName
				helmValues["global.acls.bootstrapToken.secretKey"] = bootstrapTokenSecretKey
			}

			consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)
			consulCluster.ACLToken = bootstrapToken
			consulCluster.Create(t)

			logger.Log(t, "creating static-server deployment")
			k8s.DeployKustomize(t, options, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/bases/static-server")
			t.Log("Finished deployment. Validating expected conditions now")

			// Validate that the consul-telemetry-collector service was deployed to the expected namespace.
			consulClient, _ := consulCluster.SetupConsulClient(t, c.secure)
			q := &api.QueryOptions{}
			if cfg.EnableEnterprise {
				q.Namespace = ns
			}
			instances, _, err := consulClient.Catalog().Service("consul-telemetry-collector", "", q)
			require.NoError(t, err)
			require.Len(t, instances, 1)
			require.Equal(t, "passing", instances[0].Checks.AggregatedStatus())

			for name, tc := range map[string]struct {
				refresh     *modifyTelemetryConfigBody
				refreshTime int64
				recordsPath string
				timeout     time.Duration
				wait        time.Duration
				validations *metricValidations
			}{
				"collectorExportsMetrics": {
					recordsPath: recordsPathCollector,
					//  High timeout as Collector metrics scraped every 1 minute (https://github.com/hashicorp/consul-telemetry-collector/blob/dfdbf51b91d502a18f3b143a94ab4d50cdff10b8/internal/otel/config/helpers/receivers/prometheus_receiver.go#L54)
					timeout: 5 * time.Minute,
					wait:    1 * time.Second,
					validations: &metricValidations{
						expectedLabelKeys:    []string{"service_name", "service_instance_id"},
						expectedMetricName:   "otelcol_receiver_accepted_metric_points",
						disallowedMetricName: "server.memory_heap_size",
					},
				},
				"consulPeriodicRefreshUpdateConfig": {
					refresh: &modifyTelemetryConfigBody{
						Filters: []string{"consul.state"},
						Labels:  map[string]string{"new_label": "testLabel"},
					},
					recordsPath: recordsPathConsul,
					//  High timeout as Consul server metrics exported every 1 minute (https://github.com/hashicorp/consul/blob/9776c10efb4472f196b47f88bc0db58b1bfa12ef/agent/hcp/telemetry/otel_sink.go#L27)
					timeout: 3 * time.Minute,
					wait:    1 * time.Second,
					validations: &metricValidations{
						expectedLabelKeys:    []string{"node_id", "node_name", "new_label"},
						expectedMetricName:   "consul.state.services",
						disallowedMetricName: "consul.fsm",
					},
				},
				"consulPeriodicRefreshDisabled": {
					refresh: &modifyTelemetryConfigBody{
						Filters:  []string{"consul.state"},
						Labels:   map[string]string{"new_label": "testLabel"},
						Disabled: true,
					},
					recordsPath: recordsPathConsul,
					// High timeout as Consul server metrics exported every 1 minute (https://github.com/hashicorp/consul/blob/9776c10efb4472f196b47f88bc0db58b1bfa12ef/agent/hcp/telemetry/otel_sink.go#L27)
					timeout: 3 * time.Minute,
					wait:    1 * time.Second,
					validations: &metricValidations{
						disabled: true,
					},
				},
			} {
				t.Run(name, func(t *testing.T) {
					if !c.validateCloudInteractions {
						t.Skip("skipping server metric and config validation")
					}

					// For a refresh test, we force a telemetry config update before validating metrics using fakeserver's /telemetry_config_modify endpoint.
					if tc.refresh != nil {
						refreshTime := time.Now()
						err := fsClient.modifyTelemetryConfig(tc.refresh)
						require.NoError(t, err)
						// Add 10 seconds (2 * periodic refresh interval in fakeserver) to allow a periodic refresh from Consul side to take place.
						tc.refreshTime = refreshTime.Add(10 * time.Second).UnixNano()
					}

					// Validate metrics are correct using fakeserver's /records endpoint to retrieve metric exports that occured from Consul/Collector to fakeserver.
					// We use retry as we wait for Consul or the Collector to export metrics. This is the best we can do to avoid flakiness.
					retry.RunWith(&retry.Timer{Timeout: tc.timeout, Wait: tc.wait}, t, func(r *retry.R) {
						records, err := fsClient.getRecordsForPath(tc.recordsPath, tc.refreshTime)
						require.NoError(r, err)
						validateMetrics(r, records, tc.validations, tc.refreshTime)
					})
				})
			}
		})
	}
}
