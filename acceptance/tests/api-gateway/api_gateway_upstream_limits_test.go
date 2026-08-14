// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package apigateway

// TestAPIGateway_UpstreamLimits_And_PassiveHealthCheck validates the
// RouteUpstreamLimitsFilter CRD and the gateway-wide Defaults annotation path
// end-to-end on a real Kind + Consul cluster.
//
// The test covers four independent scenarios in one cluster install so that
// the expensive helm install is only paid once:
//
//  1. Day-1 per-service limits via RouteUpstreamLimitsFilter:
//     Apply a RouteUpstreamLimitsFilter and an HTTPRoute whose backendRef
//     references it.  Assert:
//     - The Consul HTTPRoute config entry has a non-nil Limits block with the
//       expected values.
//     - The Envoy cluster config_dump for the gateway pod contains the expected
//       circuit-breaker ("max_connections") and outlier-detection fields.
//
//  2. Day-2 update of per-service limits:
//     Patch the RouteUpstreamLimitsFilter with new values, wait for the
//     reconciler to re-sync, then re-assert both the Consul config entry and
//     the Envoy config_dump.
//
//  3. Day-1 gateway-wide Defaults via annotations:
//     Annotate the Gateway with the default-max-connections annotation and the
//     default passive-health-check annotations.  Assert:
//     - The Consul APIGateway config entry has a non-nil Defaults block.
//     - The Envoy config_dump for the gateway pod contains outlier_detection.
//
//  4. Service-level limits override gateway-wide defaults:
//     With gateway-wide defaults set (max_connections=40), apply a per-service
//     RouteUpstreamLimitsFilter with max_connections=100.  Assert that:
//     - The Consul HTTPRoute config entry carries the service-level value (100).
//     - The Envoy cluster circuit-breaker reflects max_connections=100, not 40.
//
// Running locally against a Kind cluster:
//
//	./hack/run-upstream-limits-acceptance.sh
//
// (See that script for flag details.)

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	terratestk8s "github.com/gruntwork-io/terratest/modules/k8s"
	terratestLogger "github.com/gruntwork-io/terratest/modules/logger"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul-k8s/acceptance/framework/portforward"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

// upstreamLimitsFixtureDir holds the kustomize overlay for this test suite.
const upstreamLimitsFixtureDir = "../fixtures/cases/api-gateways/upstream-limits"

