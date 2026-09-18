// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package apigateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	terratestLogger "github.com/gruntwork-io/terratest/modules/logger"
	terratestk8s "github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul-k8s/acceptance/framework/portforward"
	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
)

// TestAPIGateway_MultiListenerProtocol verifies that a single API Gateway can
// have listeners added across three separate "day-N" operations and that the
// Consul APIGatewayConfigEntry accurately reflects each listener's protocol
// after every update.  After all three listeners are live it also verifies the
// Envoy xDS config that consul-dataplane pushed to the gateway pod, confirming
// that the http2/grpc protocol options are actually present in the generated
// listener and upstream cluster configs.
//
// Scenario overview
// ─────────────────
//
//	Day 1 — Create gateway with a single HTTP listener ("http-listener", port 80,
//	         no protocol annotation → Consul protocol = "http").
//	         Attach an HTTPRoute targeting http-server (ServiceDefaults: http).
//	         Verify: Consul config entry has 1 listener, protocol=http.
//	                 K8s Gateway has Accepted + ConsulAccepted conditions.
//	                 HTTPRoute is bound (Accepted + ConsulAccepted).
//
//	Day 2 — Patch the live Gateway to append grpc-listener (port 9080) and set
//	         the per-section annotation "…listener-grpc-listener-protocol: grpc".
//	         Attach an HTTPRoute targeting grpc-server (ServiceDefaults: grpc).
//	         Verify: Consul config entry has 2 listeners, protocols http + grpc.
//	                 Both routes bound; day-1 listener unchanged.
//
//	Day 3 — Patch the live Gateway to append h2-listener (port 9090) and set
//	         the per-section annotation "…listener-h2-listener-protocol: http2".
//	         Attach an HTTPRoute targeting h2-server (ServiceDefaults: http2).
//	         Verify: Consul config entry has 3 listeners, protocols http + grpc + http2.
//	                 All three routes bound.
//	                 No-op re-update does NOT bump the Consul ModifyIndex.
//
//	Envoy — After Day 3 converges, port-forward to the gateway pod's Envoy admin
//	         port (19000) and assert the xDS config:
//	         • grpc listener  → HttpConnectionManager has http2ProtocolOptions:{},
//	                            grpc_stats + grpc_http1_bridge filters present.
//	         • http2 listener → HttpConnectionManager has http2ProtocolOptions:{}.
//	         • http listener  → HttpConnectionManager has NO http2ProtocolOptions.
//	         • grpc upstream cluster  → explicitHttpConfig.http2ProtocolOptions:{}.
//	         • http2 upstream cluster → explicitHttpConfig.http2ProtocolOptions:{}.
//	         • http upstream cluster  → NO explicitHttpConfig.
//
// Backend deployments use hashicorp/http-echo (lightweight HTTP/1.1 echo
// server already used throughout this test suite).  The Consul protocol is
// governed by ServiceDefaults, not the wire protocol of the container.
func TestAPIGateway_MultiListenerProtocol(t *testing.T) {
	ctx := suite.Environment().DefaultContext(t)
	cfg := suite.Config()

	helmValues := map[string]string{
		"connectInject.enabled":        "true",
		"global.acls.manageSystemACLs": "true",
		"global.tls.enabled":           "true",
		"global.logLevel":              "trace",
	}
	if strings.HasSuffix(cfg.ConsulImage, ":local") ||
		strings.HasSuffix(cfg.ConsulK8SImage, ":local") {
		helmValues["global.imagePullPolicy"] = "IfNotPresent"
	}

	releaseName := helpers.RandomName()
	consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)
	consulCluster.Create(t)

	k8sClient := ctx.ControllerRuntimeClient(t)
	k8sOpts := ctx.KubectlOptions(t)
	ns := k8sOpts.Namespace

	// The dev OSS binary always registers a watch for routeextprocs.consul.hashicorp.com
	// but the Helm chart only creates that CRD for enterprise installs.  Without it the
	// gateway-v1 controller loops forever trying to list the missing resource and never
	// starts its workers, so Gateways are never reconciled.  Apply a minimal stub CRD
	// immediately after the Helm install so the controller can start cleanly.
	applyRouteExtProcCRDStub(t, k8sOpts)

	// Bounce the connect-injector (which hosts the gateway controller) so it
	// picks up the newly-registered CRD and starts its gateway-v1 workers.
	bounceConnectInjector(t, k8sOpts, releaseName)

	consulClient, _ := consulCluster.SetupConsulClient(t, true)

	// Allow all-to-all traffic so the gateway can reach every backend when
	// ACLs are enabled.
	_, _, err := consulClient.ConfigEntries().Set(&api.ServiceIntentionsConfigEntry{
		Kind: api.ServiceIntentions,
		Name: "*",
		Sources: []*api.SourceIntention{
			{Name: "*", Action: api.IntentionActionAllow},
		},
	}, nil)
	require.NoError(t, err)

	const (
		gwName       = "multi-listener-gw"
		gwClassName  = "multi-listener-gwclass"
		routeHTTP    = "route-http"
		routeGRPC    = "route-grpc"
		routeH2      = "route-h2"
		listenerHTTP = "http-listener"
		listenerGRPC = "grpc-listener"
		listenerH2   = "h2-listener"
	)
	portHTTP := gwv1.PortNumber(80)
	// Ports ≥ 1024 are not subject to the mapPrivilegedContainerPorts (+8000)
	// offset applied by the GatewayClassConfig.  portHTTP(80) → container 8080
	// via +8000.  Using 9080/9090 avoids colliding with that mapped port.
	portGRPC := gwv1.PortNumber(9080)
	portH2 := gwv1.PortNumber(9090)

	annotationKeyGRPC := common.ListenerProtocolAnnotationPrefix + listenerGRPC + common.ListenerProtocolAnnotationSuffix
	annotationKeyH2 := common.ListenerProtocolAnnotationPrefix + listenerH2 + common.ListenerProtocolAnnotationSuffix

	fromAll := gwv1.NamespacesFromAll
	allowAll := &gwv1.AllowedRoutes{
		Namespaces: &gwv1.RouteNamespaces{From: &fromAll},
	}

	fixturePath := "../fixtures/cases/api-gateways/multi-listener-protocol"

	// ── Deploy shared infrastructure ─────────────────────────────────────────
	logger.Log(t, "deploying multi-listener-protocol shared fixtures")
	out, err := k8s.RunKubectlAndGetOutputE(t, k8sOpts, "apply", "-k", fixturePath)
	require.NoError(t, err, out)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		k8s.RunKubectlAndGetOutputE(t, k8sOpts, "delete", "-k", fixturePath)
	})

	logger.Log(t, "waiting for backend deployments")
	for _, deploy := range []string{"http-server", "grpc-server", "h2-server"} {
		k8s.RunKubectl(t, k8sOpts, "wait", "--for=condition=available",
			"--timeout=5m", "deploy/"+deploy)
	}
	logger.Log(t, "waiting for backend services to register in Consul catalog")
	for _, svc := range []string{"http-server", "grpc-server", "h2-server"} {
		waitForConsulServiceRegistered(t, consulClient, svc)
	}

	// ── makeRoute builds an HTTPRoute attached to a specific listener section ─
	makeRoute := func(name, listenerSection, backendSvc string, backendPort gwv1.PortNumber) *gwv1.HTTPRoute {
		section := gwv1.SectionName(listenerSection)
		return &gwv1.HTTPRoute{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
			Spec: gwv1.HTTPRouteSpec{
				CommonRouteSpec: gwv1.CommonRouteSpec{
					ParentRefs: []gwv1.ParentReference{{
						Name:        gwv1.ObjectName(gwName),
						SectionName: &section,
					}},
				},
				Rules: []gwv1.HTTPRouteRule{{
					BackendRefs: []gwv1.HTTPBackendRef{{
						BackendRef: gwv1.BackendRef{
							BackendObjectReference: gwv1.BackendObjectReference{
								Name: gwv1.ObjectName(backendSvc),
								Port: ptr.To(backendPort),
							},
						},
					}},
				}},
			},
		}
	}

	// ════════════════════════════════════════════════════════════════════════
	// DAY 1 — single HTTP listener, no protocol annotation.
	// ════════════════════════════════════════════════════════════════════════
	logger.Log(t, "day1: creating gateway with a single http-listener (no annotation)")

	gw := &gwv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gwName,
			Namespace: ns,
			Labels:    map[string]string{"component": "api-gateway"},
		},
		Spec: gwv1.GatewaySpec{
			GatewayClassName: gwClassName,
			Listeners: []gwv1.Listener{
				{
					Name:          gwv1.SectionName(listenerHTTP),
					Protocol:      gwv1.HTTPProtocolType,
					Port:          portHTTP,
					AllowedRoutes: allowAll,
				},
			},
		},
	}
	require.NoError(t, k8sClient.Create(context.Background(), gw))
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		k8sClient.Delete(context.Background(), gw)
	})

	routeHTTPObj := makeRoute(routeHTTP, listenerHTTP, "http-server", 80)
	require.NoError(t, k8sClient.Create(context.Background(), routeHTTPObj))
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		k8sClient.Delete(context.Background(), routeHTTPObj)
	})

	// Verify day-1 state.
	logger.Log(t, "day1: waiting for Consul config entry — 1 listener, protocol=http")
	retryCheck(t, 60, func(r *retry.R) {
		gwEntry := mustGetAPIGWConfigEntry(r, consulClient, gwName)
		require.Len(r, gwEntry.Listeners, 1,
			"day1: expected exactly 1 listener in Consul config entry")
		assertConsulListenerProtocol(r, gwEntry, listenerHTTP, "http")
	})

	logger.Log(t, "day1: verifying K8s Gateway Accepted+ConsulAccepted")
	retryCheck(t, 60, func(r *retry.R) {
		var k8sGW gwv1.Gateway
		require.NoError(r, k8sClient.Get(context.Background(),
			types.NamespacedName{Name: gwName, Namespace: ns}, &k8sGW))
		checkStatusCondition(r, k8sGW.Status.Conditions, trueCondition("Accepted", "Accepted"))
		checkStatusCondition(r, k8sGW.Status.Conditions, trueCondition("ConsulAccepted", "Accepted"))
		require.Len(r, k8sGW.Status.Listeners, 1)
		checkStatusCondition(r, k8sGW.Status.Listeners[0].Conditions, trueCondition("Accepted", "Accepted"))
	})

	logger.Log(t, "day1: verifying http route is bound")
	checkRouteBound(t, k8sClient, routeHTTP, ns, gwName)
	logger.Log(t, "day1: PASS")

	// ════════════════════════════════════════════════════════════════════════
	// DAY 2 — append grpc-listener (port 9080) with protocol annotation grpc.
	// ════════════════════════════════════════════════════════════════════════
	logger.Log(t, "day2: adding grpc-listener to the existing gateway")

	// updateKubernetes reads the live object, applies the mutation, and retries
	// on 409 Conflict — the safest way to patch a Gateway that the controller
	// is actively reconciling.
	updateKubernetes(t, k8sClient, gw, func(g *gwv1.Gateway) {
		// Append the new listener (idempotent — won't duplicate if already present).
		for _, l := range g.Spec.Listeners {
			if l.Name == listenerGRPC {
				return
			}
		}
		g.Spec.Listeners = append(g.Spec.Listeners, gwv1.Listener{
			Name:          gwv1.SectionName(listenerGRPC),
			Protocol:      gwv1.HTTPProtocolType,
			Port:          portGRPC,
			AllowedRoutes: allowAll,
		})
		// Set the per-section protocol annotation.
		if g.Annotations == nil {
			g.Annotations = make(map[string]string)
		}
		g.Annotations[annotationKeyGRPC] = "grpc"
	})

	routeGRPCObj := makeRoute(routeGRPC, listenerGRPC, "grpc-server", 9000)
	require.NoError(t, k8sClient.Create(context.Background(), routeGRPCObj))
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		k8sClient.Delete(context.Background(), routeGRPCObj)
	})

	// Verify day-2 state.
	logger.Log(t, "day2: waiting for Consul config entry — 2 listeners, http+grpc")
	retryCheck(t, 60, func(r *retry.R) {
		gwEntry := mustGetAPIGWConfigEntry(r, consulClient, gwName)
		require.Len(r, gwEntry.Listeners, 2,
			"day2: expected exactly 2 listeners in Consul config entry")
		assertConsulListenerProtocol(r, gwEntry, listenerHTTP, "http")
		assertConsulListenerProtocol(r, gwEntry, listenerGRPC, "grpc")
	})

	logger.Log(t, "day2: verifying K8s Gateway ConsulAccepted with 2 listeners")
	retryCheck(t, 60, func(r *retry.R) {
		var k8sGW gwv1.Gateway
		require.NoError(r, k8sClient.Get(context.Background(),
			types.NamespacedName{Name: gwName, Namespace: ns}, &k8sGW))
		checkStatusCondition(r, k8sGW.Status.Conditions, trueCondition("Accepted", "Accepted"))
		checkStatusCondition(r, k8sGW.Status.Conditions, trueCondition("ConsulAccepted", "Accepted"))
		require.Len(r, k8sGW.Status.Listeners, 2)
		for _, ls := range k8sGW.Status.Listeners {
			checkStatusCondition(r, ls.Conditions, trueCondition("Accepted", "Accepted"))
		}
	})

	logger.Log(t, "day2: verifying both routes bound (http-route and grpc-route)")
	checkRouteBound(t, k8sClient, routeHTTP, ns, gwName)
	checkRouteBound(t, k8sClient, routeGRPC, ns, gwName)
	logger.Log(t, "day2: PASS")

	// ════════════════════════════════════════════════════════════════════════
	// DAY 3 — append h2-listener (port 9090) with protocol annotation http2.
	// ════════════════════════════════════════════════════════════════════════
	logger.Log(t, "day3: adding h2-listener to the existing gateway")

	updateKubernetes(t, k8sClient, gw, func(g *gwv1.Gateway) {
		for _, l := range g.Spec.Listeners {
			if l.Name == listenerH2 {
				return
			}
		}
		g.Spec.Listeners = append(g.Spec.Listeners, gwv1.Listener{
			Name:          gwv1.SectionName(listenerH2),
			Protocol:      gwv1.HTTPProtocolType,
			Port:          portH2,
			AllowedRoutes: allowAll,
		})
		if g.Annotations == nil {
			g.Annotations = make(map[string]string)
		}
		// Keep the existing grpc annotation so it is not accidentally cleared.
		g.Annotations[annotationKeyGRPC] = "grpc"
		g.Annotations[annotationKeyH2] = "http2"
	})

	routeH2Obj := makeRoute(routeH2, listenerH2, "h2-server", 8090)
	require.NoError(t, k8sClient.Create(context.Background(), routeH2Obj))
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		k8sClient.Delete(context.Background(), routeH2Obj)
	})

	// Verify day-3 state.
	logger.Log(t, "day3: waiting for Consul config entry — 3 listeners, http+grpc+http2")
	retryCheck(t, 60, func(r *retry.R) {
		gwEntry := mustGetAPIGWConfigEntry(r, consulClient, gwName)
		require.Len(r, gwEntry.Listeners, 3,
			"day3: expected exactly 3 listeners in Consul config entry")
		assertConsulListenerProtocol(r, gwEntry, listenerHTTP, "http")
		assertConsulListenerProtocol(r, gwEntry, listenerGRPC, "grpc")
		assertConsulListenerProtocol(r, gwEntry, listenerH2, "http2")
	})

	logger.Log(t, "day3: verifying K8s Gateway ConsulAccepted with 3 listeners")
	retryCheck(t, 60, func(r *retry.R) {
		var k8sGW gwv1.Gateway
		require.NoError(r, k8sClient.Get(context.Background(),
			types.NamespacedName{Name: gwName, Namespace: ns}, &k8sGW))
		checkStatusCondition(r, k8sGW.Status.Conditions, trueCondition("Accepted", "Accepted"))
		checkStatusCondition(r, k8sGW.Status.Conditions, trueCondition("ConsulAccepted", "Accepted"))
		require.Len(r, k8sGW.Status.Listeners, 3)
		for _, ls := range k8sGW.Status.Listeners {
			checkStatusCondition(r, ls.Conditions, trueCondition("Accepted", "Accepted"))
		}
	})

	logger.Log(t, "day3: verifying all three routes bound")
	checkRouteBound(t, k8sClient, routeHTTP, ns, gwName)
	checkRouteBound(t, k8sClient, routeGRPC, ns, gwName)
	checkRouteBound(t, k8sClient, routeH2, ns, gwName)

	// ── No-op stability check ──────────────────────────────────────────────
	// Re-apply the same day-3 update (identical listener list + identical
	// annotations) and confirm the Consul ModifyIndex does not increase.
	// This proves diff.EntriesEqual correctly detects no-op changes, preventing
	// infinite reconciliation.
	logger.Log(t, "day3: capturing Consul ModifyIndex for no-op stability check")
	var modifyIndex uint64
	retryCheck(t, 10, func(r *retry.R) {
		modifyIndex = mustGetAPIGWConfigEntry(r, consulClient, gwName).ModifyIndex
	})

	logger.Log(t, "day3: re-applying identical update (no-op) and checking ModifyIndex is stable")
	updateKubernetes(t, k8sClient, gw, func(g *gwv1.Gateway) {
		// Intentionally identical to current state — the controller must detect
		// no diff and skip the Consul write.
		if g.Annotations == nil {
			g.Annotations = make(map[string]string)
		}
		g.Annotations[annotationKeyGRPC] = "grpc"
		g.Annotations[annotationKeyH2] = "http2"
	})

	// Allow enough time for a spurious reconcile to complete if the diff is broken.
	time.Sleep(10 * time.Second)
	retryCheckWithWait(t, 6, 5*time.Second, func(r *retry.R) {
		gwEntry := mustGetAPIGWConfigEntry(r, consulClient, gwName)
		require.Equal(r, modifyIndex, gwEntry.ModifyIndex,
			"no-op gateway update must not increase Consul ModifyIndex "+
				"(would indicate spurious Consul write / infinite reconciliation)")
	})

	logger.Log(t, "day3: PASS — 3-listener gateway verified end-to-end, no spurious writes detected")

	// ════════════════════════════════════════════════════════════════════════
	// ENVOY — Verify the xDS config pushed to the gateway pod once all
	// three listeners and their upstream clusters are fully established.
	//
	// We port-forward to the Envoy admin port (19000) and parse:
	//   GET /config_dump?format=json  → listener HCM codec options
	//   GET /clusters?format=json     → upstream cluster HTTP/2 options
	// ════════════════════════════════════════════════════════════════════════
	logger.Log(t, "envoy: locating gateway pod for xDS config inspection")

	// Re-read the live Gateway object so its CreationTimestamp is populated —
	// that timestamp is part of the label selector used by LabelsForGateway.
	var liveGW gwv1.Gateway
	require.NoError(t, k8sClient.Get(context.Background(),
		types.NamespacedName{Name: gwName, Namespace: ns}, &liveGW))

	gatewayPodName := waitForGatewayPod(t, k8sClient, &liveGW, ns)
	logger.Log(t, "envoy: found gateway pod", "pod", gatewayPodName)

	// Port-forward to the Envoy admin interface.
	adminAddr := portforward.CreateTunnelToResourcePort(t, gatewayPodName, 19000, k8sOpts, terratestLogger.Discard)

	// ── Fetch config_dump ─────────────────────────────────────────────────
	// Both listener codec options and cluster HTTP/2 config live in config_dump.
	// The /clusters endpoint only returns runtime host stats, not typed configs.
	logger.Log(t, "envoy: fetching config_dump from Envoy admin")

	var configDump envoyConfigDump

	// Retry until xDS has pushed all three listeners (Envoy may not have
	// received them all immediately after the K8s reconcile).
	retryCheckWithWait(t, 20, 5*time.Second, func(r *retry.R) {
		configDump = fetchEnvoyConfigDump(r, adminAddr)

		// We need at least 3 dynamic listeners before asserting per-listener.
		require.GreaterOrEqualf(r, len(configDump.dynamicListeners()), 3,
			"envoy: expected ≥3 dynamic listeners, got %d — xDS not fully converged yet",
			len(configDump.dynamicListeners()))
	})

	// ── Listener assertions ────────────────────────────────────────────────
	// Listener names in Envoy use the pod IP: "<protocol>:<pod-ip>:<port>".
	// We match by protocol prefix + port suffix to avoid hardcoding the IP.
	logger.Log(t, "envoy: asserting listener codec options per protocol")

	// grpc-listener → HCM must have http2ProtocolOptions + grpc filters.
	assertEnvoyListenerHTTP2Options(t, configDump,
		"grpc", int(portGRPC), true,
		"grpc-listener must have http2ProtocolOptions in HttpConnectionManager")
	assertEnvoyListenerGRPCFilters(t, configDump,
		"grpc", int(portGRPC),
		"grpc-listener must have grpc_stats and grpc_http1_bridge filters")

	// h2-listener → HCM must have http2ProtocolOptions (no grpc filters needed).
	assertEnvoyListenerHTTP2Options(t, configDump,
		"http2", int(portH2), true,
		"h2-listener must have http2ProtocolOptions in HttpConnectionManager")

	// http-listener → HCM must NOT have http2ProtocolOptions.
	// portHTTP(80) is mapped to container port 8080 via mapPrivilegedContainerPorts(+8000).
	assertEnvoyListenerHTTP2Options(t, configDump,
		"http", int(portHTTP)+8000, false,
		"http-listener must NOT have http2ProtocolOptions in HttpConnectionManager")

	// ── Cluster assertions ─────────────────────────────────────────────────
	// Cluster configs are in the config_dump dynamic_active_clusters section,
	// not the /clusters endpoint (which only returns host/stats data).
	logger.Log(t, "envoy: asserting upstream cluster HTTP/2 options per service protocol")

	// grpc-server upstream cluster → explicit_http_config.http2_protocol_options present.
	assertEnvoyClusterHTTP2Options(t, configDump, "grpc-server", true,
		"grpc-server upstream cluster must have explicit_http_config.http2_protocol_options")

	// h2-server upstream cluster → explicit_http_config.http2_protocol_options present.
	assertEnvoyClusterHTTP2Options(t, configDump, "h2-server", true,
		"h2-server upstream cluster must have explicit_http_config.http2_protocol_options")

	// http-server upstream cluster → NO explicit_http_config.
	assertEnvoyClusterHTTP2Options(t, configDump, "http-server", false,
		"http-server upstream cluster must NOT have explicit_http_config")

	logger.Log(t, "envoy: PASS — Envoy xDS config correctly reflects http2/grpc protocol options")
}

