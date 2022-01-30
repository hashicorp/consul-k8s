package connect

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Test that Connect works in a default and a secure installation.
func TestConnectInject(t *testing.T) {
	cases := []struct {
		secure      bool
		autoEncrypt bool
	}{
		{false, false},
		{true, false},
		{true, true},
	}

	for _, c := range cases {
		name := fmt.Sprintf("secure: %t; auto-encrypt: %t", c.secure, c.autoEncrypt)
		t.Run(name, func(t *testing.T) {
			cfg := suite.Config()
			ctx := suite.Environment().DefaultContext(t)

			ConnectInjectConnectivityCheck(t, ctx, cfg, c.secure, c.autoEncrypt, false)

		})
	}
}

// Test the endpoints controller cleans up force-killed pods.
func TestConnectInject_CleanupKilledPods(t *testing.T) {
	cases := []struct {
		secure      bool
		autoEncrypt bool
	}{
		{false, false},
		{true, false},
		{true, true},
	}

	for _, c := range cases {
		name := fmt.Sprintf("secure: %t; auto-encrypt: %t", c.secure, c.autoEncrypt)
		t.Run(name, func(t *testing.T) {
			cfg := suite.Config()
			ctx := suite.Environment().DefaultContext(t)

			helmValues := map[string]string{
				"connectInject.enabled":        "true",
				"global.tls.enabled":           strconv.FormatBool(c.secure),
				"global.tls.enableAutoEncrypt": strconv.FormatBool(c.autoEncrypt),
				"global.acls.manageSystemACLs": strconv.FormatBool(c.secure),
			}

			releaseName := helpers.RandomName()
			consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)

			consulCluster.Create(t)

			logger.Log(t, "creating static-client deployment")
			k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-client-inject")

			logger.Log(t, "waiting for static-client to be registered with Consul")
			consulClient := consulCluster.SetupConsulClient(t, c.secure)
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

// Test that when Consul clients are restarted and lose all their registrations,
// the services get re-registered and can continue to talk to each other.
func TestConnectInject_RestartConsulClients(t *testing.T) {
	cfg := suite.Config()
	ctx := suite.Environment().DefaultContext(t)

	helmValues := map[string]string{
		"connectInject.enabled": "true",
	}

	releaseName := helpers.RandomName()
	consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)

	consulCluster.Create(t)

	logger.Log(t, "creating static-server and static-client deployments")
	k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")
	if cfg.EnableTransparentProxy {
		k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-client-tproxy")
	} else {
		k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-client-inject")
	}

	logger.Log(t, "checking that connection is successful")
	if cfg.EnableTransparentProxy {
		k8s.CheckStaticServerConnectionSuccessful(t, ctx.KubectlOptions(t), "http://static-server")
	} else {
		k8s.CheckStaticServerConnectionSuccessful(t, ctx.KubectlOptions(t), "http://localhost:1234")
	}

	logger.Log(t, "restarting Consul client daemonset")
	k8s.RunKubectl(t, ctx.KubectlOptions(t), "rollout", "restart", fmt.Sprintf("ds/%s-consul-client", releaseName))
	k8s.RunKubectl(t, ctx.KubectlOptions(t), "rollout", "status", fmt.Sprintf("ds/%s-consul-client", releaseName))

	logger.Log(t, "checking that connection is still successful")
	if cfg.EnableTransparentProxy {
		k8s.CheckStaticServerConnectionSuccessful(t, ctx.KubectlOptions(t), "http://static-server")
	} else {
		k8s.CheckStaticServerConnectionSuccessful(t, ctx.KubectlOptions(t), "http://localhost:1234")
	}
}

// Test that Connect works in a default and a secure installation.
func TestConnectInject_MultiportServices(t *testing.T) {
	cases := []struct {
		secure      bool
		autoEncrypt bool
	}{
		{false, false},
		{true, false},
		{true, true},
	}

	for _, c := range cases {
		name := fmt.Sprintf("secure: %t; auto-encrypt: %t", c.secure, c.autoEncrypt)
		t.Run(name, func(t *testing.T) {
			cfg := suite.Config()
			ctx := suite.Environment().DefaultContext(t)

			helmValues := map[string]string{
				"connectInject.enabled": "true",

				"global.tls.enabled":           strconv.FormatBool(c.secure),
				"global.tls.enableAutoEncrypt": strconv.FormatBool(c.autoEncrypt),
				"global.acls.manageSystemACLs": strconv.FormatBool(c.secure),
			}

			releaseName := helpers.RandomName()
			consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)

			consulCluster.Create(t)

			consulClient := consulCluster.SetupConsulClient(t, c.secure)

			// Check that the ACL token is deleted.
			if c.secure {
				// We need to register the cleanup function before we create the deployments
				// because golang will execute them in reverse order i.e. the last registered
				// cleanup function will be executed first.
				t.Cleanup(func() {
					retrier := &retry.Timer{Timeout: 5 * time.Minute, Wait: 1 * time.Second}
					retry.RunWith(retrier, t, func(r *retry.R) {
						tokens, _, err := consulClient.ACL().TokenList(nil)
						require.NoError(r, err)
						for _, token := range tokens {
							require.NotContains(r, token.Description, staticServerName)
							require.NotContains(r, token.Description, staticServerAdminName)
							require.NotContains(r, token.Description, staticClientName)
						}
					})
				})
			}

			logger.Log(t, "creating multiport static-server and static-client deployments")
			//k8s.KubectlApply(t, ctx.KubectlOptions(t), "../../../web.yaml")
			//helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
			//	k8s.KubectlDelete(t, ctx.KubectlOptions(t), "../../../web.yaml")
			//})
			k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-server-multiport")
			k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-client-inject")

			// TODO check for injection

			if c.secure {
				logger.Log(t, "checking that the connection is not successful because there's no intention")
				k8s.CheckStaticServerConnectionFailing(t, ctx.KubectlOptions(t), "http://localhost:2234")
				k8s.CheckStaticServerConnectionFailing(t, ctx.KubectlOptions(t), "http://localhost:1234")

				logger.Log(t, fmt.Sprintf("creating intention for %s", staticServerName))
				_, _, err := consulClient.ConfigEntries().Set(&api.ServiceIntentionsConfigEntry{
					Kind: api.ServiceIntentions,
					Name: staticServerName,
					Sources: []*api.SourceIntention{
						{
							Name:   staticClientName,
							Action: api.IntentionActionAllow,
						},
					},
				}, nil)
				require.NoError(t, err)
				logger.Log(t, fmt.Sprintf("creating intention for %s", staticServerAdminName))
				_, _, err = consulClient.ConfigEntries().Set(&api.ServiceIntentionsConfigEntry{
					Kind: api.ServiceIntentions,
					Name: staticServerAdminName,
					Sources: []*api.SourceIntention{
						{
							Name:   staticClientName,
							Action: api.IntentionActionAllow,
						},
					},
				}, nil)
				require.NoError(t, err)
			}

			// Check connection to web
			// TODO replace with CheckStaticServer functions
			k8s.CheckStaticServerConnectionSuccessful(t, ctx.KubectlOptions(t), "http://localhost:2234")
			//argsweb := []string{"exec", "deploy/" + staticClientName, "-c", staticClientName, "--", "curl", "-vvvsSf"}
			//argsweb = append(args, "http://localhost:2234")
			//retry.RunWith(retrier, t, func(r *retry.R) {
			//	output, err := k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), argsweb...)
			//	require.NoError(r, err)
			//	require.Contains(r, output, "hello world")
			//})

			// Check connection to web-admin
			// TODO replace with CheckStaticServer functions
			k8s.CheckStaticServerConnectionSuccessfulWithMessage(t, ctx.KubectlOptions(t), "hello world from 9090 admin", "http://localhost:1234")
			//retrier := &retry.Timer{Timeout: 80 * time.Second, Wait: 2 * time.Second}
			//args := []string{"exec", "deploy/" + staticClientName, "-c", staticClientName, "--", "curl", "-vvvsSf"}
			//args = append(args, "http://localhost:1234")
			//retry.RunWith(retrier, t, func(r *retry.R) {
			//	output, err := k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), args...)
			//	require.NoError(r, err)
			//	require.Contains(r, output, "hello world from 9090 admin")
			//})

			// Test that kubernetes readiness status is synced to Consul.
			// Create the file so that the readiness probe of the static-server pod fails.
			logger.Log(t, "testing k8s -> consul health checks sync by making the static-server unhealthy")
			k8s.RunKubectl(t, ctx.KubectlOptions(t), "exec", "deploy/"+staticServerName, "--", "touch", "/tmp/unhealthy-static-server")
			logger.Log(t, "testing k8s -> consul health checks sync by making the static-server-admin unhealthy")
			k8s.RunKubectl(t, ctx.KubectlOptions(t), "exec", "deploy/"+staticServerName, "--", "touch", "/tmp/unhealthy-static-server-admin")

			// The readiness probe should take a moment to be reflected in Consul, CheckStaticServerConnection will retry
			// until Consul marks the service instance unavailable for mesh traffic, causing the connection to fail.
			// We are expecting a "connection reset by peer" error because in a case of health checks,
			// there will be no healthy proxy host to connect to. That's why we can't assert that we receive an empty reply
			// from server, which is the case when a connection is unsuccessful due to intentions in other tests.
			k8s.CheckStaticServerConnectionMultipleFailureMessages(t, ctx.KubectlOptions(t), false, []string{"curl: (56) Recv failure: Connection reset by peer", "curl: (52) Empty reply from server"}, "", "http://localhost:2234")
			k8s.CheckStaticServerConnectionMultipleFailureMessages(t, ctx.KubectlOptions(t), false, []string{"curl: (56) Recv failure: Connection reset by peer", "curl: (52) Empty reply from server"}, "", "http://localhost:1234")

			//t.FailNow()

		})
	}
}
