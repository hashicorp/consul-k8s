package vault

import (
	"context"
	"fmt"
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
	"github.com/hashicorp/consul/sdk/testutil/retry"
	vapi "github.com/hashicorp/vault/api"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

/*
	// High level description of the functions implemented for the VaultCluster object:

	// Create will install a vault cluster via helm using the default config defined at the end of this file. It will
	// then also call bootstrap() to setup the vault cluster for testing.
	Create(t *testing.T, ctx environment.TestContext)

	// bootstrap will init and unseal the Vault cluster and enable the KV2 secret
	// engine and the Kube Auth Method.
	bootstrap(t *testing.T, ctx environment.TestContext)

	// Destroy will do a helm uninstall of the Vault installation and then delete the data PVC used by Vault and the
	// helm secrets.
	Destroy(t *testing.T)

	// SetupVaultClient will setup the port-forwarding to the Vault server so that we can create a vault client connection.
	// This is run as part of Bootstrap.
	SetupVaultClient(t *testing.T) *vapi.Client

	// VaultClient returns the client that was built as part of SetupVaultClient.
	VaultClient(t *testing.T) *vapi.Client
*/

const (
	vaultNS = "default"
)

// VaultCluster
type VaultCluster struct {
	ctx       environment.TestContext
	namespace string

	vaultHelmOptions *helm.Options
	vaultReleaseName string
	vaultClient      *vapi.Client
	rootToken        string

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
	}

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

// bootstrap initializes and unseals the Vault installation.
// It then sets up Kubernetes auth method and enables secrets engines.
func (v *VaultCluster) bootstrap(t *testing.T, ctx environment.TestContext) {

	v.vaultClient = v.SetupVaultClient(t)

	// Init the Vault Cluster and store the rootToken.
	initResp, err := v.vaultClient.Sys().Init(&vapi.InitRequest{
		// Init the cluster and only request a single Secret to be used for Unsealing.
		SecretShares:      1,
		SecretThreshold:   1,
		StoredShares:      1,
		RecoveryShares:    0,
		RecoveryThreshold: 0,
	})
	if err != nil {
		t.Fatal("unable to init Vault cluster", "err", err)
	}
	// Store the RootToken and set the client to use it so it can Unseal and finish bootstrapping.
	v.rootToken = initResp.RootToken
	v.vaultClient.SetToken(v.rootToken)

	// Unseal the Vault Cluster using the Unseal Keys from Init().
	sealResp, err := v.vaultClient.Sys().Unseal(initResp.KeysB64[0])
	if err != nil {
		t.Fatal("unable to unseal Vault cluster", "err", err)
	}
	require.Equal(t, false, sealResp.Sealed)

	// Enable the KV-V2 Secrets engine
	err = v.vaultClient.Sys().Mount("consul", &vapi.MountInput{
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
	cmdString := fmt.Sprintf("VAULT_TOKEN=%s vault write auth/kubernetes/config token_reviewer_jwt=\"$(cat /var/run/secrets/kubernetes.io/serviceaccount/token)\" kubernetes_host=\"https://${KUBERNETES_PORT_443_TCP_ADDR}:443\" kubernetes_ca_cert=@/var/run/secrets/kubernetes.io/serviceaccount/ca.crt", v.rootToken)

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
	helpers.CheckForPriorInstallations(t, v.kubernetesClient, v.vaultHelmOptions, "vault")

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

// Destroy issues an helm delete and deletes the PVC + any helm secrets related to the release that are leftover.
func (v *VaultCluster) Destroy(t *testing.T) {
	t.Helper()

	k8s.WritePodsDebugInfoIfFailed(t, v.kubectlOptions, v.debugDirectory, "release="+v.vaultReleaseName)

	// Ignore the error returned by the helm delete here so that we can
	// always idempotently clean up resources in the cluster.
	_ = helm.DeleteE(t, v.vaultHelmOptions, v.vaultReleaseName, false)

	// Delete PVCs, these are the only parts that need to be cleaned up in Vault installs.
	err := v.kubernetesClient.CoreV1().PersistentVolumeClaims(v.vaultHelmOptions.KubectlOptions.Namespace).DeleteCollection(context.Background(), metav1.DeleteOptions{}, metav1.ListOptions{LabelSelector: fmt.Sprintf("app.kubernetes.io/instance=%s", v.vaultReleaseName)})
	require.NoError(t, err)

	// Delete any secrets that have v.releaseName in their name, this is only needed to delete the Helm release secret if it is still around.
	secrets, err := v.kubernetesClient.CoreV1().Secrets(v.vaultHelmOptions.KubectlOptions.Namespace).List(context.Background(), metav1.ListOptions{})
	require.NoError(t, err)
	for _, secret := range secrets.Items {
		if strings.Contains(secret.Name, v.vaultReleaseName) {
			err := v.kubernetesClient.CoreV1().Secrets(v.vaultHelmOptions.KubectlOptions.Namespace).Delete(context.Background(), secret.Name, metav1.DeleteOptions{})
			if !errors.IsNotFound(err) {
				require.NoError(t, err)
			}
		}
	}
}

func defaultVaultValues() map[string]string {
	return map[string]string{
		"server.replicas":        "1",
		"server.bootstrapExpect": "1",
		"injector.enabled": "true",
		"global.enabled":   "true",
	}
}