// ── Envoy admin helpers ───────────────────────────────────────────────────────

// envoyConfigDump is a minimal typed wrapper around the JSON returned by
// GET /config_dump?format=json.  We only unmarshal what we need.
type envoyConfigDump struct {
	Configs []json.RawMessage `json:"configs"`
}

// dynamicListeners returns the slice of raw listener objects from the
// "dynamic_listeners" section of the config dump.
func (d envoyConfigDump) dynamicListeners() []json.RawMessage {
	for _, raw := range d.Configs {
		var entry struct {
			DynamicListeners []json.RawMessage `json:"dynamic_listeners"`
		}
		if err := json.Unmarshal(raw, &entry); err == nil && len(entry.DynamicListeners) > 0 {
			return entry.DynamicListeners
		}
	}
	return nil
}

// fetchEnvoyConfigDump fetches and parses the Envoy /config_dump endpoint.
func fetchEnvoyConfigDump(r *retry.R, adminAddr string) envoyConfigDump {
	r.Helper()
	resp, err := http.DefaultClient.Get(fmt.Sprintf("http://%s/config_dump?format=json", adminAddr))
	require.NoError(r, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(r, err)
	var dump envoyConfigDump
	require.NoError(r, json.Unmarshal(body, &dump))
	return dump
}

// assertEnvoyListenerHTTP2Options asserts whether the named dynamic listener's
// HttpConnectionManager has (or does not have) the http2ProtocolOptions field.
// The listenerName is matched against the "name" field of each dynamic listener.
// assertEnvoyListenerHTTP2Options asserts whether a dynamic listener whose
// name starts with "<protocol>:" and ends with ":<port>" has (or does not
// have) the http2ProtocolOptions field in its HttpConnectionManager.
//
// Envoy names gateway listeners as "<protocol>:<pod-ip>:<port>" so we match
// by protocol prefix and port suffix rather than the full IP-containing name.
func assertEnvoyListenerHTTP2Options(t *testing.T, dump envoyConfigDump, protocol string, port int, wantPresent bool, msg string) {
	t.Helper()

	prefix := protocol + ":"
	suffix := fmt.Sprintf(":%d", port)
	found := false
	hasH2 := false

	for _, rawListener := range dump.dynamicListeners() {
		var dl struct {
			ActiveState struct {
				Listener struct {
					Name string `json:"name"`
				} `json:"listener"`
			} `json:"active_state"`
		}
		if err := json.Unmarshal(rawListener, &dl); err != nil {
			continue
		}
		name := dl.ActiveState.Listener.Name
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
			continue
		}
		found = true
		raw := string(rawListener)
		// Envoy's /config_dump endpoint returns proto3 JSON which can use either
		// snake_case ("http2_protocol_options") or camelCase ("http2ProtocolOptions")
		// depending on the Envoy version and serialization path. Accept both.
		hasH2 = strings.Contains(raw, `"http2_protocol_options"`) ||
			strings.Contains(raw, `"http2ProtocolOptions"`)
		break
	}

	require.Truef(t, found,
		"envoy: no listener with protocol=%q port=%d found in dynamic_listeners — %s", protocol, port, msg)
	if wantPresent {
		require.Truef(t, hasH2,
			"envoy: listener %s:*:%d — http2_protocol_options missing — %s", protocol, port, msg)
	} else {
		require.Falsef(t, hasH2,
			"envoy: listener %s:*:%d — unexpected http2_protocol_options present — %s", protocol, port, msg)
	}
}

