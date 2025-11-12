// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package apigateway

import (
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"

	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

// Enabled everything possible, see if anything breaks.
func TestAPIGateway_KitchenSink(t *testing.T) {
	ctx := suite.Environment().DefaultContext(t)
	cfg := suite.Config()

	runWithEnterpriseOnlyFeatures := cfg.EnableEnterprise

	serverHelmValues := map[string]string{
		"global.acls.manageSystemACLs": "true",
		"global.tls.enabled":           "true",

		// Don't install injector, controller and cni on this cluster so that it's not installed twice.
		"connectInject.enabled":     "false",
		"connectInject.cni.enabled": "false",
	}
	serverReleaseName := helpers.RandomName()
	consulServerCluster := consul.NewHelmCluster(t, serverHelmValues, ctx, cfg, serverReleaseName)
	consulServerCluster.Create(t)

	helmValues := map[string]string{
		"server.enabled": "false",
		"connectInject.consulNamespaces.mirroringK8S": "true",
		"global.acls.manageSystemACLs":                "true",
		"global.tls.enabled":                          "true",
		"global.logLevel":                             "trace",
		"externalServers.enabled":                     "true",
		"externalServers.hosts[0]":                    fmt.Sprintf("%s-consul-server", serverReleaseName),
		"externalServers.httpsPort":                   "8501",
		"global.tls.caCert.secretName":                fmt.Sprintf("%s-consul-ca-cert", serverReleaseName),
		"global.tls.caCert.secretKey":                 "tls.crt",
		"global.acls.bootstrapToken.secretName":       fmt.Sprintf("%s-consul-bootstrap-acl-token", serverReleaseName),
		"global.acls.bootstrapToken.secretKey":        "token",
	}

	releaseName := helpers.RandomName()
	consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)
	consulCluster.SkipCheckForPreviousInstallations = true

	consulCluster.Create(t)

	// Override the default proxy config settings for this test
	consulClient, _ := consulCluster.SetupConsulClient(t, true, serverReleaseName)
	logger.Log(t, "have consul client")

	retry.Run(t, func(r *retry.R) {
		peers, err := consulClient.Status().Peers()
		require.NoError(r, err)
		require.Len(r, peers, 1)
	})
	_, _, err := consulClient.ConfigEntries().Set(&api.ProxyConfigEntry{
		Kind: api.ProxyDefaults,
		Name: api.ProxyConfigGlobal,
		Config: map[string]interface{}{
			"protocol": "http",
		},
	}, nil)
	require.NoError(t, err)
	logger.Log(t, "set consul config entry")

	logger.Log(t, "creating other namespace")
	out, err := k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "create", "namespace", "other")
	require.NoError(t, err, out)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		// Ignore errors here because if the test ran as expected
		// the custom resources will have been deleted.
		k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "delete", "namespace", "other")
	})

	k8sClient := ctx.ControllerRuntimeClient(t)

	logger.Log(t, "creating api-gateway resources")
	fixturePath := "../fixtures/cases/api-gateways/kitchen-sink"
	if runWithEnterpriseOnlyFeatures {
		fixturePath += "-ent"
	}

	// Use more frequent retries for resource creation
	applyCounter := &retry.Counter{Count: 30, Wait: 5 * time.Second}
	logger.Log(t, "applying api-gateway resources")
	retry.RunWith(applyCounter, t, func(r *retry.R) {
		out, err = k8s.RunKubectlAndGetOutputE(r, ctx.KubectlOptions(r), "apply", "-k", fixturePath)
		require.NoError(r, err, out)
	})

	k8s.RunKubectl(t, ctx.KubectlOptions(t), "wait", "--for=condition=available", "--timeout=5m", fmt.Sprintf("deploy/%s", "static-server"))

	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		// Ignore errors here because if the test ran as expected
		// the custom resources will have been deleted.
		k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "delete", "-k", fixturePath)
	})

	// Create certificate secret, we do this separately since
	// applying the secret will make an invalid certificate that breaks other tests
	logger.Log(t, "creating certificate secret")
	out, err = k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "apply", "-f", "../fixtures/bases/api-gateway/certificate.yaml")
	require.NoError(t, err, out)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		// Ignore errors here because if the test ran as expected
		// the custom resources will have been deleted.
		k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "delete", "-f", "../fixtures/bases/api-gateway/certificate.yaml")
	})

	// patch certificate with data
	logger.Log(t, "patching certificate secret with generated data")
	certificate := generateCertificate(t, nil, "gateway.test.local")
	k8s.RunKubectl(t, ctx.KubectlOptions(t), "patch", "secret", "certificate", "-p", fmt.Sprintf(`{"data":{"tls.crt":"%s","tls.key":"%s"}}`, base64.StdEncoding.EncodeToString(certificate.CertPEM), base64.StdEncoding.EncodeToString(certificate.PrivateKeyPEM)), "--type=merge")

	// Create static server and static client
	logger.Log(t, "creating static-client pod")
	k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/bases/static-client")

	// On startup, the controller can take upwards of 1m to perform
	// leader election so we may need to wait a long time for
	// the reconcile loop to run (hence the 2m timeout here).
	var gatewayAddress string

	logger.Log(t, "waiting for gateway and httproute to be ready")

	// Wait for Gateway to be ready
	gatewayAddress = waitForGatewayReady(t, ctx, k8sClient, "gateway", "default", fixturePath, applyCounter)

	// Wait for HTTPRoute to be ready
	waitForHTTPRouteReady(t, ctx, k8sClient, "http-route", "default", fixturePath, applyCounter)

	// GENERAL Asserts- test that assets were created as expected
	entry, _, err := consulClient.ConfigEntries().Get(api.APIGateway, "gateway", nil)
	require.NoError(t, err)
	gateway := entry.(*api.APIGatewayConfigEntry)

	entry, _, err = consulClient.ConfigEntries().Get(api.HTTPRoute, "http-route", nil)
	require.NoError(t, err)
	consulHTTPRoute := entry.(*api.HTTPRouteConfigEntry)

	// now check the gateway status conditions
	checkConsulStatusCondition(t, gateway.Status.Conditions, trueConsulCondition("Accepted", "Accepted"))

	// and the route status conditions
	checkConsulStatusCondition(t, consulHTTPRoute.Status.Conditions, trueConsulCondition("Bound", "Bound"))

	// finally we check that we can actually route to the service(s) via the gateway
	k8sOptions := ctx.KubectlOptions(t)
	targetHTTPAddress := fmt.Sprintf("http://%s/v1", net.JoinHostPort(gatewayAddress, "8080"))

	// Now we create the allow intention.
	_, _, err = consulClient.ConfigEntries().Set(&api.ServiceIntentionsConfigEntry{
		Kind: api.ServiceIntentions,
		Name: "static-server",
		Sources: []*api.SourceIntention{
			{
				Name:   "gateway",
				Action: api.IntentionActionAllow,
			},
		},
	}, nil)
	require.NoError(t, err)

	_, _, err = consulClient.ConfigEntries().Set(&api.ServiceIntentionsConfigEntry{
		Kind: api.ServiceIntentions,
		Name: "static-server-protected",
		Sources: []*api.SourceIntention{
			{
				Name:   "gateway",
				Action: api.IntentionActionAllow,
			},
		},
	}, nil)
	require.NoError(t, err)

	//asserts only valid when running with enterprise
	if runWithEnterpriseOnlyFeatures {
		//JWT Related Asserts
		// should fail because we're missing JWT
		logger.Log(t, "trying calls to api gateway /admin should fail without JWT token")
		k8s.CheckStaticServerHTTPConnectionFailing(t, k8sOptions, StaticClientName, targetHTTPAddress)

		// will succeed because we use the token with the correct role and the correct issuer
		logger.Log(t, "trying calls to api gateway /admin should succeed with JWT token with correct role")
		k8s.CheckStaticServerConnectionSuccessful(t, k8sOptions, StaticClientName, "-H", fmt.Sprintf("Authorization: Bearer %s", doctorToken), targetHTTPAddress)
	} else {
		// Test that we can make a call to the api gateway
		logger.Log(t, "trying calls to api gateway http")
		k8s.CheckStaticServerConnectionSuccessful(t, k8sOptions, StaticClientName, targetHTTPAddress)
	}
}

