// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package apigateway

import (
	"context"
	"testing"

	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// TestAPIGateway_HeaderMatchInvert_Lifecycle verifies that the RouteHeaderMatchInvertFilter
// CRD is correctly translated to Consul's HTTPRouteConfigEntry with Invert=true on the
// corresponding header match, and that day-2 updates (swap filter, remove filter) produce
// the expected Consul config entry changes.
//
// Scenario flow:
//   - Day 1: HTTPRoute references invert-filter-v1 (headerNames: ["x-canary"])
//     → Consul entry has Headers[0].Name="x-canary", Invert=true
//   - Day 2: swap ExtensionRef to invert-filter-v2 (headerNames: ["x-version"])
//     → Consul entry has Headers[0].Name="x-version", Invert=true
//   - Day 3: remove the ExtensionRef entirely
//     → Consul entry has Headers[0].Invert=false (plain match, no negation)
func TestAPIGateway_HeaderMatchInvert_Lifecycle(t *testing.T) {
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

	k8sClient := ctx.ControllerRuntimeClient(t)
	consulClient, _ := consulCluster.SetupConsulClient(t, true)

	defaultNamespace := "default"

	// -------------------------------------------------------------------------
	// Infrastructure: backend service
	// -------------------------------------------------------------------------
	logger.Log(t, "deploying static-server backend")
	k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")

	// -------------------------------------------------------------------------
	// Infrastructure: GatewayClassConfig + GatewayClass
	// -------------------------------------------------------------------------
	gatewayClassConfigName := "header-invert-gcc"
	gatewayClassConfig := &v1alpha1.GatewayClassConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: gatewayClassConfigName,
		},
	}
	logger.Log(t, "creating GatewayClassConfig")
	require.NoError(t, k8sClient.Create(context.Background(), gatewayClassConfig))
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		k8sClient.DeleteAllOf(context.Background(), &v1alpha1.GatewayClassConfig{})
	})

	gatewayClassName := "header-invert-class"
	createGatewayClass(t, k8sClient, gatewayClassName, gatewayClassControllerName, &gwv1.ParametersReference{
		Group: gwv1.Group(v1alpha1.ConsulHashicorpGroup),
		Kind:  gwv1.Kind(v1alpha1.GatewayClassConfigKind),
		Name:  gatewayClassConfigName,
	})
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		k8sClient.DeleteAllOf(context.Background(), &gwv1.GatewayClass{})
	})

	// -------------------------------------------------------------------------
	// Infrastructure: Gateway (plain HTTP — no TLS needed)
	// -------------------------------------------------------------------------
	gatewayName := "header-invert-gw"
	logger.Log(t, "creating Gateway")
	gw := createHTTPGateway(t, k8sClient, gatewayName, defaultNamespace, gatewayClassName)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		k8sClient.DeleteAllOf(context.Background(), &gwv1.Gateway{}, client.InNamespace(defaultNamespace))
	})

	// -------------------------------------------------------------------------
	// Infrastructure: two RouteHeaderMatchInvertFilter CRDs
	// -------------------------------------------------------------------------
	filterV1Name := "invert-filter-v1"
	filterV1 := &v1alpha1.RouteHeaderMatchInvertFilter{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "consul.hashicorp.com/v1alpha1",
			Kind:       v1alpha1.RouteHeaderMatchInvertFilterKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      filterV1Name,
			Namespace: defaultNamespace,
		},
		Spec: v1alpha1.RouteHeaderMatchInvertFilterSpec{
			HeaderNames: []string{"x-canary"},
		},
	}
	logger.Log(t, "creating RouteHeaderMatchInvertFilter v1 (x-canary)")
	require.NoError(t, k8sClient.Create(context.Background(), filterV1))
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		k8sClient.DeleteAllOf(context.Background(), &v1alpha1.RouteHeaderMatchInvertFilter{}, client.InNamespace(defaultNamespace))
	})

	filterV2Name := "invert-filter-v2"
	filterV2 := &v1alpha1.RouteHeaderMatchInvertFilter{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "consul.hashicorp.com/v1alpha1",
			Kind:       v1alpha1.RouteHeaderMatchInvertFilterKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      filterV2Name,
			Namespace: defaultNamespace,
		},
		Spec: v1alpha1.RouteHeaderMatchInvertFilterSpec{
			HeaderNames: []string{"x-version"},
		},
	}
	logger.Log(t, "creating RouteHeaderMatchInvertFilter v2 (x-version)")
	require.NoError(t, k8sClient.Create(context.Background(), filterV2))

	// -------------------------------------------------------------------------
	// Day 1: create HTTPRoute referencing invert-filter-v1
	// -------------------------------------------------------------------------
	routeName := "invert-route"
	logger.Log(t, "Day 1: creating HTTPRoute with invert-filter-v1 (x-canary)")

	filterGroup := gwv1.Group("consul.hashicorp.com")
	filterKind := gwv1.Kind(v1alpha1.RouteHeaderMatchInvertFilterKind)

	route := &gwv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      routeName,
			Namespace: defaultNamespace,
		},
		Spec: gwv1.HTTPRouteSpec{
			CommonRouteSpec: gwv1.CommonRouteSpec{
				ParentRefs: []gwv1.ParentReference{
					{Name: gwv1.ObjectName(gatewayName)},
				},
			},
			Rules: []gwv1.HTTPRouteRule{
				{
					Matches: []gwv1.HTTPRouteMatch{
						{
							Headers: []gwv1.HTTPHeaderMatch{
								{
									Type:  headerMatchTypePtr(gwv1.HeaderMatchExact),
									Name:  gwv1.HTTPHeaderName("x-canary"),
									Value: "true",
								},
							},
						},
					},
					BackendRefs: []gwv1.HTTPBackendRef{
						{BackendRef: gwv1.BackendRef{
							BackendObjectReference: gwv1.BackendObjectReference{
								Name: gwv1.ObjectName("static-server"),
							},
						}},
					},
					Filters: []gwv1.HTTPRouteFilter{
						{
							Type: gwv1.HTTPRouteFilterExtensionRef,
							ExtensionRef: &gwv1.LocalObjectReference{
								Group: filterGroup,
								Kind:  filterKind,
								Name:  gwv1.ObjectName(filterV1Name),
							},
						},
					},
				},
			},
		},
	}
	require.NoError(t, k8sClient.Create(context.Background(), route))
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		k8sClient.DeleteAllOf(context.Background(), &gwv1.HTTPRoute{}, client.InNamespace(defaultNamespace))
	})

	// route must be bound to the gateway
	logger.Log(t, "Day 1: checking route is bound to gateway")
	checkRouteBound(t, k8sClient, routeName, defaultNamespace, gatewayName)

	// Consul config entry must have Invert=true on the x-canary header
	logger.Log(t, "Day 1: verifying Consul config entry has x-canary with Invert=true")
	checkConsulHeaderInvert(t, consulClient, routeName, "x-canary", true)

	// -------------------------------------------------------------------------
	// Day 2: swap ExtensionRef to invert-filter-v2 (x-version)
	// -------------------------------------------------------------------------
	logger.Log(t, "Day 2: updating HTTPRoute to reference invert-filter-v2 (x-version)")
	updateKubernetes(t, k8sClient, route, func(r *gwv1.HTTPRoute) {
		r.Spec.Rules[0].Matches[0].Headers[0].Name = gwv1.HTTPHeaderName("x-version")
		r.Spec.Rules[0].Filters[0].ExtensionRef.Name = gwv1.ObjectName(filterV2Name)
	})

	// route must still be bound
	logger.Log(t, "Day 2: checking route is still bound to gateway")
	checkRouteBound(t, k8sClient, routeName, defaultNamespace, gatewayName)

	// Consul config entry must now reflect x-version with Invert=true
	logger.Log(t, "Day 2: verifying Consul config entry has x-version with Invert=true")
	checkConsulHeaderInvert(t, consulClient, routeName, "x-version", true)

	// -------------------------------------------------------------------------
	// Day 3: remove the ExtensionRef entirely → Invert must revert to false
	// -------------------------------------------------------------------------
	logger.Log(t, "Day 3: removing ExtensionRef from HTTPRoute")
	updateKubernetes(t, k8sClient, route, func(r *gwv1.HTTPRoute) {
		r.Spec.Rules[0].Filters = nil
	})

	// route must still be bound
	logger.Log(t, "Day 3: checking route is still bound to gateway")
	checkRouteBound(t, k8sClient, routeName, defaultNamespace, gatewayName)

	// Consul config entry must now have Invert=false (no negation)
	logger.Log(t, "Day 3: verifying Consul config entry has Invert=false after filter removal")
	checkConsulHeaderInvert(t, consulClient, routeName, "x-version", false)

	// Scenario: deleting the gateway cleans up Consul state
	logger.Log(t, "deleting Gateway and verifying Consul cleanup")
	require.NoError(t, k8sClient.Delete(context.Background(), gw))
	checkConsulNotExists(t, consulClient, api.APIGateway, gatewayName)
	checkConsulNotExists(t, consulClient, api.HTTPRoute, routeName)
}

