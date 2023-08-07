// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package terminatinggateway

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/go-version"
	"github.com/stretchr/testify/require"
)

// Test that egress Destinations route through terminating gateways.
// Destinations are only valid when operating in transparent mode.
func TestTerminatingGatewayDestinations(t *testing.T) {
	cfg := suite.Config()
	if !cfg.EnableTransparentProxy {
		t.Skipf("skipping this test because -enable-transparent-proxy is not set")
	}

	ver, err := version.NewVersion("1.13.0")
	require.NoError(t, err)
	if cfg.ConsulVersion != nil && cfg.ConsulVersion.LessThan(ver) {
		t.Skipf("skipping this test because Destinations are not supported in version %v", cfg.ConsulVersion.String())
	}

	const (
		staticServerServiceName = "static-server.default"
		staticServerHostnameID  = "static-server-hostname"
		staticServerIPID        = "static-server-ip"
		terminatingGatewayRules = `service_prefix "static-server" {
		  policy = "write"
		}`
	)

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

			helmValues := map[string]string{
				"connectInject.enabled":                    "true",
				"terminatingGateways.enabled":              "true",
				"terminatingGateways.gateways[0].name":     "terminating-gateway",
				"terminatingGateways.gateways[0].replicas": "1",

				"global.acls.manageSystemACLs": strconv.FormatBool(c.secure),
				"global.tls.enabled":           strconv.FormatBool(c.secure),
			}

			logger.Log(t, "creating consul cluster")
			releaseName := helpers.RandomName()
			consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)
			consulCluster.Create(t)
			consulClient, _ := consulCluster.SetupConsulClient(t, c.secure)

			// Deploy a static-server that will play the role of an external service.
			logger.Log(t, "creating static-server deployment")
			k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/bases/static-server-https")

			// If ACLs are enabled we need to update the role of the terminating gateway
			// with service:write permissions to the static-server service
			// so that it can request Connect certificates for it.
			if c.secure {
				updateTerminatingGatewayRole(t, consulClient, terminatingGatewayRules)
			}

			// Since we are using the transparent kube DNS, disable the ability
			// of the service to dial the server directly through the sidecar
			createMeshConfigEntry(t, consulClient, "")

			// Create the config entry for the terminating gateway.
			createTerminatingGatewayConfigEntry(t, consulClient, "", "", staticServerHostnameID, staticServerIPID)

			// Deploy the static client
			logger.Log(t, "deploying static client")
			k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-client-tproxy")

			staticServerIP, err := k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "get", "po", "-l", "app=static-server", `-o=jsonpath={.items[0].status.podIP}`)
			require.NoError(t, err)
			require.NotEmpty(t, staticServerIP)

			staticServerHostnameURL := fmt.Sprintf("https://%s", staticServerServiceName)
			staticServerIPURL := fmt.Sprintf("http://%s", staticServerIP)

			// Create the service default declaring the external service (aka Destination)
			logger.Log(t, "creating tcp-based service defaults")
			createServiceDefaultDestination(t, consulClient, "", staticServerHostnameID, "", 443, staticServerServiceName)
			createServiceDefaultDestination(t, consulClient, "", staticServerIPID, "", 80, staticServerIP)

			// If ACLs are enabled, test that intentions prevent connections.
			if c.secure {
				// With the terminating gateway up, we test that we can make a call to it
				// via the static-server. It should fail to connect with the
				// static-server pod because of intentions.
				logger.Log(t, "testing intentions prevent connections through the terminating gateway")
				k8s.CheckStaticServerConnectionFailing(t, ctx.KubectlOptions(t), staticClientName, staticServerIPURL)
				k8s.CheckStaticServerConnectionFailing(t, ctx.KubectlOptions(t), staticClientName, "-k", staticServerHostnameURL)

				logger.Log(t, "adding intentions to allow traffic from client ==> server")
				addIntention(t, consulClient, "", staticClientName, "", staticServerHostnameID)
				addIntention(t, consulClient, "", staticClientName, "", staticServerIPID)
			}

			// Test that we can make a call to the terminating gateway.
			logger.Log(t, "trying calls to terminating gateway")
			k8s.CheckStaticServerConnectionSuccessful(t, ctx.KubectlOptions(t), staticClientName, staticServerIPURL)
			k8s.CheckStaticServerConnectionSuccessful(t, ctx.KubectlOptions(t), staticClientName, "-k", staticServerHostnameURL)

			// Try running some different scenarios
			staticServerHostnameURL = fmt.Sprintf("http://%s", staticServerServiceName)
			staticServerIPURL = fmt.Sprintf("http://%s", staticServerIP)

			// Update the service default declaring the external service (aka Destination)
			logger.Log(t, "updating service defaults to try other scenarios")

			// You can't use TLS w/ protocol set to anything L7; Envoy can't snoop the traffic when the client encrypts it
			createServiceDefaultDestination(t, consulClient, "", staticServerHostnameID, "http", 80, staticServerServiceName)
			createServiceDefaultDestination(t, consulClient, "", staticServerIPID, "http", 80, staticServerIP)

			logger.Log(t, "trying calls to terminating gateway")
			k8s.CheckStaticServerConnectionSuccessful(t, ctx.KubectlOptions(t), staticClientName, staticServerIPURL)
			k8s.CheckStaticServerConnectionSuccessful(t, ctx.KubectlOptions(t), staticClientName, staticServerHostnameURL)
		})
	}
}
func createServiceDefaultDestination(t *testing.T, consulClient *api.Client, serviceNamespace string, name string, protocol string, port int, addresses ...string) {
	t.Helper()

	logger.Log(t, "creating config entry")

	if serviceNamespace != "" {
		logger.Logf(t, "creating the %s namespace in Consul", serviceNamespace)
		_, _, err := consulClient.Namespaces().Create(&api.Namespace{
			Name: serviceNamespace,
		}, nil)
		require.NoError(t, err)
	}

	configEntry := &api.ServiceConfigEntry{
		Kind:      api.ServiceDefaults,
		Name:      name,
		Namespace: serviceNamespace,
		Protocol:  protocol,
		Destination: &api.DestinationConfig{
			Addresses: addresses,
			Port:      port,
		},
	}

	created, _, err := consulClient.ConfigEntries().Set(configEntry, nil)
	require.NoError(t, err)
	require.True(t, created, "failed to create config entry")
}

func createMeshConfigEntry(t *testing.T, consulClient *api.Client, namespace string) {
	t.Helper()

	logger.Log(t, "creating mesh config entry to enable MeshDestinationOnly")
	created, _, err := consulClient.ConfigEntries().Set(&api.MeshConfigEntry{
		Namespace: namespace,
		TransparentProxy: api.TransparentProxyMeshConfig{
			MeshDestinationsOnly: true,
		},
	}, nil)
	require.NoError(t, err)
	require.True(t, created, "failed to create config entry")
}