// checkGatewayReady checks if the Gateway resource is ready using existing retry logic.
func checkGatewayReady(t *testing.T, k8sClient client.Client, gatewayName, namespace string) (bool, string) {
	var success bool
	var gatewayAddress string
	gatewayCounter := &retry.Counter{Count: 10, Wait: 6 * time.Second}

	// Use a loop instead of retry.RunWith to avoid runtime.Goexit() issues when require fails.
	for i := 0; i < gatewayCounter.Count; i++ {
		var gateway gwv1beta1.Gateway
		err := k8sClient.Get(context.Background(), types.NamespacedName{Name: gatewayName, Namespace: namespace}, &gateway)
		if err != nil {
			logger.Log(t, fmt.Sprintf("Gateway check attempt %d: failed to get gateway: %v", i+1, err))
			time.Sleep(gatewayCounter.Wait)
			continue
		}

		// Check all conditions, if any fail we'll continue to next attempt.
		if len(gateway.Finalizers) != 1 {
			logger.Log(t, fmt.Sprintf("Gateway check attempt %d: wrong number of finalizers", i+1))
			time.Sleep(gatewayCounter.Wait)
			continue
		}

		if gateway.Finalizers[0] != gatewayFinalizer {
			logger.Log(t, fmt.Sprintf("Gateway check attempt %d: wrong finalizer", i+1))
			time.Sleep(gatewayCounter.Wait)
			continue
		}

		// Check status conditions.
		if !helpers.HasStatusCondition(gateway.Status.Conditions, trueCondition("Accepted", "Accepted")) ||
			!helpers.HasStatusCondition(gateway.Status.Conditions, trueCondition("ConsulAccepted", "Accepted")) {
			logger.Log(t, fmt.Sprintf("Gateway check attempt %d: missing required status conditions", i+1))
			time.Sleep(gatewayCounter.Wait)
			continue
		}

		if len(gateway.Status.Listeners) != 2 {
			logger.Log(t, fmt.Sprintf("Gateway check attempt %d: wrong number of listeners", i+1))
			time.Sleep(gatewayCounter.Wait)
			continue
		}

		if gateway.Status.Listeners[0].AttachedRoutes != 1 {
			logger.Log(t, fmt.Sprintf("Gateway check attempt %d: wrong number of attached routes", i+1))
			time.Sleep(gatewayCounter.Wait)
			continue
		}

		// Check listener conditions.
		if !helpers.HasStatusCondition(gateway.Status.Listeners[0].Conditions, trueCondition("Accepted", "Accepted")) ||
			!helpers.HasStatusCondition(gateway.Status.Listeners[0].Conditions, falseCondition("Conflicted", "NoConflicts")) ||
			!helpers.HasStatusCondition(gateway.Status.Listeners[0].Conditions, trueCondition("ResolvedRefs", "ResolvedRefs")) {
			logger.Log(t, fmt.Sprintf("Gateway check attempt %d: missing required listener conditions", i+1))
			time.Sleep(gatewayCounter.Wait)
			continue
		}

		// Check that we have an address to use.
		if len(gateway.Status.Addresses) < 2 {
			logger.Log(t, fmt.Sprintf("Gateway check attempt %d: not enough addresses", i+1))
			time.Sleep(gatewayCounter.Wait)
			continue
		}

		// All checks passed.
		gatewayAddress = gateway.Status.Addresses[0].Value
		success = true
		break
	}

	if success {
		logger.Log(t, "Gateway check succeeded")
	} else {
		logger.Log(t, "Gateway check failed after all attempts")
	}

	return success, gatewayAddress
}

