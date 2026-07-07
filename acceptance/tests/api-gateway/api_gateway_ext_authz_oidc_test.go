// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package apigateway

import (
	"context"
	"fmt"
	"net"
	"regexp"
	"strings"
	"testing"
	"time"

	terratestk8s "github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
)

// idTokenRegexp extracts the id_token from a Dex token endpoint JSON response.
var idTokenRegexp = regexp.MustCompile(`"id_token"\s*:\s*"([^"]+)"`)

// TestAPIGateway_ExtAuthz_OIDC exercises the API gateway external authorization
// (ext_authz) feature end to end against a real OIDC flow, mirroring the
// hack/api-gw-ext-authz-kind "oidc" scenario:
//
//   - Dex is a minimal OIDC identity provider with a static user
//     (alice@example.com / "password").
//   - oauth2-proxy is the HTTP ext_authz backend. The gateway's builtin/ext-authz
//     Envoy extension calls oauth2-proxy for every request; oauth2-proxy returns
//     200 (allow) for a valid Dex bearer token and 302 (deny) otherwise.
//   - static-server is the protected mesh upstream.
//
// The test mints an OIDC id_token from Dex headlessly (password grant) and then
// drives traffic through the gateway, asserting:
//
//   - GET / with no token           -> 302 (denied; oauth2-proxy redirect)
//   - GET / with an invalid token   -> 302 (denied)
//   - GET / with a valid Dex token  -> 200, and the authenticated identity is
//     surfaced to the client as the x-auth-request-email response header
//     (AllowedClientHeadersOnSuccess).
//
// The api-gateway ext_authz feature (ProxyType api-gateway) is Consul Enterprise
// only, so this test is skipped unless an enterprise license is configured.
func TestAPIGateway_ExtAuthz_OIDC(t *testing.T) {
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

	fixturePath := "../fixtures/cases/api-gateways/ext-authz-oidc"

	logger.Log(t, "creating oidc ext-authz api-gateway resources")
	out, err := k8s.RunKubectlAndGetOutputE(t, k8sOptions, "apply", "-k", fixturePath)
	require.NoError(t, err, out)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		_, _ = k8s.RunKubectlAndGetOutputE(t, k8sOptions, "delete", "-k", fixturePath)
	})

	// The test client (static-client) is used to mint an OIDC token from Dex and
	// to drive requests through the gateway from inside the cluster.
	logger.Log(t, "creating static-client pod")
	k8s.DeployKustomize(t, k8sOptions, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/bases/static-client")

	// Wait for all backing workloads to be ready.
	for _, deployment := range []string{"static-server", "dex", "oauth2-proxy", StaticClientName} {
		k8s.RunKubectl(t, k8sOptions, "wait", "--for=condition=available", "--timeout=5m", "deploy/"+deployment)
	}

	// Wait for the route to be accepted and synced to Consul (k8s side).
	helpers.WaitForHTTPRouteWithRetry(t, k8sOptions, "static-server-route", fixturePath)

	// Ensure static-server is registered in the Consul catalog before asserting
	// route status. The route only reaches Accepted once its backendRef resolves
	// to a registered upstream; otherwise Consul reports NoUpstreamServicesTargeted.
	waitForConsulServiceRegistered(t, consulClient, "static-server")

	// Wait for the gateway to be accepted and to expose an address we can route to.
	k8sClient := ctx.ControllerRuntimeClient(t)
	var gatewayAddress string
	retryCheckWithWait(t, 120, 2*time.Second, func(r *retry.R) {
		var gateway gwv1beta1.Gateway
		err = k8sClient.Get(context.Background(), types.NamespacedName{Name: "gateway", Namespace: k8sNamespace}, &gateway)
		require.NoError(r, err)
		checkStatusCondition(r, gateway.Status.Conditions, trueCondition("Accepted", "Accepted"))
		checkStatusCondition(r, gateway.Status.Conditions, trueCondition("ConsulAccepted", "Accepted"))
		require.Len(r, gateway.Status.Addresses, 1)
		gatewayAddress = gateway.Status.Addresses[0].Value

		var route gwv1beta1.HTTPRoute
		err = k8sClient.Get(context.Background(), types.NamespacedName{Name: "static-server-route", Namespace: k8sNamespace}, &route)
		require.NoError(r, err)
		require.Len(r, route.Status.Parents, 1)
		checkStatusCondition(r, route.Status.Parents[0].Conditions, trueCondition("Accepted", "Accepted"))
		checkStatusCondition(r, route.Status.Parents[0].Conditions, trueCondition("ResolvedRefs", "ResolvedRefs"))
		checkStatusCondition(r, route.Status.Parents[0].Conditions, trueCondition("ConsulAccepted", "Accepted"))
	})

	// The gateway listens on port 8080 (see gateway.yaml). We reach it from the
	// static-client pod via kubectl exec so we don't need a route into the
	// cluster from the test machine.
	gatewayURL := fmt.Sprintf("http://%s/", net.JoinHostPort(gatewayAddress, "8080"))

	// Mint an OIDC id_token from Dex using the OAuth2 password grant.
	logger.Log(t, "minting an OIDC id_token from Dex")
	token := mintDexToken(t, k8sOptions)

	// Requests without a valid token are denied. oauth2-proxy responds with a 302
	// redirect to the Dex login, which Envoy forwards as the deny response.
	// Requests with a valid token are allowed through to static-server, and the
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

// mintDexToken execs into the static-client pod and requests an OIDC id_token
// from Dex using the OAuth2 password grant, returning the id_token.
func mintDexToken(t *testing.T, options *terratestk8s.KubectlOptions) string {
	t.Helper()

	var token string
	retry.RunWith(&retry.Counter{Count: 30, Wait: 5 * time.Second}, t, func(r *retry.R) {
		curl := "curl -s --connect-timeout 5 -u oauth2-proxy:oauth2-proxy-secret " +
			"-d grant_type=password -d username=alice@example.com -d password=password " +
			"-d scope=\"openid email profile\" http://dex:5556/dex/token"
		output, err := k8s.RunKubectlAndGetOutputE(r, options, "exec", "deploy/"+StaticClientName, "-c", StaticClientName, "--", "sh", "-c", curl)
		require.NoError(r, err, output)

		matches := idTokenRegexp.FindStringSubmatch(output)
		require.Lenf(r, matches, 2, "could not find id_token in Dex token response: %s", output)
		token = matches[1]
	})
	require.NotEmpty(t, token)
	return token
}

// curlGateway execs a curl against the gateway from the static-client pod and
// returns the combined output (response headers + a trailing HTTP_CODE marker).
func curlGateway(r *retry.R, options *terratestk8s.KubectlOptions, url string, extraArgs ...string) string {
	args := []string{"exec", "deploy/" + StaticClientName, "-c", StaticClientName, "--",
		"curl", "-s", "--connect-timeout", "5", "-o", "/dev/null", "-D", "-", "-w", "HTTP_CODE=%{http_code}"}
	args = append(args, extraArgs...)
	args = append(args, url)
	output, err := k8s.RunKubectlAndGetOutputE(r, options, args...)
	require.NoError(r, err, output)
	return output
}

// requireGatewayStatus asserts that a request through the gateway returns the
// expected HTTP status code.
func requireGatewayStatus(r *retry.R, options *terratestk8s.KubectlOptions, url, wantCode string, extraArgs ...string) {
	output := curlGateway(r, options, url, extraArgs...)
	require.Containsf(r, output, "HTTP_CODE="+wantCode, "expected HTTP %s, got: %s", wantCode, output)
}

// requireGatewayAllowed asserts that a request carrying a valid OIDC bearer token
// is allowed (200) and that the authenticated identity is surfaced to the client
// via the x-auth-request-email response header.
func requireGatewayAllowed(r *retry.R, options *terratestk8s.KubectlOptions, url, token string) {
	output := curlGateway(r, options, url, "-H", "Authorization: Bearer "+token)
	require.Containsf(r, output, "HTTP_CODE=200", "expected HTTP 200, got: %s", output)
	require.Containsf(r, strings.ToLower(output), "x-auth-request-email: alice@example.com",
		"expected injected identity header x-auth-request-email: alice@example.com, got: %s", output)
}
