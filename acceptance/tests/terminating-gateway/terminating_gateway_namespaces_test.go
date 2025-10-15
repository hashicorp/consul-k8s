// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package terminatinggateway

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	terratestk8s "github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul/sdk/testutil/retry"
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
				Options:             ctx.KubectlOptions(t),
				NoCleanupOnFailure:  cfg.NoCleanupOnFailure,
				NoCleanup:           cfg.NoCleanup,
				KustomizeConfigPath: "../fixtures/cases/terminating-gateway-namespaces/all-non-default/external-service-registration/",
			}

			consulOptions := helpers.ConsulOptions{
				ConsulClient:                    consulClient,
				Namespace:                       testNamespace,
				ExternalServiceNameRegistration: "static-server-registration",
			}

			helpers.RegisterExternalServiceCRD(t, k8sOptions, consulOptions)

			logger.Log(t, "creating terminating gateway")
			k8s.KubectlApplyK(t, nsK8SOptions, "../fixtures/cases/terminating-gateway-namespaces/all-non-default/terminating-gateway")
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
				k8s.KubectlDeleteK(t, nsK8SOptions, "../fixtures/cases/terminating-gateway-namespaces/all-non-default/terminating-gateway")
			})

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
			retry.RunWith(&retry.Counter{Count: 30, Wait: 5 * time.Second}, t, func(r *retry.R) {
				logger.Log(r, "trying calls to terminating gateway")
				k8s.CheckStaticServerConnectionSuccessful(t, nsK8SOptions, staticClientName, staticServerLocalAddress)
			})
		})
	}
}

