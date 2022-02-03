package connect

import (
	"context"
	"strconv"
	"testing"

	"github.com/hashicorp/consul-k8s/acceptance/framework/config"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	staticClientName = "static-client"
	staticServerName = "static-server"
)

// ConnectHelper configures and runs a consul cluster for connect injection tests.
type ConnectHelper struct {
	// ClusterGenerator creates a new instance of a cluster (e.g. a Helm cluster or CLI cluster).
	ClusterGenerator func(*testing.T, map[string]string, environment.TestContext, *config.TestConfig, string) consul.Cluster

	// RelaseName is the name of the cluster that will be generated by the ClusterGenerator.
	ReleaseName string

	// Secure sets whether TLS and acls.manageSystemACLs are enabled.
	Secure bool

	// AutoEncrypt sets whether TLS auto-encrypt is enabled.
	AutoEncrypt bool

	T   *testing.T
	Ctx environment.TestContext
	Cfg *config.TestConfig
}

// InstallThenCheckConnectInjection creates a new cluster using the ClusterGenerator function then runs its Create method
// to install Consul. It then sets up a consulClient and passes that to the testConnectInject method to test service
// mesh connectivity.
func (c *ConnectHelper) InstallThenCheckConnectInjection() {
	helmValues := map[string]string{
		"connectInject.enabled": "true",

		"global.tls.enabled":           strconv.FormatBool(c.Secure),
		"global.tls.enableAutoEncrypt": strconv.FormatBool(c.AutoEncrypt),
		"global.acls.manageSystemACLs": strconv.FormatBool(c.Secure),
	}

	consulCluster := c.ClusterGenerator(c.T, helmValues, c.Ctx, c.Cfg, c.ReleaseName)

	consulCluster.Create(c.T)
	consulClient := consulCluster.SetupConsulClient(c.T, c.Secure)

	c.testConnectInject(consulClient)
}

// ConnectInjectConnectivityCheck is a helper function used by the connect tests and cli smoke tests to test service
// mesh connectivity.
func (c *ConnectHelper) testConnectInject(consulClient *api.Client) {
	// Check that the ACL token is deleted.
	if c.Secure {
		// We need to register the cleanup function before we create the deployments
		// because golang will execute them in reverse order i.e. the last registered
		// cleanup function will be executed first.
		c.T.Cleanup(func() {
			retry.Run(c.T, func(r *retry.R) {
				tokens, _, err := consulClient.ACL().TokenList(nil)
				require.NoError(r, err)
				for _, token := range tokens {
					require.NotContains(r, token.Description, staticServerName)
					require.NotContains(r, token.Description, staticClientName)
				}
			})
		})
	}

	logger.Log(c.T, "creating static-server and static-client deployments")
	k8s.DeployKustomize(c.T, c.Ctx.KubectlOptions(c.T), c.Cfg.NoCleanupOnFailure, c.Cfg.DebugDirectory, "../fixtures/cases/static-server-inject")
	if c.Cfg.EnableTransparentProxy {
		k8s.DeployKustomize(c.T, c.Ctx.KubectlOptions(c.T), c.Cfg.NoCleanupOnFailure, c.Cfg.DebugDirectory, "../fixtures/cases/static-client-tproxy")
	} else {
		k8s.DeployKustomize(c.T, c.Ctx.KubectlOptions(c.T), c.Cfg.NoCleanupOnFailure, c.Cfg.DebugDirectory, "../fixtures/cases/static-client-inject")
	}

	// Check that both static-server and static-client have been injected and now have 2 containers.
	for _, labelSelector := range []string{"app=static-server", "app=static-client"} {
		podList, err := c.Ctx.KubernetesClient(c.T).CoreV1().Pods(c.Ctx.KubectlOptions(c.T).Namespace).List(context.Background(), metav1.ListOptions{
			LabelSelector: labelSelector,
		})
		require.NoError(c.T, err)
		require.Len(c.T, podList.Items, 1)
		require.Len(c.T, podList.Items[0].Spec.Containers, 2)
	}

	if c.Secure {
		logger.Log(c.T, "checking that the connection is not successful because there's no intention")
		if c.Cfg.EnableTransparentProxy {
			k8s.CheckStaticServerConnectionFailing(c.T, c.Ctx.KubectlOptions(c.T), staticClientName, "http://static-server")
		} else {
			k8s.CheckStaticServerConnectionFailing(c.T, c.Ctx.KubectlOptions(c.T), staticClientName, "http://localhost:1234")
		}

		logger.Log(c.T, "creating intention")
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
		require.NoError(c.T, err)
	}

	logger.Log(c.T, "checking that connection is successful")
	if c.Cfg.EnableTransparentProxy {
		// todo: add an assertion that the traffic is going through the proxy
		k8s.CheckStaticServerConnectionSuccessful(c.T, c.Ctx.KubectlOptions(c.T), staticClientName, "http://static-server")
	} else {
		k8s.CheckStaticServerConnectionSuccessful(c.T, c.Ctx.KubectlOptions(c.T), staticClientName, "http://localhost:1234")
	}

	// Test that kubernetes readiness status is synced to Consul.
	// Create the file so that the readiness probe of the static-server pod fails.
	logger.Log(c.T, "testing k8s -> consul health checks sync by making the static-server unhealthy")
	k8s.RunKubectl(c.T, c.Ctx.KubectlOptions(c.T), "exec", "deploy/"+staticServerName, "--", "touch", "/tmp/unhealthy")

	// The readiness probe should take a moment to be reflected in Consul, CheckStaticServerConnection will retry
	// until Consul marks the service instance unavailable for mesh traffic, causing the connection to fail.
	// We are expecting a "connection reset by peer" error because in a case of health checks,
	// there will be no healthy proxy host to connect to. That's why we can't assert that we receive an empty reply
	// from server, which is the case when a connection is unsuccessful due to intentions in other tests.
	logger.Log(c.T, "checking that connection is unsuccessful")
	if c.Cfg.EnableTransparentProxy {
		k8s.CheckStaticServerConnectionMultipleFailureMessages(c.T, c.Ctx.KubectlOptions(c.T), staticClientName, false, []string{"curl: (56) Recv failure: Connection reset by peer", "curl: (52) Empty reply from server", "curl: (7) Failed to connect to static-server port 80: Connection refused"}, "", "http://static-server")
	} else {
		k8s.CheckStaticServerConnectionMultipleFailureMessages(c.T, c.Ctx.KubectlOptions(c.T), staticClientName, false, []string{"curl: (56) Recv failure: Connection reset by peer", "curl: (52) Empty reply from server"}, "", "http://localhost:1234")
	}
}
