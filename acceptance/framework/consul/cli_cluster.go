// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package consul

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gruntwork-io/terratest/modules/helm"
	terratestk8s "github.com/gruntwork-io/terratest/modules/k8s"
	terratestLogger "github.com/gruntwork-io/terratest/modules/logger"
	"github.com/hashicorp/consul-k8s/acceptance/framework/cli"
	"github.com/hashicorp/consul-k8s/acceptance/framework/config"
	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	consulNS       = "consul"
	CLIReleaseName = "consul"
)

// CLICluster.
type CLICluster struct {
	ctx                environment.TestContext
	namespace          string
	helmOptions        *helm.Options
	kubectlOptions     *terratestk8s.KubectlOptions
	values             map[string]string
	releaseName        string
	kubernetesClient   kubernetes.Interface
	kubeConfig         string
	kubeContext        string
	noCleanupOnFailure bool
	noCleanup          bool
	debugDirectory     string
	logger             terratestLogger.TestLogger
	cli                cli.CLI
	enableOpenshift    bool
}

// NewCLICluster creates a new Consul cluster struct which can be used to create
// and destroy a Consul cluster using the Consul K8s CLI.
func NewCLICluster(
	t *testing.T,
	helmValues map[string]string,
	ctx environment.TestContext,
	cfg *config.TestConfig,
	releaseName string,
) *CLICluster {
	// Create the namespace so the PSPs, SCCs, and enterprise secret can be
	// created in the right namespace.
	createOrUpdateNamespace(t, ctx.KubernetesClient(t), consulNS)

	if cfg.EnablePodSecurityPolicies {
		configurePSA(t, ctx.KubernetesClient(t), cfg, consulNS)
	}

	if cfg.EnableOpenshift && cfg.EnableTransparentProxy {
		configureSCCs(t, ctx.KubernetesClient(t), cfg, consulNS)
	}

	if cfg.EnterpriseLicense != "" {
		createOrUpdateLicenseSecret(t, ctx.KubernetesClient(t), cfg, consulNS)
	}

	// Deploy with the following defaults unless helmValues overwrites it.
	values := defaultValues()
	valuesFromConfig, err := cfg.HelmValuesFromConfig()
	require.NoError(t, err)

	// Merge all helm values
	helpers.MergeMaps(values, valuesFromConfig)
	helpers.MergeMaps(values, helmValues)

	if cfg.UseOpenshift || cfg.EnableOpenshift {
		applyOpenShiftDefaults(t, cfg, values)
	}

	logger := terratestLogger.New(logger.TestLogger{})

	kopts := ctx.KubectlOptions(t)
	kopts.Namespace = consulNS
	hopts := &helm.Options{
		SetValues:      values,
		KubectlOptions: kopts,
		Logger:         logger,
	}

	cli, err := cli.NewCLI()
	require.NoError(t, err)

	require.Greater(t, len(cfg.KubeEnvs), 0)
	return &CLICluster{
		ctx:                ctx,
		helmOptions:        hopts,
		kubectlOptions:     kopts,
		namespace:          cfg.GetPrimaryKubeEnv().KubeNamespace,
		values:             values,
		releaseName:        releaseName,
		kubernetesClient:   ctx.KubernetesClient(t),
		kubeConfig:         cfg.GetPrimaryKubeEnv().KubeConfig,
		kubeContext:        cfg.GetPrimaryKubeEnv().KubeContext,
		noCleanupOnFailure: cfg.NoCleanupOnFailure,
		noCleanup:          cfg.NoCleanup,
		debugDirectory:     cfg.DebugDirectory,
		logger:             logger,
		cli:                *cli,
		enableOpenshift:    cfg.UseOpenshift || cfg.EnableOpenshift,
	}
}

