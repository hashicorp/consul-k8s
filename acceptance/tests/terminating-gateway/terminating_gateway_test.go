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
)

// Test that terminating gateways work in a default and secure installations.
func TestTerminatingGateway(t *testing.T) {
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

			// Deploy a static-server that will play the role of an external service.
			logger.Log(t, "creating static-server deployment")
			k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/bases/static-server")

			// Once the cluster is up, register the external service, then create the config entry.
			consulClient, _ := consulCluster.SetupConsulClient(t, c.secure)

			// Register the external service
			helpers.RegisterExternalService(t, consulClient, "", staticServerName, staticServerName, 80)

			// If ACLs are enabled we need to update the role of the terminating gateway
			// with service:write permissions to the static-server service
			// so that it can request Connect certificates for it.
			if c.secure {
				UpdateTerminatingGatewayRole(t, consulClient, staticServerPolicyRules)
			}

			// Create the config entry for the terminating gateway.
			CreateTerminatingGatewayConfigEntry(t, consulClient, "", "", staticServerName)

			// Deploy the static client
			logger.Log(t, "deploying static client")
			k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-client-inject")

			// If ACLs are enabled, test that intentions prevent connections.
			if c.secure {
				// With the terminating gateway up, we test that we can make a call to it
				// via the static-server. It should fail to connect with the
				// static-server pod because of intentions.
				logger.Log(t, "testing intentions prevent connections through the terminating gateway")
				k8s.CheckStaticServerConnectionFailing(t, ctx.KubectlOptions(t), staticClientName, staticServerLocalAddress)

				logger.Log(t, "adding intentions to allow traffic from client ==> server")
				AddIntention(t, consulClient, "", "", staticClientName, "", staticServerName)
			}

			// Test that we can make a call to the terminating gateway.
			logger.Log(t, "trying calls to terminating gateway")
			k8s.CheckStaticServerConnectionSuccessful(t, ctx.KubectlOptions(t), staticClientName, staticServerLocalAddress)
		})
	}
}

const staticServerPolicyRules = `service "static-server" {
  policy = "write"
}`
