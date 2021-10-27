package vault

import (
	"context"
	"fmt"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"
	"time"

	"github.com/gruntwork-io/terratest/modules/helm"
	terratestk8s "github.com/gruntwork-io/terratest/modules/k8s"
	terratestLogger "github.com/gruntwork-io/terratest/modules/logger"
	"github.com/hashicorp/consul-k8s/acceptance/framework/config"
	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	vapi "github.com/hashicorp/vault/api"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes"
)

const (
	vaultNS           = "default"
	vaultChartVersion = "0.17.0"
	vaultRootToken    = "abcd1234"
)

// VaultCluster
type VaultCluster struct {
	ctx       environment.TestContext
	namespace string

	vaultHelmOptions *helm.Options
	vaultReleaseName string
	vaultChartName   string
	vaultClient      *vapi.Client

	kubectlOptions *terratestk8s.KubectlOptions
	values         map[string]string

	kubernetesClient kubernetes.Interface
	kubeConfig       string
	kubeContext      string

	noCleanupOnFailure bool
	debugDirectory     string
	logger             terratestLogger.TestLogger
}

// NewVaultCluster creates a VaultCluster which will be used to install Vault using Helm.
func NewVaultCluster(
	t *testing.T,
	helmValues map[string]string,
	ctx environment.TestContext,
	cfg *config.TestConfig,
	releaseName string,
) *VaultCluster {

	logger := terratestLogger.New(logger.TestLogger{})

	kopts := ctx.KubectlOptions(t)
	kopts.Namespace = vaultNS

	vaultHelmOpts := &helm.Options{
		SetValues:      defaultVaultValues(),
		KubectlOptions: kopts,
		Logger:         logger,
		Version:        vaultChartVersion,
	}
	// Add the vault helm repo in case it is missing, and do an update so we can utilise `vaultChartVersion` to install.
	helm.AddRepo(t, &helm.Options{}, "hashicorp/vault", "https://helm.releases.hashicorp.com")
	helm.RunHelmCommandAndGetOutputE(t, &helm.Options{}, "repo", "update")

	return &VaultCluster{
		ctx:                ctx,
		vaultHelmOptions:   vaultHelmOpts,
		kubectlOptions:     kopts,
		namespace:          cfg.KubeNamespace,
		values:             helmValues,
		kubernetesClient:   ctx.KubernetesClient(t),
		kubeConfig:         cfg.Kubeconfig,
		kubeContext:        cfg.KubeContext,
		noCleanupOnFailure: cfg.NoCleanupOnFailure,
		debugDirectory:     cfg.DebugDirectory,
		logger:             logger,
		vaultReleaseName:   releaseName,
		vaultChartName:     fmt.Sprintf("vault-%s", vaultChartVersion),
	}
}

// VaultClient returns the vault client.
func (v *VaultCluster) VaultClient(t *testing.T) *vapi.Client { return v.vaultClient }

// Setup Vault Client returns a Vault Client.
// TODO: We need to support Vault with TLS enabled.
func (v *VaultCluster) SetupVaultClient(t *testing.T) *vapi.Client {
	t.Helper()

	config := vapi.DefaultConfig()
	localPort := terratestk8s.GetAvailablePort(t)
	remotePort := 8200 // use non-secure by default

	serverPod := fmt.Sprintf("%s-vault-0", v.vaultReleaseName)
	tunnel := terratestk8s.NewTunnelWithLogger(
		v.vaultHelmOptions.KubectlOptions,
		terratestk8s.ResourceTypePod,
		serverPod,
		localPort,
		remotePort,
		v.logger)

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

	config.Address = fmt.Sprintf("http://127.0.0.1:%d", localPort)
	vaultClient, err := vapi.NewClient(config)
	require.NoError(t, err)
	return vaultClient
}

