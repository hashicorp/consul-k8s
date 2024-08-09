// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package terminatinggateway

import (
	"fmt"
	"strconv"
	"testing"
	"time"

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

			logger.Log(t, "creating terminating gateway")
			k8s.KubectlApplyK(t, ctx.KubectlOptions(t), "../fixtures/bases/terminating-gateway")
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
				k8s.KubectlDeleteK(t, ctx.KubectlOptions(t), "../fixtures/bases/terminating-gateway")
			})

			time.Sleep(5 * time.Second)
			// helpers.WaitForInput(t)

			// if c.secure {
			// UpdateTerminatingGatewayRole(t, consulClient, staticServerPolicyRules)
			// }
			// Register the external service
			k8sOpts := helpers.K8sOptions{
				Options:             ctx.KubectlOptions(t),
				NoCleanupOnFailure:  cfg.NoCleanupOnFailure,
				NoCleanup:           cfg.NoCleanup,
				KustomizeConfigPath: "../fixtures/bases/external-service-registration",
			}

			consulOpts := helpers.ConsulOptions{
				ConsulClient:                    consulClient,
				ExternalServiceNameRegistration: "static-server-registration",
			}

			helpers.RegisterExternalServiceCRD(t, k8sOpts, consulOpts)

			// CreateTerminatingGatewayConfigEntry(t, consulClient, "", "", "static-server")

			// if c.secure {
			// UpdateTerminatingGatewayRole(t, consulClient, staticServerPolicyRules)
			// }
			// helpers.WaitForInput(t)
			//

			helpers.CheckExternalServiceConditions(t, "static-server-registration", k8sOpts.Options)

			// Deploy the static client
			logger.Log(t, "deploying static client")
			k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-client-inject")

			// if c.secure {
			// UpdateTerminatingGatewayRole(t, consulClient, staticServerPolicyRules)
			// }
			// Create the config entry for the terminating gateway.

			// k8s.KubectlDeleteK(t, ctx.KubectlOptions(t), "../fixtures/bases/terminating-gateway")
			// k8s.KubectlApplyK(t, ctx.KubectlOptions(t), "../fixtures/bases/terminating-gateway")

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

			helpers.WaitForInput(t)

			// Test that we can make a call to the terminating gateway.
			logger.Log(t, "trying calls to terminating gateway")
			k8s.CheckStaticServerConnectionSuccessful(t, ctx.KubectlOptions(t), staticClientName, staticServerLocalAddress)
		})
	}
}

const staticServerPolicyRules = `service "static-server" {
  policy = "write"
}`
