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
			consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)

			consulCluster.Create(t)

			consulClient, _ := consulCluster.SetupConsulClient(t, c.secure)

			// Create the destination namespace in the non-secure case.
			// In the secure installation, this namespace is created by the server-acl-init job.
			if !c.secure {
				logger.Logf(t, "creating the %s namespace in Consul", testNamespace)
				_, _, err := consulClient.Namespaces().Create(&api.Namespace{
					Name: testNamespace,
				}, nil)
				require.NoError(t, err)
			}

			logger.Log(t, "upgrading with terminating gateways enabled")
			consulCluster.Upgrade(t, map[string]string{
				"terminatingGateways.enabled":                     "true",
				"terminatingGateways.gateways[0].name":            "terminating-gateway",
				"terminatingGateways.gateways[0].replicas":        "1",
				"terminatingGateways.gateways[0].consulNamespace": testNamespace,
			})

			logger.Logf(t, "creating Kubernetes namespace %s", testNamespace)
			k8s.RunKubectl(t, ctx.KubectlOptions(t), "create", "ns", testNamespace)
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "delete", "ns", testNamespace)
			})

			nsK8SOptions := &terratestk8s.KubectlOptions{
				ContextName: ctx.KubectlOptions(t).ContextName,
				ConfigPath:  ctx.KubectlOptions(t).ConfigPath,
				Namespace:   testNamespace,
			}

			// Deploy a static-server that will play the role of an external service.
			logger.Log(t, "creating static-server deployment")
			k8s.DeployKustomize(t, nsK8SOptions, cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/bases/static-server")

			// Register the external service.
			registerExternalService(t, consulClient, testNamespace)

			// If ACLs are enabled we need to update the role of the terminating gateway
			// with service:write permissions to the static-server service
			// so that it can request Connect certificates for it.
			if c.secure {
				updateTerminatingGatewayRole(t, consulClient, fmt.Sprintf(staticServerPolicyRulesNamespace, testNamespace))
			}

			// Create the config entry for the terminating gateway.
			createTerminatingGatewayConfigEntry(t, consulClient, testNamespace, testNamespace, staticServerName)

			// Deploy the static client.
			logger.Log(t, "deploying static client")
			k8s.DeployKustomize(t, nsK8SOptions, cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-client-namespaces")

			// If ACLs are enabled, test that intentions prevent connections.
			if c.secure {
				// With the terminating gateway up, we test that we can make a call to it
				// via the static-server. It should fail to connect with the
				// static-server pod because of intentions.
				logger.Log(t, "testing intentions prevent connections through the terminating gateway")
				k8s.CheckStaticServerConnectionFailing(t, nsK8SOptions, staticClientName, staticServerLocalAddress)

				logger.Log(t, "adding intentions to allow traffic from client ==> server")
				addIntention(t, consulClient, testNamespace, staticClientName, testNamespace, staticServerName)
			}

			// Test that we can make a call to the terminating gateway.
			logger.Log(t, "trying calls to terminating gateway")
			k8s.CheckStaticServerConnectionSuccessful(t, nsK8SOptions, staticClientName, staticServerLocalAddress)
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
			consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)

			consulCluster.Create(t)

			consulClient, _ := consulCluster.SetupConsulClient(t, c.secure)

			logger.Logf(t, "creating Kubernetes namespace %s", testNamespace)
			k8s.RunKubectl(t, ctx.KubectlOptions(t), "create", "ns", testNamespace)
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "delete", "ns", testNamespace)
			})

			StaticClientNamespace := "ns2"
			logger.Logf(t, "creating Kubernetes namespace %s", StaticClientNamespace)
			k8s.RunKubectl(t, ctx.KubectlOptions(t), "create", "ns", StaticClientNamespace)
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "delete", "ns", StaticClientNamespace)
			})

			ns1K8SOptions := &terratestk8s.KubectlOptions{
				ContextName: ctx.KubectlOptions(t).ContextName,
				ConfigPath:  ctx.KubectlOptions(t).ConfigPath,
				Namespace:   testNamespace,
			}
			ns2K8SOptions := &terratestk8s.KubectlOptions{
				ContextName: ctx.KubectlOptions(t).ContextName,
				ConfigPath:  ctx.KubectlOptions(t).ConfigPath,
				Namespace:   StaticClientNamespace,
			}

			// Deploy a static-server that will play the role of an external service.
			logger.Log(t, "creating static-server deployment")
			k8s.DeployKustomize(t, ns1K8SOptions, cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/bases/static-server")

			// Register the external service
			registerExternalService(t, consulClient, testNamespace)

			// If ACLs are enabled we need to update the role of the terminating gateway
			// with service:write permissions to the static-server service
			// so that it can request Connect certificates for it.
			if c.secure {
				updateTerminatingGatewayRole(t, consulClient, fmt.Sprintf(staticServerPolicyRulesNamespace, testNamespace))
			}

			// Create the config entry for the terminating gateway
			createTerminatingGatewayConfigEntry(t, consulClient, "", testNamespace, staticServerName)

			// Deploy the static client
			logger.Log(t, "deploying static client")
			k8s.DeployKustomize(t, ns2K8SOptions, cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-client-namespaces")

			// If ACLs are enabled, test that intentions prevent connections.
			if c.secure {
				// With the terminating gateway up, we test that we can make a call to it
				// via the static-server. It should fail to connect with the
				// static-server pod because of intentions.
				logger.Log(t, "testing intentions prevent connections through the terminating gateway")
				k8s.CheckStaticServerConnectionFailing(t, ns2K8SOptions, staticClientName, staticServerLocalAddress)

				logger.Log(t, "adding intentions to allow traffic from client ==> server")
				addIntention(t, consulClient, StaticClientNamespace, staticClientName, testNamespace, staticServerName)
			}

			// Test that we can make a call to the terminating gateway
			logger.Log(t, "trying calls to terminating gateway")
			k8s.CheckStaticServerConnectionSuccessful(t, ns2K8SOptions, staticClientName, staticServerLocalAddress)
		})
	}
}

const staticServerPolicyRulesNamespace = `namespace %q {
service "static-server" {
  policy = "write"
}}`