// Create uses the `consul-k8s install` command to create a Consul cluster. The command itself will fail if there are
// prior installations of Consul in the cluster so it is sufficient to run the install command without a pre-check.
func (c *CLICluster) Create(t *testing.T) {
	t.Helper()

	// Make sure we delete the cluster if we receive an interrupt signal and
	// register cleanup so that we delete the cluster when test finishes.
	helpers.Cleanup(t, c.noCleanupOnFailure, c.noCleanup, func() {
		c.Destroy(t)
	})

	// Set the args for running the install command.
	args := []string{"install"}

	for k, v := range c.values {
		args = append(args, "-set", fmt.Sprintf("%s=%s", k, v))
	}

	// Match the timeout for the helm tests.
	args = append(args, "-timeout", "15m")
	args = append(args, "-auto-approve")

	// On OpenShift, clean up any stale consul Helm releases across all namespaces before
	// installing. A previous failed/interrupted test may have left a release in a different
	// namespace (e.g. "default") which causes `consul-k8s install` to refuse with
	// "A Consul cluster is already installed".
	if c.enableOpenshift {
		c.cleanupStaleConsulReleasesAllNamespaces(t)
	}

	// On OpenShift, transient Kubernetes API errors (e.g. context deadline exceeded from
	// admission webhooks) can cause the install to fail. Wrap the install in a retry loop
	// so that transient failures are recovered by cleaning up the partial release and retrying.
	if c.enableOpenshift {
		retry.RunWith(&retry.Counter{Wait: retryWaitDuration, Count: retryMaxCount}, t, func(r *retry.R) {
			out, err := c.cli.Run(r, c.kubectlOptions, args...)
			if err != nil {
				c.logger.Logf(r, "error running command `consul-k8s %s`: %s", strings.Join(args, " "), err.Error())
				c.logger.Logf(r, "command stdout: %s", string(out))
				if isCLIOutputRetryable(string(out)) {
					// Attempt to clean up any partial/failed Helm release before retrying.
					destroyArgs := []string{"uninstall", "-auto-approve", "-wipe-data"}
					if _, destroyErr := c.cli.Run(r, c.kubectlOptions, destroyArgs...); destroyErr != nil {
						c.logger.Logf(r, "cleanup before retry failed (ignoring): %s", destroyErr.Error())
					}
					r.Errorf("retrying consul-k8s install after transient error: %v\noutput: %s", err, string(out))
					return
				}
			}
			require.NoError(r, err)
		})
	} else {
		out, err := c.cli.Run(t, c.kubectlOptions, args...)
		if err != nil {
			c.logger.Logf(t, "error running command `consul-k8s %s`: %s", strings.Join(args, " "), err.Error())
			c.logger.Logf(t, "command stdout: %s", string(out))
		}
		require.NoError(t, err)
	}

	k8s.WaitForAllPodsToBeReady(t, c.kubernetesClient, consulNS, fmt.Sprintf("release=%s", c.releaseName))
}

// Upgrade uses the `consul-k8s upgrade` command to upgrade a Consul cluster.
func (c *CLICluster) Upgrade(t *testing.T, helmValues map[string]string) {
	t.Helper()

	k8s.WritePodsDebugInfoIfFailed(t, c.kubectlOptions, c.debugDirectory, "release="+c.releaseName)
	if t.Failed() {
		c.logger.Logf(t, "skipping upgrade due to previous failure")
		return
	}

	// Set the args for running the upgrade command.
	args := []string{"upgrade"}
	args = c.setKube(args)

	helpers.MergeMaps(c.helmOptions.SetValues, helmValues)
	for k, v := range c.helmOptions.SetValues {
		args = append(args, "-set", fmt.Sprintf("%s=%s", k, v))
	}

	// Match the timeout for the helm tests.
	args = append(args, "-timeout", "15m")
	args = append(args, "-auto-approve")

	out, err := c.cli.Run(t, c.kubectlOptions, args...)
	if err != nil {
		c.logger.Logf(t, "error running command `consul-k8s %s`: %s", strings.Join(args, " "), err.Error())
		c.logger.Logf(t, "command stdout: %s", string(out))
	}
	require.NoError(t, err)

	k8s.WaitForAllPodsToBeReady(t, c.kubernetesClient, consulNS, fmt.Sprintf("release=%s", c.releaseName))
}