// waitForGatewayReady waits for Gateway to be ready with recreation attempts.
func waitForGatewayReady(t *testing.T, ctx environment.TestContext, k8sClient client.Client, gatewayName, namespace, fixturePath string, applyCounter *retry.Counter) string {
	maxRetries := 5

	for attempt := range maxRetries {
		if attempt > 0 {
			logger.Log(t, fmt.Sprintf("Attempt %d: Recreating Gateway resource", attempt+1))

			// Delete the Gateway resource
			k8s.RunKubectl(t, ctx.KubectlOptions(t), "delete", "gateway", gatewayName, "--ignore-not-found=true")

			// Wait for deletion
			time.Sleep(10 * time.Second)

			// Recreate the Gateway by reapplying the resources
			retry.RunWith(applyCounter, t, func(r *retry.R) {
				out, err := k8s.RunKubectlAndGetOutputE(r, ctx.KubectlOptions(r), "apply", "-k", fixturePath)
				require.NoError(r, err, out)
			})

			// Wait for resource creation
			time.Sleep(5 * time.Second)
		}

		success, gatewayAddress := checkGatewayReady(t, k8sClient, gatewayName, namespace)
		if success {
			logger.Log(t, "Gateway is ready")
			return gatewayAddress
		}

		if attempt < maxRetries-1 {
			logger.Log(t, "Gateway failed to become ready, will recreate")
		}
	}

	require.Fail(t, fmt.Sprintf("Gateway failed to become ready after %d attempts", maxRetries))
	return ""
}

