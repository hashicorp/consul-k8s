// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package apigateway

import (
	"context"
	"encoding/base64"
	"fmt"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"testing"
	"time"

	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
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
	out, err = k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "apply", "-k", fixturePath)
	require.NoError(t, err, out)
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
	k8s.RunKubectl(t, ctx.KubectlOptions(t), "wait", "--for=condition=available", "--timeout=5m", fmt.Sprintf("deploy/%s", "static-server"))

	// On startup, the controller can take upwards of 1m to perform
	// leader election so we may need to wait a long time for
	// the reconcile loop to run (hence the 2m timeout here).
	var (
		gatewayAddress string
		httpRoute      gwv1beta1.HTTPRoute
	)

	counter := &retry.Counter{Count: 60, Wait: 2 * time.Second}
	retry.RunWith(counter, t, func(r *retry.R) {
		var gateway gwv1beta1.Gateway
		err = k8sClient.Get(context.Background(), types.NamespacedName{Name: "gateway", Namespace: "default"}, &gateway)
		require.NoError(r, err)

		//CHECK TO MAKE SURE EVERYTHING WAS SET UP CORECTLY BEFORE RUNNING TESTS
		require.Len(r, gateway.Finalizers, 1)
		require.EqualValues(r, gatewayFinalizer, gateway.Finalizers[0])

		// check our statuses
		checkStatusCondition(r, gateway.Status.Conditions, trueCondition("Accepted", "Accepted"))
		checkStatusCondition(r, gateway.Status.Conditions, trueCondition("ConsulAccepted", "Accepted"))
		require.Len(r, gateway.Status.Listeners, 2)

		// http route checks
		err = k8sClient.Get(context.Background(), types.NamespacedName{Name: "http-route", Namespace: "default"}, &httpRoute)
		require.NoError(r, err)

		require.EqualValues(r, int32(1), gateway.Status.Listeners[0].AttachedRoutes)
		checkStatusCondition(r, gateway.Status.Listeners[0].Conditions, trueCondition("Accepted", "Accepted"))
		checkStatusCondition(r, gateway.Status.Listeners[0].Conditions, falseCondition("Conflicted", "NoConflicts"))
		checkStatusCondition(r, gateway.Status.Listeners[0].Conditions, trueCondition("ResolvedRefs", "ResolvedRefs"))

		// check that we have an address to use
		require.Len(r, gateway.Status.Addresses, 2)
		// now we know we have an address, set it so we can use it
		gatewayAddress = gateway.Status.Addresses[0].Value

		// check our finalizers
		require.Len(r, httpRoute.Finalizers, 1)
		require.EqualValues(r, gatewayFinalizer, httpRoute.Finalizers[0])

		// check parent status
		require.Len(r, httpRoute.Status.Parents, 1)
		require.EqualValues(r, gatewayClassControllerName, httpRoute.Status.Parents[0].ControllerName)
		require.EqualValues(r, "gateway", httpRoute.Status.Parents[0].ParentRef.Name)
		checkStatusCondition(r, httpRoute.Status.Parents[0].Conditions, trueCondition("Accepted", "Accepted"))
		checkStatusCondition(r, httpRoute.Status.Parents[0].Conditions, trueCondition("ResolvedRefs", "ResolvedRefs"))
		checkStatusCondition(r, httpRoute.Status.Parents[0].Conditions, trueCondition("ConsulAccepted", "Accepted"))

	})

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
	targetHTTPAddress := fmt.Sprintf("http://%s:8080/v1", gatewayAddress)

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
