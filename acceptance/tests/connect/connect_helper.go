package connect

import (
	"context"
	"strconv"
	"testing"
	"time"

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

const (
	StaticClientName = "static-client"
	staticServerName = "static-server"
)

// ConnectHelper configures a Consul cluster for connect injection tests.
// It also provides helper methods to exercise the connect functionality.
type ConnectHelper struct {
	// ClusterKind is the kind of Consul cluster to use (e.g. "Helm", "CLI").
	ClusterKind consul.ClusterKind

	// Secure configures the Helm chart for the test to use ACL tokens.
	Secure bool

	// AutoEncrypt configures the Helm chart for the test to use AutoEncrypt.
	AutoEncrypt bool

	// HelmValues are the additional helm values to use when installing or
	// upgrading the cluster beyond connectInject.enabled, global.tls.enabled,
	// global.tls.enableAutoEncrypt, global.acls.mangageSystemACLs which are
	// set by the Secure and AutoEncrypt fields.
	HelmValues map[string]string

	// RelaseName is the name of the Consul cluster.
	ReleaseName string

	Ctx environment.TestContext
	Cfg *config.TestConfig

	// consulCluster is the cluster to use for the test.
	consulCluster consul.Cluster

	// consulClient is the client used to test service mesh connectivity.
	consulClient *api.Client
}

// Setup creates a new cluster using the New*Cluster function and assigns it
// to the consulCluster field.
func (c *ConnectHelper) Setup(t *testing.T) {
	switch c.ClusterKind {
	case consul.Helm:
		c.consulCluster = consul.NewHelmCluster(t, c.helmValues(), c.Ctx, c.Cfg, c.ReleaseName)
	case consul.CLI:
		c.consulCluster = consul.NewCLICluster(t, c.helmValues(), c.Ctx, c.Cfg, c.ReleaseName)
	}
}

// Install uses the consulCluster to install Consul onto the Kubernetes cluster.
func (c *ConnectHelper) Install(t *testing.T) {
	logger.Log(t, "Installing Consul cluster")
	c.consulCluster.Create(t)
	c.consulClient, _ = c.consulCluster.SetupConsulClient(t, c.Secure)
}

// Upgrade uses the existing Consul cluster and upgrades it using Helm values
// set by the Secure, AutoEncrypt, and HelmValues fields.
func (c *ConnectHelper) Upgrade(t *testing.T) {
	require.NotNil(t, c.consulCluster, "consulCluster must be set before calling Upgrade (Call Install first).")
	require.NotNil(t, c.consulClient, "consulClient must be set before calling Upgrade (Call Install first).")

	logger.Log(t, "upgrading Consul cluster")
	c.consulCluster.Upgrade(t, c.helmValues())
}

// DeployClientAndServer deploys a client and server pod to the Kubernetes
// cluster which will be used to test service mesh connectivity. If the Secure
// flag is true, a pre-check is done to ensure that the ACL tokens for the test
// are deleted. The status of the deployment and injection is checked after the
// deployment is complete to ensure success.
func (c *ConnectHelper) DeployClientAndServer(t *testing.T) {
	// Check that the ACL token is deleted.
	if c.Secure {
		// We need to register the cleanup function before we create the
		// deployments because golang will execute them in reverse order
		// (i.e. the last registered cleanup function will be executed first).
		t.Cleanup(func() {
			retrier := &retry.Timer{Timeout: 30 * time.Second, Wait: 100 * time.Millisecond}
			retry.RunWith(retrier, t, func(r *retry.R) {
				tokens, _, err := c.consulClient.ACL().TokenList(nil)
				require.NoError(r, err)
				for _, token := range tokens {
					require.NotContains(r, token.Description, staticServerName)
					require.NotContains(r, token.Description, StaticClientName)
				}
			})
		})
	}

	logger.Log(t, "creating static-server and static-client deployments")

	k8s.DeployKustomize(t, c.Ctx.KubectlOptions(t), c.Cfg.NoCleanupOnFailure, c.Cfg.DebugDirectory, "../fixtures/cases/static-server-inject")
	if c.Cfg.EnableTransparentProxy {
		k8s.DeployKustomize(t, c.Ctx.KubectlOptions(t), c.Cfg.NoCleanupOnFailure, c.Cfg.DebugDirectory, "../fixtures/cases/static-client-tproxy")
	} else {
		k8s.DeployKustomize(t, c.Ctx.KubectlOptions(t), c.Cfg.NoCleanupOnFailure, c.Cfg.DebugDirectory, "../fixtures/cases/static-client-inject")
	}

	// Check that both static-server and static-client have been injected and
	// now have 2 containers.
	for _, labelSelector := range []string{"app=static-server", "app=static-client"} {
		podList, err := c.Ctx.KubernetesClient(t).CoreV1().Pods(c.Ctx.KubectlOptions(t).Namespace).List(context.Background(), metav1.ListOptions{
			LabelSelector: labelSelector,
		})
		require.NoError(t, err)
		require.Len(t, podList.Items, 1)
		require.Len(t, podList.Items[0].Spec.Containers, 2)
	}
}

// TestConnectionFailureWithoutIntention ensures the connection to the static
// server fails when no intentions are configured.
func (c *ConnectHelper) TestConnectionFailureWithoutIntention(t *testing.T) {
	logger.Log(t, "checking that the connection is not successful because there's no intention")
	if c.Cfg.EnableTransparentProxy {
		k8s.CheckStaticServerConnectionFailing(t, c.Ctx.KubectlOptions(t), StaticClientName, "http://static-server")
	} else {
		k8s.CheckStaticServerConnectionFailing(t, c.Ctx.KubectlOptions(t), StaticClientName, "http://localhost:1234")
	}
}

// CreateIntention creates an intention for the static-server pod to connect to
// the static-client pod.
func (c *ConnectHelper) CreateIntention(t *testing.T) {
	logger.Log(t, "creating intention")
	_, _, err := c.consulClient.ConfigEntries().Set(&api.ServiceIntentionsConfigEntry{
		Kind: api.ServiceIntentions,
		Name: staticServerName,
		Sources: []*api.SourceIntention{
			{
				Name:   StaticClientName,
				Action: api.IntentionActionAllow,
			},
		},
	}, nil)
	require.NoError(t, err)
}

// TestConnectionSuccessful ensures the static-server pod can connect to the
// static-client pod once the intention is set.
func (c *ConnectHelper) TestConnectionSuccess(t *testing.T) {
	logger.Log(t, "checking that connection is successful")
	if c.Cfg.EnableTransparentProxy {
		// todo: add an assertion that the traffic is going through the proxy
		k8s.CheckStaticServerConnectionSuccessful(t, c.Ctx.KubectlOptions(t), StaticClientName, "http://static-server")
	} else {
		k8s.CheckStaticServerConnectionSuccessful(t, c.Ctx.KubectlOptions(t), StaticClientName, "http://localhost:1234")
	}
}

// TestConnectionFailureWhenUnhealthy sets the static-server pod to be unhealthy
// and ensures the connection fails. It restores the pod to a healthy state
// after this check.
func (c *ConnectHelper) TestConnectionFailureWhenUnhealthy(t *testing.T) {
	// Test that kubernetes readiness status is synced to Consul.
	// Create a file called "unhealthy" at "/tmp/" so that the readiness probe
	// of the static-server pod fails.
	logger.Log(t, "testing k8s -> consul health checks sync by making the static-server unhealthy")
	k8s.RunKubectl(t, c.Ctx.KubectlOptions(t), "exec", "deploy/"+staticServerName, "--", "touch", "/tmp/unhealthy")

	// The readiness probe should take a moment to be reflected in Consul,
	// CheckStaticServerConnection will retry until Consul marks the service
	// instance unavailable for mesh traffic, causing the connection to fail.
	// We are expecting a "connection reset by peer" error because in a case of
	// health checks, there will be no healthy proxy host to connect to.
	// That's why we can't assert that we receive an empty reply from server,
	// which is the case when a connection is unsuccessful due to intentions in
	// other tests.
	logger.Log(t, "checking that connection is unsuccessful")
	if c.Cfg.EnableTransparentProxy {
		k8s.CheckStaticServerConnectionMultipleFailureMessages(t, c.Ctx.KubectlOptions(t), StaticClientName, false, []string{
			"curl: (56) Recv failure: Connection reset by peer",
			"curl: (52) Empty reply from server",
			"curl: (7) Failed to connect to static-server port 80: Connection refused",
		}, "", "http://static-server")
	} else {
		k8s.CheckStaticServerConnectionMultipleFailureMessages(t, c.Ctx.KubectlOptions(t), StaticClientName, false, []string{
			"curl: (56) Recv failure: Connection reset by peer",
			"curl: (52) Empty reply from server",
		}, "", "http://localhost:1234")
	}

	// Return the static-server to a "healthy state".
	k8s.RunKubectl(t, c.Ctx.KubectlOptions(t), "exec", "deploy/"+staticServerName, "--", "rm", "/tmp/unhealthy")
}

// helmValues uses the Secure and AutoEncrypt fields to set values for the Helm
// Chart which are merged with the HelmValues field with the values set by the
// Secure and AutoEncrypt fields taking precedence.
func (c *ConnectHelper) helmValues() map[string]string {
	helmValues := map[string]string{
		"connectInject.enabled":        "true",
		"global.tls.enabled":           strconv.FormatBool(c.Secure),
		"global.tls.enableAutoEncrypt": strconv.FormatBool(c.AutoEncrypt),
		"global.acls.manageSystemACLs": strconv.FormatBool(c.Secure),
	}

	helpers.MergeMaps(helmValues, c.HelmValues)

	return helmValues
}
