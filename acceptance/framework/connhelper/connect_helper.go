// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package connhelper

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	terratestK8s "github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hashicorp/consul-k8s/acceptance/framework/config"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
)

const (
	StaticClientName = "static-client"
	StaticServerName = "static-server"
	JobName          = "job-client"

	retryTimeout = 120 * time.Second
)

// ConnectHelper configures a Consul cluster for connect injection tests.
// It also provides helper methods to exercise the connect functionality.
type ConnectHelper struct {
	// ClusterKind is the kind of Consul cluster to use (e.g. "Helm", "CLI").
	ClusterKind consul.ClusterKind

	// Secure configures the Helm chart for the test to use ACL tokens.
	Secure bool

	// HelmValues are the additional helm values to use when installing or
	// upgrading the cluster beyond connectInject.enabled, global.tls.enabled,
	// global.tls.enableAutoEncrypt, global.acls.manageSystemACLs which are
	// set by the Secure and AutoEncrypt fields.
	HelmValues map[string]string

	// ReleaseName is the name of the Consul cluster.
	ReleaseName string

	// Ctx is used to deploy Consul
	Ctx environment.TestContext

	// UseAppNamespace is used top optionally deploy applications into a separate namespace.
	// If unset, the namespace associated with Ctx is used.
	UseAppNamespace bool

	Cfg *config.TestConfig

	// consulCluster is the cluster to use for the test.
	consulCluster consul.Cluster

	// ConsulClient is the client used to test service mesh connectivity.
	ConsulClient *api.Client
}

// ConnHelperOpts allows for configuring optional parameters to be passed into the
// conn helper methods. This provides added flexibility, although not every value will be used
// by every method. See documentation for more details.
type ConnHelperOpts struct {
	ClientType string
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
	c.ConsulClient, _ = c.consulCluster.SetupConsulClient(t, c.Secure)
}

// Upgrade uses the existing Consul cluster and upgrades it using Helm values
// set by the Secure, AutoEncrypt, and HelmValues fields.
func (c *ConnectHelper) Upgrade(t *testing.T) {
	require.NotNil(t, c.consulCluster, "consulCluster must be set before calling Upgrade (Call Install first).")
	require.NotNil(t, c.ConsulClient, "ConsulClient must be set before calling Upgrade (Call Install first).")

	logger.Log(t, "upgrading Consul cluster")
	c.consulCluster.Upgrade(t, c.helmValues())
}

// KubectlOptsForApp returns options using the -apps appended namespace if
// UseAppNamespace is enabled. Otherwise, it returns the ctx options.
func (c *ConnectHelper) KubectlOptsForApp(t *testing.T) *terratestK8s.KubectlOptions {
	opts := c.Ctx.KubectlOptions(t)
	if !c.UseAppNamespace {
		return opts
	}
	return c.Ctx.KubectlOptionsForNamespace(opts.Namespace + "-apps")
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
			retrier := &retry.Timer{Timeout: retryTimeout, Wait: 100 * time.Millisecond}
			retry.RunWith(retrier, t, func(r *retry.R) {
				tokens, _, err := c.ConsulClient.ACL().TokenList(nil)
				require.NoError(r, err)
				for _, token := range tokens {
					require.NotContains(r, token.Description, StaticServerName)
					require.NotContains(r, token.Description, StaticClientName)
				}
			})
		})
	}

	logger.Log(t, "creating static-server and static-client deployments")

	c.SetupAppNamespace(t)

	opts := c.KubectlOptsForApp(t)
	if c.Cfg.EnableCNI && c.Cfg.EnableOpenshift {
		// On OpenShift with the CNI, we need to create a network attachment definition in the namespace
		// where the applications are running, and the app deployment configs need to reference that network
		// attachment definition.

		// TODO: A base fixture is the wrong place for these files
		k8s.KubectlApply(t, opts, "../fixtures/bases/openshift/")
		helpers.Cleanup(t, c.Cfg.NoCleanupOnFailure, c.Cfg.NoCleanup, func() {
			k8s.KubectlDelete(t, opts, "../fixtures/bases/openshift/")
		})

		k8s.DeployKustomize(t, opts, c.Cfg.NoCleanupOnFailure, c.Cfg.NoCleanup, c.Cfg.DebugDirectory, "../fixtures/cases/static-server-openshift")
		if c.Cfg.EnableTransparentProxy {
			k8s.DeployKustomize(t, opts, c.Cfg.NoCleanupOnFailure, c.Cfg.NoCleanup, c.Cfg.DebugDirectory, "../fixtures/cases/static-client-openshift-tproxy")
		} else {
			k8s.DeployKustomize(t, opts, c.Cfg.NoCleanupOnFailure, c.Cfg.NoCleanup, c.Cfg.DebugDirectory, "../fixtures/cases/static-client-openshift-inject")
		}
	} else {
		k8s.DeployKustomize(t, opts, c.Cfg.NoCleanupOnFailure, c.Cfg.NoCleanup, c.Cfg.DebugDirectory, "../fixtures/cases/static-server-inject")
		if c.Cfg.EnableTransparentProxy {
			k8s.DeployKustomize(t, opts, c.Cfg.NoCleanupOnFailure, c.Cfg.NoCleanup, c.Cfg.DebugDirectory, "../fixtures/cases/static-client-tproxy")
		} else {
			k8s.DeployKustomize(t, opts, c.Cfg.NoCleanupOnFailure, c.Cfg.NoCleanup, c.Cfg.DebugDirectory, "../fixtures/cases/static-client-inject")
		}
	}
	// Check that both static-server and static-client have been injected and
	// now have 2 containers.
	retry.RunWith(
		&retry.Timer{Timeout: retryTimeout, Wait: 100 * time.Millisecond}, t,
		func(r *retry.R) {
			for _, labelSelector := range []string{"app=static-server", "app=static-client"} {
				podList, err := c.Ctx.KubernetesClient(r).CoreV1().
					Pods(opts.Namespace).
					List(context.Background(), metav1.ListOptions{
						LabelSelector: labelSelector,
						FieldSelector: `status.phase=Running`,
					})
				require.NoError(r, err)
				require.Len(r, podList.Items, 1)
				require.Len(r, podList.Items[0].Spec.Containers, 2)
			}
		})
}

