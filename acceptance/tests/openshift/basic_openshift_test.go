// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package openshift

import (
	"context"
	"crypto/tls"
	"fmt"
	"github.com/go-logr/logr"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"k8s.io/apimachinery/pkg/types"
	"log"
	"net/http"
	"os/exec"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
	"testing"
	"time"

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
			cfg := suite.Config()

			//namespaceName := helpers.RandomName()
			//TODO for some reason NewHelmCluster creates consul server pod that runs as root which
			// isn't allowed in openshift. In order to test openshift properly we have to call helm and k8s directly to bypass
			// but ideally we would just fix the helper function running the pod as root
			cmd := exec.Command("helm", "upgrade", "--install", "consul", "hashicorp/consul", "--create-namespace",
				"--namespace", "consul",
				"--set", "connectInject.enabled=true",
				"--set", "connectInject.transparentProxy.defaultEnabled=false",
				"--set", "connectInject.apiGateway.managedGatewayClass.mapPrivilegedContainerPorts=8000",
				"--set", "global.acls.manageSystemACLs=true",
				"--set", "global.tls.enabled=true",
				"--set", "global.tls.enableAutoEncrypt=true",
				"--set", "global.openshift.enabled=true",
				"--set", "global.image=docker.mirror.hashicorp.services/hashicorppreview/consul:1.21-dev",
				"--set", "global.imageK8S=docker.mirror.hashicorp.services/hashicorppreview/consul-k8s-control-plane:1.7-dev",
			)
			output, err := cmd.CombinedOutput()
			if err != nil {
				log.Fatal(string(output))
				require.NoError(t, err)
			}

			helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
				cmd := exec.Command("helm", "uninstall", "consul",
					"--namespace", "consul",
				)
				output, err := cmd.CombinedOutput()
				if err != nil {
					log.Fatal(string(output))
				}
				kubectlCmd := exec.Command("kubectl", "delete", "namespace", "consul")

				output, err = kubectlCmd.CombinedOutput()
				if err != nil {
					log.Fatal(string(output))
					require.NoError(t, err)
				}
			})
			//this is normally called by the environment, but because we have to bypass we have to call it explicitly
			logf.SetLogger(logr.New(nil))
			logger.Log(t, "creating api-gateway resources")

			kubectlCmd := exec.Command("kubectl", "apply", "-f", "../fixtures/cases/openshift/basic")

			output, err = kubectlCmd.CombinedOutput()
			if err != nil {
				log.Fatal(string(output))
				require.NoError(t, err)
			}

			helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
				kubectlCmd := exec.Command("kubectl", "delete", "-f", "../fixtures/cases/openshift/basic")

				output, err := kubectlCmd.CombinedOutput()
				if err != nil {
					log.Fatal(string(output))
					require.NoError(t, err)
				}
			})

			//// Grab a kubernetes client so that we can verify binding
			//// behavior prior to issuing requests through the gateway.
			ctx := suite.Environment().DefaultContext(t)
			k8sClient := ctx.ControllerRuntimeClient(t)
			//
			//// On startup, the controller can take upwards of 1m to perform
			//// leader election so we may need to wait a long time for
			//// the reconcile loop to run (hence the timeout here).
			var gatewayAddress string
			counter := &retry.Counter{Count: 120, Wait: 2 * time.Second}
			retry.RunWith(counter, t, func(r *retry.R) {
				var gateway gwv1beta1.Gateway
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: "api-gateway", Namespace: "consul"}, &gateway)
				require.NoError(r, err)

				// check that we have an address to use
				require.Len(r, gateway.Status.Addresses, 1)
				// now we know we have an address, set it so we can use it
				gatewayAddress = gateway.Status.Addresses[0].Value
			})
			fmt.Println(gatewayAddress)

			// now that we've satisfied those assertions, we know reconciliation is done
			// so we can run assertions on the routes and the other objects

			//// finally we check that we can actually route to the service via the gateway
			//k8sOptions := ctx.KubectlOptions(t)
			tr := &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			}
			client := &http.Client{Transport: tr}
			targetHTTPSAddress := fmt.Sprintf("https://%s", gatewayAddress)
			resp, err := client.Get(targetHTTPSAddress)
			require.NoError(t, err)
			require.Equal(t, resp.StatusCode, http.StatusOK)
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
