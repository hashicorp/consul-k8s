// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package apigateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	terratestk8s "github.com/gruntwork-io/terratest/modules/k8s"
	terratestlogger "github.com/gruntwork-io/terratest/modules/logger"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul-k8s/acceptance/framework/portforward"
)

func TestAPIGateway_SDS_HTTPRoute(t *testing.T) {
	cfg := suite.Config()
	ctx := suite.Environment().DefaultContext(t)
	k8sNamespace, k8sOptions := createNamespace(t, ctx, cfg)

	helmValues := map[string]string{
		"connectInject.enabled":        "true",
		"global.acls.manageSystemACLs": "true",
		"global.tls.enabled":           "true",
	}

	logger.Log(t, "creating sds servers before consul install")
	out, err := k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "apply", "-f", "../fixtures/cases/api-gateways/sds-httproute/sds-server.yaml")
	require.NoError(t, err, out)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		_, _ = k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "delete", "-f", "../fixtures/cases/api-gateways/sds-httproute/sds-server.yaml")
	})
	k8s.RunKubectl(t, ctx.KubectlOptions(t), "wait", "--for=condition=available", "--timeout=5m", "-n", "default", "deploy/sds-server-1")

	releaseName := helpers.RandomName()
	consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)
	consulCluster.Create(t)

	consulClient, _ := consulCluster.SetupConsulClient(t, true)

	logger.Log(t, "applying proxy defaults and service defaults for sds cluster")
	_, _, err = consulClient.ConfigEntries().Set(&api.ProxyConfigEntry{
		Kind: api.ProxyDefaults,
		Name: api.ProxyConfigGlobal,
		Config: map[string]interface{}{
			"protocol": "http",
			"envoy_extra_static_clusters_json": `{
		  "name": "sds-cluster",
		  "type": "STRICT_DNS",
		  "connect_timeout": "2s",
		  "lb_policy": "ROUND_ROBIN",
		  "typed_extension_protocol_options": {
			"envoy.extensions.upstreams.http.v3.HttpProtocolOptions": {
			  "@type": "type.googleapis.com/envoy.extensions.upstreams.http.v3.HttpProtocolOptions",
			  "explicit_http_config": {
				"http2_protocol_options": {}
			  }
			}
		  },
		  "load_assignment": {
			"cluster_name": "sds-cluster",
			"endpoints": [
			  {
				"lb_endpoints": [
				  {
					"endpoint": {
					  "address": {
						"socket_address": {
						  "address": "sds-cluster.default.svc.cluster.local",
						  "port_value": 1234
						}
					  }
					}
				  }
				]
			  }
			]
		  }
		}`,
		},
	}, nil)
	require.NoError(t, err)

	_, _, err = consulClient.ConfigEntries().Set(&api.ServiceConfigEntry{
		Kind:     api.ServiceDefaults,
		Name:     "sds-cluster",
		Protocol: "grpc",
	}, nil)
	require.NoError(t, err)

	logger.Log(t, "creating backend services")
	k8s.DeployKustomize(t, k8sOptions, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")
	out, err = k8s.RunKubectlAndGetOutputE(t, k8sOptions, "apply", "-f", "../fixtures/cases/api-gateways/sds-httproute/static-server-override.yaml")
	require.NoError(t, err, out)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		_, _ = k8s.RunKubectlAndGetOutputE(t, k8sOptions, "delete", "-f", "../fixtures/cases/api-gateways/sds-httproute/static-server-override.yaml")
	})
	k8s.RunKubectl(t, k8sOptions, "wait", "--for=condition=available", "--timeout=5m", "deploy/static-server")
	k8s.RunKubectl(t, k8sOptions, "wait", "--for=condition=available", "--timeout=5m", "deploy/static-server-override")

	logger.Log(t, "creating backend services in default namespace")
	k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")
	out, err = k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "apply", "-f", "../fixtures/cases/api-gateways/sds-httproute/static-server-override.yaml")
	require.NoError(t, err, out)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		_, _ = k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "delete", "-f", "../fixtures/cases/api-gateways/sds-httproute/static-server-override.yaml")
	})
	k8s.RunKubectl(t, ctx.KubectlOptions(t), "wait", "--for=condition=available", "--timeout=5m", "deploy/static-server")
	k8s.RunKubectl(t, ctx.KubectlOptions(t), "wait", "--for=condition=available", "--timeout=5m", "deploy/static-server-override")
	k8s.DeployKustomize(t, k8sOptions, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/bases/static-client")

	logger.Log(t, "creating sds api-gateway resources")
	out, err = k8s.RunKubectlAndGetOutputE(t, k8sOptions, "apply", "-k", "../fixtures/cases/api-gateways/sds-httproute")
	require.NoError(t, err, out)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		_, _ = k8s.RunKubectlAndGetOutputE(t, k8sOptions, "delete", "-k", "../fixtures/cases/api-gateways/sds-httproute")
	})

	helpers.WaitForHTTPRouteWithRetry(t, k8sOptions, "http-route-no-override", "../fixtures/cases/api-gateways/sds-httproute")
	helpers.WaitForHTTPRouteWithRetry(t, k8sOptions, "http-route-with-override", "../fixtures/cases/api-gateways/sds-httproute")

	k8sClient := ctx.ControllerRuntimeClient(t)
	var gatewayAddress string
	retryCheck(t, 60, func(r *retry.R) {
		var gateway gwv1beta1.Gateway
		err = k8sClient.Get(context.Background(), types.NamespacedName{Name: "gateway", Namespace: k8sNamespace}, &gateway)
		require.NoError(r, err)

		checkStatusCondition(r, gateway.Status.Conditions, trueCondition("Accepted", "Accepted"))
		checkStatusCondition(r, gateway.Status.Conditions, trueCondition("ConsulAccepted", "Accepted"))
		require.Len(r, gateway.Status.Listeners, 1)
		require.EqualValues(r, int32(2), gateway.Status.Listeners[0].AttachedRoutes)
		checkStatusCondition(r, gateway.Status.Listeners[0].Conditions, trueCondition("Accepted", "Accepted"))
		checkStatusCondition(r, gateway.Status.Listeners[0].Conditions, trueCondition("ResolvedRefs", "ResolvedRefs"))
		require.NotEmpty(r, gateway.Status.Addresses)
		gatewayAddress = gateway.Status.Addresses[0].Value

		for _, routeName := range []string{"http-route-no-override", "http-route-with-override"} {
			var route gwv1beta1.HTTPRoute
			err = k8sClient.Get(context.Background(), types.NamespacedName{Name: routeName, Namespace: k8sNamespace}, &route)
			require.NoError(r, err)
			require.Len(r, route.Status.Parents, 1)
			checkStatusCondition(r, route.Status.Parents[0].Conditions, trueCondition("Accepted", "Accepted"))
			checkStatusCondition(r, route.Status.Parents[0].Conditions, trueCondition("ResolvedRefs", "ResolvedRefs"))
			checkStatusCondition(r, route.Status.Parents[0].Conditions, trueCondition("ConsulAccepted", "Accepted"))
		}
	})

	var routeNoOverride *api.HTTPRouteConfigEntry
	var routeWithOverride *api.HTTPRouteConfigEntry
	var consulQueryOpts *api.QueryOptions
	retryCheck(t, 60, func(r *retry.R) {
		entry, _, queryOpts, getErr := getConfigEntryWithFallback(consulClient, api.APIGateway, "gateway")
		require.NoError(r, getErr)
		_ = entry.(*api.APIGatewayConfigEntry)
		consulQueryOpts = queryOpts

		entry, _, _, getErr = getConfigEntryWithFallback(consulClient, api.HTTPRoute, "http-route-no-override")
		require.NoError(r, getErr)
		routeNoOverride = entry.(*api.HTTPRouteConfigEntry)

		entry, _, _, getErr = getConfigEntryWithFallback(consulClient, api.HTTPRoute, "http-route-with-override")
		require.NoError(r, getErr)
		routeWithOverride = entry.(*api.HTTPRouteConfigEntry)

		checkConsulStatusCondition(r, routeNoOverride.Status.Conditions, trueConsulCondition("Bound", "Bound"))
		checkConsulStatusCondition(r, routeWithOverride.Status.Conditions, trueConsulCondition("Bound", "Bound"))
	})

	gatewayRaw := getConfigEntryRaw(t, consulClient, api.APIGateway, "gateway", consulQueryOpts)
	routeNoOverrideRaw := getConfigEntryRaw(t, consulClient, api.HTTPRoute, "http-route-no-override", consulQueryOpts)
	routeWithOverrideRaw := getConfigEntryRaw(t, consulClient, api.HTTPRoute, "http-route-with-override", consulQueryOpts)

	listenerRaw := findGatewayListenerRaw(t, gatewayRaw, "https-sds")
	listenerSDS := getMapField(t, getMapField(t, listenerRaw, "TLS"), "SDS")
	require.Equal(t, "sds-cluster", getStringField(t, listenerSDS, "ClusterName"))
	require.Equal(t, "wildcard.ingress.consul", getStringField(t, listenerSDS, "CertResource"))

	for _, service := range collectHTTPRouteServicesRaw(t, routeNoOverrideRaw) {
		require.Equal(t, "default", getStringField(t, service, "Namespace"))
		require.Nilf(t, service["TLS"], "expected no service-level TLS override for %s", getStringField(t, service, "Name"))
	}

	overriddenService := findHTTPServiceByNameRaw(t, routeWithOverrideRaw, "static-server")
	require.Equal(t, "default", getStringField(t, overriddenService, "Namespace"))
	overrideSDS := getMapField(t, getMapField(t, overriddenService, "TLS"), "SDS")
	require.Equal(t, "sds-cluster", getStringField(t, overrideSDS, "ClusterName"))
	require.Equal(t, "foo.example.com", getStringField(t, overrideSDS, "CertResource"))

	inheritedService := findHTTPServiceByNameRaw(t, routeWithOverrideRaw, "static-server-override")
	require.Equal(t, "default", getStringField(t, inheritedService, "Namespace"))
	require.Nil(t, inheritedService["TLS"])

	for _, serviceName := range []string{"static-server", "static-server-override"} {
		intention := &api.ServiceIntentionsConfigEntry{
			Kind: api.ServiceIntentions,
			Name: serviceName,
			Sources: []*api.SourceIntention{{
				Name:   "gateway",
				Action: api.IntentionActionAllow,
			}},
		}
		_, _, err = consulClient.ConfigEntries().Set(intention, nil)
		require.NoError(t, err)
	}

	connectToGateway := fmt.Sprintf("a.example.test:8443:%s:8443", gatewayAddress)
	targetNoOverrideA := "https://a.example.test:8443/no-override/a"
	targetNoOverrideB := "https://a.example.test:8443/no-override/b"
	targetWithOverrideA := "https://a.example.test:8443/with-override/a"
	targetWithOverrideB := "https://a.example.test:8443/with-override/b"

	// Validate runtime routing via hostname/SNI for listener SDS defaults and per-backend SDS override.
	k8s.CheckStaticServerConnectionSuccessfulWithMessage(t, k8sOptions, StaticClientName, "hello world", "-k", "--connect-to", connectToGateway, targetNoOverrideA)
	k8s.CheckStaticServerConnectionSuccessfulWithMessage(t, k8sOptions, StaticClientName, "hello world override", "-k", "--connect-to", connectToGateway, targetNoOverrideB)
	k8s.CheckStaticServerConnectionSuccessfulWithMessage(t, k8sOptions, StaticClientName, "hello world", "-k", "--connect-to", connectToGateway, targetWithOverrideA)
	k8s.CheckStaticServerConnectionSuccessfulWithMessage(t, k8sOptions, StaticClientName, "hello world override", "-k", "--connect-to", connectToGateway, targetWithOverrideB)

	verifyGatewayDynamicActiveSecrets(t, k8sOptions, []string{"wildcard.ingress.consul", "foo.example.com"})
}