// assertEnvoyListenerGRPCFilters checks that a dynamic listener identified by
// protocol prefix + port suffix contains both the grpc_stats and
// grpc_http1_bridge HTTP filters.
func assertEnvoyListenerGRPCFilters(t *testing.T, dump envoyConfigDump, protocol string, port int, msg string) {
	t.Helper()

	prefix := protocol + ":"
	suffix := fmt.Sprintf(":%d", port)

	for _, rawListener := range dump.dynamicListeners() {
		var dl struct {
			ActiveState struct {
				Listener struct {
					Name string `json:"name"`
				} `json:"listener"`
			} `json:"active_state"`
		}
		if err := json.Unmarshal(rawListener, &dl); err != nil {
			continue
		}
		name := dl.ActiveState.Listener.Name
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
			continue
		}
		raw := string(rawListener)
		require.Truef(t, strings.Contains(raw, "envoy.filters.http.grpc_stats"),
			"envoy: listener %s:*:%d — grpc_stats filter missing — %s", protocol, port, msg)
		require.Truef(t, strings.Contains(raw, "envoy.filters.http.grpc_http1_bridge"),
			"envoy: listener %s:*:%d — grpc_http1_bridge filter missing — %s", protocol, port, msg)
		return
	}
	t.Fatalf("envoy: no listener with protocol=%q port=%d found for gRPC filter check — %s", protocol, port, msg)
}

