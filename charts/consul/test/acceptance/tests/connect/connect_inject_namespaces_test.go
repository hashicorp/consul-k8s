package connect

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"

	terratestk8s "github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/hashicorp/consul-helm/test/acceptance/framework/consul"
	"github.com/hashicorp/consul-helm/test/acceptance/framework/helpers"
	"github.com/hashicorp/consul-helm/test/acceptance/framework/k8s"
	"github.com/hashicorp/consul-helm/test/acceptance/framework/logger"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const staticServerNamespace = "ns1"
const staticClientNamespace = "ns2"

// Test that Connect works with Consul Enterprise namespaces.
// These tests currently only test non-secure and secure without auto-encrypt installations
// because in the case of namespaces there isn't a significant distinction in code between auto-encrypt
// and non-auto-encrypt secure installations, so testing just one is enough.
func TestConnectInjectNamespaces(t *testing.T) {
	cfg := suite.Config()
	if !cfg.EnableEnterprise {
		t.Skipf("skipping this test because -enable-enterprise is not set")
	}

	cases := []struct {
		name                 string
		destinationNamespace string
		mirrorK8S            bool
		secure               bool
	}{
		{
			"single destination namespace",
			staticServerNamespace,
			false,
			false,
		},
		{
			"single destination namespace; secure",
			staticServerNamespace,
			false,
			true,
		},
		{
			"mirror k8s namespaces",
			staticServerNamespace,
			true,
			false,
		},
		{
			"mirror k8s namespaces; secure",
			staticServerNamespace,
			true,
			true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := suite.Environment().DefaultContext(t)
			cfg := suite.Config()

			helmValues := map[string]string{
				"global.enableConsulNamespaces": "true",
				"connectInject.enabled":         "true",
				// When mirroringK8S is set, this setting is ignored.
				"connectInject.consulNamespaces.consulDestinationNamespace": c.destinationNamespace,
				"connectInject.consulNamespaces.mirroringK8S":               strconv.FormatBool(c.mirrorK8S),

				"global.acls.manageSystemACLs": strconv.FormatBool(c.secure),
				"global.tls.enabled":           strconv.FormatBool(c.secure),
			}

			releaseName := helpers.RandomName()
			consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)

			consulCluster.Create(t)

			staticServerOpts := &terratestk8s.KubectlOptions{
				ContextName: ctx.KubectlOptions(t).ContextName,
				ConfigPath:  ctx.KubectlOptions(t).ConfigPath,
				Namespace:   staticServerNamespace,
			}
			staticClientOpts := &terratestk8s.KubectlOptions{
				ContextName: ctx.KubectlOptions(t).ContextName,
				ConfigPath:  ctx.KubectlOptions(t).ConfigPath,
				Namespace:   staticClientNamespace,
			}

			logger.Logf(t, "creating namespaces %s and %s", staticServerNamespace, staticClientNamespace)
			k8s.RunKubectl(t, ctx.KubectlOptions(t), "create", "ns", staticServerNamespace)
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "delete", "ns", staticServerNamespace)
			})

			k8s.RunKubectl(t, ctx.KubectlOptions(t), "create", "ns", staticClientNamespace)
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
				// Note: this deletion will take longer in cases when the static-client deployment
				// hasn't yet fully terminated.
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "delete", "ns", staticClientNamespace)
			})

			consulClient := consulCluster.SetupConsulClient(t, c.secure)

			serverQueryOpts := &api.QueryOptions{Namespace: staticServerNamespace}
			clientQueryOpts := &api.QueryOptions{Namespace: staticClientNamespace}

			if !c.mirrorK8S {
				serverQueryOpts = &api.QueryOptions{Namespace: c.destinationNamespace}
				clientQueryOpts = &api.QueryOptions{Namespace: c.destinationNamespace}
			}

			// Check that the ACL token is deleted.
			if c.secure {
				// We need to register the cleanup function before we create the deployments
				// because golang will execute them in reverse order i.e. the last registered
				// cleanup function will be executed first.
				t.Cleanup(func() {
					if c.secure {
						retry.Run(t, func(r *retry.R) {
							tokens, _, err := consulClient.ACL().TokenList(serverQueryOpts)
							require.NoError(r, err)
							for _, token := range tokens {
								require.NotContains(r, token.Description, staticServerName)
							}

							tokens, _, err = consulClient.ACL().TokenList(clientQueryOpts)
							require.NoError(r, err)
							for _, token := range tokens {
								require.NotContains(r, token.Description, staticClientName)
							}
						})
					}
				})
			}

			logger.Log(t, "creating static-server and static-client deployments")
			k8s.DeployKustomize(t, staticServerOpts, cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")
			if cfg.EnableTransparentProxy {
				k8s.DeployKustomize(t, staticClientOpts, cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-client-tproxy")
			} else {
				k8s.DeployKustomize(t, staticClientOpts, cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-client-namespaces")
			}

			// Check that both static-server and static-client have been injected and now have 2 containers.
			for _, labelSelector := range []string{"app=static-server", "app=static-client"} {
				podList, err := ctx.KubernetesClient(t).CoreV1().Pods(metav1.NamespaceAll).List(context.Background(), metav1.ListOptions{
					LabelSelector: labelSelector,
				})
				require.NoError(t, err)
				require.Len(t, podList.Items, 1)
				require.Len(t, podList.Items[0].Spec.Containers, 2)
			}

			// Make sure that services are registered in the correct namespace.
			// If mirroring is enabled, we expect services to be registered in the
			// Consul namespace with the same name as their source
			// Kubernetes namespace.
			// If a single destination namespace is set, we expect all services
			// to be registered in that destination Consul namespace.
			services, _, err := consulClient.Catalog().Service(staticServerName, "", serverQueryOpts)
			require.NoError(t, err)
			require.Len(t, services, 1)

			services, _, err = consulClient.Catalog().Service(staticClientName, "", clientQueryOpts)
			require.NoError(t, err)
			require.Len(t, services, 1)

			if c.secure {
				logger.Log(t, "checking that the connection is not successful because there's no intention")
				if cfg.EnableTransparentProxy {
					k8s.CheckStaticServerConnectionFailing(t, staticClientOpts, fmt.Sprintf("http://static-server.%s", staticServerNamespace))
				} else {
					k8s.CheckStaticServerConnectionFailing(t, staticClientOpts, "http://localhost:1234")
				}

				intention := &api.Intention{
					SourceName:      staticClientName,
					SourceNS:        staticClientNamespace,
					DestinationName: staticServerName,
					DestinationNS:   staticServerNamespace,
					Action:          api.IntentionActionAllow,
				}

				// Set the destination namespace to be the same
				// unless mirrorK8S is true.
				if !c.mirrorK8S {
					intention.SourceNS = c.destinationNamespace
					intention.DestinationNS = c.destinationNamespace
				}

				logger.Log(t, "creating intention")
				_, err := consulClient.Connect().IntentionUpsert(intention, nil)
				require.NoError(t, err)
			}

			logger.Log(t, "checking that connection is successful")
			if cfg.EnableTransparentProxy {
				k8s.CheckStaticServerConnectionSuccessful(t, staticClientOpts, fmt.Sprintf("http://static-server.%s", staticServerNamespace))
			} else {
				k8s.CheckStaticServerConnectionSuccessful(t, staticClientOpts, "http://localhost:1234")
			}

			// Test that kubernetes readiness status is synced to Consul.
			// Create the file so that the readiness probe of the static-server pod fails.
			logger.Log(t, "testing k8s -> consul health checks sync by making the static-server unhealthy")
			k8s.RunKubectl(t, staticServerOpts, "exec", "deploy/"+staticServerName, "--", "touch", "/tmp/unhealthy")

			// The readiness probe should take a moment to be reflected in Consul, CheckStaticServerConnection will retry
			// until Consul marks the service instance unavailable for mesh traffic, causing the connection to fail.
			// We are expecting a "connection reset by peer" error because in a case of health checks,
			// there will be no healthy proxy host to connect to. That's why we can't assert that we receive an empty reply
			// from server, which is the case when a connection is unsuccessful due to intentions in other tests.
			logger.Log(t, "checking that connection is unsuccessful")
			if cfg.EnableTransparentProxy {
				k8s.CheckStaticServerConnectionMultipleFailureMessages(t, staticClientOpts, false, []string{"curl: (56) Recv failure: Connection reset by peer", "curl: (52) Empty reply from server", "curl: (7) Failed to connect to static-server.ns1 port 80: Connection refused"}, fmt.Sprintf("http://static-server.%s", staticServerNamespace))
			} else {
				k8s.CheckStaticServerConnectionMultipleFailureMessages(t, staticClientOpts, false, []string{"curl: (56) Recv failure: Connection reset by peer", "curl: (52) Empty reply from server"}, "http://localhost:1234")
			}
		})
	}
}

