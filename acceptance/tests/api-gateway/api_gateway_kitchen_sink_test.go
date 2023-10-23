// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package apigateway

import (
	"context"
	"encoding/base64"
	"fmt"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
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

// Enabled everything possible, see if anything breaks
func TestAPIGateway_KitchenSink(t *testing.T) {
	ctx := suite.Environment().DefaultContext(t)
	cfg := suite.Config()

	if !cfg.EnableEnterprise {
		t.Skipf("skipping this test because -enable-enterprise is not set")
	}

	helmValues := map[string]string{
		"connectInject.enabled":                       "true",
		"connectInject.consulNamespaces.mirroringK8S": "true",
		"global.acls.manageSystemACLs":                "true",
		"global.tls.enabled":                          "true",
		"global.logLevel":                             "trace",
	}

	releaseName := helpers.RandomName()
	consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)

	consulCluster.Create(t)

	// this is necesary when running tests with ACLs enabled
	runTestsAsSecure := true
	// Override the default proxy config settings for this test
	consulClient, _ := consulCluster.SetupConsulClient(t, runTestsAsSecure)
	_, _, err := consulClient.ConfigEntries().Set(&api.ProxyConfigEntry{
		Kind: api.ProxyDefaults,
		Name: api.ProxyConfigGlobal,
		Config: map[string]interface{}{
			"protocol": "http",
		},
	}, nil)
	require.NoError(t, err)

	logger.Log(t, "creating other namespace")
	out, err := k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "create", "namespace", "other")
	require.NoError(t, err, out)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		// Ignore errors here because if the test ran as expected
		// the custom resources will have been deleted.
		k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "delete", "namespace", "other")
	})

	// create a GatewayClassConfig with configuration set
	gatewayClassConfigName := "kitchen-sink-gateway-class-config"
	gatewayClassName := "kitchen-sink"
	gatewayClassConfig := &v1alpha1.GatewayClassConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: gatewayClassConfigName,
		},
		Spec: v1alpha1.GatewayClassConfigSpec{
			DeploymentSpec: v1alpha1.DeploymentSpec{
				DefaultInstances: pointer.Int32(2),
				MaxInstances:     pointer.Int32(3),
				MinInstances:     pointer.Int32(1),
			},
		},
	}
	k8sClient := ctx.ControllerRuntimeClient(t)

	logger.Log(t, "creating gateway class config")
	err = k8sClient.Create(context.Background(), gatewayClassConfig)
	require.NoError(t, err)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		logger.Log(t, "deleting all gateway class configs")
		k8sClient.DeleteAllOf(context.Background(), &v1alpha1.GatewayClassConfig{})
	})

	gatewayParametersRef := &gwv1beta1.ParametersReference{
		Group: gwv1beta1.Group(v1alpha1.ConsulHashicorpGroup),
		Kind:  gwv1beta1.Kind(v1alpha1.GatewayClassConfigKind),
		Name:  gatewayClassConfigName,
	}

	// Create gateway class referencing gateway-class-config.
	logger.Log(t, "creating controlled gateway class")
	createGatewayClass(t, k8sClient, gatewayClassName, gatewayClassControllerName, gatewayParametersRef)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		logger.Log(t, "deleting all gateway classes")
		k8sClient.DeleteAllOf(context.Background(), &gwv1beta1.GatewayClass{})
	})

	logger.Log(t, "creating api-gateway resources")
	out, err = k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "apply", "-k", "../fixtures/cases/api-gateways/kitchen-sink")
	require.NoError(t, err, out)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		// Ignore errors here because if the test ran as expected
		// the custom resources will have been deleted.
		k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "delete", "-k", "../fixtures/cases/api-gateways/kitchen-sink")
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

	// We use the static-client pod so that we can make calls to the api gateway
	// via kubectl exec without needing a route into the cluster from the test machine.
	logger.Log(t, "creating static-client pod")
	k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/bases/static-client")

	k8s.RunKubectl(t, ctx.KubectlOptions(t), "wait", "--for=condition=available", "--timeout=5m", fmt.Sprintf("deploy/%s", "static-server"))

	// On startup, the controller can take upwards of 1m to perform
	// leader election so we may need to wait a long time for
	// the reconcile loop to run (hence the 2m timeout here).
	var (
		gatewayAddress string
		gatewayClass   gwv1beta1.GatewayClass
		httpRoute      gwv1beta1.HTTPRoute
	)

	counter := &retry.Counter{Count: 60, Wait: 2 * time.Second}
	retry.RunWith(counter, t, func(r *retry.R) {
		var gateway gwv1beta1.Gateway
		err = k8sClient.Get(context.Background(), types.NamespacedName{Name: "gateway", Namespace: "default"}, &gateway)
		require.NoError(r, err)

		// check our finalizers
		require.Len(r, gateway.Finalizers, 1)
		require.EqualValues(r, gatewayFinalizer, gateway.Finalizers[0])

		// check our statuses
		checkStatusCondition(r, gateway.Status.Conditions, trueCondition("Accepted", "Accepted"))
		checkStatusCondition(r, gateway.Status.Conditions, trueCondition("ConsulAccepted", "Accepted"))
		require.Len(r, gateway.Status.Listeners, 1)

		require.EqualValues(r, int32(1), gateway.Status.Listeners[0].AttachedRoutes)
		checkStatusCondition(r, gateway.Status.Listeners[0].Conditions, trueCondition("Accepted", "Accepted"))
		checkStatusCondition(r, gateway.Status.Listeners[0].Conditions, falseCondition("Conflicted", "NoConflicts"))
		checkStatusCondition(r, gateway.Status.Listeners[0].Conditions, trueCondition("ResolvedRefs", "ResolvedRefs"))

		// check that we have an address to use
		require.Len(r, gateway.Status.Addresses, 1)
		// now we know we have an address, set it so we can use it
		gatewayAddress = gateway.Status.Addresses[0].Value

		// gateway class checks
		err = k8sClient.Get(context.Background(), types.NamespacedName{Name: gatewayClassName}, &gatewayClass)
		require.NoError(r, err)

		// check our finalizers
		require.Len(r, gatewayClass.Finalizers, 1)
		require.EqualValues(r, gatewayClassFinalizer, gatewayClass.Finalizers[0])

		// http route checks
		err = k8sClient.Get(context.Background(), types.NamespacedName{Name: "http-route", Namespace: "default"}, &httpRoute)
		require.NoError(r, err)

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

	// check that the Consul entries were created
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
	targetHTTPAddress := fmt.Sprintf("http://%s/v1", gatewayAddress)

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

	// Test that we can make a call to the api gateway
	logger.Log(t, "trying calls to api gateway http")
	k8s.CheckStaticServerConnectionSuccessful(t, k8sOptions, StaticClientName, targetHTTPAddress)

	// ensure that overrides -> route extension -> default by making a request to the admin route with a JWT that a "role" of "doctor"
	// we can see that:
	// * the "role" verification in the route extension takes precedence over the "role" verification in the gateway default

	// should fail because we're missing JWT
	logger.Log(t, "trying calls to api gateway /admin should fail without JWT token")
	k8s.CheckStaticServerHTTPConnectionFailing(t, k8sOptions, StaticClientName, targetHTTPAddressAdmin)

	// should fail because we use the token with the wrong role and correct issuer
	logger.Log(t, "trying calls to api gateway /admin should fail with wrong role")
	k8s.CheckStaticServerHTTPConnectionFailing(t, k8sOptions, StaticClientName, "-H", fmt.Sprintf("Authorization: Bearer %s", petToken), targetHTTPAddressAdmin)

	// will succeed because we use the token with the correct role and the correct issuer
	logger.Log(t, "trying calls to api gateway /admin should succeed with JWT token with correct role")
	k8s.CheckStaticServerConnectionSuccessful(t, k8sOptions, StaticClientName, "-H", fmt.Sprintf("Authorization: Bearer %s", doctorToken), targetHTTPAddressAdmin)

	// ensure that overrides -> route extension -> default by making a request to the admin route with a JWT that has a "role" of "pet"
	// the route does not define
	// we can see that:
	// * the "role" verification in the gateway default is used

	// should fail because we're missing JWT
	logger.Log(t, "trying calls to api gateway /pet should fail without JWT token")
	k8s.CheckStaticServerHTTPConnectionFailing(t, k8sOptions, StaticClientName, targetHTTPAddressPet)

	// should fail because we use the token with the wrong role and correct issuer
	logger.Log(t, "trying calls to api gateway /pet should fail with wrong role")
	k8s.CheckStaticServerHTTPConnectionFailing(t, k8sOptions, StaticClientName, "-H", fmt.Sprintf("Authorization: Bearer %s", doctorToken), targetHTTPAddressPet)

	// will succeed because we use the token with the correct role and the correct issuer
	logger.Log(t, "trying calls to api gateway /pet should succeed with JWT token with correct role")
	k8s.CheckStaticServerConnectionSuccessful(t, k8sOptions, StaticClientName, "-H", fmt.Sprintf("Authorization: Bearer %s", petToken), targetHTTPAddressPet)

	// ensure that routes attached to the same gateway don't cause changes in another route
	// * the verification on the gateway is the only one used as this route does not define any JWT configuration

	// should fail because we're missing JWT
	logger.Log(t, "trying calls to api gateway /pet-no-auth should fail without JWT token")
	k8s.CheckStaticServerHTTPConnectionFailing(t, k8sOptions, StaticClientName, targetHTTPAddressPetNotAuthOnRoute)

	// should fail because we use the token with the wrong role and correct issuer
	logger.Log(t, "trying calls to api gateway /pet-no-auth should fail with wrong role")
	k8s.CheckStaticServerHTTPConnectionFailing(t, k8sOptions, StaticClientName, "-H", fmt.Sprintf("Authorization: Bearer %s", doctorToken), targetHTTPAddressPetNotAuthOnRoute)

	// will succeed because we use the token with the correct role and the correct issuer
	logger.Log(t, "trying calls to api gateway /pet-no-auth should succeed with JWT token with correct role")
	k8s.CheckStaticServerConnectionSuccessful(t, k8sOptions, StaticClientName, "-H", fmt.Sprintf("Authorization: Bearer %s", petToken), targetHTTPAddressPetNotAuthOnRoute)

	// should fail because we're missing JWT
	logger.Log(t, "trying calls to api gateway /admin-no-auth should fail without JWT token")
	k8s.CheckStaticServerHTTPConnectionFailing(t, k8sOptions, StaticClientName, targetHTTPAddressAdminNoAuthOnRoute)

	// should fail because we use the token with the wrong role and correct issuer
	logger.Log(t, "trying calls to api gateway /admin-no-auth should fail with wrong role")
	k8s.CheckStaticServerHTTPConnectionFailing(t, k8sOptions, StaticClientName, "-H", fmt.Sprintf("Authorization: Bearer %s", doctorToken), targetHTTPAddressAdminNoAuthOnRoute)

	// will succeed because we use the token with the correct role and the correct issuer
	logger.Log(t, "trying calls to api gateway /admin-no-auth should succeed with JWT token with correct role")
	k8s.CheckStaticServerConnectionSuccessful(t, k8sOptions, StaticClientName, "-H", fmt.Sprintf("Authorization: Bearer %s", petToken), targetHTTPAddressAdminNoAuthOnRoute)

	// multiple routes can use the same external ref
	// we can see that:
	// * the "role" verification in the route extension takes precedence over the "role" verification in the gateway default

	// should fail because we're missing JWT
	logger.Log(t, "trying calls to api gateway /admin-2 should fail without JWT token")
	k8s.CheckStaticServerHTTPConnectionFailing(t, k8sOptions, StaticClientName, targetHTTPAddressAdmin2)

	// should fail because we use the token with the wrong role and correct issuer
	logger.Log(t, "trying calls to api gateway /admin-2 should fail with wrong role")
	k8s.CheckStaticServerHTTPConnectionFailing(t, k8sOptions, StaticClientName, "-H", fmt.Sprintf("Authorization: Bearer %s", petToken), targetHTTPAddressAdmin2)

	// will succeed because we use the token with the correct role and the correct issuer
	logger.Log(t, "trying calls to api gateway /admin-2 should succeed with JWT token with correct role")
	k8s.CheckStaticServerConnectionSuccessful(t, k8sOptions, StaticClientName, "-H", fmt.Sprintf("Authorization: Bearer %s", doctorToken), targetHTTPAddressAdmin2)

	// should fail because we're missing JWT
	logger.Log(t, "trying calls to api gateway /pet-2 should fail without JWT token")
	k8s.CheckStaticServerHTTPConnectionFailing(t, k8sOptions, StaticClientName, targetHTTPAddressPet2)

	// should fail because we use the token with the wrong role and correct issuer
	logger.Log(t, "trying calls to api gateway /pet-2 should fail with wrong role")
	k8s.CheckStaticServerHTTPConnectionFailing(t, k8sOptions, StaticClientName, "-H", fmt.Sprintf("Authorization: Bearer %s", doctorToken), targetHTTPAddressPet2)

	// will succeed because we use the token with the correct role and the correct issuer
	logger.Log(t, "trying calls to api gateway /pet-2 should succeed with JWT token with correct role")
	k8s.CheckStaticServerConnectionSuccessful(t, k8sOptions, StaticClientName, "-H", fmt.Sprintf("Authorization: Bearer %s", petToken), targetHTTPAddressPet2)
}