// assertEnvoyClusterHTTP2Options asserts whether an upstream cluster matching
// clusterSubstring has (or does not have) explicit_http_config.http2_protocol_options
// in its typed extension config inside the config_dump dynamic_active_clusters.
// This is the marker that consul-dataplane configured the upstream for HTTP/2.
func assertEnvoyClusterHTTP2Options(t *testing.T, dump envoyConfigDump, clusterSubstring string, wantPresent bool, msg string) {
	t.Helper()

	found := false
	hasH2 := false

	for _, raw := range dump.Configs {
		var section struct {
			DynamicActiveClusters []json.RawMessage `json:"dynamic_active_clusters"`
		}
		if err := json.Unmarshal(raw, &section); err != nil || len(section.DynamicActiveClusters) == 0 {
			continue
		}
		for _, rawCluster := range section.DynamicActiveClusters {
			var cl struct {
				Cluster struct {
					Name string `json:"name"`
				} `json:"cluster"`
			}
			if err := json.Unmarshal(rawCluster, &cl); err != nil {
				continue
			}
			if !strings.Contains(cl.Cluster.Name, clusterSubstring) {
				continue
			}
			found = true
			s := string(rawCluster)
			// Accept both snake_case (live Envoy) and camelCase (golden-file format).
			hasH2 = (strings.Contains(s, `"explicit_http_config"`) || strings.Contains(s, `"explicitHttpConfig"`)) &&
				(strings.Contains(s, `"http2_protocol_options"`) || strings.Contains(s, `"http2ProtocolOptions"`))
			break
		}
		if found {
			break
		}
	}

	require.Truef(t, found,
		"envoy: cluster containing %q not found in config_dump dynamic_active_clusters — %s", clusterSubstring, msg)
	if wantPresent {
		require.Truef(t, hasH2,
			"envoy: cluster %q — explicit_http_config.http2_protocol_options missing — %s", clusterSubstring, msg)
	} else {
		require.Falsef(t, hasH2,
			"envoy: cluster %q — unexpected explicit_http_config.http2_protocol_options present — %s", clusterSubstring, msg)
	}
}

