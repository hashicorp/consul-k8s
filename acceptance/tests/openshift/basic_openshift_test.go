// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package openshift

import (
	"fmt"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul/api"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	StaticClientName           = "static-client"
	gatewayClassControllerName = "consul.hashicorp.com/gateway-controller"
	gatewayClassFinalizer      = "gateway-exists-finalizer.consul.hashicorp.com"
	gatewayFinalizer           = "gateway-finalizer.consul.hashicorp.com"
)

// Test that api gateway basic functionality works in a default installation and a secure installation.
func TestOpenshift_Basic(t *testing.T) {
	cases := []struct {
		secure bool
	}{
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
				"connectInject.enabled":                                                    "true",
				"connectInject.transparentProxy.defaultEnabled":                            "false",
				"connectInject.apiGateway.managedGatewayClass.mapPrivilegedContainerPorts": "8000",
				"global.acls.manageSystemACLs":                                             strconv.FormatBool(c.secure),
				"global.tls.enabled":                                                       strconv.FormatBool(c.secure),
				"global.tls.enableAutoEncrypt":                                             strconv.FormatBool(c.secure),
				"global.logLevel":                                                          "trace",
				"global.openshift.enabled":                                                 "true",
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
			out, err := k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "apply", "-k", "../fixtures/cases/openshift/basic")
			require.NoError(t, err, out)
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
				// Ignore errors here because if the test ran as expected
				// the custom resources will have been deleted.
				k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "delete", "-k", "../fixtures/cases/openshift/basic")
			})

			//// Grab a kubernetes client so that we can verify binding
			//// behavior prior to issuing requests through the gateway.
			//k8sClient := ctx.ControllerRuntimeClient(t)
			//
			//// On startup, the controller can take upwards of 1m to perform
			//// leader election so we may need to wait a long time for
			//// the reconcile loop to run (hence the timeout here).
			//var gatewayAddress string
			//counter := &retry.Counter{Count: 120, Wait: 2 * time.Second}

			// now that we've satisfied those assertions, we know reconciliation is done
			// so we can run assertions on the routes and the other objects

			//// finally we check that we can actually route to the service via the gateway
			//k8sOptions := ctx.KubectlOptions(t)
			//targetHTTPAddress := fmt.Sprintf("http://%s", gatewayAddress)
			//targetHTTPSAddress := fmt.Sprintf("https://%s", gatewayAddress)
			//targetTCPAddress := fmt.Sprintf("http://%s:81", gatewayAddress)

			//// Test that we can make a call to the api gateway
			//// via the static-client pod. It should route to the static-server pod.
			//logger.Log(t, "trying calls to api gateway http")
			//k8s.CheckStaticServerConnectionSuccessful(t, k8sOptions, StaticClientName, targetHTTPAddress)

		})
	}
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
