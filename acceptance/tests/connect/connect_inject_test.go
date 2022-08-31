package connect

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/consul-k8s/acceptance/framework/cli"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const ipv4RegEx = "(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\\.(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\\.(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\\.(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)"

// TestConnectInject tests that Connect works in a default and a secure installation.
func TestConnectInject(t *testing.T) {
	cases := map[string]struct {
		clusterKind consul.ClusterKind
		releaseName string
		secure      bool
	}{
		"Helm install without secure": {
			clusterKind: consul.Helm,
			releaseName: helpers.RandomName(),
		},
		"Helm install with secure": {
			clusterKind: consul.Helm,
			releaseName: helpers.RandomName(),
			secure:      true,
		},
		"CLI install without secure": {
			clusterKind: consul.CLI,
			releaseName: consul.CLIReleaseName,
		},
		"CLI install with secure": {
			clusterKind: consul.CLI,
			releaseName: consul.CLIReleaseName,
			secure:      true,
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			cli, err := cli.NewCLI()
			require.NoError(t, err)

			cfg := suite.Config()
			ctx := suite.Environment().DefaultContext(t)

			connHelper := ConnectHelper{
				ClusterKind: c.clusterKind,
				Secure:      c.secure,
				ReleaseName: c.releaseName,
				Ctx:         ctx,
				Cfg:         cfg,
			}

			connHelper.Setup(t)

			connHelper.Install(t)
			connHelper.DeployClientAndServer(t)
			if c.secure {
				connHelper.TestConnectionFailureWithoutIntention(t)
				connHelper.CreateIntention(t)
			}

			// Run proxy list and get the two results.
			listOut, err := cli.Run(t, ctx.KubectlOptions(t), "proxy", "list")
			require.NoError(t, err)
			logger.Log(t, string(listOut))
			list := translateListOutput(listOut)
			require.Equal(t, 2, len(list))
			for _, proxyType := range list {
				require.Equal(t, "Sidecar", proxyType)
			}

			// Run proxy read and check that the connection is present in the output.
			retrier := &retry.Timer{Timeout: 160 * time.Second, Wait: 2 * time.Second}
			retry.RunWith(retrier, t, func(r *retry.R) {
				for podName := range list {
					out, err := cli.Run(t, ctx.KubectlOptions(t), "proxy", "read", podName)
					require.NoError(t, err)

					output := string(out)
					logger.Log(t, output)

					// Both proxies must see their own local agent and app as clusters.
					require.Regexp(r, "consul-dataplane.*STATIC", output)
					require.Regexp(r, "local_app.*STATIC", output)

					// Static Client must have Static Server as a cluster and endpoint.
					if strings.Contains(podName, "static-client") {
						require.Regexp(r, "static-server.*static-server\\.default\\.dc1\\.internal.*EDS", output)
						require.Regexp(r, ipv4RegEx+".*static-server.default.dc1.internal", output)
					}
				}
			})

			connHelper.TestConnectionSuccess(t)
			connHelper.TestConnectionFailureWhenUnhealthy(t)
		})
	}
}

// TestConnectInjectOnUpgrade tests that Connect works before and after an
// upgrade is performed on the cluster.
func TestConnectInjectOnUpgrade(t *testing.T) {
	t.Skipf("skipping this test because it's not yet supported with agentless")
	cases := map[string]struct {
		clusterKind      consul.ClusterKind
		releaseName      string
		initial, upgrade map[string]string
	}{
		"CLI upgrade changes nothing": {
			clusterKind: consul.CLI,
			releaseName: consul.CLIReleaseName,
		},
		"CLI upgrade to enable ingressGateway": {
			clusterKind: consul.CLI,
			releaseName: consul.CLIReleaseName,
			initial:     map[string]string{},
			upgrade: map[string]string{
				"ingressGateways.enabled":           "true",
				"ingressGateways.defaults.replicas": "1",
			},
		},
		"CLI upgrade to enable UI": {
			clusterKind: consul.CLI,
			releaseName: consul.CLIReleaseName,
			initial:     map[string]string{},
			upgrade: map[string]string{
				"ui.enabled": "true",
			},
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := suite.Config()
			ctx := suite.Environment().DefaultContext(t)

			connHelper := ConnectHelper{
				ClusterKind: c.clusterKind,
				HelmValues:  c.initial,
				ReleaseName: c.releaseName,
				Ctx:         ctx,
				Cfg:         cfg,
			}

			connHelper.Setup(t)

			connHelper.Install(t)
			connHelper.DeployClientAndServer(t)
			connHelper.TestConnectionSuccess(t)
			connHelper.TestConnectionFailureWhenUnhealthy(t)

			connHelper.HelmValues = c.upgrade

			connHelper.Upgrade(t)
			connHelper.TestConnectionSuccess(t)
			connHelper.TestConnectionFailureWhenUnhealthy(t)
		})
	}
}

// Test the endpoints controller cleans up force-killed pods.
func TestConnectInject_CleanupKilledPods(t *testing.T) {
	for _, secure := range []bool{false, true} {
		name := fmt.Sprintf("secure: %t", secure)
		t.Run(name, func(t *testing.T) {
			cfg := suite.Config()
			ctx := suite.Environment().DefaultContext(t)

			helmValues := map[string]string{
				"connectInject.enabled":        "true",
				"global.tls.enabled":           strconv.FormatBool(secure),
				"global.acls.manageSystemACLs": strconv.FormatBool(secure),
			}

			releaseName := helpers.RandomName()
			consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)

			consulCluster.Create(t)

			logger.Log(t, "creating static-client deployment")
			k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-client-inject")

			logger.Log(t, "waiting for static-client to be registered with Consul")
			consulClient, _ := consulCluster.SetupConsulClient(t, secure)
			retry.Run(t, func(r *retry.R) {
				for _, name := range []string{"static-client", "static-client-sidecar-proxy"} {
					instances, _, err := consulClient.Catalog().Service(name, "", nil)
					r.Check(err)

					if len(instances) != 1 {
						r.Errorf("expected 1 instance of %s", name)
					}
				}
			})

			ns := ctx.KubectlOptions(t).Namespace
			pods, err := ctx.KubernetesClient(t).CoreV1().Pods(ns).List(context.Background(), metav1.ListOptions{LabelSelector: "app=static-client"})
			require.NoError(t, err)
			require.Len(t, pods.Items, 1)
			podName := pods.Items[0].Name

			logger.Logf(t, "force killing the static-client pod %q", podName)
			var gracePeriod int64 = 0
			err = ctx.KubernetesClient(t).CoreV1().Pods(ns).Delete(context.Background(), podName, metav1.DeleteOptions{GracePeriodSeconds: &gracePeriod})
			require.NoError(t, err)

			logger.Log(t, "ensuring pod is deregistered")
			retry.Run(t, func(r *retry.R) {
				for _, name := range []string{"static-client", "static-client-sidecar-proxy"} {
					instances, _, err := consulClient.Catalog().Service(name, "", nil)
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

const multiport = "multiport"
const multiportAdmin = "multiport-admin"

// Test that Connect works for an application with multiple ports. The multiport application is a Pod listening on
// two ports. This tests inbound connections to each port of the multiport app, and outbound connections from the
// multiport app to static-server.
func TestConnectInject_MultiportServices(t *testing.T) {
	t.Skipf("skipping until multi-port workaround is supported")
	for _, secure := range []bool{false, true} {
		name := fmt.Sprintf("secure: %t", secure)
		t.Run(name, func(t *testing.T) {
			cfg := suite.Config()
			ctx := suite.Environment().DefaultContext(t)

			// Multi port apps don't work with transparent proxy.
			if cfg.EnableTransparentProxy {
				t.Skipf("skipping this test because transparent proxy is enabled")
			}

			helmValues := map[string]string{
				"connectInject.enabled": "true",

				"global.tls.enabled":           strconv.FormatBool(secure),
				"global.acls.manageSystemACLs": strconv.FormatBool(secure),
			}

			releaseName := helpers.RandomName()
			consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)

			consulCluster.Create(t)

			consulClient, _ := consulCluster.SetupConsulClient(t, secure)

			// Check that the ACL token is deleted.
			if secure {
				// We need to register the cleanup function before we create the deployments
				// because golang will execute them in reverse order i.e. the last registered
				// cleanup function will be executed first.
				t.Cleanup(func() {
					retrier := &retry.Timer{Timeout: 5 * time.Minute, Wait: 1 * time.Second}
					retry.RunWith(retrier, t, func(r *retry.R) {
						tokens, _, err := consulClient.ACL().TokenList(nil)
						require.NoError(r, err)
						for _, token := range tokens {
							require.NotContains(r, token.Description, multiport)
							require.NotContains(r, token.Description, multiportAdmin)
							require.NotContains(r, token.Description, StaticClientName)
							require.NotContains(r, token.Description, staticServerName)
						}
					})
				})
			}

			logger.Log(t, "creating multiport static-server and static-client deployments")
			k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/bases/multiport-app")
			k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-client-inject-multiport")

			// Check that static-client has been injected and now has 2 containers.
			podList, err := ctx.KubernetesClient(t).CoreV1().Pods(ctx.KubectlOptions(t).Namespace).List(context.Background(), metav1.ListOptions{
				LabelSelector: "app=static-client",
			})
			require.NoError(t, err)
			require.Len(t, podList.Items, 1)
			require.Len(t, podList.Items[0].Spec.Containers, 2)

			// Check that multiport has been injected and now has 4 containers.
			podList, err = ctx.KubernetesClient(t).CoreV1().Pods(ctx.KubectlOptions(t).Namespace).List(context.Background(), metav1.ListOptions{
				LabelSelector: "app=multiport",
			})
			require.NoError(t, err)
			require.Len(t, podList.Items, 1)
			require.Len(t, podList.Items[0].Spec.Containers, 4)

			if secure {
				logger.Log(t, "checking that the connection is not successful because there's no intention")
				k8s.CheckStaticServerConnectionFailing(t, ctx.KubectlOptions(t), StaticClientName, "http://localhost:1234")
				k8s.CheckStaticServerConnectionFailing(t, ctx.KubectlOptions(t), StaticClientName, "http://localhost:2234")

				logger.Log(t, fmt.Sprintf("creating intention for %s", multiport))
				_, _, err := consulClient.ConfigEntries().Set(&api.ServiceIntentionsConfigEntry{
					Kind: api.ServiceIntentions,
					Name: multiport,
					Sources: []*api.SourceIntention{
						{
							Name:   StaticClientName,
							Action: api.IntentionActionAllow,
						},
					},
				}, nil)
				require.NoError(t, err)
				logger.Log(t, fmt.Sprintf("creating intention for %s", multiportAdmin))
				_, _, err = consulClient.ConfigEntries().Set(&api.ServiceIntentionsConfigEntry{
					Kind: api.ServiceIntentions,
					Name: multiportAdmin,
					Sources: []*api.SourceIntention{
						{
							Name:   StaticClientName,
							Action: api.IntentionActionAllow,
						},
					},
				}, nil)
				require.NoError(t, err)
			}

			// Check connection from static-client to multiport.
			k8s.CheckStaticServerConnectionSuccessful(t, ctx.KubectlOptions(t), StaticClientName, "http://localhost:1234")

			// Check connection from static-client to multiport-admin.
			k8s.CheckStaticServerConnectionSuccessfulWithMessage(t, ctx.KubectlOptions(t), StaticClientName, "hello world from 9090 admin", "http://localhost:2234")

			// Now that we've checked inbound connections to a multi port pod, check outbound connection from multi port
			// pod to static-server.

			// Deploy static-server.
			k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")

			// For outbound connections from the multi port pod, only intentions from the first service in the multiport
			// pod need to be created, since all upstream connections are made through the first service's envoy proxy.
			if secure {
				logger.Log(t, "checking that the connection is not successful because there's no intention")

				k8s.CheckStaticServerConnectionFailing(t, ctx.KubectlOptions(t), multiport, "http://localhost:3234")

				logger.Log(t, fmt.Sprintf("creating intention for %s", staticServerName))
				_, _, err := consulClient.ConfigEntries().Set(&api.ServiceIntentionsConfigEntry{
					Kind: api.ServiceIntentions,
					Name: staticServerName,
					Sources: []*api.SourceIntention{
						{
							Name:   multiport,
							Action: api.IntentionActionAllow,
						},
					},
				}, nil)
				require.NoError(t, err)
			}

			// Check the connection from the multi port pod to static-server.
			k8s.CheckStaticServerConnectionSuccessful(t, ctx.KubectlOptions(t), multiport, "http://localhost:3234")

			// Test that kubernetes readiness status is synced to Consul. This will make the multi port pods unhealthy
			// and check inbound connections to the multi port pods' services.
			// Create the files so that the readiness probes of the multi port pod fails.
			logger.Log(t, "testing k8s -> consul health checks sync by making the multiport unhealthy")
			k8s.RunKubectl(t, ctx.KubectlOptions(t), "exec", "deploy/"+multiport, "--", "touch", "/tmp/unhealthy-multiport")
			logger.Log(t, "testing k8s -> consul health checks sync by making the multiport-admin unhealthy")
			k8s.RunKubectl(t, ctx.KubectlOptions(t), "exec", "deploy/"+multiport, "--", "touch", "/tmp/unhealthy-multiport-admin")

			// The readiness probe should take a moment to be reflected in Consul, CheckStaticServerConnection will retry
			// until Consul marks the service instance unavailable for mesh traffic, causing the connection to fail.
			// We are expecting a "connection reset by peer" error because in a case of health checks,
			// there will be no healthy proxy host to connect to. That's why we can't assert that we receive an empty reply
			// from server, which is the case when a connection is unsuccessful due to intentions in other tests.
			k8s.CheckStaticServerConnectionMultipleFailureMessages(t, ctx.KubectlOptions(t), StaticClientName, false, []string{"curl: (56) Recv failure: Connection reset by peer", "curl: (52) Empty reply from server"}, "", "http://localhost:1234")
			k8s.CheckStaticServerConnectionMultipleFailureMessages(t, ctx.KubectlOptions(t), StaticClientName, false, []string{"curl: (56) Recv failure: Connection reset by peer", "curl: (52) Empty reply from server"}, "", "http://localhost:2234")
		})
	}
}

// translateListOutput takes the raw output from the proxy list command and
// translates the table into a map.
func translateListOutput(raw []byte) map[string]string {
	formatted := make(map[string]string)
	for _, pod := range strings.Split(strings.TrimSpace(string(raw)), "\n")[3:] {
		row := strings.Split(strings.TrimSpace(pod), "\t")

		var name string
		if len(row) == 3 { // Handle the case where namespace is present
			name = fmt.Sprintf("%s/%s", strings.TrimSpace(row[0]), strings.TrimSpace(row[1]))
		} else if len(row) == 2 {
			name = strings.TrimSpace(row[0])
		}
		formatted[name] = row[len(row)-1]
	}

	return formatted
}