// Test we can connect through the terminating gateway when the terminating gateway,
// the external service, and the connect service are in different combinations of namespaces.
func TestTerminatingGatewayNamespaceMirroring(t *testing.T) {
	cfg := suite.Config()
	if !cfg.EnableEnterprise {
		t.Skipf("skipping this test because -enable-enterprise is not set")
	}

	type config struct {
		path      string
		namespace string
	}

	// for simplicity/to keep from an explosion of test cases we're keeping the registration in the same namespace as the
	// service being registered, this shouldn't matter because external services should be outside of the cluster typically
	cases := map[string]struct {
		termGWConfig                      config
		externalServiceRegistrationConfig config
		staticServerConfig                config
		staticClientConfig                config
	}{
		"all in default namespace": {
			termGWConfig: config{
				path:      "../fixtures/bases/terminating-gateway",
				namespace: "default",
			},
			externalServiceRegistrationConfig: config{
				path:      "../fixtures/bases/external-service-registration",
				namespace: "default",
			},
			staticServerConfig: config{
				path:      "../fixtures/bases/static-server",
				namespace: "default",
			},
			staticClientConfig: config{
				path:      "../fixtures/cases/static-client-inject",
				namespace: "default",
			},
		},
		"all in same non-default namespace": {
			termGWConfig: config{
				path:      "../fixtures/cases/terminating-gateway-namespaces/all-non-default/terminating-gateway",
				namespace: "ns1",
			},
			externalServiceRegistrationConfig: config{
				path:      "../fixtures/cases/terminating-gateway-namespaces/all-non-default/external-service-registration",
				namespace: "ns1",
			},
			staticServerConfig: config{
				path:      "../fixtures/bases/static-server",
				namespace: "ns1",
			},
			staticClientConfig: config{
				path:      "../fixtures/cases/static-client-namespaces",
				namespace: "ns1",
			},
		},
		"mesh service in default namespace everything else in non-default namespace": {
			termGWConfig: config{
				path:      "../fixtures/cases/terminating-gateway-namespaces/all-non-default/terminating-gateway",
				namespace: "ns1",
			},
			externalServiceRegistrationConfig: config{
				path:      "../fixtures/cases/terminating-gateway-namespaces/all-non-default/external-service-registration",
				namespace: "ns1",
			},
			staticServerConfig: config{
				path:      "../fixtures/bases/static-server",
				namespace: "ns1",
			},
			staticClientConfig: config{
				path:      "../fixtures/cases/static-client-namespaces",
				namespace: "default",
			},
		},
		"external service in default namespace everything else in non-default namespace": {
			termGWConfig: config{
				path:      "../fixtures/cases/terminating-gateway-namespaces/client-non-default/terminating-gateway",
				namespace: "ns1",
			},
			externalServiceRegistrationConfig: config{
				path:      "../fixtures/bases/external-service-registration",
				namespace: "default",
			},
			staticServerConfig: config{
				path:      "../fixtures/bases/static-server",
				namespace: "default",
			},
			staticClientConfig: config{
				path:      "../fixtures/cases/terminating-gateway-namespaces/client-non-default/static-client-inject",
				namespace: "ns1",
			},
		},
		// TODO: (NET-10248) need to dig in more on why this isn't working when acls are enabled.
		// "terminating gateway in default namespace everything else in non-default namespace": {
		// termGWConfig: config{
		// path:      "../fixtures/cases/terminating-gateway-namespaces/all-non-default/terminating-gateway",
		// namespace: "default",
		// },
		// externalServiceRegistrationConfig: config{
		// path:      "../fixtures/cases/terminating-gateway-namespaces/all-non-default/external-service-registration",
		// namespace: "ns1",
		// },
		// staticServerConfig: config{
		// path:      "../fixtures/bases/static-server",
		// namespace: "ns1",
		// },
		// staticClientConfig: config{
		// path:      "../fixtures/cases/static-client-namespaces",
		// namespace: "ns1",
		// },
		// },
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

					"terminatingGateways.enabled":                     "true",
					"terminatingGateways.gateways[0].name":            "terminating-gateway",
					"terminatingGateways.gateways[0].replicas":        "1",
					"terminatingGateways.gateways[0].consulNamespace": tc.termGWConfig.namespace,
				}

				releaseName := helpers.RandomName()
				consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)

				consulCluster.Create(t)

				consulClient, _ := consulCluster.SetupConsulClient(t, secure)

				seen := make(map[string]struct{}, 4)
				for _, ns := range []string{tc.externalServiceRegistrationConfig.namespace, tc.staticServerConfig.namespace, tc.staticClientConfig.namespace, tc.termGWConfig.namespace} {
					_, ok := seen[ns]
					if ns != "default" && !ok {
						logger.Logf(t, "creating Kubernetes namespace %s", ns)
						k8s.RunKubectl(t, ctx.KubectlOptions(t), "create", "ns", ns)
						helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
							k8s.RunKubectl(t, ctx.KubectlOptions(t), "delete", "ns", ns)
						})
						seen[ns] = struct{}{}
					}
				}

				staticServerNSOpts := &terratestk8s.KubectlOptions{
					ContextName: ctx.KubectlOptions(t).ContextName,
					ConfigPath:  ctx.KubectlOptions(t).ConfigPath,
					Namespace:   tc.staticServerConfig.namespace,
				}

				staticClientNSOpts := &terratestk8s.KubectlOptions{
					ContextName: ctx.KubectlOptions(t).ContextName,
					ConfigPath:  ctx.KubectlOptions(t).ConfigPath,
					Namespace:   tc.staticClientConfig.namespace,
				}

				termGWNSOpts := &terratestk8s.KubectlOptions{
					ContextName: ctx.KubectlOptions(t).ContextName,
					ConfigPath:  ctx.KubectlOptions(t).ConfigPath,
					Namespace:   tc.termGWConfig.namespace,
				}

				externalServiceRegistrationNSOpts := &terratestk8s.KubectlOptions{
					ContextName: ctx.KubectlOptions(t).ContextName,
					ConfigPath:  ctx.KubectlOptions(t).ConfigPath,
					Namespace:   tc.externalServiceRegistrationConfig.namespace,
				}

				// Deploy a static-server that will play the role of an external service.
				logger.Log(t, "creating static-server deployment")
				k8s.DeployKustomize(t, staticServerNSOpts, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, tc.staticServerConfig.path)

				// Create the config entry for the terminating gateway.
				logger.Log(t, "creating terminating gateway")
				k8s.KubectlApplyK(t, termGWNSOpts, tc.termGWConfig.path)
				helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
					k8s.KubectlDeleteK(t, termGWNSOpts, tc.termGWConfig.path)
				})

				k8sOpts := helpers.K8sOptions{
					Options:             externalServiceRegistrationNSOpts,
					NoCleanupOnFailure:  cfg.NoCleanupOnFailure,
					NoCleanup:           cfg.NoCleanup,
					KustomizeConfigPath: tc.externalServiceRegistrationConfig.path,
				}

				consulOpts := helpers.ConsulOptions{
					ConsulClient:                    consulClient,
					Namespace:                       tc.externalServiceRegistrationConfig.namespace,
					ExternalServiceNameRegistration: "static-server-registration",
				}

				helpers.RegisterExternalServiceCRD(t, k8sOpts, consulOpts)

				// Deploy the static client
				logger.Log(t, "deploying static client")
				k8s.DeployKustomize(t, staticClientNSOpts, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, tc.staticClientConfig.path)
				// If ACLs are enabled, test that intentions prevent connections.
				if secure {
					// With the terminating gateway up, we test that we can make a call to it
					// via the static-server. It should fail to connect with the
					// static-server pod because of intentions.
					logger.Log(t, "testing intentions prevent connections through the terminating gateway")
					retry.RunWith(&retry.Counter{Wait: 5 * time.Second, Count: 60}, t, func(r *retry.R) {
						k8s.CheckStaticServerConnectionFailing(t, staticClientNSOpts, staticClientName, staticServerLocalAddress)
					})
					logger.Log(t, "adding intentions to allow traffic from client ==> server")
					AddIntention(t, consulClient, "", tc.staticClientConfig.namespace, staticClientName, tc.staticServerConfig.namespace, staticServerName)
				}

				// Test that we can make a call to the terminating gateway
				logger.Log(t, "trying calls to terminating gateway")
				retry.RunWith(&retry.Counter{Wait: 5 * time.Second, Count: 60}, t, func(r *retry.R) {
					k8s.CheckStaticServerConnectionSuccessful(t, staticClientNSOpts, staticClientName, staticServerLocalAddress)
				})
			})
		}
	}
}
