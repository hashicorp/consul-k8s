package terminatinggateway

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/hashicorp/consul-helm/test/acceptance/framework"
	"github.com/hashicorp/consul-helm/test/acceptance/helpers"
	"github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
)

const testNamespace = "ns1"

// Test we can connect through the terminating gateway when both
// the terminating gateway and the connect service are in the same namespace.
// These tests currently only test non-secure and secure without auto-encrypt installations
// because in the case of namespaces there isn't a significant distinction in code between auto-encrypt
// and non-auto-encrypt secure installations, so testing just one is enough.
func TestTerminatingGatewaySingleNamespace(t *testing.T) {
	cfg := suite.Config()
	if !cfg.EnableEnterprise {
		t.Skipf("skipping this test because -enable-enterprise is not set")
	}

	cases := []struct {
		secure bool
	}{
		{
			false,
		},
		{
			true,
		},
	}
	for _, c := range cases {
		name := fmt.Sprintf("secure: %t", c.secure)
		t.Run(name, func(t *testing.T) {
			ctx := suite.Environment().DefaultContext(t)

			// Install the Helm chart without the terminating gateway first
			// so that we can create the namespace for it.
			helmValues := map[string]string{
				"connectInject.enabled": "true",
				"connectInject.consulNamespaces.consulDestinationNamespace": testNamespace,

				"global.enableConsulNamespaces": "true",
				"global.acls.manageSystemACLs":  strconv.FormatBool(c.secure),
				"global.tls.enabled":            strconv.FormatBool(c.secure),
			}

			releaseName := helpers.RandomName()
			consulCluster := framework.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)

			consulCluster.Create(t)

			consulClient := consulCluster.SetupConsulClient(t, c.secure)

			// Create the destination namespace in the non-secure case.
			// In the secure installation, this namespace is created by the server-acl-init job.
			if !c.secure {
				t.Logf("creating the %s namespace in Consul", testNamespace)
				_, _, err := consulClient.Namespaces().Create(&api.Namespace{
					Name: testNamespace,
				}, nil)
				require.NoError(t, err)
			}

			t.Log("upgrading with terminating gateways enabled")
			consulCluster.Upgrade(t, map[string]string{
				"terminatingGateways.enabled":                     "true",
				"terminatingGateways.gateways[0].name":            "terminating-gateway",
				"terminatingGateways.gateways[0].replicas":        "1",
				"terminatingGateways.gateways[0].consulNamespace": testNamespace,
			})

			t.Logf("creating Kubernetes namespace %s", testNamespace)
			helpers.RunKubectl(t, ctx.KubectlOptions(), "create", "ns", testNamespace)
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
				helpers.RunKubectl(t, ctx.KubectlOptions(), "delete", "ns", testNamespace)
			})

			nsK8SOptions := &k8s.KubectlOptions{
				ContextName: ctx.KubectlOptions().ContextName,
				ConfigPath:  ctx.KubectlOptions().ConfigPath,
				Namespace:   testNamespace,
			}

			// Deploy a static-server that will play the role of an external service
			t.Log("creating static-server deployment")
			helpers.DeployKustomize(t, nsK8SOptions, cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/bases/static-server")

			// Register the external service
			registerExternalService(t, consulClient, testNamespace)

			// If ACLs are enabled we need to update the token of the terminating gateway
			// with service:write permissions to the static-server service
			// so that it can can request Connect certificates for it.
			if c.secure {
				updateTerminatingGatewayToken(t, consulClient, fmt.Sprintf(staticServerPolicyRulesNamespace, testNamespace))
			}

			// Create the config entry for the terminating gateway
			createTerminatingGatewayConfigEntry(t, consulClient, testNamespace, testNamespace)

			// Deploy the static client
			t.Log("deploying static client")
			helpers.DeployKustomize(t, nsK8SOptions, cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-client-namespaces")

			// If ACLs are enabled, test that intentions prevent connections.
			if c.secure {
				// With the terminating gateway up, we test that we can make a call to it
				// via the static-server. It should fail to connect with the
				// static-server pod because of intentions.
				assertNoConnectionAndAddIntention(t, consulClient, nsK8SOptions, testNamespace, testNamespace)
			}

			// Test that we can make a call to the terminating gateway
			t.Log("trying calls to terminating gateway")
			helpers.CheckStaticServerConnection(t, nsK8SOptions, true, staticClientName, "http://localhost:1234")
		})
	}
}

