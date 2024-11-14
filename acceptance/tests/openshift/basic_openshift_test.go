// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package openshift

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"log"
	"net/http"
	"os/exec"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/types"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Test that api gateway basic functionality works in a default installation and a secure installation.
func TestOpenshift_Basic(t *testing.T) {
	cfg := suite.Config()

	cmd := exec.Command("helm", "repo", "add", "hashicorp", "https://helm.releases.hashicorp.com")
	output, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "failed to add hashicorp helm repo: %s", string(output))

	// namespaceName := helpers.RandomName()
	// FUTURE for some reason NewHelmCluster creates a consul server pod that runs as root which
	//   isn't allowed in OpenShift. In order to test OpenShift properly, we have to call helm and k8s
	//   directly to bypass. Ideally we would just fix the framework that is running the pod as root.
	cmd = exec.Command("helm", "upgrade", "--install", "consul", "hashicorp/consul",
		"--namespace", "consul", "--create-namespace",
		"--set", "global.name=consul",
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
	output, err = cmd.CombinedOutput()
	require.NoErrorf(t, err, "failed to install consul: %s", string(output))

	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		cmd := exec.Command("helm", "uninstall", "consul", "--namespace", "consul")
		output, err := cmd.CombinedOutput()
		require.NoErrorf(t, err, "failed to uninstall consul: %s", string(output))

		cmd = exec.Command("kubectl", "delete", "namespace", "consul")
		output, err = cmd.CombinedOutput()
		assert.NoErrorf(t, err, "failed to delete namespace: %s", string(output))
	})

	// this is normally called by the environment, but because we have to bypass we have to call it explicitly
	logf.SetLogger(logr.New(nil))
	logger.Log(t, "creating resources for OpenShift test")

	kubectlCmd := exec.Command("kubectl", "apply", "-f", "../fixtures/cases/openshift/basic")

	output, err = kubectlCmd.CombinedOutput()
	if err != nil {
		log.Fatal(string(output))
		require.NoError(t, err)
	}

	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		cmd := exec.Command("kubectl", "delete", "-f", "../fixtures/cases/openshift/basic")
		output, err := cmd.CombinedOutput()
		assert.NoErrorf(t, err, "failed to delete resources: %s", string(output))
	})

	// Grab a kubernetes client so that we can verify binding
	// behavior prior to issuing requests through the gateway.
	ctx := suite.Environment().DefaultContext(t)
	k8sClient := ctx.ControllerRuntimeClient(t)

	// Get the public IP address of the API gateway that we created from its status.
	//
	// On startup, the controller can take upwards of 1m to perform leader election,
	// so we may need to wait a long time for the reconcile loop to run (hence the timeout).
	var gatewayIP string
	counter := &retry.Counter{Count: 120, Wait: 2 * time.Second}
	retry.RunWith(counter, t, func(r *retry.R) {
		var gateway gwv1beta1.Gateway
		err := k8sClient.Get(context.Background(), types.NamespacedName{Name: "api-gateway", Namespace: "consul"}, &gateway)
		require.NoError(r, err)

		require.Len(r, gateway.Status.Addresses, 1)
		gatewayIP = gateway.Status.Addresses[0].Value
	})
	logger.Log(t, "API gateway is reachable at:", gatewayIP)

	// Verify that we can reach the services that we created in the mesh
	// via the API gateway that we created.
	//
	// The request goes Gateway --> Frontend --> Backend
	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}}

	resp, err := client.Get("https://" + gatewayIP)
	require.NoError(t, err)
	assert.Equal(t, resp.StatusCode, http.StatusOK)

	var body struct {
		Body          string `json:"body"`
		Code          int    `json:"code"`
		Name          string `json:"name"`
		UpstreamCalls map[string]struct {
			Body string `json:"body"`
			Code int    `json:"code"`
			Name string `json:"name"`
		} `json:"upstream_calls"`
		URI string `json:"uri"`
	}

	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "Hello World", body.Body)
	assert.Equal(t, 200, body.Code)
	assert.Equal(t, "frontend", body.Name)
	assert.Equal(t, "/", body.URI)

	require.Len(t, body.UpstreamCalls, 1)
	require.Contains(t, body.UpstreamCalls, "http://backend.backend:8080")

	backend := body.UpstreamCalls["http://backend.backend:8080"]
	assert.Equal(t, "Hello World", body.Body)
	assert.Equal(t, 200, backend.Code)
	assert.Equal(t, "backend", backend.Name)
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