func verifyGatewayDynamicActiveSecrets(t *testing.T, options *terratestk8s.KubectlOptions, expectedCertResources []string) {
	t.Helper()

	retryCheck(t, 30, func(r *retry.R) {
		secretNames, err := getGatewayDynamicActiveSecretNames(t, options)
		require.NoError(r, err)
		for _, certResource := range expectedCertResources {
			require.Conditionf(r, func() bool {
				for _, secretName := range secretNames {
					if strings.Contains(secretName, certResource) {
						return true
					}
				}
				return false
			}, "expected dynamic active secret containing cert resource %q, got %v", certResource, secretNames)
		}
	})
}

func getGatewayDynamicActiveSecretNames(t *testing.T, options *terratestk8s.KubectlOptions) ([]string, error) {
	t.Helper()

	podName, err := k8s.RunKubectlAndGetOutputE(t, options, "get", "pod", "-l", "component=api-gateway", "-o", "jsonpath={.items[0].metadata.name}")
	if err != nil {
		return nil, err
	}
	podName = strings.TrimSpace(podName)
	if podName == "" {
		return nil, fmt.Errorf("api-gateway pod not found")
	}

	adminAddress := portforward.CreateTunnelToResourcePort(t, podName, 19000, options, terratestlogger.Discard)
	resp, err := http.Get(fmt.Sprintf("http://%s/config_dump?format=json", adminAddress))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var dump struct {
		Configs []json.RawMessage `json:"configs"`
	}
	if err := json.Unmarshal(body, &dump); err != nil {
		return nil, err
	}

	secretNames := make([]string, 0)
	for _, cfg := range dump.Configs {
		var typeOnly struct {
			Type string `json:"@type"`
		}
		if err := json.Unmarshal(cfg, &typeOnly); err != nil {
			continue
		}
		if typeOnly.Type != "type.googleapis.com/envoy.admin.v3.SecretsConfigDump" {
			continue
		}

		var secretsDump struct {
			DynamicActiveSecrets []struct {
				Name string `json:"name"`
			} `json:"dynamic_active_secrets"`
		}
		if err := json.Unmarshal(cfg, &secretsDump); err != nil {
			continue
		}

		for _, secret := range secretsDump.DynamicActiveSecrets {
			if secret.Name != "" {
				secretNames = append(secretNames, secret.Name)
			}
		}
	}

	if len(secretNames) == 0 {
		return nil, fmt.Errorf("no dynamic active secrets found in gateway config_dump")
	}

	return secretNames, nil
}

