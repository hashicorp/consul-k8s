package consul

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/gruntwork-io/terratest/modules/helm"
	terratestk8s "github.com/gruntwork-io/terratest/modules/k8s"
	terratestLogger "github.com/gruntwork-io/terratest/modules/logger"
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
	CLIReleaseName = "consul"
)

var cliDefaultArgs = []string{"-timeout", "15m"}

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
	debugDirectory     string
	logger             terratestLogger.TestLogger
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
	// Use "consul" as the default namespace, same as the CLI.
	if cfg.KubeNamespace == "" {
		cfg.KubeNamespace = "consul"
	}

	// Create the namespace so the PSPs, SCCs, and enterprise secret can be
	// created in the right namespace.
	createOrUpdateNamespace(t, ctx.KubernetesClient(t), cfg.KubeNamespace)

	if cfg.EnablePodSecurityPolicies {
		configurePodSecurityPolicies(t, ctx.KubernetesClient(t), cfg, cfg.KubeNamespace)
	}

	if cfg.EnableOpenshift && cfg.EnableTransparentProxy {
		configureSCCs(t, ctx.KubernetesClient(t), cfg, cfg.KubeNamespace)
	}

	if cfg.EnterpriseLicense != "" {
		createOrUpdateLicenseSecret(t, ctx.KubernetesClient(t), cfg, cfg.KubeNamespace)
	}

	// Deploy with the following defaults unless helmValues overwrites it.
	values := defaultValues()
	valuesFromConfig, err := cfg.HelmValuesFromConfig()
	require.NoError(t, err)

	// Merge all helm values
	helpers.MergeMaps(values, valuesFromConfig)
	helpers.MergeMaps(values, helmValues)

	logger := terratestLogger.New(logger.TestLogger{})

	kopts := ctx.KubectlOptions(t)
	kopts.Namespace = cfg.KubeNamespace
	hopts := &helm.Options{
		SetValues:      values,
		KubectlOptions: kopts,
		Logger:         logger,
	}

	return &CLICluster{
		ctx:                ctx,
		helmOptions:        hopts,
		kubectlOptions:     kopts,
		namespace:          cfg.KubeNamespace,
		values:             values,
		releaseName:        releaseName,
		kubernetesClient:   ctx.KubernetesClient(t),
		kubeConfig:         cfg.Kubeconfig,
		kubeContext:        cfg.KubeContext,
		noCleanupOnFailure: cfg.NoCleanupOnFailure,
		debugDirectory:     cfg.DebugDirectory,
		logger:             logger,
	}
}

// Create uses the `consul-k8s install` command to create a Consul cluster. The command itself will fail if there are
// prior installations of Consul in the cluster so it is sufficient to run the install command without a pre-check.
func (c *CLICluster) Create(t *testing.T, args ...string) {
	t.Helper()

	// Make sure we delete the cluster if we receive an interrupt signal and
	// register cleanup so that we delete the cluster when test finishes.
	helpers.Cleanup(t, c.noCleanupOnFailure, func() {
		c.Destroy(t)
	})

	// Set the args for running the install command.
	if len(args) == 0 {
		args = cliDefaultArgs
	}
	args = append([]string{"install"}, args...)
	args = c.setKube(args)
	for k, v := range c.values {
		args = append(args, "-set", fmt.Sprintf("%s=%s", k, v))
	}
	args = append(args, "-auto-approve")
	args = append(args, "-namespace", c.namespace)

	out, err := c.runCLI(args)
	if err != nil {
		c.logger.Logf(t, "error running command `consul-k8s %s`: %s", strings.Join(args, " "), err.Error())
		c.logger.Logf(t, "command stdout: %s", string(out))
	}
	require.NoError(t, err)

	k8s.WaitForAllPodsToBeReady(t, c.kubernetesClient, c.namespace, fmt.Sprintf("release=%s", c.releaseName))
}

