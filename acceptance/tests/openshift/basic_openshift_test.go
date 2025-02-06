// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package openshift

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"os/exec"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
)

// Test that api gateway basic functionality works in a default installation and a secure installation.
func TestOpenshift_Basic(t *testing.T) {
	cfg := suite.Config()
	newOpenshiftCluster(t, cfg, true, false)

	// this is normally called by the environment, but because we have to bypass we have to call it explicitly
	logf.SetLogger(logr.New(nil))
	logger.Log(t, "creating resources for OpenShift test")

	cmd := exec.Command("kubectl", "apply", "-f", "../fixtures/cases/openshift/basic")
	output, err := cmd.CombinedOutput()
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		cmd := exec.Command("kubectl", "delete", "-f", "../fixtures/cases/openshift/basic")
		output, err := cmd.CombinedOutput()
		assert.NoErrorf(t, err, "failed to delete resources: %s", string(output))
	})

	require.NoErrorf(t, err, "failed to create resources: %s", string(output))

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

	var resp *http.Response
	counter = &retry.Counter{Count: 120, Wait: 2 * time.Second}
	retry.RunWith(counter, t, func(r *retry.R) {
		resp, err = client.Get("https://" + gatewayIP)
		require.NoErrorf(r, err, "request to API gateway failed: %s", err)
		assert.Equalf(r, resp.StatusCode, http.StatusOK, "request to API gateway returned failure code: %d", resp.StatusCode)
	})

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