func getConfigEntryRaw(t *testing.T, client *api.Client, kind, name string, queryOpts *api.QueryOptions) map[string]interface{} {
	t.Helper()

	var payload map[string]interface{}
	_, err := client.Raw().Query(fmt.Sprintf("/v1/config/%s/%s", kind, name), &payload, queryOpts)
	require.NoError(t, err)

	// Re-marshal once to normalize `map[any]any` variants from decoder implementations.
	normalizedBytes, err := json.Marshal(payload)
	require.NoError(t, err)

	var normalized map[string]interface{}
	err = json.Unmarshal(normalizedBytes, &normalized)
	require.NoError(t, err)

	return normalized
}

func getConfigEntryWithFallback(client *api.Client, kind, name string) (api.ConfigEntry, *api.QueryMeta, *api.QueryOptions, error) {
	queryCandidates := []*api.QueryOptions{
		nil,
		{Namespace: "default"},
		{Partition: "default"},
		{Namespace: "default", Partition: "default"},
	}

	var errs []error
	for _, query := range queryCandidates {
		entry, meta, err := client.ConfigEntries().Get(kind, name, query)
		if err == nil {
			return entry, meta, query, nil
		}
		errs = append(errs, err)
	}

	return nil, nil, nil, fmt.Errorf("failed to read config entry %q/%q with query fallbacks: %v", kind, name, errs)
}

