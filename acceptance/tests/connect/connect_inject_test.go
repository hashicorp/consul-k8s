// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package connect

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/consul-k8s/acceptance/framework/connhelper"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestConnectInject tests that Connect works in a default and a secure installation using Helm CLI.
func TestConnectInject(t *testing.T) {
	cases := map[string]struct {
		secure bool
	}{
		"not-secure": {secure: false},
		"secure":     {secure: true},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := suite.Config()
			ctx := suite.Environment().DefaultContext(t)

			releaseName := helpers.RandomName()
			connHelper := connhelper.ConnectHelper{
				ClusterKind:     consul.Helm,
				Secure:          c.secure,
				ReleaseName:     releaseName,
				Ctx:             ctx,
				UseAppNamespace: cfg.EnableRestrictedPSAEnforcement,
				Cfg:             cfg,
			}

			connHelper.Setup(t)

			connHelper.Install(t)
			connHelper.DeployClientAndServer(t)
			if c.secure {
				connHelper.TestConnectionFailureWithoutIntention(t, connhelper.ConnHelperOpts{})
				connHelper.CreateIntention(t, connhelper.IntentionOpts{})
			}

			connHelper.TestConnectionSuccess(t, connhelper.ConnHelperOpts{})
			connHelper.TestConnectionFailureWhenUnhealthy(t)
		})
	}
}

// TestConnectInject_VirtualIPFailover ensures that KubeDNS entries are saved to the virtual IP address table in Consul.
func TestConnectInject_VirtualIPFailover(t *testing.T) {
	cfg := suite.Config()
	if !cfg.EnableTransparentProxy {
		// This can only be tested in transparent proxy mode.
		t.SkipNow()
	}
	ctx := suite.Environment().DefaultContext(t)

	releaseName := helpers.RandomName()
	connHelper := connhelper.ConnectHelper{
		ClusterKind:     consul.Helm,
		Secure:          true,
		ReleaseName:     releaseName,
		Ctx:             ctx,
		UseAppNamespace: cfg.EnableRestrictedPSAEnforcement,
		Cfg:             cfg,
	}

	connHelper.Setup(t)

	connHelper.Install(t)
	connHelper.CreateResolverRedirect(t)
	connHelper.DeployClientAndServer(t)

	opts := connHelper.KubectlOptsForApp(t)
	k8s.CheckStaticServerConnectionSuccessful(t, opts, "static-client", "http://resolver-redirect")
}

// Test the endpoints controller cleans up force-killed pods.
func TestConnectInject_CleanupKilledPods(t *testing.T) {
	for _, secure := range []bool{false, true} {
		name := fmt.Sprintf("secure: %t", secure)
		t.Run(name, func(t *testing.T) {
			cfg := suite.Config()

			cfg.SkipWhenOpenshiftAndCNI(t)

			ctx := suite.Environment().DefaultContext(t)

			helmValues := map[string]string{
				"connectInject.enabled":           "true",
				"global.tls.enabled":              strconv.FormatBool(secure),
				"global.acls.manageSystemACLs":    strconv.FormatBool(secure),
				"global.dualStack.defaultEnabled": cfg.GetDualStack(),
			}

			releaseName := helpers.RandomName()
			consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)

			consulCluster.Create(t)

			logger.Log(t, "creating static-client deployment")
			k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-client-inject")

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

			// Ensure the token exists
			if secure {
				retry.Run(t, func(r *retry.R) {
					tokens, _, err := consulClient.ACL().TokenListFiltered(
						api.ACLTokenFilterOptions{ServiceName: "static-client"}, nil)
					require.NoError(r, err)
					// Ensure that the tokens exist. Note that we must iterate over the tokens and scan for the name,
					// because older versions of Consul do not support the filtered query param and will return
					// the full list of tokens instead.
					count := 0
					for _, t := range tokens {
						if len(t.ServiceIdentities) > 0 && t.ServiceIdentities[0].ServiceName == "static-client" {
							count++
						}
					}
					require.Greater(r, count, 0)
				})
			}
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
			// Ensure the token is cleaned up
			if secure {
				retry.Run(t, func(r *retry.R) {
					tokens, _, err := consulClient.ACL().TokenList(nil)
					require.NoError(r, err)
					for _, t := range tokens {
						if strings.Contains(t.Description, podName) {
							r.Errorf("Found a token that was supposed to be deleted for pod %v", podName)
						}
					}
				})
			}
		})
	}
}

const multiport = "multiport"
const multiportAdmin = "multiport-admin"