// checkHTTPRouteReady checks if the HTTPRoute resource is ready using existing retry logic.
func checkHTTPRouteReady(t *testing.T, k8sClient client.Client, routeName, namespace string) bool {
	var success bool
	var httpRoute gwv1beta1.HTTPRoute
	httpRouteCounter := &retry.Counter{Count: 10, Wait: 6 * time.Second}

	// Use a loop instead of retry.RunWith to avoid runtime.Goexit() issues when require fails.
	for i := 0; i < httpRouteCounter.Count; i++ {
		err := k8sClient.Get(context.Background(), types.NamespacedName{Name: routeName, Namespace: namespace}, &httpRoute)
		if err != nil {
			logger.Log(t, fmt.Sprintf("HTTPRoute check attempt %d: failed to get httproute: %v", i+1, err))
			time.Sleep(httpRouteCounter.Wait)
			continue
		}

		// Check all conditions, if any fail we'll continue to next attempt
		if len(httpRoute.Finalizers) != 1 {
			logger.Log(t, fmt.Sprintf("HTTPRoute check attempt %d: wrong number of finalizers", i+1))
			time.Sleep(httpRouteCounter.Wait)
			continue
		}

		if httpRoute.Finalizers[0] != gatewayFinalizer {
			logger.Log(t, fmt.Sprintf("HTTPRoute check attempt %d: wrong finalizer", i+1))
			time.Sleep(httpRouteCounter.Wait)
			continue
		}

		// Check parent status
		if len(httpRoute.Status.Parents) != 1 {
			logger.Log(t, fmt.Sprintf("HTTPRoute check attempt %d: wrong number of parents", i+1))
			time.Sleep(httpRouteCounter.Wait)
			continue
		}

		if string(httpRoute.Status.Parents[0].ControllerName) != gatewayClassControllerName {
			logger.Log(t, fmt.Sprintf("HTTPRoute check attempt %d: wrong controller name", i+1))
			time.Sleep(httpRouteCounter.Wait)
			continue
		}

		if string(httpRoute.Status.Parents[0].ParentRef.Name) != "gateway" {
			logger.Log(t, fmt.Sprintf("HTTPRoute check attempt %d: wrong parent ref name", i+1))
			time.Sleep(httpRouteCounter.Wait)
			continue
		}

		// Check parent conditions
		if !helpers.HasStatusCondition(httpRoute.Status.Parents[0].Conditions, trueCondition("Accepted", "Accepted")) ||
			!helpers.HasStatusCondition(httpRoute.Status.Parents[0].Conditions, trueCondition("ResolvedRefs", "ResolvedRefs")) ||
			!helpers.HasStatusCondition(httpRoute.Status.Parents[0].Conditions, trueCondition("ConsulAccepted", "Accepted")) {
			logger.Log(t, fmt.Sprintf("HTTPRoute check attempt %d: missing required parent conditions", i+1))
			time.Sleep(httpRouteCounter.Wait)
			continue
		}

		// All checks passed
		success = true
		break
	}

	if success {
		logger.Log(t, "HTTPRoute check succeeded")
	} else {
		logger.Log(t, "HTTPRoute check failed after all attempts")
	}

	return success
}

// waitForHTTPRouteReady waits for HTTPRoute to be ready with recreation attempts
func waitForHTTPRouteReady(t *testing.T, ctx environment.TestContext, k8sClient client.Client, routeName, namespace, fixturePath string, applyCounter *retry.Counter) {
	maxRetries := 5

	for attempt := range maxRetries {
		if attempt > 0 {
			logger.Log(t, fmt.Sprintf("Attempt %d: Recreating HTTPRoute resource", attempt+1))

			// Delete the HTTPRoute resource
			k8s.RunKubectl(t, ctx.KubectlOptions(t), "delete", "httproute", routeName, "--ignore-not-found=true")

			// Wait for deletion
			time.Sleep(10 * time.Second)

			// Recreate the HTTPRoute by reapplying the resources
			retry.RunWith(applyCounter, t, func(r *retry.R) {
				out, err := k8s.RunKubectlAndGetOutputE(r, ctx.KubectlOptions(r), "apply", "-k", fixturePath)
				require.NoError(r, err, out)
			})

			// Wait for resource creation
			time.Sleep(5 * time.Second)
		}

		if checkHTTPRouteReady(t, k8sClient, routeName, namespace) {
			logger.Log(t, "HTTPRoute is ready")
			return
		}

		if attempt < maxRetries-1 {
			logger.Log(t, "HTTPRoute failed to become ready, will recreate")
		}
	}

	require.Fail(t, fmt.Sprintf("HTTPRoute failed to become ready after %d attempts", maxRetries))
}