// waitForGatewayPod waits until at least one Ready pod exists with the
// LabelsForGateway label set for the given gateway, then returns its name.
func waitForGatewayPod(t *testing.T, k8sClient client.Client, gw *gwv1.Gateway, ns string) string {
	t.Helper()
	labels := common.LabelsForGateway(gw)
	var podName string
	retryCheckWithWait(t, 30, 5*time.Second, func(r *retry.R) {
		var pods corev1.PodList
		require.NoError(r, k8sClient.List(context.Background(), &pods,
			client.InNamespace(ns),
			client.MatchingLabels(labels)))
		require.NotEmpty(r, pods.Items, "no gateway pods found yet")
		for _, pod := range pods.Items {
			if pod.Status.Phase == corev1.PodRunning {
				for _, cond := range pod.Status.Conditions {
					if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
						podName = pod.Name
						return
					}
				}
			}
		}
		r.Fatal("no Ready gateway pod found yet")
	})
	return podName
}

// ── Test-local helper functions ───────────────────────────────────────────────

// mustGetAPIGWConfigEntry fetches the named APIGatewayConfigEntry from Consul,
// failing the retry if it is absent or cannot be type-asserted.
func mustGetAPIGWConfigEntry(r *retry.R, consulClient *api.Client, name string) *api.APIGatewayConfigEntry {
	r.Helper()
	entry, _, err := consulClient.ConfigEntries().Get(api.APIGateway, name, nil)
	require.NoError(r, err)
	gw, ok := entry.(*api.APIGatewayConfigEntry)
	require.True(r, ok, "config entry %q is not an *api.APIGatewayConfigEntry", name)
	return gw
}

