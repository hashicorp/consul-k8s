// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package apigateway

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"testing"
	"time"

	gwv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

const (
	StaticClientName           = "static-client"
	gatewayClassControllerName = "consul.hashicorp.com/gateway-controller"
	gatewayClassFinalizer      = "gateway-exists-finalizer.consul.hashicorp.com"
	gatewayFinalizer           = "gateway-finalizer.consul.hashicorp.com"
)

// Test that api gateway basic functionality works in a default installation and a secure installation.
func TestAPIGateway_Basic(t *testing.T) {
	cases := []struct {
		secure bool
	}{
		{
			secure: false,
		},
		{
			secure: true,
		},
	}
	for _, c := range cases {
		name := fmt.Sprintf("secure: %t", c.secure)
		t.Run(name, func(t *testing.T) {
			ctx := suite.Environment().DefaultContext(t)
			cfg := suite.Config()
			helmValues := map[string]string{
				"connectInject.enabled":        "true",
				"global.acls.manageSystemACLs": strconv.FormatBool(c.secure),
				"global.tls.enabled":           strconv.FormatBool(c.secure),
				"global.logLevel":              "trace",
			}

			releaseName := helpers.RandomName()
			consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)

			consulCluster.Create(t)

			// Override the default proxy config settings for this test
			consulClient, _ := consulCluster.SetupConsulClient(t, c.secure)
			_, _, err := consulClient.ConfigEntries().Set(&api.ProxyConfigEntry{
				Kind: api.ProxyDefaults,
				Name: api.ProxyConfigGlobal,
				Config: map[string]interface{}{
					"protocol": "http",
				},
			}, nil)
			require.NoError(t, err)

			logger.Log(t, "creating api-gateway resources")
			out, err := k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "apply", "-k", "../fixtures/bases/api-gateway")
			require.NoError(t, err, out)
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
				// Ignore errors here because if the test ran as expected
				// the custom resources will have been deleted.
				k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "delete", "-k", "../fixtures/bases/api-gateway")
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

			logger.Log(t, "creating target http server")
			k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")

			// We use the static-client pod so that we can make calls to the api gateway
			// via kubectl exec without needing a route into the cluster from the test machine.
			logger.Log(t, "creating static-client pod")
			k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/bases/static-client")

			logger.Log(t, "patching route to target http server")
			k8s.RunKubectl(t, ctx.KubectlOptions(t), "patch", "httproute", "http-route", "-p", `{"spec":{"rules":[{"backendRefs":[{"name":"static-server","port":80}]}]}}`, "--type=merge")

			logger.Log(t, "creating target tcp server")
			k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/bases/static-server-tcp")

			logger.Log(t, "creating tcp-route")
			k8s.RunKubectl(t, ctx.KubectlOptions(t), "apply", "-f", "../fixtures/cases/api-gateways/tcproute/route.yaml")
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
				// Ignore errors here because if the test ran as expected
				// the custom resources will have been deleted.
				k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "delete", "-f", "../fixtures/cases/api-gateways/tcproute/route.yaml")
			})

			// Grab a kubernetes client so that we can verify binding
			// behavior prior to issuing requests through the gateway.
			k8sClient := ctx.ControllerRuntimeClient(t)

			// On startup, the controller can take upwards of 1m to perform
			// leader election so we may need to wait a long time for
			// the reconcile loop to run (hence the 1m timeout here).
			var gatewayAddress string
			counter := &retry.Counter{Count: 120, Wait: 2 * time.Second}
			retry.RunWith(counter, t, func(r *retry.R) {
				var gateway gwv1beta1.Gateway
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: "gateway", Namespace: "default"}, &gateway)
				require.NoError(r, err)

				// check our finalizers
				require.Len(r, gateway.Finalizers, 1)
				require.EqualValues(r, gatewayFinalizer, gateway.Finalizers[0])

				// check our statuses
				checkStatusCondition(r, gateway.Status.Conditions, trueCondition("Accepted", "Accepted"))
				checkStatusCondition(r, gateway.Status.Conditions, trueCondition("ConsulAccepted", "Accepted"))
				require.Len(r, gateway.Status.Listeners, 3)

				require.EqualValues(r, 1, gateway.Status.Listeners[0].AttachedRoutes)
				checkStatusCondition(r, gateway.Status.Listeners[0].Conditions, trueCondition("Accepted", "Accepted"))
				checkStatusCondition(r, gateway.Status.Listeners[0].Conditions, falseCondition("Conflicted", "NoConflicts"))
				checkStatusCondition(r, gateway.Status.Listeners[0].Conditions, trueCondition("ResolvedRefs", "ResolvedRefs"))
				require.EqualValues(r, 1, gateway.Status.Listeners[1].AttachedRoutes)
				checkStatusCondition(r, gateway.Status.Listeners[1].Conditions, trueCondition("Accepted", "Accepted"))
				checkStatusCondition(r, gateway.Status.Listeners[1].Conditions, falseCondition("Conflicted", "NoConflicts"))
				checkStatusCondition(r, gateway.Status.Listeners[1].Conditions, trueCondition("ResolvedRefs", "ResolvedRefs"))
				require.EqualValues(r, 1, gateway.Status.Listeners[2].AttachedRoutes)
				checkStatusCondition(r, gateway.Status.Listeners[2].Conditions, trueCondition("Accepted", "Accepted"))
				checkStatusCondition(r, gateway.Status.Listeners[2].Conditions, falseCondition("Conflicted", "NoConflicts"))
				checkStatusCondition(r, gateway.Status.Listeners[2].Conditions, trueCondition("ResolvedRefs", "ResolvedRefs"))

				// check that we have an address to use
				require.Len(r, gateway.Status.Addresses, 1)
				// now we know we have an address, set it so we can use it
				gatewayAddress = gateway.Status.Addresses[0].Value
			})

			// now that we've satisfied those assertions, we know reconciliation is done
			// so we can run assertions on the routes and the other objects

			// gateway class checks
			var gatewayClass gwv1beta1.GatewayClass
			err = k8sClient.Get(context.Background(), types.NamespacedName{Name: "gateway-class"}, &gatewayClass)
			require.NoError(t, err)

			// check our finalizers
			require.Len(t, gatewayClass.Finalizers, 1)
			require.EqualValues(t, gatewayClassFinalizer, gatewayClass.Finalizers[0])

			// http route checks
			var httproute gwv1beta1.HTTPRoute
			err = k8sClient.Get(context.Background(), types.NamespacedName{Name: "http-route", Namespace: "default"}, &httproute)
			require.NoError(t, err)

			// check our finalizers
			require.Len(t, httproute.Finalizers, 1)
			require.EqualValues(t, gatewayFinalizer, httproute.Finalizers[0])

			// check parent status
			require.Len(t, httproute.Status.Parents, 1)
			require.EqualValues(t, gatewayClassControllerName, httproute.Status.Parents[0].ControllerName)
			require.EqualValues(t, "gateway", httproute.Status.Parents[0].ParentRef.Name)
			checkStatusCondition(t, httproute.Status.Parents[0].Conditions, trueCondition("Accepted", "Accepted"))
			checkStatusCondition(t, httproute.Status.Parents[0].Conditions, trueCondition("ResolvedRefs", "ResolvedRefs"))
			checkStatusCondition(t, httproute.Status.Parents[0].Conditions, trueCondition("ConsulAccepted", "Accepted"))

			// tcp route checks
			var tcpRoute gwv1alpha2.TCPRoute
			err = k8sClient.Get(context.Background(), types.NamespacedName{Name: "tcp-route", Namespace: "default"}, &tcpRoute)
			require.NoError(t, err)

			// check our finalizers
			require.Len(t, tcpRoute.Finalizers, 1)
			require.EqualValues(t, gatewayFinalizer, tcpRoute.Finalizers[0])

			// check parent status
			require.Len(t, tcpRoute.Status.Parents, 1)
			require.EqualValues(t, gatewayClassControllerName, tcpRoute.Status.Parents[0].ControllerName)
			require.EqualValues(t, "gateway", tcpRoute.Status.Parents[0].ParentRef.Name)
			checkStatusCondition(t, tcpRoute.Status.Parents[0].Conditions, trueCondition("Accepted", "Accepted"))
			checkStatusCondition(t, tcpRoute.Status.Parents[0].Conditions, trueCondition("ResolvedRefs", "ResolvedRefs"))
			checkStatusCondition(t, tcpRoute.Status.Parents[0].Conditions, trueCondition("ConsulAccepted", "Accepted"))

			// check that the Consul entries were created
			var gateway *api.APIGatewayConfigEntry
			var httpRoute *api.HTTPRouteConfigEntry
			var route *api.TCPRouteConfigEntry
			retry.RunWith(counter, t, func(r *retry.R) {
				entry, _, err := consulClient.ConfigEntries().Get(api.APIGateway, "gateway", nil)
				require.NoError(r, err)
				gateway = entry.(*api.APIGatewayConfigEntry)

				entry, _, err = consulClient.ConfigEntries().Get(api.HTTPRoute, "http-route", nil)
				require.NoError(r, err)
				httpRoute = entry.(*api.HTTPRouteConfigEntry)

				entry, _, err = consulClient.ConfigEntries().Get(api.TCPRoute, "tcp-route", nil)
				require.NoError(r, err)
				route = entry.(*api.TCPRouteConfigEntry)
			})

			// now check the gateway status conditions
			checkConsulStatusCondition(t, gateway.Status.Conditions, trueConsulCondition("Accepted", "Accepted"))

			// and the route status conditions
			checkConsulStatusCondition(t, httpRoute.Status.Conditions, trueConsulCondition("Bound", "Bound"))
			checkConsulStatusCondition(t, route.Status.Conditions, trueConsulCondition("Bound", "Bound"))

			// finally we check that we can actually route to the service via the gateway
			k8sOptions := ctx.KubectlOptions(t)
			targetHTTPAddress := fmt.Sprintf("http://%s", gatewayAddress)
			targetHTTPSAddress := fmt.Sprintf("https://%s", gatewayAddress)
			targetTCPAddress := fmt.Sprintf("http://%s:81", gatewayAddress)

			if c.secure {
				// check that intentions keep our connection from happening
				k8s.CheckStaticServerHTTPConnectionFailing(t, k8sOptions, StaticClientName, targetHTTPAddress)

				k8s.CheckStaticServerConnectionFailing(t, k8sOptions, StaticClientName, targetTCPAddress)

				k8s.CheckStaticServerHTTPConnectionFailing(t, k8sOptions, StaticClientName, "-k", targetHTTPSAddress)

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

				// Now we create the allow intention tcp.
				_, _, err = consulClient.ConfigEntries().Set(&api.ServiceIntentionsConfigEntry{
					Kind: api.ServiceIntentions,
					Name: "static-server-tcp",
					Sources: []*api.SourceIntention{
						{
							Name:   "gateway",
							Action: api.IntentionActionAllow,
						},
					},
				}, nil)
				require.NoError(t, err)
			}

			// Test that we can make a call to the api gateway
			// via the static-client pod. It should route to the static-server pod.
			logger.Log(t, "trying calls to api gateway http")
			k8s.CheckStaticServerConnectionSuccessful(t, k8sOptions, StaticClientName, targetHTTPAddress)

			logger.Log(t, "trying calls to api gateway tcp")
			k8s.CheckStaticServerConnectionSuccessful(t, k8sOptions, StaticClientName, targetTCPAddress)

			logger.Log(t, "trying calls to api gateway https")
			k8s.CheckStaticServerConnectionSuccessful(t, k8sOptions, StaticClientName, targetHTTPSAddress, "-k")
		})
	}
}