func (c *ConnectHelper) CreateNamespace(t *testing.T, namespace string) {
	opts := c.Ctx.KubectlOptions(t)
	_, err := k8s.RunKubectlAndGetOutputE(t, opts, "create", "ns", namespace)
	if err != nil && strings.Contains(err.Error(), "AlreadyExists") {
		return
	}
	require.NoError(t, err)
	helpers.Cleanup(t, c.Cfg.NoCleanupOnFailure, c.Cfg.NoCleanup, func() {
		k8s.RunKubectl(t, opts, "delete", "ns", namespace)
	})
}

// DeployJob deploys a job pod to the Kubernetes
// cluster which will be used to test service mesh connectivity. If the Secure
// flag is true, a pre-check is done to ensure that the ACL tokens for the test
// are deleted. The status of the deployment and injection is checked after the
// deployment is complete to ensure success.
func (c *ConnectHelper) DeployJob(t *testing.T, path string) {
	// Check that the ACL token is deleted.
	if c.Secure {
		// We need to register the cleanup function before we create the
		// deployments because golang will execute them in reverse order
		// (i.e. the last registered cleanup function will be executed first).
		t.Cleanup(func() {
			retrier := &retry.Timer{Timeout: 30 * time.Second, Wait: 100 * time.Millisecond}
			retry.RunWith(retrier, t, func(r *retry.R) {
				tokens, _, err := c.ConsulClient.ACL().TokenList(nil)
				require.NoError(r, err)
				for _, token := range tokens {
					require.NotContains(r, token.Description, JobName)
				}
			})
		})
	}

	logger.Log(t, "creating job-client deployment")
	k8s.DeployJob(t, c.Ctx.KubectlOptions(t), c.Cfg.NoCleanupOnFailure, c.Cfg.NoCleanup, c.Cfg.DebugDirectory, path)

	// Check that job-client has been injected and
	// now have 2 containers.
	for _, labelSelector := range []string{"app=job-client"} {
		podList, err := c.Ctx.KubernetesClient(t).CoreV1().Pods(c.Ctx.KubectlOptions(t).Namespace).List(context.Background(), metav1.ListOptions{
			LabelSelector: labelSelector,
		})
		require.NoError(t, err)
		require.Len(t, podList.Items, 1)
		require.Len(t, podList.Items[0].Spec.Containers, 2)
	}
}