// assertConsulListenerProtocol finds the named listener in a Consul
// APIGatewayConfigEntry and asserts it has wantProtocol, failing the retry if
// the listener is missing or the protocol does not match.
func assertConsulListenerProtocol(r *retry.R, gwEntry *api.APIGatewayConfigEntry, listenerName, wantProtocol string) {
	r.Helper()
	for _, l := range gwEntry.Listeners {
		if l.Name == listenerName {
			require.Equalf(r, wantProtocol, l.Protocol,
				"listener %q: expected protocol=%q, got %q", listenerName, wantProtocol, l.Protocol)
			return
		}
	}
	r.Errorf("listener %q not found in Consul APIGatewayConfigEntry %q (have: %v)",
		listenerName, gwEntry.Name, listenerNames(gwEntry))
}

// listenerNames returns the names of all listeners in a config entry, for use
// in error messages.
func listenerNames(gwEntry *api.APIGatewayConfigEntry) []string {
	names := make([]string, len(gwEntry.Listeners))
	for i, l := range gwEntry.Listeners {
		names[i] = l.Name
	}
	return names
}

// applyRouteExtProcCRDStub applies a minimal stub CRD for
// routeextprocs.consul.hashicorp.com.  The Helm chart only installs this CRD
// for enterprise, but the OSS dev binary always registers a watch for it.
// Without the CRD the gateway-v1 controller never starts its workers.
func applyRouteExtProcCRDStub(t *testing.T, k8sOpts *terratestk8s.KubectlOptions) {
	t.Helper()
	const stubCRD = `
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: routeextprocs.consul.hashicorp.com
spec:
  group: consul.hashicorp.com
  names:
    kind: RouteExtProc
    listKind: RouteExtProcList
    plural: routeextprocs
    singular: routeextproc
  scope: Namespaced
  versions:
    - name: v1alpha1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object
          x-kubernetes-preserve-unknown-fields: true
`
	tmpFile, err := os.CreateTemp("", "routeextproc-crd-*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())
	_, err = tmpFile.WriteString(stubCRD)
	require.NoError(t, err)
	require.NoError(t, tmpFile.Close())

	out, err := k8s.RunKubectlAndGetOutputE(t, k8sOpts, "apply", "-f", tmpFile.Name())
	require.NoError(t, err, "failed to apply RouteExtProc CRD stub: %s", out)
	logger.Log(t, "routeextprocs CRD stub applied")
}

// bounceConnectInjector restarts the connect-injector deployment (which hosts
// the gateway-v1 controller) so it picks up the newly-registered CRD and
// starts its gateway workers, then waits for the rollout to complete.
func bounceConnectInjector(t *testing.T, k8sOpts *terratestk8s.KubectlOptions, releaseName string) {
	t.Helper()
	deployName := releaseName + "-consul-connect-injector"
	out, err := k8s.RunKubectlAndGetOutputE(t, k8sOpts,
		"rollout", "restart", "deployment/"+deployName)
	require.NoError(t, err, "failed to restart connect-injector: %s", out)
	out, err = k8s.RunKubectlAndGetOutputE(t, k8sOpts,
		"rollout", "status", "deployment/"+deployName, "--timeout=120s")
	require.NoError(t, err, "connect-injector rollout did not complete: %s", out)
	logger.Log(t, "connect-injector restarted and ready")
}
