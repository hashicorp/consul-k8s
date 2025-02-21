// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package apigateway

import (
	"context"
	"fmt"
	"testing"

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

// TestAPIGateway_ExternalServers tests that connect works when using external servers.
// It sets up an external Consul server in the same cluster but a different Helm installation
// and then treats this server as external.
func TestAPIGateway_ExternalServers(t *testing.T) {
	cfg := suite.Config()
	ctx := suite.Environment().DefaultContext(t)

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
		"server.enabled":                        "false",
		"global.acls.manageSystemACLs":          "true",
		"global.tls.enabled":                    "true",
		"connectInject.enabled":                 "true",
		"externalServers.enabled":               "true",
		"externalServers.hosts[0]":              fmt.Sprintf("%s-consul-server", serverReleaseName),
		"externalServers.httpsPort":             "8501",
		"global.tls.caCert.secretName":          fmt.Sprintf("%s-consul-ca-cert", serverReleaseName),
		"global.tls.caCert.secretKey":           "tls.crt",
		"global.acls.bootstrapToken.secretName": fmt.Sprintf("%s-consul-bootstrap-acl-token", serverReleaseName),
		"global.acls.bootstrapToken.secretKey":  "token",
	}

	releaseName := helpers.RandomName()
	consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)
	consulCluster.SkipCheckForPreviousInstallations = true

	consulCluster.Create(t)

	logger.Log(t, "creating static-server and static-client deployments")
	k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")
	k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-client-inject")

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

	// Create certificate secret, we do this separately since
	// applying the secret will make an invalid certificate that breaks other tests
	logger.Log(t, "creating certificate secret")
	out, err := k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "apply", "-f", "../fixtures/bases/api-gateway/certificate.yaml")
	require.NoError(t, err, out)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		// Ignore errors here because if the test ran as expected
		// the custom resources will have been deleted.
		k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "delete", "-f", "../fixtures/bases/api-gateway/certificate.yaml")
	})

	logger.Log(t, "creating api-gateway resources")
	out, err = k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "apply", "-k", "../fixtures/bases/api-gateway")
	require.NoError(t, err, out)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		// Ignore errors here because if the test ran as expected
		// the custom resources will have been deleted.
		k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "delete", "-k", "../fixtures/bases/api-gateway")
	})

	logger.Log(t, "patching route to target server")
	k8s.RunKubectl(t, ctx.KubectlOptions(t), "patch", "httproute", "http-route", "-p", `{"spec":{"rules":[{"backendRefs":[{"name":"static-server","port":80}]}]}}`, "--type=merge")

	// Grab a kubernetes client so that we can verify binding
	// behavior prior to issuing requests through the gateway.
	k8sClient := ctx.ControllerRuntimeClient(t)

	// On startup, the controller can take upwards of 1m to perform
	// leader election so we may need to wait a long time for
	// the reconcile loop to run (hence a ~1m timeout here).
	var gatewayAddress string
	retryCheck(t, 60, func(r *retry.R) {
		var gateway gwv1beta1.Gateway
		err := k8sClient.Get(context.Background(), types.NamespacedName{Name: "gateway", Namespace: "default"}, &gateway)
		require.NoError(r, err)

		// check that we have an address to use
		require.Len(r, gateway.Status.Addresses, 1)
		// now we know we have an address, set it so we can use it
		gatewayAddress = gateway.Status.Addresses[0].Value
	})

	k8sOptions := ctx.KubectlOptions(t)
	targetAddress := fmt.Sprintf("http://%s/", gatewayAddress)

	// check that intentions keep our connection from happening
	k8s.CheckStaticServerHTTPConnectionFailing(t, k8sOptions, StaticClientName, targetAddress)

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

	// Test that we can make a call to the api gateway
	// via the static-client pod. It should route to the static-server pod.
	logger.Log(t, "trying calls to api gateway")
	k8s.CheckStaticServerConnectionSuccessful(t, k8sOptions, StaticClientName, targetAddress)
}
