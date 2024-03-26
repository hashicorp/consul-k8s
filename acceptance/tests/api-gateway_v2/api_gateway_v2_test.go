// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package apigatewayv2

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"

	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	meshv2beta1 "github.com/hashicorp/consul-k8s/control-plane/api/mesh/v2beta1"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
)

// Test that api gateway basic functionality works in a default installation and a secure installation for V2.
func TestAPIGateway_V2_Basic(t *testing.T) {

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
				"global.experiments[0]":        "resource-apis",
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
			out, err := k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "apply", "-k", "../fixtures/bases/api-gateway-v2")
			require.NoError(t, err, out)
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
				// Ignore errors here because if the test ran as expected
				// the custom resources will have been deleted.
				k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "delete", "-k", "../fixtures/bases/api-gateway-v2")
			})

			// Create certificate secret, we do this separately since
			// applying the secret will make an invalid certificate that breaks other tests
			logger.Log(t, "creating certificate secret")
			out, err = k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "apply", "-f", "../fixtures/bases/api-gateway-v2/certificate.yaml")
			require.NoError(t, err, out)
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
				// Ignore errors here because if the test ran as expected
				// the custom resources will have been deleted.
				k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "delete", "-f", "../fixtures/bases/api-gateway-v2/certificate.yaml")
			})

			// patch certificate with data
			logger.Log(t, "patching certificate secret with generated data")
			certificate := generateCertificate(t, nil, "gateway.test.local")
			k8s.RunKubectl(t, ctx.KubectlOptions(t), "patch", "secret", "certificate", "-p", fmt.Sprintf(`{"data":{"tls.crt":"%s","tls.key":"%s"}}`, base64.StdEncoding.EncodeToString(certificate.CertPEM), base64.StdEncoding.EncodeToString(certificate.PrivateKeyPEM)), "--type=merge")

			// We use the static-client pod so that we can make calls to the api gateway
			// via kubectl exec without needing a route into the cluster from the test machine.
			logger.Log(t, "creating static-client pod")
			k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/bases/static-client")

			logger.Log(t, "creating target tcp server")
			k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/bases/static-server-tcp")

			logger.Log(t, "creating tcp-route")
			k8s.RunKubectl(t, ctx.KubectlOptions(t), "apply", "-f", "../fixtures/cases/api-gateways-v2/tcproute/route.yaml")
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
				// Ignore errors here because if the test ran as expected
				// the custom resources will have been deleted.
				k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "delete", "-f", "../fixtures/cases/api-gateways-v2/tcproute/route.yaml")
			})

			// Grab a kubernetes client so that we can verify binding
			// behavior prior to issuing requests through the gateway.
			k8sClient := ctx.ControllerRuntimeClient(t)

			// On startup, the controller can take upwards of 1m to perform
			// leader election so we may need to wait a long time for
			// the reconcile loop to run (hence the timeout here).
			var gatewayAddress string
			counter := &retry.Counter{Count: 120, Wait: 2 * time.Second}
			retry.RunWith(counter, t, func(r *retry.R) {
				var gateway meshv2beta1.APIGateway
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: "gateway", Namespace: "default"}, &gateway)
				require.NoError(r, err)

				// check our finalizers
				require.Len(r, gateway.Finalizers, 1)
				require.EqualValues(r, gatewayFinalizer, gateway.Finalizers()[0])

				// check our statuses
				checkV2StatusCondition(r, gateway.APIGatewayStatus.Conditions, trueV2Condition("Accepted", "Accepted"))
				checkV2StatusCondition(r, gateway.APIGatewayStatus.Conditions, trueV2Condition("ConsulAccepted", "Accepted"))
				require.Len(r, gateway.APIGatewayStatus.Listeners, 3)

				require.EqualValues(r, 1, gateway.APIGatewayStatus.Listeners[0].AttachedRoutes)
				checkV2StatusCondition(r, gateway.APIGatewayStatus.Listeners[0].Conditions, trueV2Condition("Accepted", "Accepted"))
				checkV2StatusCondition(r, gateway.APIGatewayStatus.Listeners[0].Conditions, falseV2Condition("Conflicted", "NoConflicts"))
				checkV2StatusCondition(r, gateway.APIGatewayStatus.Listeners[0].Conditions, trueV2Condition("ResolvedRefs", "ResolvedRefs"))
				require.EqualValues(r, 1, gateway.APIGatewayStatus.Listeners[1].AttachedRoutes)
				checkV2StatusCondition(r, gateway.APIGatewayStatus.Listeners[1].Conditions, trueV2Condition("Accepted", "Accepted"))
				checkV2StatusCondition(r, gateway.APIGatewayStatus.Listeners[1].Conditions, falseV2Condition("Conflicted", "NoConflicts"))
				checkV2StatusCondition(r, gateway.APIGatewayStatus.Listeners[1].Conditions, trueV2Condition("ResolvedRefs", "ResolvedRefs"))
				require.EqualValues(r, 1, gateway.APIGatewayStatus.Listeners[2].AttachedRoutes)
				checkV2StatusCondition(r, gateway.APIGatewayStatus.Listeners[2].Conditions, trueV2Condition("Accepted", "Accepted"))
				checkV2StatusCondition(r, gateway.APIGatewayStatus.Listeners[2].Conditions, falseV2Condition("Conflicted", "NoConflicts"))
				checkV2StatusCondition(r, gateway.APIGatewayStatus.Listeners[2].Conditions, trueV2Condition("ResolvedRefs", "ResolvedRefs"))

				// check that we have an address to use
				require.Len(r, gateway.APIGatewayStatus.Addresses, 1)
				// now we know we have an address, set it so we can use it
				gatewayAddress = gateway.APIGatewayStatus.Addresses[0].Value
			})

			// now that we've satisfied those assertions, we know reconciliation is done
			// so we can run assertions on the routes and the other objects

			// gateway class checks
			var gatewayClass meshv2beta1.GatewayClass
			err = k8sClient.Get(context.Background(), types.NamespacedName{Name: "gateway-class"}, &gatewayClass)
			require.NoError(t, err)

			// check our finalizers
			require.Len(t, gatewayClass.Finalizers, 1)
			require.EqualValues(t, gatewayClassFinalizer, gatewayClass.Finalizers[0])

			// tcp route checks
			var tcpRoute meshv2beta1.TCPRoute
			err = k8sClient.Get(context.Background(), types.NamespacedName{Name: "tcp-route", Namespace: "default"}, &tcpRoute)
			require.NoError(t, err)

			// check our finalizers
			require.Len(t, tcpRoute.Finalizers, 1)
			require.EqualValues(t, gatewayFinalizer, tcpRoute.Finalizers()[0])

			// TODO check values actually created in the resource API

			// finally we check that we can actually route to the service via the gateway
			k8sOptions := ctx.KubectlOptions(t)
			targetTCPAddress := fmt.Sprintf("http://%s:81", gatewayAddress)

			// Test that we can make a call to the api gateway
			// via the static-client pod. It should route to the static-server pod.
			logger.Log(t, "trying calls to api gateway tcp")
			k8s.CheckStaticServerConnectionSuccessful(t, k8sOptions, StaticClientName, targetTCPAddress)

		})
	}
}