// checkConsulHeaderInvert fetches the named http-route config entry from Consul and asserts
// that Rules[0].Matches[0].Headers[0].Name == headerName and .Invert == wantInvert.
func checkConsulHeaderInvert(t *testing.T, consulClient *api.Client, routeName, headerName string, wantInvert bool) {
	t.Helper()

	retryCheck(t, 60, func(r *retry.R) {
		entry, _, err := consulClient.ConfigEntries().Get(api.HTTPRoute, routeName, nil)
		require.NoError(r, err)

		httpRoute, ok := entry.(*api.HTTPRouteConfigEntry)
		require.True(r, ok, "config entry is not an HTTPRouteConfigEntry")

		require.NotEmpty(r, httpRoute.Rules, "expected at least one rule")
		require.NotEmpty(r, httpRoute.Rules[0].Matches, "expected at least one match in rule 0")
		require.NotEmpty(r, httpRoute.Rules[0].Matches[0].Headers, "expected at least one header match")

		hdr := httpRoute.Rules[0].Matches[0].Headers[0]
		require.Equal(r, headerName, hdr.Name, "header name mismatch")
		require.Equal(r, wantInvert, hdr.Invert, "Invert flag mismatch for header %q", headerName)
	})
}

// headerMatchTypePtr returns a pointer to the given HTTPHeaderMatchType.
func headerMatchTypePtr(t gwv1.HeaderMatchType) *gwv1.HeaderMatchType {
	return &t
}