func TestAPIGateway_JWTAuth_Basic(t *testing.T) {
	t.Skip()
	ctx := suite.Environment().DefaultContext(t)
	cfg := suite.Config()
	helmValues := map[string]string{
		"connectInject.enabled":        "true",
		"global.acls.manageSystemACLs": "true", // acls must be enabled for JWT auth to take place
		"global.tls.enabled":           "true",
		"global.logLevel":              "trace",
	}

	releaseName := helpers.RandomName()
	consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)

	consulCluster.Create(t)

	// Override the default proxy config settings for this test
	consulClient, _ := consulCluster.SetupConsulClient(t, true)
	_, _, err := consulClient.ConfigEntries().Set(&api.ProxyConfigEntry{
		Kind: api.ProxyDefaults,
		Name: api.ProxyConfigGlobal,
		Config: map[string]interface{}{
			"protocol": "http",
		},
	}, nil)
	require.NoError(t, err)

	logger.Log(t, "creating api-gateway resources")
	out, err := k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "apply", "-k", "../fixtures/cases/api-gateways/jwt-auth")
	require.NoError(t, err, out)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		// Ignore errors here because if the test ran as expected
		// the custom resources will have been deleted.
		k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "delete", "-k", "../fixtures/cases/api-gateways/jwt-auth")
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
	// Grab a kubernetes client so that we can verify binding
	// behavior prior to issuing requests through the gateway.
	k8sClient := ctx.ControllerRuntimeClient(t)

	// On startup, the controller can take upwards of 1m to perform
	// leader election so we may need to wait a long time for
	// the reconcile loop to run (hence the 1m timeout here).
	var (
		gatewayAddress string
		gatewayClass   gwv1beta1.GatewayClass
		httproute      gwv1beta1.HTTPRoute
		httprouteAuth  gwv1beta1.HTTPRoute
	)

	counter := &retry.Counter{Count: 60, Wait: 2 * time.Second}
	retry.RunWith(counter, t, func(r *retry.R) {
		var gateway gwv1beta1.Gateway
		err := k8sClient.Get(context.Background(), types.NamespacedName{Name: "gateway", Namespace: "default"}, &gateway)
		require.NoError(r, err)

		// check our finalizers
		require.Len(r, gateway.Finalizers, 1)
		require.EqualValues(r, gatewayFinalizer, gateway.Finalizers[0])

		// check our statuses
		checkStatusCondition(r, gateway.Status.Conditions, trueCondition("Accepted", "Accepted"))
		checkStatusCondition(r, gateway.Status.Conditions, trueCondition("ConsulAccepted", "Accepted"))
		require.Len(r, gateway.Status.Listeners, 4)

		require.EqualValues(r, 1, gateway.Status.Listeners[0].AttachedRoutes)
		checkStatusCondition(r, gateway.Status.Listeners[0].Conditions, trueCondition("Accepted", "Accepted"))
		checkStatusCondition(r, gateway.Status.Listeners[0].Conditions, falseCondition("Conflicted", "NoConflicts"))
		checkStatusCondition(r, gateway.Status.Listeners[0].Conditions, trueCondition("ResolvedRefs", "ResolvedRefs"))
		require.EqualValues(r, 1, gateway.Status.Listeners[1].AttachedRoutes)
		checkStatusCondition(r, gateway.Status.Listeners[1].Conditions, trueCondition("Accepted", "Accepted"))
		checkStatusCondition(r, gateway.Status.Listeners[1].Conditions, falseCondition("Conflicted", "NoConflicts"))
		checkStatusCondition(r, gateway.Status.Listeners[1].Conditions, trueCondition("ResolvedRefs", "ResolvedRefs"))

		// check that we have an address to use
		require.Len(r, gateway.Status.Addresses, 1)
		// now we know we have an address, set it so we can use it
		gatewayAddress = gateway.Status.Addresses[0].Value

		// gateway class checks
		err = k8sClient.Get(context.Background(), types.NamespacedName{Name: "gateway-class"}, &gatewayClass)
		require.NoError(r, err)

		// check our finalizers
		require.Len(r, gatewayClass.Finalizers, 1)
		require.EqualValues(r, gatewayClassFinalizer, gatewayClass.Finalizers[0])

		// http route checks
		err = k8sClient.Get(context.Background(), types.NamespacedName{Name: "http-route", Namespace: "default"}, &httproute)
		require.NoError(r, err)

		// http route checks
		err = k8sClient.Get(context.Background(), types.NamespacedName{Name: "http-route-auth", Namespace: "default"}, &httprouteAuth)
		require.NoError(r, err)

		// check our finalizers
		require.Len(t, httproute.Finalizers, 1)
		require.EqualValues(r, gatewayFinalizer, httproute.Finalizers[0])

		// check parent status
		require.Len(t, httproute.Status.Parents, 1)
		require.EqualValues(r, gatewayClassControllerName, httproute.Status.Parents[0].ControllerName)
		require.EqualValues(r, "gateway", httproute.Status.Parents[0].ParentRef.Name)
		checkStatusCondition(r, httproute.Status.Parents[0].Conditions, trueCondition("Accepted", "Accepted"))
		checkStatusCondition(r, httproute.Status.Parents[0].Conditions, trueCondition("ResolvedRefs", "ResolvedRefs"))
		checkStatusCondition(r, httproute.Status.Parents[0].Conditions, trueCondition("ConsulAccepted", "Accepted"))

		// check our finalizers
		require.Len(r, httprouteAuth.Finalizers, 1)
		require.EqualValues(r, gatewayFinalizer, httprouteAuth.Finalizers[0])

		// check parent status
		require.Len(r, httprouteAuth.Status.Parents, 1)
		require.EqualValues(r, gatewayClassControllerName, httprouteAuth.Status.Parents[0].ControllerName)
		require.EqualValues(r, "gateway", httprouteAuth.Status.Parents[0].ParentRef.Name)
		checkStatusCondition(r, httprouteAuth.Status.Parents[0].Conditions, trueCondition("Accepted", "Accepted"))
		checkStatusCondition(r, httprouteAuth.Status.Parents[0].Conditions, trueCondition("ResolvedRefs", "ResolvedRefs"))
		checkStatusCondition(r, httprouteAuth.Status.Parents[0].Conditions, trueCondition("ConsulAccepted", "Accepted"))
	})

	// check that the Consul entries were created
	entry, _, err := consulClient.ConfigEntries().Get(api.APIGateway, "gateway", nil)
	require.NoError(t, err)
	gateway := entry.(*api.APIGatewayConfigEntry)

	entry, _, err = consulClient.ConfigEntries().Get(api.HTTPRoute, "http-route", nil)
	require.NoError(t, err)
	httpRoute := entry.(*api.HTTPRouteConfigEntry)

	entry, _, err = consulClient.ConfigEntries().Get(api.HTTPRoute, "http-route-auth", nil)
	require.NoError(t, err)
	httpRouteAuth := entry.(*api.HTTPRouteConfigEntry)

	// now check the gateway status conditions
	checkConsulStatusCondition(t, gateway.Status.Conditions, trueConsulCondition("Accepted", "Accepted"))

	// and the route status conditions
	checkConsulStatusCondition(t, httpRoute.Status.Conditions, trueConsulCondition("Bound", "Bound"))
	checkConsulStatusCondition(t, httpRouteAuth.Status.Conditions, trueConsulCondition("Bound", "Bound"))

	// finally we check that we can actually route to the service(s) via the gateway
	k8sOptions := ctx.KubectlOptions(t)
	targetHTTPAddress := fmt.Sprintf("http://%s/v1", gatewayAddress)
	targetHTTPAddressAdmin := fmt.Sprintf("http://%s:8080/admin", gatewayAddress)
	targetHTTPAddressPet := fmt.Sprintf("http://%s:8080/pet", gatewayAddress)
	// valid JWT token with role of "doctor"
	doctorToken := "eyJ0eXAiOiJKV1QiLCJhbGciOiJQUzI1NiIsImtpZCI6IkMtRTFuQ2p3Z0JDLVB1R00yTzQ2N0ZSRGhLeDhBa1ZjdElTQWJvM3JpZXcifQ.eyJpc3MiOiJsb2NhbCIsInJvbGUiOiJkb2N0b3IifQ.FfgpzjMf8Evh6K-fJ1cLXklfIXOm-vojVbWlPPbGVFtzxZ9hxMxoyAY_G8i36SfGrpUlp-RJ6ohMvprMrEgyRgbenu7u5kkm5iGHW-zpMus4izXRxPELBcpWOGF105HIssT2NYRstXieNR8EVzvGfLdvR0GW8ttEERgseqGvuAfdb4-aNYsysGwUUHbsZjazA6H1rZmWqHdCLOJ2ZwFsIdckO9CadnkyTILpcPUmLYyUVJdtlLGOySb0GG8c_dPML_IR5jSXCSUZt6S2JBNBNBdqukrlqpA-fIaaWft0dbWVMhv8DqPC8znult8dKvLZ1qXeU0itsqqJUyE16ihJjw"
	// valid JWT token with role of "pet"
	petToken := "eyJ0eXAiOiJKV1QiLCJhbGciOiJQUzI1NiIsImtpZCI6IkMtRTFuQ2p3Z0JDLVB1R00yTzQ2N0ZSRGhLeDhBa1ZjdElTQWJvM3JpZXcifQ.eyJpc3MiOiJsb2NhbCIsInJvbGUiOiJwZXQifQ.l94rJayGGTMB426HwEw5ipSjaIHjm-UWDHiBAlB_Slmi814AxAfl_0AdRwSz67UDnkoygKbvPpR5xUB03JCXNshLZuKLegWsBeQg_OJYvZGmFagl5NglBFvH7Jbta4e1eQoAxZI6Xyy1jHbu7jFBjQPVnK8EaRvWoW8Pe8a8rp_5xhub0pomhvRF6Pm5kAS4cMnxvqpVc5Oo5nO7ws_SmoNnbt2Ok14k23Zx5E2EWmGStOfbgFsdbhVbepB2DMzqv1j8jvBbwa_OxCwc_7pEOthOOxRV6L3ZjgbRSB4GumlXAOCBYXD1cRLgrMSrWB1GkefAKu8PV0Ho1px6sI9Evg"

	// check that intentions keep our connection from happening
	k8s.CheckStaticServerHTTPConnectionFailing(t, k8sOptions, StaticClientName, targetHTTPAddress)
	k8s.CheckStaticServerHTTPConnectionFailing(t, k8sOptions, StaticClientName, targetHTTPAddressAdmin)
	k8s.CheckStaticServerHTTPConnectionFailing(t, k8sOptions, StaticClientName, "-H", doctorToken, targetHTTPAddressAdmin)
	k8s.CheckStaticServerHTTPConnectionFailing(t, k8sOptions, StaticClientName, "-H", petToken, targetHTTPAddressAdmin)

	k8s.CheckStaticServerHTTPConnectionFailing(t, k8sOptions, StaticClientName, targetHTTPAddressPet)
	k8s.CheckStaticServerHTTPConnectionFailing(t, k8sOptions, StaticClientName, "-H", doctorToken, targetHTTPAddressPet)
	k8s.CheckStaticServerHTTPConnectionFailing(t, k8sOptions, StaticClientName, "-H", petToken, targetHTTPAddressPet)

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

	// ensure that overrides -> route extension -> default by making a request to the admin route with a JWT that has an issuer of "local" and a "role" of "doctor"
	// we can see that:
	// * the "iss" verification in the gateway override takes precedence over the "iss" verification in the route filter
	// * the "role" verification in the route extension takes precedence over the "role" verification in the gateway default
	// should fail because we're missing JWT
	logger.Log(t, "trying calls to api gateway /admin should fail without JWT token")
	k8s.CheckStaticServerHTTPConnectionFailing(t, k8sOptions, StaticClientName, targetHTTPAddressAdmin)

	// should fail because we use the token with the wrong role and correct issuer
	logger.Log(t, "trying calls to api gateway /admin should fail with wrong role")
	k8s.CheckStaticServerHTTPConnectionFailing(t, k8sOptions, StaticClientName, "-H", petToken, targetHTTPAddressAdmin)

	// will succeed because we use the token with the correct role and the correct issuer
	logger.Log(t, "trying calls to api gateway /admin should succeed with JWT token with correct role")
	k8s.CheckStaticServerConnectionSuccessful(t, k8sOptions, StaticClientName, "-H", doctorToken, targetHTTPAddressAdmin)

	// ensure that overrides -> route extension -> default by making a request to the admin route with a JWT that has an issuer of "local" and a "role" of "pet"
	// the route does not define
	// we can see that:
	// * the "iss" verification in the gateway override takes precedence over the "iss" verification in the route filter
	// * the "role" verification in the route extension takes precedence over the "role" verification in the gateway default
	// should fail because we're missing JWT
	logger.Log(t, "trying calls to api gateway /pet should fail without JWT token")
	k8s.CheckStaticServerHTTPConnectionFailing(t, k8sOptions, StaticClientName, targetHTTPAddressPet)

	// should fail because we use the token with the wrong role and correct issuer
	logger.Log(t, "trying calls to api gateway /pet should fail with wrong role")
	k8s.CheckStaticServerHTTPConnectionFailing(t, k8sOptions, StaticClientName, "-H", doctorToken, targetHTTPAddressPet)

	// will succeed because we use the token with the correct role and the correct issuer
	logger.Log(t, "trying calls to api gateway /pet should succeed with JWT token with correct role")
	k8s.CheckStaticServerConnectionSuccessful(t, k8sOptions, StaticClientName, "-H", petToken, targetHTTPAddressPet)
}