// Test that Connect works for an application with multiple ports. The multiport application is a Pod listening on
// two ports. This tests inbound connections to each port of the multiport app, and outbound connections from the
// multiport app to static-server.
func TestConnectInject_MultiportServices(t *testing.T) {
	for _, secure := range []bool{false, true} {
		name := fmt.Sprintf("secure: %t", secure)
		t.Run(name, func(t *testing.T) {
			cfg := suite.Config()
			cfg.SkipWhenOpenshiftAndCNI(t)

			ctx := suite.Environment().DefaultContext(t)

			helmValues := map[string]string{
				"connectInject.enabled": "true",
				// Enable DNS so we can test that DNS redirection _isn't_ set in the pod.
				"dns.enabled": "true",

				"global.tls.enabled":              strconv.FormatBool(secure),
				"global.acls.manageSystemACLs":    strconv.FormatBool(secure),
				"global.dualStack.defaultEnabled": cfg.GetDualStack(),
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
							require.NotContains(r, token.Description, connhelper.StaticClientName)
							require.NotContains(r, token.Description, connhelper.StaticServerName)
						}
					})
				})
			}

			logger.Log(t, "creating multiport static-server and static-client deployments")
			k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/bases/multiport-app")
			k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-client-inject-multiport")

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
				k8s.CheckStaticServerConnectionFailing(t, ctx.KubectlOptions(t), connhelper.StaticClientName, "http://localhost:1234")
				k8s.CheckStaticServerConnectionFailing(t, ctx.KubectlOptions(t), connhelper.StaticClientName, "http://localhost:2234")

				logger.Log(t, fmt.Sprintf("creating intention for %s", multiport))
				_, _, err := consulClient.ConfigEntries().Set(&api.ServiceIntentionsConfigEntry{
					Kind: api.ServiceIntentions,
					Name: multiport,
					Sources: []*api.SourceIntention{
						{
							Name:   connhelper.StaticClientName,
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
							Name:   connhelper.StaticClientName,
							Action: api.IntentionActionAllow,
						},
					},
				}, nil)
				require.NoError(t, err)
			}

			retry.RunWith(&retry.Counter{Count: 30, Wait: 5 * time.Second}, t, func(r *retry.R) {

				// Check connection from static-client to multiport.
				k8s.CheckStaticServerConnectionSuccessful(t, ctx.KubectlOptions(r), connhelper.StaticClientName, "http://localhost:1234")

				// Check connection from static-client to multiport-admin.
				k8s.CheckStaticServerConnectionSuccessfulWithMessage(t, ctx.KubectlOptions(r), connhelper.StaticClientName, "hello world from 9090 admin", "http://localhost:2234")
			})
			// Now that we've checked inbound connections to a multi port pod, check outbound connection from multi port
			// pod to static-server.

			// Deploy static-server.
			k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")

			// For outbound connections from the multi port pod, only intentions from the first service in the multiport
			// pod need to be created, since all upstream connections are made through the first service's envoy proxy.
			if secure {
				logger.Log(t, "checking that the connection is not successful because there's no intention")

				k8s.CheckStaticServerConnectionFailing(t, ctx.KubectlOptions(t), multiport, "http://localhost:3234")

				logger.Log(t, fmt.Sprintf("creating intention for %s", connhelper.StaticServerName))
				_, _, err := consulClient.ConfigEntries().Set(&api.ServiceIntentionsConfigEntry{
					Kind: api.ServiceIntentions,
					Name: connhelper.StaticServerName,
					Sources: []*api.SourceIntention{
						{
							Name:   multiport,
							Action: api.IntentionActionAllow,
						},
					},
				}, nil)
				require.NoError(t, err)
			}
			retry.RunWith(&retry.Counter{Count: 30, Wait: 5 * time.Second}, t, func(r *retry.R) {
				// Check the connection from the multi port pod to static-server.
				k8s.CheckStaticServerConnectionSuccessful(t, ctx.KubectlOptions(r), multiport, "http://localhost:3234")
			})

			retry.RunWith(&retry.Counter{Count: 30, Wait: 5 * time.Second}, t, func(r *retry.R) {

				// Test that kubernetes readiness status is synced to Consul. This will make the multi port pods unhealthy
				// and check inbound connections to the multi port pods' services.
				// Create the files so that the readiness probes of the multi port pod fails.
				logger.Log(t, "testing k8s -> consul health checks sync by making the multiport unhealthy")
				k8s.RunKubectl(t, ctx.KubectlOptions(r), "exec", "deploy/"+multiport, "-c", "multiport", "--", "touch", "/tmp/unhealthy-multiport")
				logger.Log(t, "testing k8s -> consul health checks sync by making the multiport-admin unhealthy")
				k8s.RunKubectl(t, ctx.KubectlOptions(r), "exec", "deploy/"+multiport, "-c", "multiport-admin", "--", "touch", "/tmp/unhealthy-multiport-admin")

				// The readiness probe should take a moment to be reflected in Consul, CheckStaticServerConnection will retry
				// until Consul marks the service instance unavailable for mesh traffic, causing the connection to fail.
				// We are expecting a "connection reset by peer" error because in a case of health checks,
				// there will be no healthy proxy host to connect to. That's why we can't assert that we receive an empty reply
				// from server, which is the case when a connection is unsuccessful due to intentions in other tests.
				k8s.CheckStaticServerConnectionMultipleFailureMessages(t, ctx.KubectlOptions(r), connhelper.StaticClientName, false, []string{"curl: (56) Recv failure: Connection reset by peer", "curl: (52) Empty reply from server"}, "", "http://localhost:1234")
				k8s.CheckStaticServerConnectionMultipleFailureMessages(t, ctx.KubectlOptions(r), connhelper.StaticClientName, false, []string{"curl: (56) Recv failure: Connection reset by peer", "curl: (52) Empty reply from server"}, "", "http://localhost:2234")
			})
		})
	}
}
