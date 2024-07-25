// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package terminatinggateway

import (
	"fmt"
	"strconv"
	"testing"

	terratestk8s "github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
)

const testNamespace = "ns1"

// Test we can connect through the terminating gateway when both
// the terminating gateway and the connect service are in the same namespace.
func TestTerminatingGatewaySingleNamespace(t *testing.T) {
	cfg := suite.Config()
	if !cfg.EnableEnterprise {
		t.Skipf("skipping this test because -enable-enterprise is not set")
	}

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
				"connectInject.enabled": "true",
				"connectInject.consulNamespaces.consulDestinationNamespace": testNamespace,

				"global.enableConsulNamespaces": "true",
				"global.acls.manageSystemACLs":  strconv.FormatBool(c.secure),
				"global.tls.enabled":            strconv.FormatBool(c.secure),

				"terminatingGateways.enabled":                     "true",
				"terminatingGateways.gateways[0].name":            "terminating-gateway",
				"terminatingGateways.gateways[0].replicas":        "1",
				"terminatingGateways.gateways[0].consulNamespace": testNamespace,
			}

			releaseName := helpers.RandomName()
			consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)
			consulCluster.Create(t)

			consulClient, _ := consulCluster.SetupConsulClient(t, c.secure)

			logger.Logf(t, "creating Kubernetes namespace %s", testNamespace)
			k8s.RunKubectl(t, ctx.KubectlOptions(t), "create", "ns", testNamespace)
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "delete", "ns", testNamespace)
			})

			nsK8SOptions := &terratestk8s.KubectlOptions{
				ContextName: ctx.KubectlOptions(t).ContextName,
				ConfigPath:  ctx.KubectlOptions(t).ConfigPath,
				Namespace:   testNamespace,
			}

			// Deploy a static-server that will play the role of an external service.
			logger.Log(t, "creating static-server deployment")
			k8s.DeployKustomize(t, nsK8SOptions, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/bases/static-server")

			// Register the external service
			k8sOptions := helpers.K8sOptions{
				Options:            ctx.KubectlOptions(t),
				NoCleanupOnFailure: cfg.NoCleanupOnFailure,
				NoCleanup:          cfg.NoCleanup,
				ConfigPath:         "../fixtures/cases/terminating-gateway-namespaces/external-service.yaml",
			}

			consulOptions := helpers.ConsulOptions{
				ConsulClient: consulClient,
			}

			helpers.RegisterExternalServiceCRD(t, k8sOptions, consulOptions)

			// If ACLs are enabled we need to update the role of the terminating gateway
			// with service:write permissions to the static-server service
			// so that it can request Connect certificates for it.
			if c.secure {
				UpdateTerminatingGatewayRole(t, consulClient, fmt.Sprintf(staticServerPolicyRulesNamespace, testNamespace))
			}

			// Create the config entry for the terminating gateway.
			// This case cannot be replicated using CRDs because the consul namespace does not match the kubernetes namespace the terminating gateway is in
			CreateTerminatingGatewayConfigEntry(t, consulClient, testNamespace, testNamespace, staticServerName)

			// Deploy the static client.
			logger.Log(t, "deploying static client")
			k8s.DeployKustomize(t, nsK8SOptions, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-client-namespaces")

			// If ACLs are enabled, test that intentions prevent connections.
			if c.secure {
				// With the terminating gateway up, we test that we can make a call to it
				// via the static-server. It should fail to connect with the
				// static-server pod because of intentions.
				logger.Log(t, "testing intentions prevent connections through the terminating gateway")
				k8s.CheckStaticServerConnectionFailing(t, nsK8SOptions, staticClientName, staticServerLocalAddress)

				logger.Log(t, "adding intentions to allow traffic from client ==> server")
				AddIntention(t, consulClient, "", testNamespace, staticClientName, testNamespace, staticServerName)
			}

			// Test that we can make a call to the terminating gateway.
			logger.Log(t, "trying calls to terminating gateway")
			k8s.CheckStaticServerConnectionSuccessful(t, nsK8SOptions, staticClientName, staticServerLocalAddress)
		})
	}
}