func checkStatusCondition(t require.TestingT, conditions []metav1.Condition, toCheck metav1.Condition) {
	for _, c := range conditions {
		if c.Type == toCheck.Type {
			require.EqualValues(t, toCheck.Reason, c.Reason)
			require.EqualValues(t, toCheck.Status, c.Status)
			return
		}
	}

	t.Errorf("expected condition not found: %s", toCheck.Type)
}

func trueCondition(conditionType, reason string) metav1.Condition {
	return metav1.Condition{
		Type:   conditionType,
		Reason: reason,
		Status: metav1.ConditionTrue,
	}
}

func falseCondition(conditionType, reason string) metav1.Condition {
	return metav1.Condition{
		Type:   conditionType,
		Reason: reason,
		Status: metav1.ConditionFalse,
	}
}

func checkConsulStatusCondition(t require.TestingT, conditions []api.Condition, toCheck api.Condition) {
	for _, c := range conditions {
		if c.Type == toCheck.Type {
			require.EqualValues(t, toCheck.Reason, c.Reason)
			require.EqualValues(t, toCheck.Status, c.Status)
			return
		}
	}

	t.Errorf("expected condition not found: %s", toCheck.Type)
}

func trueConsulCondition(conditionType, reason string) api.Condition {
	return api.Condition{
		Type:   conditionType,
		Reason: reason,
		Status: "True",
	}
}