func findGatewayListenerRaw(t *testing.T, entry map[string]interface{}, name string) map[string]interface{} {
	t.Helper()

	listeners := getSliceField(t, entry, "Listeners")
	for _, listener := range listeners {
		listenerMap, ok := listener.(map[string]interface{})
		require.True(t, ok)
		if getStringField(t, listenerMap, "Name") == name {
			return listenerMap
		}
	}

	require.FailNowf(t, "listener not found", "listener %q not found", name)
	return nil
}

func findHTTPServiceByNameRaw(t *testing.T, route map[string]interface{}, name string) map[string]interface{} {
	t.Helper()

	for _, service := range collectHTTPRouteServicesRaw(t, route) {
		if getStringField(t, service, "Name") == name {
			return service
		}
	}

	require.FailNowf(t, "service not found", "service %q not found", name)
	return nil
}

func collectHTTPRouteServicesRaw(t *testing.T, route map[string]interface{}) []map[string]interface{} {
	t.Helper()

	services := make([]map[string]interface{}, 0)
	rules := getSliceField(t, route, "Rules")
	for _, rule := range rules {
		ruleMap, ok := rule.(map[string]interface{})
		require.True(t, ok)
		for _, service := range getSliceField(t, ruleMap, "Services") {
			serviceMap, ok := service.(map[string]interface{})
			require.True(t, ok)
			services = append(services, serviceMap)
		}
	}
	return services
}

func getMapField(t *testing.T, value map[string]interface{}, key string) map[string]interface{} {
	t.Helper()
	raw, ok := value[key]
	require.Truef(t, ok, "missing key %q", key)
	mapValue, ok := raw.(map[string]interface{})
	require.Truef(t, ok, "key %q is not an object", key)
	return mapValue
}

func getSliceField(t *testing.T, value map[string]interface{}, key string) []interface{} {
	t.Helper()
	raw, ok := value[key]
	require.Truef(t, ok, "missing key %q", key)
	sliceValue, ok := raw.([]interface{})
	require.Truef(t, ok, "key %q is not an array", key)
	return sliceValue
}

func getStringField(t *testing.T, value map[string]interface{}, key string) string {
	t.Helper()
	raw, ok := value[key]
	require.Truef(t, ok, "missing key %q", key)
	stringValue, ok := raw.(string)
	require.Truef(t, ok, "key %q is not a string", key)
	return stringValue
}