// DeployServer deploys a server pod to the Kubernetes
// cluster which will be used to test service mesh connectivity. If the Secure
// flag is true, a pre-check is done to ensure that the ACL tokens for the test
// are deleted. The status of the deployment and injection is checked after the
// deployment is complete to ensure success.
func (c *ConnectHelper) DeployServer(t *testing.T) {
	// Check that the ACL token is deleted.
	if c.Secure {
		// We need to register the cleanup function before we create the
		// deployments because golang will execute them in reverse order
		// (i.e. the last registered cleanup function will be executed first).
		t.Cleanup(func() {
			retrier := &retry.Timer{Timeout: 30 * time.Second, Wait: 100 * time.Millisecond}
			retry.RunWith(retrier, t, func(r *retry.R) {
				tokens, _, err := c.ConsulClient.ACL().TokenList(nil)
				require.NoError(r, err)
				for _, token := range tokens {
					require.NotContains(r, token.Description, StaticServerName)
				}
			})
		})
	}

	logger.Log(t, "creating static-server deployment")
	k8s.DeployKustomize(t, c.Ctx.KubectlOptions(t), c.Cfg.NoCleanupOnFailure, c.Cfg.NoCleanup, c.Cfg.DebugDirectory, "../fixtures/cases/static-server-inject")

	// Check that  static-server has been injected and
	// now have 2 containers.
	for _, labelSelector := range []string{"app=static-server"} {
		podList, err := c.Ctx.KubernetesClient(t).CoreV1().Pods(c.Ctx.KubectlOptions(t).Namespace).List(context.Background(), metav1.ListOptions{
			LabelSelector: labelSelector,
		})
		require.NoError(t, err)
		require.Len(t, podList.Items, 1)
		require.Len(t, podList.Items[0].Spec.Containers, 2)
	}
}

// SetupAppNamespace creates a namespace where applications are deployed. This
// does nothing if UseAppNamespace is not set. The app namespace is relevant
// when testing with restricted PSA enforcement enabled.
func (c *ConnectHelper) SetupAppNamespace(t *testing.T) {
	if !c.UseAppNamespace {
		return
	}
	opts := c.KubectlOptsForApp(t)
	// If we are deploying apps in another namespace, create the namespace.

	c.CreateNamespace(t, opts.Namespace)

	if c.Cfg.EnableRestrictedPSAEnforcement {
		// Allow anything to run in the app namespace.
		k8s.RunKubectl(t, opts, "label", "--overwrite", "ns", opts.Namespace,
			"pod-security.kubernetes.io/enforce=privileged",
			"pod-security.kubernetes.io/enforce-version=v1.24",
		)
	}
}

// CreateResolverRedirect creates a resolver that redirects to a static-server, a corresponding k8s service,
// and intentions. This helper is primarily used to ensure that the virtual-ips are persisted to consul properly.
func (c *ConnectHelper) CreateResolverRedirect(t *testing.T) {
	logger.Log(t, "creating resolver redirect")
	opts := c.KubectlOptsForApp(t)
	c.SetupAppNamespace(t)
	kustomizeDir := "../fixtures/cases/resolver-redirect-virtualip"
	k8s.KubectlApplyK(t, opts, kustomizeDir)

	helpers.Cleanup(t, c.Cfg.NoCleanupOnFailure, c.Cfg.NoCleanup, func() {
		k8s.KubectlDeleteK(t, opts, kustomizeDir)
	})
}

// TestConnectionFailureWithoutIntention ensures the connection to the static
// server fails when no intentions are configured. When provided with a ClientType option
// the client is overridden, otherwise a default will be used.
func (c *ConnectHelper) TestConnectionFailureWithoutIntention(t *testing.T, connHelperOpts ConnHelperOpts) {
	logger.Log(t, "checking that the connection is not successful because there's no intention")
	opts := c.KubectlOptsForApp(t)
	//Default to deploying static-client. If a client type is passed in (ex. job-client), use that instead.
	client := StaticClientName
	if connHelperOpts.ClientType != "" {
		client = connHelperOpts.ClientType
	}

	if c.Cfg.EnableTransparentProxy {
		k8s.CheckStaticServerConnectionFailing(t, opts, client, "http://static-server")
	} else {
		k8s.CheckStaticServerConnectionFailing(t, opts, client, "http://localhost:1234")
	}
}

type IntentionOpts struct {
	ConnHelperOpts
	SourceNamespace      string
	DestinationNamespace string
}