func TestAPIGateway_UpstreamLimits_And_PassiveHealthCheck(t *testing.T) {
	ctx := suite.Environment().DefaultContext(t)
	cfg := suite.Config()

	helmValues := map[string]string{
		"global.logLevel":              "trace",
		"connectInject.enabled":        "true",
		"global.acls.manageSystemACLs": "true",
		"global.tls.enabled":           "true",
	}

	releaseName := helpers.RandomName()
	consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)
	consulCluster.Create(t)

	consulClient, _ := consulCluster.SetupConsulClient(t, true)

	// Set the global proxy protocol so that HTTP-level circuit-breaker fields
	// are honoured by Envoy.
	_, _, err := consulClient.ConfigEntries().Set(&api.ProxyConfigEntry{
		Kind: api.ProxyDefaults,
		Name: api.ProxyConfigGlobal,
		Config: map[string]interface{}{
			"protocol": "http",
		},
	}, nil)
	require.NoError(t, err)

	// Deploy the backend service that the gateway will route to.
	logger.Log(t, "deploying static-server backend")
	k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")
	k8s.RunKubectl(t, ctx.KubectlOptions(t), "wait", "--for=condition=available", "--timeout=5m", "deploy/static-server")

	// Allow the gateway to call the backend.
	logger.Log(t, "creating service intention for static-server")
	_, _, err = consulClient.ConfigEntries().Set(&api.ServiceIntentionsConfigEntry{
		Kind: api.ServiceIntentions,
		Name: "static-server",
		Sources: []*api.SourceIntention{{
			Name:   "gateway",
			Action: api.IntentionActionAllow,
		}},
	}, nil)
	require.NoError(t, err)

	// -----------------------------------------------------------------------
	// Apply the fixture: GatewayClassConfig, GatewayClass, Gateway, a
	// RouteUpstreamLimitsFilter, and an HTTPRoute that references it.
	// -----------------------------------------------------------------------
	logger.Log(t, "applying upstream-limits fixture")
	retry.Run(t, func(r *retry.R) {
		out, applyErr := k8s.RunKubectlAndGetOutputE(r, ctx.KubectlOptions(r), "apply", "-k", upstreamLimitsFixtureDir)
		require.NoError(r, applyErr, out)
	})
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		_, _ = k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "delete", "-k", upstreamLimitsFixtureDir)
	})

	helpers.WaitForHTTPRouteWithRetry(t, ctx.KubectlOptions(t), "limits-route", upstreamLimitsFixtureDir)

	k8sClient := ctx.ControllerRuntimeClient(t)

	// Wait for the gateway to be fully accepted and have an address.
	var gatewayAddress string
	retryCheckWithWait(t, 120, 2*time.Second, func(r *retry.R) {
		var gw gwv1.Gateway
		require.NoError(r, k8sClient.Get(context.Background(),
			types.NamespacedName{Name: "limits-gateway", Namespace: "default"}, &gw))

		checkStatusCondition(r, gw.Status.Conditions, trueCondition("Accepted", "Accepted"))
		checkStatusCondition(r, gw.Status.Conditions, trueCondition("ConsulAccepted", "Accepted"))
		require.NotEmpty(r, gw.Status.Addresses)
		gatewayAddress = gw.Status.Addresses[0].Value
	})
	logger.Logf(t, "gateway address: %s", gatewayAddress)

	// -----------------------------------------------------------------------
	// Scenario 1 – Day-1: Consul config-entry has per-service Limits.
	// -----------------------------------------------------------------------
	t.Run("day1/consul-config-entry-has-limits", func(t *testing.T) {
		retryCheckWithWait(t, 60, 2*time.Second, func(r *retry.R) {
			entry, _, err := consulClient.ConfigEntries().Get(api.HTTPRoute, "limits-route", nil)
			require.NoError(r, err)
			route := entry.(*api.HTTPRouteConfigEntry)

			require.NotEmpty(r, route.Rules, "no rules in HTTPRoute config entry")
			require.NotEmpty(r, route.Rules[0].Services, "no services in HTTPRoute rule")
			limits := route.Rules[0].Services[0].Limits
			require.NotNilf(r, limits, "expected non-nil Limits on service, got nil")

			// Values set in the RouteUpstreamLimitsFilter fixture.
			require.NotNil(r, limits.MaxConnections)
			require.Equal(r, 25, *limits.MaxConnections)
			require.NotNil(r, limits.MaxPendingRequests)
			require.Equal(r, 50, *limits.MaxPendingRequests)
			require.NotNil(r, limits.MaxConcurrentRequests)
			require.Equal(r, 100, *limits.MaxConcurrentRequests)
			require.NotNil(r, limits.PassiveHealthCheck)
			require.Equal(r, 10*time.Second, limits.PassiveHealthCheck.Interval)
			require.Equal(r, uint32(3), limits.PassiveHealthCheck.MaxFailures)
		})
	})

	// -----------------------------------------------------------------------
	// Scenario 1b – Day-1: Envoy cluster config shows circuit-breaker /
	// outlier detection fields for the upstream.
	// -----------------------------------------------------------------------
	t.Run("day1/envoy-config-has-circuit-breaker-and-outlier-detection", func(t *testing.T) {
		requireEnvoyUpstreamLimitsFields(t, ctx.KubectlOptions(t), "limits-gateway",
			envoyLimitsExpectation{
				maxConnections: 25,
				hasOutlierDetection: true,
			})
	})

	// -----------------------------------------------------------------------
	// Scenario 2 – Day-2: Update the RouteUpstreamLimitsFilter and check that
	// both the Consul config entry and the Envoy config are updated.
	// -----------------------------------------------------------------------
	t.Run("day2/update-limits-and-verify", func(t *testing.T) {
		logger.Log(t, "updating RouteUpstreamLimitsFilter with new values")

		// Fetch the current filter so we have a valid resourceVersion for the update.
		var limitsFilter v1alpha1.RouteUpstreamLimitsFilter
		retryCheckWithWait(t, 20, 2*time.Second, func(r *retry.R) {
			require.NoError(r, k8sClient.Get(context.Background(),
				types.NamespacedName{Name: "static-server-limits", Namespace: "default"}, &limitsFilter))
		})

		newMaxConnections := 75
		limitsFilter.Spec.MaxConnections = &newMaxConnections
		newMaxPending := 150
		limitsFilter.Spec.MaxPendingRequests = &newMaxPending
		newMaxConcurrent := 300
		limitsFilter.Spec.MaxConcurrentRequests = &newMaxConcurrent
		// Also update PHC max failures.
		newMaxFailures := uint32(7)
		limitsFilter.Spec.PassiveHealthCheck.MaxFailures = newMaxFailures

		retryCheckWithWait(t, 20, 2*time.Second, func(r *retry.R) {
			require.NoError(r, k8sClient.Update(context.Background(), &limitsFilter))
		})

		// Consul config entry must reflect the new values.
		retryCheckWithWait(t, 60, 2*time.Second, func(r *retry.R) {
			entry, _, getErr := consulClient.ConfigEntries().Get(api.HTTPRoute, "limits-route", nil)
			require.NoError(r, getErr)
			route := entry.(*api.HTTPRouteConfigEntry)

			require.NotEmpty(r, route.Rules)
			require.NotEmpty(r, route.Rules[0].Services)
			limits := route.Rules[0].Services[0].Limits
			require.NotNilf(r, limits, "expected non-nil Limits after update")
			require.NotNil(r, limits.MaxConnections)
			require.Equal(r, 75, *limits.MaxConnections)
			require.NotNil(r, limits.PassiveHealthCheck)
			require.Equal(r, uint32(7), limits.PassiveHealthCheck.MaxFailures)
		})

		// Envoy should also reflect the updated max_connections.
		requireEnvoyUpstreamLimitsFields(t, ctx.KubectlOptions(t), "limits-gateway",
			envoyLimitsExpectation{
				maxConnections:      75,
				hasOutlierDetection: true,
			})
	})

	// -----------------------------------------------------------------------
	// Scenario 3 – Day-1 gateway-wide Defaults via annotations.
	// -----------------------------------------------------------------------
	t.Run("day1/gateway-defaults-via-annotations", func(t *testing.T) {
		// Annotate the gateway with gateway-wide defaults.
		logger.Log(t, "annotating gateway with gateway-wide upstream limit defaults")
		retryCheckWithWait(t, 20, 2*time.Second, func(r *retry.R) {
			var gw gwv1.Gateway
			require.NoError(r, k8sClient.Get(context.Background(),
				types.NamespacedName{Name: "limits-gateway", Namespace: "default"}, &gw))

			if gw.Annotations == nil {
				gw.Annotations = make(map[string]string)
			}
			gw.Annotations["api-gateway.consul.hashicorp.com/default-max-connections"] = "40"
			gw.Annotations["api-gateway.consul.hashicorp.com/default-passive-health-check-interval"] = "15s"
			gw.Annotations["api-gateway.consul.hashicorp.com/default-passive-health-check-max-failures"] = "5"
			gw.Annotations["api-gateway.consul.hashicorp.com/default-passive-health-check-max-ejection-percent"] = "20"

			require.NoError(r, k8sClient.Update(context.Background(), &gw))
		})

		// Consul APIGateway config entry must have a non-nil Defaults block.
		retryCheckWithWait(t, 60, 2*time.Second, func(r *retry.R) {
			entry, _, getErr := consulClient.ConfigEntries().Get(api.APIGateway, "limits-gateway", nil)
			require.NoError(r, getErr)
			gw := entry.(*api.APIGatewayConfigEntry)

			require.NotNilf(r, gw.Defaults, "expected non-nil Defaults on APIGatewayConfigEntry")
			require.NotNil(r, gw.Defaults.MaxConnections)
			require.Equal(r, 40, *gw.Defaults.MaxConnections)
			require.NotNil(r, gw.Defaults.PassiveHealthCheck)
			require.Equal(r, 15*time.Second, gw.Defaults.PassiveHealthCheck.Interval)
			require.Equal(r, uint32(5), gw.Defaults.PassiveHealthCheck.MaxFailures)
		})

		// Envoy outlier_detection must be present.
		requireEnvoyUpstreamLimitsFields(t, ctx.KubectlOptions(t), "limits-gateway",
			envoyLimitsExpectation{hasOutlierDetection: true})
	})

	// -----------------------------------------------------------------------
	// Scenario 3b – Day-2 update of gateway-wide Defaults.
	// -----------------------------------------------------------------------
	t.Run("day2/update-gateway-defaults-and-verify", func(t *testing.T) {
		logger.Log(t, "updating gateway-wide default-max-connections annotation")
		retryCheckWithWait(t, 20, 2*time.Second, func(r *retry.R) {
			var gw gwv1.Gateway
			require.NoError(r, k8sClient.Get(context.Background(),
				types.NamespacedName{Name: "limits-gateway", Namespace: "default"}, &gw))

			gw.Annotations["api-gateway.consul.hashicorp.com/default-max-connections"] = "80"
			gw.Annotations["api-gateway.consul.hashicorp.com/default-passive-health-check-max-failures"] = "9"
			require.NoError(r, k8sClient.Update(context.Background(), &gw))
		})

		retryCheckWithWait(t, 60, 2*time.Second, func(r *retry.R) {
			entry, _, getErr := consulClient.ConfigEntries().Get(api.APIGateway, "limits-gateway", nil)
			require.NoError(r, getErr)
			gw := entry.(*api.APIGatewayConfigEntry)

			require.NotNilf(r, gw.Defaults, "expected non-nil Defaults after update")
			require.NotNil(r, gw.Defaults.MaxConnections)
			require.Equal(r, 80, *gw.Defaults.MaxConnections)
			require.NotNil(r, gw.Defaults.PassiveHealthCheck)
			require.Equal(r, uint32(9), gw.Defaults.PassiveHealthCheck.MaxFailures)
		})
	})

	// -----------------------------------------------------------------------
	// Scenario 4 – Service-level limits override gateway-wide defaults.
	//
	// Gateway defaults are already set from Scenario 3 (currently max_connections=80
	// after Scenario 3b's day-2 update).  Reset the gateway default to a
	// deliberately lower value (40) then apply a per-service filter with
	// max_connections=100 and assert the service-level value wins everywhere.
	// -----------------------------------------------------------------------
	t.Run("service-level-overrides-gateway-defaults", func(t *testing.T) {
		// Step 1: lower the gateway-wide default to 40 so it is clearly distinct
		// from both the previous route value (75) and the new override (100).
		logger.Log(t, "setting gateway-wide default-max-connections=40 for override test")
		retryCheckWithWait(t, 20, 2*time.Second, func(r *retry.R) {
			var gw gwv1.Gateway
			require.NoError(r, k8sClient.Get(context.Background(),
				types.NamespacedName{Name: "limits-gateway", Namespace: "default"}, &gw))
			if gw.Annotations == nil {
				gw.Annotations = make(map[string]string)
			}
			gw.Annotations["api-gateway.consul.hashicorp.com/default-max-connections"] = "40"
			require.NoError(r, k8sClient.Update(context.Background(), &gw))
		})

		// Confirm the gateway-wide default is now 40 in the Consul config entry.
		retryCheckWithWait(t, 60, 2*time.Second, func(r *retry.R) {
			entry, _, getErr := consulClient.ConfigEntries().Get(api.APIGateway, "limits-gateway", nil)
			require.NoError(r, getErr)
			gwEntry := entry.(*api.APIGatewayConfigEntry)
			require.NotNil(r, gwEntry.Defaults)
			require.NotNil(r, gwEntry.Defaults.MaxConnections)
			require.Equal(r, 40, *gwEntry.Defaults.MaxConnections, "gateway default should be 40")
		})

		// Step 2: update the per-service RouteUpstreamLimitsFilter to 100 —
		// this should override the gateway-wide default of 40.
		logger.Log(t, "updating RouteUpstreamLimitsFilter to max_connections=100 to override gateway default")
		var limitsFilter v1alpha1.RouteUpstreamLimitsFilter
		retryCheckWithWait(t, 20, 2*time.Second, func(r *retry.R) {
			require.NoError(r, k8sClient.Get(context.Background(),
				types.NamespacedName{Name: "static-server-limits", Namespace: "default"}, &limitsFilter))
		})
		newMax := 100
		limitsFilter.Spec.MaxConnections = &newMax
		retryCheckWithWait(t, 20, 2*time.Second, func(r *retry.R) {
			require.NoError(r, k8sClient.Update(context.Background(), &limitsFilter))
		})

		// Step 3: the Consul HTTPRoute config entry must carry the service-level
		// value (100), not the gateway default (40).
		retryCheckWithWait(t, 60, 2*time.Second, func(r *retry.R) {
			entry, _, getErr := consulClient.ConfigEntries().Get(api.HTTPRoute, "limits-route", nil)
			require.NoError(r, getErr)
			route := entry.(*api.HTTPRouteConfigEntry)

			require.NotEmpty(r, route.Rules)
			require.NotEmpty(r, route.Rules[0].Services)
			limits := route.Rules[0].Services[0].Limits
			require.NotNilf(r, limits, "expected non-nil service Limits")
			require.NotNil(r, limits.MaxConnections)
			require.Equal(r, 100, *limits.MaxConnections,
				"service-level max_connections (100) should override gateway default (40)")
		})

		// Step 4: Envoy must reflect the service-level override (100), not the
		// gateway default (40).
		requireEnvoyUpstreamLimitsFields(t, ctx.KubectlOptions(t), "limits-gateway",
			envoyLimitsExpectation{
				maxConnections:      100,
				hasOutlierDetection: true,
			})
	})

}

