// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package consul

import (
	"context"
	"fmt"
	"io"
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
	"github.com/hashicorp/consul-k8s/acceptance/framework/portforward"
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

	// consulManagedByLabelSelector matches the secrets that Consul's server-acl-init
	// and tls-init jobs create at runtime (ACL tokens, CA and server certs). It
	// mirrors the managed-by=consul-k8s label the CLI uses in
	// validation.ListConsulSecrets to detect leftover secrets from a previous install.
	consulManagedByLabelSelector = "managed-by=consul-k8s"

	// openAPIValidationRetryCount and openAPIValidationRetryWait bound an in-place
	// fast retry for a transient failure while Helm downloads the API server's
	// OpenAPI schema to validate the manifest. That download happens before any
	// resources are applied, so a failure leaves no release behind and is cheap to
	// re-run immediately, unlike the up-to-15m post-apply wait handled by the outer
	// retry loop.
	openAPIValidationRetryCount = 8
	openAPIValidationRetryWait  = 15 * time.Second
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

	// On OpenShift, an install can fail either from a transient Kubernetes API
	// error (e.g. context deadline exceeded from admission webhooks) or because a
	// previous attempt left a partial Helm release behind. Wrap the install in a
	// retry loop that cleans up and retries those recoverable cases. A crashlooping
	// Consul server, by contrast, is fatal (e.g. an expired enterprise license) and
	// is surfaced immediately instead of being retried for hours.
	if c.enableOpenshift {
		// Remove any leftover Consul installation from a previous test run before
		// installing. A stale release (for example a crashlooping server left by an
		// earlier run) would otherwise trip the fail-fast check below and abort the
		// fresh install before it can start.
		c.cleanupExistingInstall(t)

		var fatalErr string
		retry.RunWith(&retry.Counter{Wait: retryWaitDuration, Count: retryMaxCount}, t, func(r *retry.R) {
			out, err := c.cli.Run(r, c.kubectlOptions, args...)

			// Helm validates the rendered manifest by downloading the API server's
			// OpenAPI schema before applying anything. On large OpenShift clusters this
			// big transfer intermittently fails ("connection reset by peer"), leaving no
			// release behind. Retry that specific, cheap failure in place a few times so
			// a brief network blip neither consumes the outer retry budget (which also
			// covers the expensive up-to-15m post-apply wait) nor triggers needless
			// cleanup. Newer consul-k8s skips this download entirely via
			// CONSUL_K8S_SKIP_OPENAPI_VALIDATION, in which case this loop never triggers.
			for attempt := 1; err != nil && isCLIOutputManifestBuildFailure(string(out)) && attempt <= openAPIValidationRetryCount; attempt++ {
				c.logger.Logf(r, "consul-k8s install failed downloading the OpenAPI schema to validate the manifest; fast retry %d/%d:\n%s",
					attempt, openAPIValidationRetryCount, string(out))
				time.Sleep(openAPIValidationRetryWait)
				out, err = c.cli.Run(r, c.kubectlOptions, args...)
			}

			if err != nil {
				c.logger.Logf(r, "error running command `consul-k8s %s`: %s", strings.Join(args, " "), err.Error())
				c.logger.Logf(r, "command stdout: %s", string(out))

				// An expired or otherwise invalid Kubernetes credential surfaces as an
				// HTTP 401 (Unauthorized). On OpenShift/ROSA the kubeconfig holds a
				// static bearer token that can lapse partway through the up-to-15m
				// install (helm's post-install wait then reports it). Retrying with the
				// same, still-expired token cannot recover and would burn minutes per
				// attempt, so fail fast with an actionable message.
				if isCLIOutputUnauthorized(string(out)) {
					fatalErr = fmt.Sprintf("the Kubernetes API rejected the request as Unauthorized (HTTP 401); "+
						"the cluster credential in kubeconfig %q (context %q) has likely expired during the install. "+
						"Re-authenticate (for example `oc login`) to refresh the token, then re-run the test.\noutput: %s",
						c.kubeConfig, c.kubeContext, string(out))
					return
				}

				// If the Consul server is crashlooping, the install cannot succeed and
				// retrying (which re-runs the 15m install plus a cleanup) only wastes
				// time. Capture the server's own error and stop retrying so the real,
				// actionable failure (e.g. an expired enterprise license) is surfaced
				// instead of a misleading "already installed" loop.
				if serverErr := c.serverStartupFailure(); serverErr != "" {
					fatalErr = serverErr
					return
				}

				// A transient Kubernetes API error, or leftover state from a previous
				// failed attempt in this retry loop, are all recovered by cleaning up any
				// existing release and retrying. The install pre-check lists releases in
				// every state (including failed and pending-install), so a partially-
				// installed release from an earlier attempt surfaces as an "already
				// installed" error, and runtime-created Consul secrets (ACL tokens, CA and
				// server certs) that outlive `helm uninstall` surface as a "Found Consul
				// secrets" error, on the next attempt. cleanupExistingInstall removes both.
				if isCLIOutputRetryable(string(out)) || isCLIOutputAlreadyInstalled(string(out)) || isCLIOutputConsulSecretsExist(string(out)) {
					c.cleanupExistingInstall(t)
					r.Errorf("retrying consul-k8s install after recoverable error: %v\noutput: %s", err, string(out))
					return
				}
			}
			require.NoError(r, err)
		})
		require.Emptyf(t, fatalErr,
			"consul-k8s install failed and will not recover by retrying:\n%s", fatalErr)
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

	// Use a monitored port-forward tunnel (as HelmCluster.SetupConsulClient does)
	// so the Consul API connection self-heals if the port-forward drops. On remote
	// clusters (e.g. OpenShift/ROSA) an unmonitored tunnel can silently die during a
	// long-running test step, after which every Consul API call (such as creating an
	// intention) fails with "connection refused". CreateTunnelToResourcePort spawns a
	// monitor goroutine that rebuilds the tunnel on the same local port when needed.
	config.Address = portforward.CreateTunnelToResourcePort(t, serverPod, remotePort, c.kubectlOptions, c.logger)
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

// isCLIOutputManifestBuildFailure reports whether a failed consul-k8s install
// failed while Helm was building/validating the release manifest, before any
// resources were applied. Building the manifest downloads the API server's
// OpenAPI schema, a large transfer on OpenShift that can fail transiently (for
// example "connection reset by peer"). Because no release is created, this is
// cheap to re-run in place.
func isCLIOutputManifestBuildFailure(output string) bool {
	outputLower := strings.ToLower(output)
	return strings.Contains(outputLower, "unable to build kubernetes objects from release manifest") ||
		strings.Contains(outputLower, "failed to download openapi")
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

// isCLIOutputAlreadyInstalled reports whether the CLI stdout output from a
// failed consul-k8s install indicates that a Consul release is already present
// in the cluster. In the OpenShift install retry loop this typically means an
// earlier attempt created the Helm release but then failed with a transient
// error, leaving a partial or failed release behind. That leftover release must
// be uninstalled before the install can be retried, otherwise every subsequent
// attempt fast-fails with the same "already installed" pre-check error.
func isCLIOutputAlreadyInstalled(output string) bool {
	return strings.Contains(strings.ToLower(output), "is already installed")
}

// isCLIOutputConsulSecretsExist reports whether the CLI stdout output from a
// failed consul-k8s install indicates the install pre-check found leftover Consul
// secrets from a previous installation. Consul's server-acl-init and tls-init jobs
// create these secrets (ACL tokens, CA and server certs) at runtime, and they are
// not removed by `helm uninstall`, so a previous failed secure install leaves them
// behind and blocks the next install. cleanupExistingInstall deletes them, so this
// is recoverable by cleaning up and retrying.
func isCLIOutputConsulSecretsExist(output string) bool {
	return strings.Contains(strings.ToLower(output), "found consul secrets")
}

// isCLIOutputUnauthorized reports whether the CLI stdout output from a failed
// consul-k8s install indicates the Kubernetes API rejected the request with an
// authentication error (HTTP 401). On OpenShift/ROSA this typically means the
// static bearer token in the kubeconfig expired during the install. The framework
// has no credentials to refresh that token, so this condition is treated as fatal
// (surfaced with re-authentication guidance) rather than retried.
func isCLIOutputUnauthorized(output string) bool {
	outputLower := strings.ToLower(output)
	return strings.Contains(outputLower, "unauthorized") ||
		strings.Contains(outputLower, "status code received: 401") ||
		strings.Contains(outputLower, "must be logged in to the server")
}

// serverStartupFailure returns a non-empty message when the Consul server pod
// has hit a fatal, non-transient startup problem that retrying the install will
// not fix, so the caller should fail fast and surface the server's own logs.
// Two cases are treated as fatal:
//   - a crashlooping server container (repeated restarts / CrashLoopBackOff), and
//   - a server that has started but rejects its enterprise license (for example
//     an expired license), which can leave the container running-but-never-ready
//     rather than crashlooping.
//
// It returns an empty string when the server is healthy, merely slow to become
// ready, or cannot be inspected, leaving those cases to the retry loop.
func (c *CLICluster) serverStartupFailure() string {
	ctx := context.Background()
	pods, err := c.kubernetesClient.CoreV1().Pods(consulNS).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("release=%s,component=server", c.releaseName),
	})
	if err != nil {
		return ""
	}
	for _, pod := range pods.Items {
		for _, cs := range pod.Status.ContainerStatuses {
			if cs.Name != "consul" {
				continue
			}

			crashLooping := cs.RestartCount >= 3
			if w := cs.State.Waiting; w != nil && w.Reason == "CrashLoopBackOff" {
				crashLooping = true
			}

			// A ready server is not the cause of the install failure; only inspect
			// logs when the server has not become ready or is already crashlooping.
			if cs.Ready && !crashLooping {
				continue
			}

			// Prefer the previous (crashed) container instance's logs; fall back to
			// the current instance if the previous logs are unavailable.
			logs := c.serverContainerLogs(ctx, pod.Name, true)
			if strings.TrimSpace(logs) == "" {
				logs = c.serverContainerLogs(ctx, pod.Name, false)
			}

			// An expired or invalid enterprise license is a common, fatal cause: the
			// server rejects the license and never becomes ready (it may crashloop or
			// simply stay unready), so retrying cannot help. Detect it from the
			// server's own logs regardless of restart count and surface an actionable
			// message.
			if msg := licenseFailureMessage(logs); msg != "" {
				return fmt.Sprintf("consul server pod %q failed to start: %s\nlogs:\n%s",
					pod.Name, msg, strings.TrimSpace(logs))
			}

			// Otherwise only a crashlooping server is treated as fatal; a server that
			// is merely slow to become ready is left to the retry loop.
			if crashLooping {
				return fmt.Sprintf("consul server pod %q is crashlooping (restartCount=%d); logs:\n%s",
					pod.Name, cs.RestartCount, strings.TrimSpace(logs))
			}
		}
	}
	return ""
}

// serverContainerLogs returns up to the last 30 lines of the consul container
// logs for the given server pod. When previous is true it reads the logs of the
// last terminated (crashed) container instance.
func (c *CLICluster) serverContainerLogs(ctx context.Context, podName string, previous bool) string {
	tail := int64(30)
	req := c.kubernetesClient.CoreV1().Pods(consulNS).GetLogs(podName, &v1.PodLogOptions{
		Container: "consul",
		Previous:  previous,
		TailLines: &tail,
	})
	stream, err := req.Stream(ctx)
	if err != nil {
		return ""
	}
	defer stream.Close()
	data, err := io.ReadAll(stream)
	if err != nil {
		return ""
	}
	return string(data)
}

// licenseFailureMessage inspects Consul server logs for evidence that the
// enterprise license is expired or otherwise invalid. Consul rejects such a
// license at startup and the server never becomes healthy, so this is a fatal
// condition that retrying the install cannot fix. The matched phrases are
// error-specific (they do not appear in the benign license-expiration line that
// a healthy server logs), so a valid installation is not misdetected. It returns
// an actionable message when a license failure is detected, or an empty string
// otherwise.
func licenseFailureMessage(logs string) string {
	l := strings.ToLower(logs)
	if !strings.Contains(l, "license") {
		return ""
	}
	licenseErrorMarkers := []string{
		"no longer valid",
		"license expired",
		"license is expired",
		"license has expired",
		"expiration date has passed",
		"termination time",
		"invalid license",
		"license is invalid",
		"failed to initialize license",
		"failed to set up enterprise",
	}
	for _, m := range licenseErrorMarkers {
		if strings.Contains(l, m) {
			return fmt.Sprintf("the Consul enterprise license appears to be expired or invalid; "+
				"update the license the test uses (the CONSUL_ENT_LICENSE environment variable, "+
				"which is written to the %q Kubernetes secret) to a currently-valid license and re-run",
				config.LicenseSecretName)
		}
	}
	return ""
}

// cleanupExistingInstall removes any existing Consul release and its leftover
// resources so a fresh install can start from a clean slate. It runs both before
// the initial OpenShift install (to clear a stale installation from a previous
// test run) and between install retries (to clear a partial release left by a
// failed attempt).
//
// It uses `helm uninstall --no-hooks` with a short timeout so that a broken
// release — whose pre-delete hooks would otherwise block on an unhealthy Consul —
// cannot hang cleanup for a long time. `helm uninstall` removes the Helm-managed
// resources (workloads, CRDs, webhooks, RBAC); this function additionally
// force-deletes any lingering release pods (for example a crashlooping server)
// and deletes the server PVCs, which Helm does not manage. Errors are best-effort
// and ignored; the enterprise license Secret is created outside Helm and is left
// untouched.
func (c *CLICluster) cleanupExistingInstall(t *testing.T) {
	if _, err := helm.RunHelmCommandAndGetOutputE(t, c.helmOptions,
		"uninstall", c.releaseName, "--no-hooks", "--timeout", "60s"); err != nil {
		c.logger.Logf(t, "helm uninstall --no-hooks during cleanup failed (ignoring): %s", err.Error())
	}

	releaseSelector := metav1.ListOptions{LabelSelector: fmt.Sprintf("release=%s", c.releaseName)}

	// Force-delete any lingering pods (for example a crashlooping server that the
	// StatefulSet deletion has not finished terminating) so the install does not
	// observe them.
	gracePeriod := int64(0)
	if err := c.kubernetesClient.CoreV1().Pods(consulNS).DeleteCollection(
		context.Background(), metav1.DeleteOptions{GracePeriodSeconds: &gracePeriod}, releaseSelector); err != nil {
		c.logger.Logf(t, "force-deleting leftover pods during cleanup failed (ignoring): %s", err.Error())
	}

	// Helm does not manage the server StatefulSet's volumeClaimTemplate PVCs, so
	// delete them explicitly to mirror `consul-k8s uninstall -wipe-data`.
	if err := c.kubernetesClient.CoreV1().PersistentVolumeClaims(consulNS).DeleteCollection(
		context.Background(), metav1.DeleteOptions{}, releaseSelector); err != nil {
		c.logger.Logf(t, "deleting PVCs during cleanup failed (ignoring): %s", err.Error())
	}

	// Consul's server-acl-init and tls-init jobs create secrets (ACL tokens, CA
	// and server certs) at runtime, labeled managed-by=consul-k8s. These are not
	// part of the Helm release, so `helm uninstall` leaves them behind and the next
	// install's pre-check ("Found Consul secrets, possibly from a previous
	// installation") refuses to proceed. Delete them using the same label the CLI
	// matches in validation.ListConsulSecrets. The test's own license secret carries
	// no labels, so it is not affected.
	consulSecretSelector := metav1.ListOptions{LabelSelector: consulManagedByLabelSelector}
	if err := c.kubernetesClient.CoreV1().Secrets(consulNS).DeleteCollection(
		context.Background(), metav1.DeleteOptions{}, consulSecretSelector); err != nil {
		c.logger.Logf(t, "deleting Consul-generated secrets during cleanup failed (ignoring): %s", err.Error())
	}
}