// CreateIntention creates an intention for the static-server pod to connect to
// the static-client pod. opts parameter allows for overriding of some fields. If opts is empty
// then all namespaces and clients use defaults.
func (c *ConnectHelper) CreateIntention(t *testing.T, opts IntentionOpts) {
	logger.Log(t, "creating intention")
	//Default to deploying static-client. If a client type is passed in (ex. job-client), use that instead.
	client := StaticClientName
	if opts.ClientType != "" {
		client = opts.ClientType
	}

	sourceNamespace := c.KubectlOptsForApp(t).Namespace
	if opts.SourceNamespace != "" {
		sourceNamespace = opts.SourceNamespace
	}

	destinationNamespace := c.KubectlOptsForApp(t).Namespace
	if opts.DestinationNamespace != "" {
		destinationNamespace = opts.DestinationNamespace
	}

	retrier := &retry.Timer{Timeout: retryTimeout, Wait: 100 * time.Millisecond}
	retry.RunWith(retrier, t, func(r *retry.R) {
		_, _, err := c.ConsulClient.ConfigEntries().Set(&api.ServiceIntentionsConfigEntry{
			Kind:      api.ServiceIntentions,
			Name:      StaticServerName,
			Namespace: destinationNamespace,
			Sources: []*api.SourceIntention{
				{
					Namespace: sourceNamespace,
					Name:      client,
					Action:    api.IntentionActionAllow,
				},
			},
		}, nil)
		require.NoError(r, err)
	})
}

// TestConnectionSuccess ensures the static-server pod can connect to the
// static-client pod once the intention is set. When provided with a ClientType option
// the client is overridden, otherwise a default will be used.
func (c *ConnectHelper) TestConnectionSuccess(t *testing.T, connHelperOpts ConnHelperOpts) {
	logger.Log(t, "checking that connection is successful")
	opts := c.KubectlOptsForApp(t)
	//Default to deploying static-client. If a client type is passed in (ex. job-client), use that instead.
	client := StaticClientName
	if connHelperOpts.ClientType != "" {
		client = connHelperOpts.ClientType
	}

	if c.Cfg.EnableTransparentProxy {
		// todo: add an assertion that the traffic is going through the proxy
		k8s.CheckStaticServerConnectionSuccessful(t, opts, client, "http://static-server")
	} else {
		k8s.CheckStaticServerConnectionSuccessful(t, opts, client, "http://localhost:1234")
	}
}

// TestConnectionFailureWhenUnhealthy sets the static-server pod to be unhealthy
// and ensures the connection fails. It restores the pod to a healthy state
// after this check.
func (c *ConnectHelper) TestConnectionFailureWhenUnhealthy(t *testing.T) {
	// Test that kubernetes readiness status is synced to Consul.
	// Create a file called "unhealthy" at "/tmp/" so that the readiness probe
	// of the static-server pod fails.
	opts := c.KubectlOptsForApp(t)

	logger.Log(t, "testing k8s -> consul health checks sync by making the static-server unhealthy")
	k8s.RunKubectl(t, opts, "exec", "deploy/"+StaticServerName, "-c", "static-server", "--", "touch", "/tmp/unhealthy")

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
		k8s.CheckStaticServerConnectionMultipleFailureMessages(t, opts, StaticClientName, false, []string{
			"curl: (56) Recv failure: Connection reset by peer",
			"curl: (52) Empty reply from server",
			"curl: (7) Failed to connect to static-server port 80: Connection refused",
		}, "", "http://static-server")
	} else {
		k8s.CheckStaticServerConnectionMultipleFailureMessages(t, opts, StaticClientName, false, []string{
			"curl: (56) Recv failure: Connection reset by peer",
			"curl: (52) Empty reply from server",
		}, "", "http://localhost:1234")
	}

	// Return the static-server to a "healthy state".
	k8s.RunKubectl(t, opts, "exec", "deploy/"+StaticServerName, "-c", "static-server", "--", "rm", "/tmp/unhealthy")
}

// helmValues uses the Secure and AutoEncrypt fields to set values for the Helm
// Chart which are merged with the HelmValues field with the values set by the
// Secure and AutoEncrypt fields taking precedence.
func (c *ConnectHelper) helmValues() map[string]string {
	helmValues := map[string]string{
		"connectInject.enabled":        "true",
		"global.tls.enabled":           strconv.FormatBool(c.Secure),
		"global.acls.manageSystemACLs": strconv.FormatBool(c.Secure),
		"dns.enabled":                  "true",
		"dns.enableRedirection":        "true",

		"global.dualStack.defaultEnabled": c.Cfg.GetDualStack(),
	}

	helpers.MergeMaps(helmValues, c.HelmValues)

	return helmValues
}