// ---------------------------------------------------------------------------
// Envoy config_dump assertions
// ---------------------------------------------------------------------------

// envoyLimitsExpectation describes what fields we expect in the Envoy config.
type envoyLimitsExpectation struct {
	// maxConnections is the expected circuit_breakers.thresholds[0].max_connections value (0 = skip check).
	maxConnections int
	// hasOutlierDetection asserts that at least one upstream cluster has an
	// outlier_detection block.
	hasOutlierDetection bool
}

// requireEnvoyUpstreamLimitsFields port-forwards to the named gateway's Envoy
// admin interface (:19000) and checks /config_dump for circuit-breaker and
// outlier-detection fields in the static-server upstream cluster.
//
// The /clusters?format=json endpoint only returns runtime health stats and does
// NOT include outlier_detection or full circuit_breaker config.  The full xDS
// cluster config (including outlier_detection) is only visible in /config_dump
// under the ClustersConfigDump section.
func requireEnvoyUpstreamLimitsFields(t *testing.T, opts *terratestk8s.KubectlOptions, gatewayName string, expect envoyLimitsExpectation) {
	t.Helper()

	podName, err := k8s.RunKubectlAndGetOutputE(t, opts,
		"get", "pods",
		"-l", "gateway.consul.hashicorp.com/name="+gatewayName,
		"-o", "jsonpath={.items[0].metadata.name}")
	require.NoError(t, err, "failed to find gateway pod")
	podName = strings.TrimSpace(podName)
	require.NotEmptyf(t, podName, "no pod found for gateway %s", gatewayName)

	addr := portforward.CreateTunnelToResourcePort(t, podName, 19000, opts, terratestLogger.Discard)

	retryCheckWithWait(t, 30, 2*time.Second, func(r *retry.R) {
		// /config_dump contains the full xDS cluster config including
		// circuit_breakers and outlier_detection from the ClustersConfigDump.
		resp, httpErr := http.Get(fmt.Sprintf("http://%s/config_dump", addr))
		require.NoError(r, httpErr)
		defer func() { _ = resp.Body.Close() }()
		require.Equal(r, http.StatusOK, resp.StatusCode)

		body, readErr := io.ReadAll(resp.Body)
		require.NoError(r, readErr)
		require.NotEmpty(r, body)

		// Find the static-server cluster entry inside ClustersConfigDump.
		clusterJSON := extractStaticServerClusterJSON(r, body, gatewayName)
		require.NotEmpty(r, clusterJSON,
			"gateway %s: static-server cluster not found in config_dump", gatewayName)

		if expect.hasOutlierDetection {
			require.Containsf(r, clusterJSON, `"outlier_detection"`,
				"gateway %s: expected outlier_detection in static-server cluster config", gatewayName)
		}

		if expect.maxConnections > 0 {
			expected := fmt.Sprintf(`"max_connections": %d`, expect.maxConnections)
			require.Containsf(r, clusterJSON, expected,
				"gateway %s: expected max_connections=%d in static-server cluster config", gatewayName, expect.maxConnections)
		}
	})
}

