package consul

import (
	"context"
	"fmt"
	"os/exec"
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
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// CLICluster
type CLICluster struct {
	ctx                environment.TestContext
	namespace          string
	helmOptions        *helm.Options
	values             map[string]string
	releaseName        string
	kubernetesClient   kubernetes.Interface
	kubeConfig         string
	kubeContext        string
	noCleanupOnFailure bool
	debugDirectory     string
	logger             terratestLogger.TestLogger
}

func NewCLICluster(
	t *testing.T,
	helmValues map[string]string,
	ctx environment.TestContext,
	cfg *config.TestConfig,
	releaseName string,
) Cluster {

	if cfg.EnablePodSecurityPolicies {
		configurePodSecurityPolicies(t, ctx.KubernetesClient(t), cfg, ctx.KubectlOptions(t).Namespace)
	}

	if cfg.EnableOpenshift && cfg.EnableTransparentProxy {
		configureSCCs(t, ctx.KubernetesClient(t), cfg, ctx.KubectlOptions(t).Namespace)
	}

	if cfg.EnterpriseLicense != "" {
		createOrUpdateLicenseSecret(t, ctx.KubernetesClient(t), cfg, ctx.KubectlOptions(t).Namespace)
	}

	// Deploy with the following defaults unless helmValues overwrites it.
	values := map[string]string{
		"server.replicas":              "1",
		"server.bootstrapExpect":       "1",
		"connectInject.envoyExtraArgs": "--log-level debug",
		"connectInject.logLevel":       "debug",
		// Disable DNS since enabling it changes the policy for the anonymous token,
		// which could result in tests passing due to that token having privileges to read services
		// (false positive).
		"dns.enabled": "false",
	}
	valuesFromConfig, err := cfg.HelmValuesFromConfig()
	require.NoError(t, err)

	// Merge all helm values
	mergeMaps(values, valuesFromConfig)
	mergeMaps(values, helmValues)

	logger := terratestLogger.New(logger.TestLogger{})

	// Wait up to 15 min for K8s resources to be in a ready state. Increasing
	// this from the default of 5 min could help with flakiness in environments
	// like AKS where volumes take a long time to mount.
	extraArgs := map[string][]string{
		"install": {"--timeout", "15m"},
	}

	opts := &helm.Options{
		SetValues:      values,
		KubectlOptions: ctx.KubectlOptions(t),
		Logger:         logger,
		ExtraArgs:      extraArgs,
	}
	opts.KubectlOptions.Namespace = "consul"
	return &CLICluster{
		ctx:                ctx,
		helmOptions:        opts,
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

func (h *CLICluster) Create(t *testing.T) {
	t.Helper()

	// Make sure we delete the cluster if we receive an interrupt signal and
	// register cleanup so that we delete the cluster when test finishes.
	helpers.Cleanup(t, h.noCleanupOnFailure, func() {
		h.Destroy(t)
	})

	// Fail if there are any existing installations of the Helm chart.
	//h.checkForPriorInstallations(t)
	args := []string{"install"}
	kubeconfig := h.kubeConfig
	if kubeconfig != "" {
		args = append(args, "-kubeconfig", kubeconfig)
	}
	kubecontext := h.kubeContext
	if kubecontext != "" {
		args = append(args, "-kubecontext", kubecontext)
	}

	for k, v := range h.values {
		args = append(args, "-set", fmt.Sprintf("%s=%s", k, v))

	}
	args = append(args, "-auto-approve")
	fmt.Println(args)
	cmd := exec.Command("consul-k8s", args...)

	require.NoError(t, cmd.Run())

	helpers.WaitForAllPodsToBeReady(t, h.kubernetesClient, "consul", fmt.Sprintf("release=%s", h.releaseName))
}

func (h *CLICluster) Destroy(t *testing.T) {
	t.Helper()

	k8s.WritePodsDebugInfoIfFailed(t, h.helmOptions.KubectlOptions, h.debugDirectory, "release="+h.releaseName)

	args := []string{"uninstall"}
	kubeconfig := h.kubeConfig
	if kubeconfig != "" {
		args = append(args, "-kubeconfig", kubeconfig)
	}
	kubecontext := h.kubeContext
	if kubecontext != "" {
		args = append(args, "-kubecontext", kubecontext)
	}

	args = append(args, "-auto-approve")
	args = append(args, "-wipe-data")
	fmt.Println(args)
	cmd := exec.Command("consul-k8s", args...)

	require.NoError(t, cmd.Run())
}

func (h *CLICluster) Upgrade(t *testing.T, helmValues map[string]string) {
	t.Helper()

	mergeMaps(h.helmOptions.SetValues, helmValues)
	helm.Upgrade(t, h.helmOptions, config.HelmChartPath, h.releaseName)
	helpers.WaitForAllPodsToBeReady(t, h.kubernetesClient, h.helmOptions.KubectlOptions.Namespace, fmt.Sprintf("release=%s", h.releaseName))
}

func (h *CLICluster) SetupConsulClient(t *testing.T, secure bool) *api.Client {
	t.Helper()

	namespace := h.helmOptions.KubectlOptions.Namespace
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

		aclSecretName := fmt.Sprintf("%s-consul-bootstrap-acl-token", h.releaseName)
		if h.releaseName == "consul" {
			aclSecretName = "consul-bootstrap-acl-token"
		}
		aclSecret, err := h.kubernetesClient.CoreV1().Secrets(namespace).Get(context.Background(), aclSecretName, metav1.GetOptions{})
		if err != nil && errors.IsNotFound(err) {
			federationSecret := fmt.Sprintf("%s-consul-federation", h.releaseName)
			if h.releaseName == "consul" {
				federationSecret = "consul-federation"
			}
			aclSecret, err = h.kubernetesClient.CoreV1().Secrets(namespace).Get(context.Background(), federationSecret, metav1.GetOptions{})
			require.NoError(t, err)
			config.Token = string(aclSecret.Data["replicationToken"])
		} else if err == nil {
			config.Token = string(aclSecret.Data["token"])
		} else {
			require.NoError(t, err)
		}
	}

	serverPod := fmt.Sprintf("%s-consul-server-0", h.releaseName)
	if h.releaseName == "consul" {
		serverPod = "consul-server-0"
	}
	tunnel := terratestk8s.NewTunnelWithLogger(
		h.helmOptions.KubectlOptions,
		terratestk8s.ResourceTypePod,
		serverPod,
		localPort,
		remotePort,
		h.logger)

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