// bootstrap sets up Kubernetes auth method and enables secrets engines.
func (v *VaultCluster) bootstrap(t *testing.T, ctx environment.TestContext) {

	v.vaultClient = v.SetupVaultClient(t)
	v.vaultClient.SetToken(vaultRootToken)

	// Enable the KV-V2 Secrets engine.
	err := v.vaultClient.Sys().Mount("consul", &vapi.MountInput{
		Type:   "kv-v2",
		Config: vapi.MountConfigInput{},
	})
	if err != nil {
		t.Fatal("unable to mount kv-v2 secrets engine", "err", err)
	}
	// TODO: add the PKI Secrets Engine when we have a need for it.

	// Enable Kube Auth.
	err = v.vaultClient.Sys().EnableAuthWithOptions("kubernetes", &vapi.EnableAuthOptions{
		Type:   "kubernetes",
		Config: vapi.MountConfigInput{},
	})
	if err != nil {
		t.Fatal("unable to enable kube auth", "err", err)
	}
	// We need to kubectl exec this one on the vault server:
	// This is taken from https://learn.hashicorp.com/tutorials/vault/kubernetes-google-cloud-gke?in=vault/kubernetes#configure-kubernetes-authentication
	cmdString := fmt.Sprintf("VAULT_TOKEN=%s vault write auth/kubernetes/config token_reviewer_jwt=\"$(cat /var/run/secrets/kubernetes.io/serviceaccount/token)\" kubernetes_host=\"https://${KUBERNETES_PORT_443_TCP_ADDR}:443\" kubernetes_ca_cert=@/var/run/secrets/kubernetes.io/serviceaccount/ca.crt", vaultRootToken)

	v.logger.Logf(t, "updating vault kube auth config")
	k8s.RunKubectl(t, ctx.KubectlOptions(t), "exec", "-i", fmt.Sprintf("%s-vault-0", v.vaultReleaseName), "--", "sh", "-c", cmdString)
}

// Create installs Vault via Helm and then calls bootstrap to initialize it.
func (v *VaultCluster) Create(t *testing.T, ctx environment.TestContext) {
	t.Helper()

	// Make sure we delete the cluster if we receive an interrupt signal and
	// register cleanup so that we delete the cluster when test finishes.
	helpers.Cleanup(t, v.noCleanupOnFailure, func() {
		v.Destroy(t)
	})

	// Fail if there are any existing installations of the Helm chart.
	helpers.CheckForPriorInstallations(t, v.kubernetesClient, v.vaultHelmOptions, v.vaultChartName)

	// Install Vault.
	helm.Install(t, v.vaultHelmOptions, "hashicorp/vault", v.vaultReleaseName)
	// Wait for the injector pod to become Ready, but not the server.
	helpers.WaitForAllPodsToBeReady(t, v.kubernetesClient, v.vaultHelmOptions.KubectlOptions.Namespace, "app.kubernetes.io/name=vault-agent-injector")
	// Wait for the server pod to be PodRunning, it will not be Ready because it has not been Init+Unseal'd yet.
	// The vault server has health checks bound to unseal status, and Unseal is done as part of bootstrap (below).
	retry.RunWith(&retry.Counter{Wait: 1 * time.Second, Count: 30}, t, func(r *retry.R) {
		pod, err := v.kubernetesClient.CoreV1().Pods(v.vaultHelmOptions.KubectlOptions.Namespace).Get(context.Background(), fmt.Sprintf("%s-vault-0", v.vaultReleaseName), metav1.GetOptions{})
		require.NoError(r, err)
		require.Equal(r, pod.Status.Phase, corev1.PodRunning)
	})
	// Now call bootstrap()
	v.bootstrap(t, ctx)
}

// Destroy issues a helm delete and deletes the PVC + any helm secrets related to the release that are leftover.
func (v *VaultCluster) Destroy(t *testing.T) {
	t.Helper()

	k8s.WritePodsDebugInfoIfFailed(t, v.kubectlOptions, v.debugDirectory, "release="+v.vaultReleaseName)
	// Ignore the error returned by the helm delete here so that we can
	// always idempotently clean up resources in the cluster.
	_ = helm.DeleteE(t, v.vaultHelmOptions, v.vaultReleaseName, true)
	// We do not need to do any PVC deletion in vault dev mode.
}

func defaultVaultValues() map[string]string {
	return map[string]string{
		"server.replicas":         "1",
		"server.dev.enabled":      "true",
		"server.dev.devRootToken": vaultRootToken,
		"server.bootstrapExpect":  "1",
		"injector.enabled":        "true",
		"global.enabled":          "true",
	}
}