// extractStaticServerClusterJSON parses an Envoy /config_dump response body
// and returns the JSON string for the static-server dynamic active cluster.
// Returns "" if not found.
func extractStaticServerClusterJSON(t require.TestingT, body []byte, gatewayName string) string {
	var dump struct {
		Configs []json.RawMessage `json:"configs"`
	}
	if err := json.Unmarshal(body, &dump); err != nil {
		return ""
	}
	for _, raw := range dump.Configs {
		var typeCheck struct {
			Type string `json:"@type"`
		}
		_ = json.Unmarshal(raw, &typeCheck)
		if !strings.Contains(typeCheck.Type, "ClustersConfigDump") {
			continue
		}
		var clustersDump struct {
			DynamicActiveClusters []struct {
				Cluster json.RawMessage `json:"cluster"`
			} `json:"dynamic_active_clusters"`
		}
		if err := json.Unmarshal(raw, &clustersDump); err != nil {
			continue
		}
		for _, entry := range clustersDump.DynamicActiveClusters {
			s := string(entry.Cluster)
			if strings.Contains(s, "static-server") {
				return s
			}
		}
	}
	return ""
}

// requireEnvoyConfigDumpContains port-forwards to the Envoy admin and checks
// /config_dump for an arbitrary substring.  Useful for one-off assertions.
func requireEnvoyConfigDumpContains(t *testing.T, opts *terratestk8s.KubectlOptions, gatewayName, substring string) {
	t.Helper()

	podName, err := k8s.RunKubectlAndGetOutputE(t, opts,
		"get", "pods",
		"-l", "gateway.consul.hashicorp.com/name="+gatewayName,
		"-o", "jsonpath={.items[0].metadata.name}")
	require.NoError(t, err)
	podName = strings.TrimSpace(podName)
	require.NotEmpty(t, podName)

	addr := portforward.CreateTunnelToResourcePort(t, podName, 19000, opts, terratestLogger.Discard)

	retryCheckWithWait(t, 30, 2*time.Second, func(r *retry.R) {
		resp, httpErr := http.Get(fmt.Sprintf("http://%s/config_dump", addr))
		require.NoError(r, httpErr)
		defer func() { _ = resp.Body.Close() }()
		require.Equal(r, http.StatusOK, resp.StatusCode)
		body, readErr := io.ReadAll(resp.Body)
		require.NoError(r, readErr)
		require.Containsf(r, string(body), substring,
			"gateway %s: config_dump missing expected substring", gatewayName)
	})
}