// Destroy uses the `consul-k8s uninstall` command to destroy a Consul cluster.
func (c *CLICluster) Destroy(t *testing.T) {
	t.Helper()

	k8s.WritePodsDebugInfoIfFailed(t, c.kubectlOptions, c.debugDirectory, "release="+c.releaseName)

	// Set the args for running the uninstall command.
	args := []string{"uninstall"}
	args = append(args, "-auto-approve", "-wipe-data")

	// Use `go run` so that the CLI is recompiled and therefore uses the local
	// charts directory rather than the directory from whenever it was last
	// compiled.
	out, err := c.cli.Run(t, c.kubectlOptions, args...)
	if err != nil {
		c.logger.Logf(t, "error running command `consul-k8s %s`: %s", strings.Join(args, " "), err.Error())
		c.logger.Logf(t, "command stdout: %s", string(out))
	}
	require.NoError(t, err)
}

func (c *CLICluster) SetupConsulClient(t *testing.T, secure bool, release ...string) (*api.Client, string) {
	t.Helper()

	releaseName := c.releaseName
	if len(release) > 0 {
		releaseName = release[0]
	}

	namespace := c.kubectlOptions.Namespace
	config := api.DefaultConfig()
	localPort := terratestk8s.GetAvailablePort(t)
	remotePort := 8500 // use non-secure by default

	if secure {
		// Overwrite remote port to HTTPS.
		remotePort = 8501

		// It's OK to skip TLS verification for local traffic.
		config.TLSConfig.InsecureSkipVerify = true
		config.Scheme = "https"

		// Get the ACL token. First, attempt to read it from the bootstrap token (this will be true in primary Consul servers).
		// If the bootstrap token doesn't exist, it means we are running against a secondary cluster
		// and will try to read the replication token from the federation secret.
		// In secondary servers, we don't create a bootstrap token since ACLs are only bootstrapped in the primary.
		// Instead, we provide a replication token that serves the role of the bootstrap token.

		aclSecretName := fmt.Sprintf("%s-consul-bootstrap-acl-token", releaseName)
		if c.releaseName == CLIReleaseName {
			aclSecretName = "consul-bootstrap-acl-token"
		}
		aclSecret, err := c.kubernetesClient.CoreV1().Secrets(namespace).Get(context.Background(), aclSecretName, metav1.GetOptions{})
		if err != nil && errors.IsNotFound(err) {
			federationSecret := fmt.Sprintf("%s-consul-federation", releaseName)
			if c.releaseName == CLIReleaseName {
				federationSecret = "consul-federation"
			}
			aclSecret, err = c.kubernetesClient.CoreV1().Secrets(namespace).Get(context.Background(), federationSecret, metav1.GetOptions{})
			require.NoError(t, err)
			config.Token = string(aclSecret.Data["replicationToken"])
		} else if err == nil {
			config.Token = string(aclSecret.Data["token"])
		} else {
			require.NoError(t, err)
		}
	}

	serverPod := fmt.Sprintf("%s-consul-server-0", releaseName)
	if releaseName == CLIReleaseName {
		serverPod = "consul-server-0"
	}
	tunnel := terratestk8s.NewTunnelWithLogger(
		c.kubectlOptions,
		terratestk8s.ResourceTypePod,
		serverPod,
		localPort,
		remotePort,
		c.logger)

	// Retry creating the port forward since it can fail occasionally.
	retry.RunWith(&retry.Counter{Wait: 3 * time.Second, Count: 60}, t, func(r *retry.R) {
		require.NoError(r, tunnel.ForwardPortE(r))
	})

	t.Cleanup(func() {
		tunnel.Close()
	})

	config.Address = fmt.Sprintf("localhost:%d", localPort)
	consulClient, err := api.NewClient(config)
	require.NoError(t, err)

	return consulClient, config.Address
}

