package connect

import (
	"context"
	"strconv"
	"testing"

	"github.com/hashicorp/consul-k8s/acceptance/framework/config"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const staticClientName = "static-client"
const staticServerName = "static-server"

// ConnectInjectConnectivityCheck is a helper function used by the connect tests and cli smoke tests to test service
// mesh connectivity.
func ConnectInjectConnectivityCheck(t *testing.T, ctx environment.TestContext, cfg *config.TestConfig, secure bool, autoEncrypt bool, cli bool) {
	helmValues := map[string]string{
		"connectInject.enabled": "true",

		"global.tls.enabled":           strconv.FormatBool(secure),
		"global.tls.enableAutoEncrypt": strconv.FormatBool(autoEncrypt),
		"global.acls.manageSystemACLs": strconv.FormatBool(secure),
	}

	releaseName := helpers.RandomName()
	var consulCluster consul.Cluster
	if cli {
		consulCluster = consul.NewCLICluster(t, helmValues, ctx, cfg, consul.CLIReleaseName)
	} else {
		consulCluster = consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)
	}

	consulCluster.Create(t)

	consulClient := consulCluster.SetupConsulClient(t, secure)

	// Check that the ACL token is deleted.
	if secure {
		// We need to register the cleanup function before we create the deployments
		// because golang will execute them in reverse order i.e. the last registered
		// cleanup function will be executed first.
		t.Cleanup(func() {
			retry.Run(t, func(r *retry.R) {
				tokens, _, err := consulClient.ACL().TokenList(nil)
				require.NoError(r, err)
				for _, token := range tokens {
					require.NotContains(r, token.Description, staticServerName)
					require.NotContains(r, token.Description, staticClientName)
				}
			})
		})
	}

	logger.Log(t, "creating static-server and static-client deployments")
	k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")
	if cfg.EnableTransparentProxy {
		k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-client-tproxy")
	} else {
		k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-client-inject")
	}

	// Check that both static-server and static-client have been injected and now have 2 containers.
	for _, labelSelector := range []string{"app=static-server", "app=static-client"} {
		podList, err := ctx.KubernetesClient(t).CoreV1().Pods(ctx.KubectlOptions(t).Namespace).List(context.Background(), metav1.ListOptions{
			LabelSelector: labelSelector,
		})
		require.NoError(t, err)
		require.Len(t, podList.Items, 1)
		require.Len(t, podList.Items[0].Spec.Containers, 2)
	}

	if secure {
		logger.Log(t, "checking that the connection is not successful because there's no intention")
		if cfg.EnableTransparentProxy {
			k8s.CheckStaticServerConnectionFailing(t, ctx.KubectlOptions(t), "http://static-server")
		} else {
			k8s.CheckStaticServerConnectionFailing(t, ctx.KubectlOptions(t), "http://localhost:1234")
		}

		logger.Log(t, "creating intention")
		_, err := consulClient.Connect().IntentionUpsert(&api.Intention{
			SourceName:      staticClientName,
			DestinationName: staticServerName,
			Action:          api.IntentionActionAllow,
		}, nil)
		require.NoError(t, err)
	}

	logger.Log(t, "checking that connection is successful")
	if cfg.EnableTransparentProxy {
		// todo: add an assertion that the traffic is going through the proxy
		k8s.CheckStaticServerConnectionSuccessful(t, ctx.KubectlOptions(t), "http://static-server")
	} else {
		k8s.CheckStaticServerConnectionSuccessful(t, ctx.KubectlOptions(t), "http://localhost:1234")
	}

	// Test that kubernetes readiness status is synced to Consul.
	// Create the file so that the readiness probe of the static-server pod fails.
	logger.Log(t, "testing k8s -> consul health checks sync by making the static-server unhealthy")
	k8s.RunKubectl(t, ctx.KubectlOptions(t), "exec", "deploy/"+staticServerName, "--", "touch", "/tmp/unhealthy")

	// The readiness probe should take a moment to be reflected in Consul, CheckStaticServerConnection will retry
	// until Consul marks the service instance unavailable for mesh traffic, causing the connection to fail.
	// We are expecting a "connection reset by peer" error because in a case of health checks,
	// there will be no healthy proxy host to connect to. That's why we can't assert that we receive an empty reply
	// from server, which is the case when a connection is unsuccessful due to intentions in other tests.
	logger.Log(t, "checking that connection is unsuccessful")
	if cfg.EnableTransparentProxy {
		k8s.CheckStaticServerConnectionMultipleFailureMessages(t, ctx.KubectlOptions(t), false, []string{"curl: (56) Recv failure: Connection reset by peer", "curl: (52) Empty reply from server", "curl: (7) Failed to connect to static-server port 80: Connection refused"}, "http://static-server")
	} else {
		k8s.CheckStaticServerConnectionMultipleFailureMessages(t, ctx.KubectlOptions(t), false, []string{"curl: (56) Recv failure: Connection reset by peer", "curl: (52) Empty reply from server"}, "http://localhost:1234")
	}
}