// createHTTPGateway creates a plain HTTP Gateway (no TLS listener).
func createHTTPGateway(t *testing.T, k8sClient client.Client, name, namespace, gatewayClass string) *gwv1.Gateway {
	t.Helper()

	gw := &gwv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    map[string]string{"component": "api-gateway"},
		},
		Spec: gwv1.GatewaySpec{
			GatewayClassName: gwv1.ObjectName(gatewayClass),
			Listeners: []gwv1.Listener{{
				Name:     gwv1.SectionName("listener"),
				Protocol: gwv1.HTTPProtocolType,
				Port:     8080,
				AllowedRoutes: &gwv1.AllowedRoutes{
					Namespaces: &gwv1.RouteNamespaces{
						From: routeNamespacesFromPtr(gwv1.NamespacesFromAll),
					},
				},
			}},
		},
	}

	err := k8sClient.Create(context.Background(), gw)
	require.NoError(t, err)
	return gw
}

// routeNamespacesFromPtr returns a pointer to the given NamespacesFromType.
func routeNamespacesFromPtr(f gwv1.FromNamespaces) *gwv1.FromNamespaces {
	return &f
}

// (Helpers checkConsulNotExists, checkRouteBound, updateKubernetes,
// createGatewayClass, retryCheck are defined in other files in this package.)