func (c *CLICluster) CLI() cli.CLI {
	return c.cli
}

func createOrUpdateNamespace(t *testing.T, client kubernetes.Interface, namespace string) {
	_, err := client.CoreV1().Namespaces().Get(context.Background(), namespace, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		_, err := client.CoreV1().Namespaces().Create(context.Background(), &v1.Namespace{
			TypeMeta: metav1.TypeMeta{},
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}, metav1.CreateOptions{})
		require.NoError(t, err)
	} else {
		require.NoError(t, err)
	}
}

// setKube adds the args for KubeConfig and KubeCluster if they have been set on the CLICluster.
func (c *CLICluster) setKube(args []string) []string {
	kubeconfig := c.kubeConfig
	if kubeconfig != "" {
		args = append(args, "-kubeconfig", kubeconfig)
	}

	kubecontext := c.kubeContext
	if kubecontext != "" {
		args = append(args, "-context", kubecontext)
	}

	return args
}

// cleanupStaleConsulReleasesAllNamespaces finds and force-removes any stale consul
// Helm releases across all namespaces using helm directly (with --no-hooks to bypass
// the gateway-cleanup job that may hang on a failed cluster). This prevents
// `consul-k8s install` from refusing with "A Consul cluster is already installed".
func (c *CLICluster) cleanupStaleConsulReleasesAllNamespaces(t *testing.T) {
	t.Helper()

	// Build helm options with no namespace so helm list -A searches everywhere.
	koptsCopy := *c.kubectlOptions
	koptsCopy.Namespace = ""
	optsCopy := *c.helmOptions
	optsCopy.KubectlOptions = &koptsCopy

	output, err := helm.RunHelmCommandAndGetOutputE(t, &optsCopy, "list", "-A", "--output", "json")
	if err != nil {
		c.logger.Logf(t, "warning: failed to list helm releases for pre-install cleanup: %s", err)
		return
	}

	var releases []struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
		Chart     string `json:"chart"`
	}
	if err := json.Unmarshal([]byte(output), &releases); err != nil {
		c.logger.Logf(t, "warning: failed to parse helm list output for pre-install cleanup: %s", err)
		return
	}

	for _, release := range releases {
		if !strings.Contains(release.Chart, "consul") {
			continue
		}
		c.logger.Logf(t, "Removing stale consul Helm release %s in namespace %s before CLI install", release.Name, release.Namespace)
		nsKopts := *c.kubectlOptions
		nsKopts.Namespace = release.Namespace
		nsOpts := *c.helmOptions
		nsOpts.KubectlOptions = &nsKopts
		if _, delErr := helm.RunHelmCommandAndGetOutputE(t, &nsOpts,
			"uninstall", release.Name, "--no-hooks", "--timeout", "30s",
		); delErr != nil {
			c.logger.Logf(t, "warning: failed to uninstall stale release %s/%s: %s", release.Namespace, release.Name, delErr)
		}
	}
}

// isCLIOutputRetryable reports whether the CLI stdout output from a failed
// consul-k8s install indicates a transient Kubernetes API error that is safe
// to retry.  The CLI exits with status 1 for all errors, so we inspect the
// human-readable output rather than the error itself.
func isCLIOutputRetryable(output string) bool {
	outputLower := strings.ToLower(output)
	retryableSubstrings := []string{
		"tls handshake timeout",
		"connection reset by peer",
		"connection refused",
		"i/o timeout",
		"context deadline exceeded",
		"unexpected eof",
		"http2: client connection lost",
		"unable to connect to the server",
	}
	for _, s := range retryableSubstrings {
		if strings.Contains(outputLower, s) {
			return true
		}
	}
	return false
}