// Test we can connect through the terminating gateway when the terminating gateway,
// the external service, and the connect service are in different namespace.
// These tests currently only test non-secure and secure without auto-encrypt installations
// because in the case of namespaces there isn't a significant distinction in code between auto-encrypt
// and non-auto-encrypt secure installations, so testing just one is enough.
func TestTerminatingGatewayNamespaceMirroring(t *testing.T) {
	cfg := suite.Config()
	if !cfg.EnableEnterprise {
		t.Skipf("skipping this test because -enable-enterprise is not set")
	}

	cases := []struct {
		secure bool
	}{
		{
			false,
		},
		{
			true,
		},
	}
	for _, c := range cases {
		name := fmt.Sprintf("secure: %t", c.secure)
		t.Run(name, func(t *testing.T) {
			ctx := suite.Environment().DefaultContext(t)

			// Install the Helm chart without the terminating gateway first
			// so that we can create the namespace for it.
			helmValues := map[string]string{
				"connectInject.enabled":                       "true",
				"connectInject.consulNamespaces.mirroringK8S": "true",

				"global.enableConsulNamespaces": "true",
				"global.acls.manageSystemACLs":  strconv.FormatBool(c.secure),
				"global.tls.enabled":            strconv.FormatBool(c.secure),

				"terminatingGateways.enabled":              "true",
				"terminatingGateways.gateways[0].name":     "terminating-gateway",
				"terminatingGateways.gateways[0].replicas": "1",
			}

			releaseName := helpers.RandomName()
			consulCluster := framework.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)

			consulCluster.Create(t)

			consulClient := consulCluster.SetupConsulClient(t, c.secure)

			t.Logf("creating Kubernetes namespace %s", testNamespace)
			helpers.RunKubectl(t, ctx.KubectlOptions(), "create", "ns", testNamespace)
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
				helpers.RunKubectl(t, ctx.KubectlOptions(), "delete", "ns", testNamespace)
			})

			staticClientNamespace := "ns2"
			t.Logf("creating Kubernetes namespace %s", staticClientNamespace)
			helpers.RunKubectl(t, ctx.KubectlOptions(), "create", "ns", staticClientNamespace)
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
				helpers.RunKubectl(t, ctx.KubectlOptions(), "delete", "ns", staticClientNamespace)
			})

			ns1K8SOptions := &k8s.KubectlOptions{
				ContextName: ctx.KubectlOptions().ContextName,
				ConfigPath:  ctx.KubectlOptions().ConfigPath,
				Namespace:   testNamespace,
			}
			ns2K8SOptions := &k8s.KubectlOptions{
				ContextName: ctx.KubectlOptions().ContextName,
				ConfigPath:  ctx.KubectlOptions().ConfigPath,
				Namespace:   staticClientNamespace,
			}

			// Deploy a static-server that will play the role of an external service.
			t.Log("creating static-server deployment")
			helpers.DeployKustomize(t, ns1K8SOptions, cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/bases/static-server")

			// Register the external service
			registerExternalService(t, consulClient, testNamespace)

			// If ACLs are enabled we need to update the token of the terminating gateway
			// with service:write permissions to the static-server service
			// so that it can can request Connect certificates for it.
			if c.secure {
				updateTerminatingGatewayToken(t, consulClient, fmt.Sprintf(staticServerPolicyRulesNamespace, testNamespace))
			}

			// Create the config entry for the terminating gateway
			createTerminatingGatewayConfigEntry(t, consulClient, ctx.KubectlOptions().Namespace, testNamespace)

			// Deploy the static client
			t.Log("deploying static client")
			helpers.DeployKustomize(t, ns2K8SOptions, cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-client-namespaces")

			// If ACLs are enabled, test that intentions prevent connections.
			if c.secure {
				// With the terminating gateway up, we test that we can make a call to it
				// via the static-server. It should fail to connect with the
				// static-server pod because of intentions.
				assertNoConnectionAndAddIntention(t, consulClient, ns2K8SOptions, staticClientNamespace, testNamespace)
			}

			// Test that we can make a call to the terminating gateway
			t.Log("trying calls to terminating gateway")
			helpers.CheckStaticServerConnection(t, ns2K8SOptions, true, staticClientName, "http://localhost:1234")
		})
	}
}

const staticServerPolicyRulesNamespace = `namespace %q {
service "static-server" {
  policy = "write"
}}`