// getEnvoyClustersDump port-forwards to the Envoy admin and returns the parsed
// /clusters?format=json response.  Use together with findEnvoyClusterByName.
func getEnvoyClustersDump(t *testing.T, opts *terratestk8s.KubectlOptions, gatewayName string) map[string]interface{} {
	t.Helper()

	podName, err := k8s.RunKubectlAndGetOutputE(t, opts,
		"get", "pods",
		"-l", "gateway.consul.hashicorp.com/name="+gatewayName,
		"-o", "jsonpath={.items[0].metadata.name}")
	require.NoError(t, err)
	podName = strings.TrimSpace(podName)
	require.NotEmpty(t, podName)

	addr := portforward.CreateTunnelToResourcePort(t, podName, 19000, opts, terratestLogger.Discard)

	resp, err := http.Get(fmt.Sprintf("http://%s/clusters?format=json", addr))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var dump map[string]interface{}
	require.NoError(t, json.Unmarshal(body, &dump))
	return dump
}

// upstreamLimitsFilterFixture builds the in-memory RouteUpstreamLimitsFilter
// that the test creates directly (when not using kustomize fixtures).
func upstreamLimitsFilterFixture(name, namespace string) *v1alpha1.RouteUpstreamLimitsFilter {
	maxConns := 25
	maxPending := 50
	maxConcurrent := 100
	maxFailures := uint32(3)

	return &v1alpha1.RouteUpstreamLimitsFilter{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "consul.hashicorp.com/v1alpha1",
			Kind:       v1alpha1.RouteUpstreamLimitsFilterKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1alpha1.RouteUpstreamLimitsFilterSpec{
			MaxConnections:        &maxConns,
			MaxPendingRequests:    &maxPending,
			MaxConcurrentRequests: &maxConcurrent,
			PassiveHealthCheck: &v1alpha1.PassiveHealthCheck{
				Interval:    metav1.Duration{Duration: 10 * time.Second},
				MaxFailures: maxFailures,
			},
		},
	}
}