// Test the cleanup controller that cleans up force-killed pods.
// These tests currently only test non-secure and secure without auto-encrypt installations
// because in the case of namespaces there isn't a significant distinction in code between auto-encrypt
// and non-auto-encrypt secure installations, so testing just one is enough.
func TestConnectInjectNamespaces_CleanupController(t *testing.T) {
	cfg := suite.Config()
	if !cfg.EnableEnterprise {
		t.Skipf("skipping this test because -enable-enterprise is not set")
	}

	consulDestNS := "consul-dest"
	cases := []struct {
		name                 string
		destinationNamespace string
		mirrorK8S            bool
		secure               bool
	}{
		{
			"single destination namespace",
			consulDestNS,
			false,
			false,
		},
		{
			"single destination namespace; secure",
			consulDestNS,
			false,
			true,
		},
		{
			"mirror k8s namespaces",
			consulDestNS,
			true,
			false,
		},
		{
			"mirror k8s namespaces; secure",
			consulDestNS,
			true,
			true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := suite.Config()
			ctx := suite.Environment().DefaultContext(t)

			helmValues := map[string]string{
				"global.enableConsulNamespaces": "true",
				"connectInject.enabled":         "true",
				// When mirroringK8S is set, this setting is ignored.
				"connectInject.consulNamespaces.consulDestinationNamespace": c.destinationNamespace,
				"connectInject.consulNamespaces.mirroringK8S":               strconv.FormatBool(c.mirrorK8S),

				"global.acls.manageSystemACLs": strconv.FormatBool(c.secure),
				"global.tls.enabled":           strconv.FormatBool(c.secure),
			}

			releaseName := helpers.RandomName()
			consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)

			consulCluster.Create(t)

			logger.Logf(t, "creating namespace %s", staticClientNamespace)
			k8s.RunKubectl(t, ctx.KubectlOptions(t), "create", "ns", staticClientNamespace)
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "delete", "ns", staticClientNamespace)
			})

			logger.Log(t, "creating static-client deployment")
			staticClientOpts := &terratestk8s.KubectlOptions{
				ContextName: ctx.KubectlOptions(t).ContextName,
				ConfigPath:  ctx.KubectlOptions(t).ConfigPath,
				Namespace:   staticClientNamespace,
			}
			k8s.DeployKustomize(t, staticClientOpts, cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-client-namespaces")

			logger.Log(t, "waiting for static-client to be registered with Consul")
			consulClient := consulCluster.SetupConsulClient(t, c.secure)
			expectedConsulNS := staticClientNamespace
			if !c.mirrorK8S {
				expectedConsulNS = c.destinationNamespace
			}
			consulQueryOpts := &api.QueryOptions{Namespace: expectedConsulNS}
			retry.Run(t, func(r *retry.R) {
				for _, name := range []string{"static-client", "static-client-sidecar-proxy"} {
					instances, _, err := consulClient.Catalog().Service(name, "", consulQueryOpts)
					r.Check(err)

					if len(instances) != 1 {
						r.Errorf("expected 1 instance of %s", name)
					}
				}
			})

			pods, err := ctx.KubernetesClient(t).CoreV1().Pods(staticClientNamespace).List(context.Background(), metav1.ListOptions{LabelSelector: "app=static-client"})
			require.NoError(t, err)
			require.Len(t, pods.Items, 1)
			podName := pods.Items[0].Name

			logger.Logf(t, "force killing the static-client pod %q", podName)
			var gracePeriod int64 = 0
			err = ctx.KubernetesClient(t).CoreV1().Pods(staticClientNamespace).Delete(context.Background(), podName, metav1.DeleteOptions{GracePeriodSeconds: &gracePeriod})
			require.NoError(t, err)

			logger.Log(t, "ensuring pod is deregistered")
			retry.Run(t, func(r *retry.R) {
				for _, name := range []string{"static-client", "static-client-sidecar-proxy"} {
					instances, _, err := consulClient.Catalog().Service(name, "", consulQueryOpts)
					r.Check(err)

					for _, instance := range instances {
						if strings.Contains(instance.ServiceID, podName) {
							r.Errorf("%s is still registered", instance.ServiceID)
						}
					}
				}
			})
		})
	}
}