// Upgrade uses the `consul-k8s upgrade` command to upgrade a Consul cluster.
func (c *CLICluster) Upgrade(t *testing.T, helmValues map[string]string, args ...string) {
	t.Helper()

	k8s.WritePodsDebugInfoIfFailed(t, c.kubectlOptions, c.debugDirectory, "release="+c.releaseName)
	if t.Failed() {
		c.logger.Logf(t, "skipping upgrade due to previous failure")
		return
	}

	// Set the args for running the upgrade command.
	if len(args) == 0 {
		args = cliDefaultArgs
	}
	args = append([]string{"upgrade"}, args...)
	args = c.setKube(args)
	helpers.MergeMaps(c.helmOptions.SetValues, helmValues)
	for k, v := range c.helmOptions.SetValues {
		args = append(args, "-set", fmt.Sprintf("%s=%s", k, v))
	}
	args = append(args, "-auto-approve")

	out, err := c.runCLI(args)
	if err != nil {
		c.logger.Logf(t, "error running command `consul-k8s %s`: %s", strings.Join(args, " "), err.Error())
		c.logger.Logf(t, "command stdout: %s", string(out))
	}
	require.NoError(t, err)

	k8s.WaitForAllPodsToBeReady(t, c.kubernetesClient, c.namespace, fmt.Sprintf("release=%s", c.releaseName))
}

// Destroy uses the `consul-k8s uninstall` command to destroy a Consul cluster.
func (c *CLICluster) Destroy(t *testing.T, args ...string) {
	t.Helper()

	k8s.WritePodsDebugInfoIfFailed(t, c.kubectlOptions, c.debugDirectory, "release="+c.releaseName)

	// Set the args for running the uninstall command.
	if len(args) == 0 {
		args = cliDefaultArgs
	}
	args = append([]string{"uninstall"}, args...)
	args = c.setKube(args)
	args = append(args, "-auto-approve", "-wipe-data")

	// Use `go run` so that the CLI is recompiled and therefore uses the local
	// charts directory rather than the directory from whenever it was last
	// compiled.
	out, err := c.runCLI(args)
	if err != nil {
		c.logger.Logf(t, "error running command `consul-k8s %s`: %s", strings.Join(args, " "), err.Error())
		c.logger.Logf(t, "command stdout: %s", string(out))
	}
	require.NoError(t, err)
}

func (c *CLICluster) SetupConsulClient(t *testing.T, secure bool) *api.Client {
	t.Helper()

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

		aclSecretName := fmt.Sprintf("%s-consul-bootstrap-acl-token", c.releaseName)
		if c.releaseName == CLIReleaseName {
			aclSecretName = "consul-bootstrap-acl-token"
		}
		aclSecret, err := c.kubernetesClient.CoreV1().Secrets(namespace).Get(context.Background(), aclSecretName, metav1.GetOptions{})
		if err != nil && errors.IsNotFound(err) {
			federationSecret := fmt.Sprintf("%s-consul-federation", c.releaseName)
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

	serverPod := fmt.Sprintf("%s-consul-server-0", c.releaseName)
	if c.releaseName == CLIReleaseName {
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
	retry.RunWith(&retry.Counter{Wait: 1 * time.Second, Count: 3}, t, func(r *retry.R) {
		// NOTE: It's okay to pass in `t` to ForwardPortE despite being in a retry
		// because we're using ForwardPortE (not ForwardPort) so the `t` won't
		// get used to fail the test, just for logging.
		require.NoError(r, tunnel.ForwardPortE(t))
	})

	t.Cleanup(func() {
		tunnel.Close()
	})

	config.Address = fmt.Sprintf("127.0.0.1:%d", localPort)
	consulClient, err := api.NewClient(config)
	require.NoError(t, err)

	return consulClient
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

// runCLI runs the CLI with the given args.
// Use `go run` so that the CLI is recompiled and therefore uses the local
// charts directory rather than the directory from whenever it was last compiled.
func (c *CLICluster) runCLI(args []string) ([]byte, error) {
	cmd := exec.Command("go", append([]string{"run", "."}, args...)...)
	cmd.Dir = config.CLIPath
	return cmd.Output()
}