// Test we can connect through the terminating gateway when the terminating gateway,
// the external service, and the connect service are in different namespace.
func TestTerminatingGatewayNamespaceMirroring(t *testing.T) {
	cfg := suite.Config()
	if !cfg.EnableEnterprise {
		t.Skipf("skipping this test because -enable-enterprise is not set")
	}

	type config struct {
		path      string
		namespace string
	}

	cases := map[string]struct {
		termGWConfigPath                      string
		externalServiceRegistrationConfigPath string
		staticClientConfigPath                string
	}{
		"all in default namespace": {
			termGWConfigPath:                      "../fixtures/cases/terminating-gateway/terminating-gateway.yaml",
			externalServiceRegistrationConfigPath: "../fixtures/cases/terminating-gateway/external-service.yaml",
			staticClientConfigPath:                "../fixtures/bases/static-server",
		},
		// "all in non-default namespace": {},
		// "external service in default namespace everything else in non-default namespace":    {},
		// "terminating gateway in default namespace everything else in non-default namespace": {},
		// "mesh service in default namespace everything else in non-default namespace":        {},
	}
	for name, tc := range cases {
		for _, secure := range []bool{true, false} {
			name := fmt.Sprintf("%s secure: %t", name, secure)
			t.Run(name, func(t *testing.T) {
				ctx := suite.Environment().DefaultContext(t)

				// Install the Helm chart without the terminating gateway first
				// so that we can create the namespace for it.
				helmValues := map[string]string{
					"connectInject.enabled":                       "true",
					"connectInject.consulNamespaces.mirroringK8S": "true",

					"global.enableConsulNamespaces": "true",
					"global.acls.manageSystemACLs":  strconv.FormatBool(secure),
					"global.tls.enabled":            strconv.FormatBool(secure),

					"terminatingGateways.enabled":              "true",
					"terminatingGateways.gateways[0].name":     "terminating-gateway",
					"terminatingGateways.gateways[0].replicas": "1",
				}

				releaseName := helpers.RandomName()
				consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)

				consulCluster.Create(t)

				consulClient, _ := consulCluster.SetupConsulClient(t, secure)

				logger.Logf(t, "creating Kubernetes namespace %s", testNamespace)
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "create", "ns", testNamespace)
				helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
					k8s.RunKubectl(t, ctx.KubectlOptions(t), "delete", "ns", testNamespace)
				})

				StaticClientNamespace := "ns2"
				logger.Logf(t, "creating Kubernetes namespace %s", StaticClientNamespace)
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "create", "ns", StaticClientNamespace)
				helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
					k8s.RunKubectl(t, ctx.KubectlOptions(t), "delete", "ns", StaticClientNamespace)
				})

				// ns1K8SOptions := &terratestk8s.KubectlOptions{
				// ContextName: ctx.KubectlOptions(t).ContextName,
				// ConfigPath:  ctx.KubectlOptions(t).ConfigPath,
				// Namespace:   testNamespace,
				// }
				// ns2K8SOptions := &terratestk8s.KubectlOptions{
				// ContextName: ctx.KubectlOptions(t).ContextName,
				// ConfigPath:  ctx.KubectlOptions(t).ConfigPath,
				// Namespace:   StaticClientNamespace,
				// }

				// Deploy a static-server that will play the role of an external service.
				logger.Log(t, "creating static-server deployment")
				k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, tc.staticClientConfigPath)

				// Register the external service
				k8sOptions := helpers.K8sOptions{
					Options:            ctx.KubectlOptions(t),
					NoCleanupOnFailure: cfg.NoCleanupOnFailure,
					NoCleanup:          cfg.NoCleanup,
					ConfigPath:         tc.externalServiceRegistrationConfigPath,
				}

				consulOptions := helpers.ConsulOptions{
					ConsulClient: consulClient,
					Namespace:    testNamespace,
				}

				// Create the config entry for the terminating gateway.
				CreateTerminatingGatewayFromCRD(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, tc.termGWConfigPath)

				helpers.RegisterExternalServiceCRD(t, k8sOptions, consulOptions)

				// Deploy the static client
				logger.Log(t, "deploying static client")
				k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-client-namespaces")

				// If ACLs are enabled, test that intentions prevent connections.
				if secure {
					// With the terminating gateway up, we test that we can make a call to it
					// via the static-server. It should fail to connect with the
					// static-server pod because of intentions.
					logger.Log(t, "testing intentions prevent connections through the terminating gateway")
					k8s.CheckStaticServerConnectionFailing(t, ctx.KubectlOptions(t), staticClientName, staticServerLocalAddress)

					logger.Log(t, "adding intentions to allow traffic from client ==> server")
					AddIntention(t, consulClient, "", StaticClientNamespace, staticClientName, testNamespace, staticServerName)
				}

				// Test that we can make a call to the terminating gateway
				logger.Log(t, "trying calls to terminating gateway")
				k8s.CheckStaticServerConnectionSuccessful(t, ctx.KubectlOptions(t), staticClientName, staticServerLocalAddress)
			})
		}
	}
}

const staticServerPolicyRulesNamespace = `namespace %q {
service "static-server" {
  policy = "write"
}}`
