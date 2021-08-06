package ingressgateway

import (
	"fmt"
	"strconv"
	"testing"

	terratestk8s "github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/hashicorp/consul-helm/test/acceptance/framework/consul"
	"github.com/hashicorp/consul-helm/test/acceptance/framework/helpers"
	"github.com/hashicorp/consul-helm/test/acceptance/framework/k8s"
	"github.com/hashicorp/consul-helm/test/acceptance/framework/logger"
	"github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
)

const testNamespace = "test"

// Test we can connect through the ingress gateway when both
// the ingress gateway and the connect service are in the same namespace.
// These tests currently only test non-secure and secure without auto-encrypt installations
// because in the case of namespaces there isn't a significant distinction in code between auto-encrypt
// and non-auto-encrypt secure installations, so testing just one is enough.
func TestIngressGatewaySingleNamespace(t *testing.T) {
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

			// Install the Helm chart without the ingress gateway first
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

			consulClient := consulCluster.SetupConsulClient(t, c.secure)

			// Create the destination namespace in the non-secure case.
			// In the secure installation, this namespace is created by the server-acl-init job.
			if !c.secure {
				logger.Logf(t, "creating the %s namespace in Consul", testNamespace)
				_, _, err := consulClient.Namespaces().Create(&api.Namespace{
					Name: testNamespace,
				}, nil)
				require.NoError(t, err)
			}

			logger.Log(t, "upgrading with ingress gateways enabled")
			consulCluster.Upgrade(t, map[string]string{
				"ingressGateways.enabled":                     "true",
				"ingressGateways.gateways[0].name":            "ingress-gateway",
				"ingressGateways.gateways[0].replicas":        "1",
				"ingressGateways.gateways[0].consulNamespace": testNamespace,
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

			logger.Logf(t, "creating server in %s namespace", testNamespace)
			k8s.DeployKustomize(t, nsK8SOptions, cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")

			// We use the static-client pod so that we can make calls to the ingress gateway
			// via kubectl exec without needing a route into the cluster from the test machine.
			logger.Logf(t, "creating static-client in %s namespace", testNamespace)
			k8s.DeployKustomize(t, nsK8SOptions, cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/bases/static-client")

			// With the cluster up, we can create our ingress-gateway config entry.
			logger.Log(t, "creating config entry")
			created, _, err := consulClient.ConfigEntries().Set(&api.IngressGatewayConfigEntry{
				Kind:      api.IngressGateway,
				Name:      "ingress-gateway",
				Namespace: testNamespace,
				Listeners: []api.IngressListener{
					{
						Port:     8080,
						Protocol: "tcp",
						Services: []api.IngressService{
							{
								Name:      "static-server",
								Namespace: testNamespace,
							},
						},
					},
				},
			}, nil)
			require.NoError(t, err)
			require.Equal(t, true, created, "config entry failed")

			ingressGatewayService := fmt.Sprintf("http://%s-consul-ingress-gateway.%s:8080/", releaseName, ctx.KubectlOptions(t).Namespace)

			// If ACLs are enabled, test that intentions prevent connections.
			if c.secure {
				// With the ingress gateway up, we test that we can make a call to it
				// via the bounce pod. It should fail to connect with the
				// static-server pod because of intentions.
				logger.Log(t, "testing intentions prevent ingress")
				k8s.CheckStaticServerConnectionFailing(t, nsK8SOptions, "-H", "Host: static-server.ingress.consul", ingressGatewayService)

				// Now we create the allow intention.
				logger.Log(t, "creating ingress-gateway => static-server intention")
				_, err = consulClient.Connect().IntentionUpsert(&api.Intention{
					SourceName:      "ingress-gateway",
					SourceNS:        testNamespace,
					DestinationName: "static-server",
					DestinationNS:   testNamespace,
					Action:          api.IntentionActionAllow,
				}, nil)
				require.NoError(t, err)
			}

			// Test that we can make a call to the ingress gateway
			// via the static-client pod. It should route to the static-server pod.
			logger.Log(t, "trying calls to ingress gateway")
			k8s.CheckStaticServerConnectionSuccessful(t, nsK8SOptions, "-H", "Host: static-server.ingress.consul", ingressGatewayService)
		})
	}
}

// Test we can connect through the ingress gateway when both
// the ingress gateway and the connect service are in different namespaces.
// These tests currently only test non-secure and secure without auto-encrypt installations
// because in the case of namespaces there isn't a significant distinction in code between auto-encrypt
// and non-auto-encrypt secure installations, so testing just one is enough.
func TestIngressGatewayNamespaceMirroring(t *testing.T) {
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

			// Install the Helm chart without the ingress gateway first
			// so that we can create the namespace for it.
			helmValues := map[string]string{
				"connectInject.enabled":                       "true",
				"connectInject.consulNamespaces.mirroringK8S": "true",

				"global.enableConsulNamespaces": "true",
				"global.acls.manageSystemACLs":  strconv.FormatBool(c.secure),
				"global.tls.enabled":            strconv.FormatBool(c.secure),

				"ingressGateways.enabled":              "true",
				"ingressGateways.gateways[0].name":     "ingress-gateway",
				"ingressGateways.gateways[0].replicas": "1",
			}

			releaseName := helpers.RandomName()
			consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)

			consulCluster.Create(t)

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

			logger.Logf(t, "creating server in %s namespace", testNamespace)
			k8s.DeployKustomize(t, nsK8SOptions, cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")

			// We use the static-client pod so that we can make calls to the ingress gateway
			// via kubectl exec without needing a route into the cluster from the test machine.
			logger.Logf(t, "creating static-client in %s namespace", testNamespace)
			k8s.DeployKustomize(t, nsK8SOptions, cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/bases/static-client")

			consulClient := consulCluster.SetupConsulClient(t, c.secure)

			// With the cluster up, we can create our ingress-gateway config entry.
			logger.Log(t, "creating config entry")
			created, _, err := consulClient.ConfigEntries().Set(&api.IngressGatewayConfigEntry{
				Kind:      api.IngressGateway,
				Name:      "ingress-gateway",
				Namespace: "default",
				Listeners: []api.IngressListener{
					{
						Port:     8080,
						Protocol: "tcp",
						Services: []api.IngressService{
							{
								Name:      "static-server",
								Namespace: testNamespace,
							},
						},
					},
				},
			}, nil)
			require.NoError(t, err)
			require.Equal(t, true, created, "config entry failed")

			ingressGatewayService := fmt.Sprintf("http://%s-consul-ingress-gateway.%s:8080/", releaseName, ctx.KubectlOptions(t).Namespace)

			// If ACLs are enabled, test that intentions prevent connections.
			if c.secure {
				// With the ingress gateway up, we test that we can make a call to it
				// via the bounce pod. It should fail to connect with the
				// static-server pod because of intentions.
				logger.Log(t, "testing intentions prevent ingress")
				k8s.CheckStaticServerConnectionFailing(t, nsK8SOptions, "-H", "Host: static-server.ingress.consul", ingressGatewayService)

				// Now we create the allow intention.
				logger.Log(t, "creating ingress-gateway => static-server intention")
				_, err = consulClient.Connect().IntentionUpsert(&api.Intention{
					SourceName:      "ingress-gateway",
					SourceNS:        "default",
					DestinationName: "static-server",
					DestinationNS:   testNamespace,
					Action:          api.IntentionActionAllow,
				}, nil)
				require.NoError(t, err)
			}

			// Test that we can make a call to the ingress gateway
			// via the static-client pod. It should route to the static-server pod.
			logger.Log(t, "trying calls to ingress gateway")
			k8s.CheckStaticServerConnectionSuccessful(t, nsK8SOptions, "-H", "Host: static-server.ingress.consul", ingressGatewayService)
		})
	}
}
