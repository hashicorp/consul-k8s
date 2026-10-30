// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package apigateway

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
)

// TestAPIGateway_ExtAuthz_OIDC_Mesh is the within-mesh counterpart of
// TestAPIGateway_ExtAuthz_OIDC. It exercises the API gateway external
// authorization (ext_authz) feature against a real OIDC flow where the HTTP auth
// backend (oauth2-proxy) is itself a mesh service.
//
// Unlike the out-of-mesh scenario (which reaches oauth2-proxy via
// HttpService.Target.URI), here oauth2-proxy runs with a Connect sidecar and the
// gateway dials it over mTLS via HttpService.Target.Service. This requires an
// intention allowing gateway -> oauth2-proxy. Everything else matches the
// out-of-mesh flow:
//
//   - Dex is a minimal OIDC identity provider with a static user
//     (alice@example.com / "password").
//   - oauth2-proxy returns 200 (allow) for a valid Dex bearer token and 302
//     (deny) otherwise.
//   - static-server is the protected mesh upstream.
//
// The test mints an OIDC id_token from Dex headlessly (password grant) and then
// drives traffic through the gateway, asserting:
//
//   - oauth2-proxy is registered in the Consul catalog (i.e. it is a mesh
//     service, proving the ext_authz Check call traverses the mesh).
//   - GET / with no token           -> 302 (denied; oauth2-proxy redirect)
//   - GET / with an invalid token   -> 302 (denied)
//   - GET / with a valid Dex token  -> 200, and the authenticated identity is
//     surfaced to the client as the x-auth-request-email response header.
//
// The api-gateway ext_authz feature (ProxyType api-gateway) is Consul Enterprise
// only, so this test is skipped unless an enterprise license is configured.
func TestAPIGateway_ExtAuthz_OIDC_Mesh(t *testing.T) {
	cfg := suite.Config()
	ctx := suite.Environment().DefaultContext(t)

	helmValues := map[string]string{
		"connectInject.enabled":        "true",
		"global.acls.manageSystemACLs": "true",
		"global.tls.enabled":           "true",
	}

	// Local-dev convenience: run against locally-built Enterprise + control-plane
	// images and a license file, mirroring hack/api-gw-ext-authz-kind. Opt-in via
	// EXT_AUTHZ_LOCAL_DEV=true so CI is unaffected.
	configureLocalExtAuthzDev(t, cfg, helmValues)

	skipUnlessEnterpriseLicenseConfigured(t)

	k8sNamespace, k8sOptions := createNamespace(t, ctx, cfg)

	releaseName := helpers.RandomName()
	consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)
	consulCluster.Create(t)

	consulClient, _ := consulCluster.SetupConsulClient(t, true)

	logger.Log(t, "setting global protocol to http")
	_, _, err := consulClient.ConfigEntries().Set(&api.ProxyConfigEntry{
		Kind: api.ProxyDefaults,
		Name: api.ProxyConfigGlobal,
		Config: map[string]interface{}{
			"protocol": "http",
		},
	}, nil)
	require.NoError(t, err)

	fixturePath := "../fixtures/cases/api-gateways/ext-authz-oidc-mesh"

	logger.Log(t, "creating within-mesh oidc ext-authz api-gateway resources")
	out, err := k8s.RunKubectlAndGetOutputE(t, k8sOptions, "apply", "-k", fixturePath)
	require.NoError(t, err, out)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		_, _ = k8s.RunKubectlAndGetOutputE(t, k8sOptions, "delete", "-k", fixturePath)
	})

	// The test client (static-client) is used to mint an OIDC token from Dex and
	// to drive requests through the gateway from inside the cluster.
	logger.Log(t, "creating static-client pod")
	k8s.DeployKustomize(t, k8sOptions, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/bases/static-client")

	// Wait for all backing workloads to be ready. oauth2-proxy now runs with a
	// Connect sidecar, so it is part of this set.
	for _, deployment := range []string{"static-server", "dex", "oauth2-proxy", StaticClientName} {
		k8s.RunKubectl(t, k8sOptions, "wait", "--for=condition=available", "--timeout=5m", "deploy/"+deployment)
	}

	// Create the HTTPRoute only after static-server is Ready and registered in the
	// Consul catalog. The route's kustomization intentionally omits httproute.yaml
	// so the route is applied here, last. If the route is reconciled while
	// static-server has no Ready endpoints, consul-k8s writes the Consul http-route
	// with zero upstreams and Consul reports NoUpstreamServicesTargeted; recovery
	// can exceed the assertion window under the heavier within-mesh scenario.
	// Creating the route after the backend is ready guarantees its first Consul
	// write already targets the upstream.
	waitForConsulServiceRegistered(t, consulClient, "static-server")

	logger.Log(t, "creating the httproute now that static-server is registered")
	routeManifest := fixturePath + "/httproute.yaml"
	out, err = k8s.RunKubectlAndGetOutputE(t, k8sOptions, "apply", "-f", routeManifest)
	require.NoError(t, err, out)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		_, _ = k8s.RunKubectlAndGetOutputE(t, k8sOptions, "delete", "-f", routeManifest, "--ignore-not-found=true")
	})

	// Wait for the route to be created and synced to Consul (k8s side).
	helpers.WaitForHTTPRouteWithRetry(t, k8sOptions, "static-server-route", fixturePath)

	// Wait for the gateway to be accepted and to expose an address we can route to.
	k8sClient := ctx.ControllerRuntimeClient(t)
	var gatewayAddress string
	retryCheckWithWait(t, 120, 2*time.Second, func(r *retry.R) {
		var gateway gwv1.Gateway
		err = k8sClient.Get(context.Background(), types.NamespacedName{Name: "gateway", Namespace: k8sNamespace}, &gateway)
		require.NoError(r, err)
		checkStatusCondition(r, gateway.Status.Conditions, trueCondition("Accepted", "Accepted"))
		checkStatusCondition(r, gateway.Status.Conditions, trueCondition("ConsulAccepted", "Accepted"))
		require.Len(r, gateway.Status.Addresses, 1)
		gatewayAddress = gateway.Status.Addresses[0].Value

		var route gwv1.HTTPRoute
		err = k8sClient.Get(context.Background(), types.NamespacedName{Name: "static-server-route", Namespace: k8sNamespace}, &route)
		require.NoError(r, err)
		require.Len(r, route.Status.Parents, 1)
		checkStatusCondition(r, route.Status.Parents[0].Conditions, trueCondition("Accepted", "Accepted"))
		checkStatusCondition(r, route.Status.Parents[0].Conditions, trueCondition("ResolvedRefs", "ResolvedRefs"))
		checkStatusCondition(r, route.Status.Parents[0].Conditions, trueCondition("ConsulAccepted", "Accepted"))
	})

	// Prove that the ext_authz backend really is in the mesh: a connect-injected
	// oauth2-proxy registers itself (and its sidecar proxy) in the Consul catalog.
	// In the out-of-mesh scenario oauth2-proxy is a plain Service and would not
	// appear here.
	retryCheckWithWait(t, 60, 2*time.Second, func(r *retry.R) {
		services, _, svcErr := consulClient.Catalog().Service("oauth2-proxy", "", nil)
		require.NoError(r, svcErr)
		require.NotEmpty(r, services, "expected oauth2-proxy to be registered as a mesh service in Consul")

		proxies, _, proxyErr := consulClient.Catalog().Service("oauth2-proxy-sidecar-proxy", "", nil)
		require.NoError(r, proxyErr)
		require.NotEmpty(r, proxies, "expected oauth2-proxy to have a mesh sidecar proxy registered in Consul")
	})

	// The gateway listens on port 8080 (see gateway.yaml). We reach it from the
	// static-client pod via kubectl exec so we don't need a route into the
	// cluster from the test machine.
	gatewayURL := fmt.Sprintf("http://%s/", net.JoinHostPort(gatewayAddress, "8080"))

	// Mint an OIDC id_token from Dex using the OAuth2 password grant.
	logger.Log(t, "minting an OIDC id_token from Dex")
	token := mintDexToken(t, k8sOptions)

	// Requests without a valid token are denied (oauth2-proxy 302). Requests with
	// a valid token are allowed through to static-server over the mesh, and the
	// authenticated identity is surfaced to the client. We retry to absorb Envoy
	// config propagation of the ext_authz extension after apply.
	retryCheckWithWait(t, 60, 5*time.Second, func(r *retry.R) {
		logger.Log(t, "GET / with no token should be denied (302)")
		requireGatewayStatus(r, k8sOptions, gatewayURL, "302")

		logger.Log(t, "GET / with an invalid token should be denied (302)")
		requireGatewayStatus(r, k8sOptions, gatewayURL, "302", "-H", "Authorization: Bearer not-a-valid-token")

		logger.Log(t, "GET / with a valid Dex token should be allowed (200) and surface the identity")
		requireGatewayAllowed(r, k8sOptions, gatewayURL, token)
	})
}
