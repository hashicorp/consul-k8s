package vault

import (
	"context"
	"fmt"
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
	"github.com/hashicorp/consul-k8s/control-plane/helper/cert"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	vapi "github.com/hashicorp/vault/api"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	vaultPodLabel = "app.kubernetes.io/instance="
)

// VaultCluster represents a vault installation.
type VaultCluster struct {
	ctx environment.TestContext

	helmOptions *helm.Options
	releaseName string
	vaultClient *vapi.Client

	kubectlOptions *terratestk8s.KubectlOptions

	kubernetesClient kubernetes.Interface

	noCleanupOnFailure bool
	debugDirectory     string
	logger             terratestLogger.TestLogger
}

// NewVaultCluster creates a VaultCluster which will be used to install Vault using Helm.
func NewVaultCluster(t *testing.T, ctx environment.TestContext, cfg *config.TestConfig, releaseName string) *VaultCluster {

	logger := terratestLogger.New(logger.TestLogger{})

	kopts := ctx.KubectlOptions(t)

	vaultHelmOpts := &helm.Options{
		SetValues:      defaultVaultValues(releaseName),
		KubectlOptions: kopts,
		Logger:         logger,
	}
	helm.AddRepo(t, vaultHelmOpts, "hashicorp", "https://helm.releases.hashicorp.com")
	// Ignoring the error from `helm repo update` as it could fail due to stale cache or unreachable servers and we're
	// asserting a chart version on Install which would fail in an obvious way should this not succeed.
	_, err := helm.RunHelmCommandAndGetOutputE(t, &helm.Options{}, "repo", "update")
	if err != nil {
		logger.Logf(t, "Unable to update helm repository, proceeding anyway: %s.", err)
	}

	return &VaultCluster{
		ctx:                ctx,
		helmOptions:        vaultHelmOpts,
		kubectlOptions:     kopts,
		kubernetesClient:   ctx.KubernetesClient(t),
		noCleanupOnFailure: cfg.NoCleanupOnFailure,
		debugDirectory:     cfg.DebugDirectory,
		logger:             logger,
		releaseName:        releaseName,
	}
}

// VaultClient returns the vault client.
func (v *VaultCluster) VaultClient(t *testing.T) *vapi.Client { return v.vaultClient }

// SetupVaultClient sets up and returns a Vault Client.
func (v *VaultCluster) SetupVaultClient(t *testing.T) *vapi.Client {
	t.Helper()

	if v.vaultClient != nil {
		return v.vaultClient
	}

	config := vapi.DefaultConfig()
	localPort := terratestk8s.GetAvailablePort(t)
	remotePort := 8200 // use non-secure by default

	serverPod := fmt.Sprintf("%s-vault-0", v.releaseName)
	tunnel := terratestk8s.NewTunnelWithLogger(
		v.helmOptions.KubectlOptions,
		terratestk8s.ResourceTypePod,
		serverPod,
		localPort,
		remotePort,
		v.logger)

	// Retry creating the port forward since it can fail occasionally.
	retry.RunWith(&retry.Counter{Wait: 1 * time.Second, Count: 60}, t, func(r *retry.R) {
		// NOTE: It's okay to pass in `t` to ForwardPortE despite being in a retry
		// because we're using ForwardPortE (not ForwardPort) so the `t` won't
		// get used to fail the test, just for logging.
		require.NoError(r, tunnel.ForwardPortE(t))
	})

	t.Cleanup(func() {
		tunnel.Close()
	})

	config.Address = fmt.Sprintf("https://127.0.0.1:%d", localPort)
	err := config.ConfigureTLS(&vapi.TLSConfig{Insecure: true})
	require.NoError(t, err)
	vaultClient, err := vapi.NewClient(config)
	require.NoError(t, err)
	return vaultClient
}

// bootstrap sets up Kubernetes auth method and enables secrets engines.
func (v *VaultCluster) bootstrap(t *testing.T, ctx environment.TestContext) {

	v.vaultClient = v.SetupVaultClient(t)

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
		Type: "kubernetes",
	})
	if err != nil {
		t.Fatal("unable to enable kube auth", "err", err)
	}

	v.logger.Logf(t, "updating vault kube auth config")
	namespace := v.helmOptions.KubectlOptions.Namespace
	sa, err := v.kubernetesClient.CoreV1().ServiceAccounts(namespace).Get(context.Background(), fmt.Sprintf("%s-vault", v.releaseName), metav1.GetOptions{})
	require.NoError(t, err)
	require.Len(t, sa.Secrets, 1)
	tokenSecret, err := v.kubernetesClient.CoreV1().Secrets(namespace).Get(context.Background(), sa.Secrets[0].Name, metav1.GetOptions{})
	require.NoError(t, err)
	v.vaultClient.Logical().Write("auth/kubernetes/config", map[string]interface{}{
		"token_reviewer_jwt": tokenSecret.StringData["token"],
		"kubernetes_ca_cert": tokenSecret.StringData["ca.crt"],
		"kubernetes_host":    "https://kubernetes.default.svc",
	})
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
	helpers.CheckForPriorInstallations(t, v.kubernetesClient, v.helmOptions, "", fmt.Sprintf("%s=%s", vaultPodLabel, v.releaseName))

	v.createTLSCerts(t)

	// Install Vault.
	helm.Install(t, v.helmOptions, "hashicorp/vault", v.releaseName)

	// Wait for the injector and vault server pods to become Ready.
	helpers.WaitForAllPodsToBeReady(t, v.kubernetesClient, v.helmOptions.KubectlOptions.Namespace, "app.kubernetes.io/name=vault-agent-injector")

	v.initAndUnseal(t)

	// Now call bootstrap().
	v.bootstrap(t, ctx)
}

// Destroy issues a helm delete and deletes the PVC + any helm secrets related to the release that are leftover.
func (v *VaultCluster) Destroy(t *testing.T) {
	t.Helper()

	k8s.WritePodsDebugInfoIfFailed(t, v.kubectlOptions, v.debugDirectory, "release="+v.releaseName)
	// Ignore the error returned by the helm delete here so that we can
	// always idempotently clean up resources in the cluster.
	_ = helm.DeleteE(t, v.helmOptions, v.releaseName, true)
	// We do not need to do any PVC deletion in vault dev mode. todo
}

func defaultVaultValues(releaseName string) map[string]string {
	certSecret := certSecretName(releaseName)
	caSecret := CASecretName(releaseName)

	serverConfig := fmt.Sprintf(`
      listener "tcp" {
        address = "[::]:8200"
        cluster_address = "[::]:8201"
        tls_cert_file = "/vault/userconfig/%s/tls.crt"
        tls_key_file  = "/vault/userconfig/%s/tls.key"
        tls_client_ca_file = "/vault/userconfig/%s/tls.crt"
      }

      storage "file" {
        path = "/vault/data"
      }`, certSecret, certSecret, caSecret)

	return map[string]string{
		"global.tlsDisable":                        "false",
		"server.extraEnvironmentVars.VAULT_CACERT": fmt.Sprintf("/vault/userconfig/%s/tls.crt", caSecret),
		"server.extraVolumes[0].name":              caSecret,
		"server.extraVolumes[0].type":              "secret",
		"server.extraVolumes[1].name":              certSecret,
		"server.extraVolumes[1].type":              "secret",
		"server.standalone.enabled":                "true",
		"server.standalone.config":                 serverConfig,
		"injector.enabled":                         "true",
	}
}

func certSecretName(releaseName string) string {
	return fmt.Sprintf("%s-vault-server-tls", releaseName)
}

func CASecretName(releaseName string) string {
	return fmt.Sprintf("%s-vault-ca", releaseName)
}

func (v *VaultCluster) createTLSCerts(t *testing.T) {
	namespace := v.helmOptions.KubectlOptions.Namespace

	// Generate CA and cert and create secrets for them.
	signer, _, caPem, caCertTmpl, err := cert.GenerateCA("Vault CA")
	require.NoError(t, err)
	vaultService := fmt.Sprintf("%s-vault", v.releaseName)
	certSANs := []string{
		vaultService,
		fmt.Sprintf("%s.default", vaultService),
		fmt.Sprintf("%s.default.svc", vaultService),
	}
	certPem, keyPem, err := cert.GenerateCert("Vault server", 24*time.Hour, caCertTmpl, signer, certSANs)
	require.NoError(t, err)

	t.Cleanup(func() {
		if !v.noCleanupOnFailure {
			// We're ignoring error here because secret deletion is best effort.
			_ = v.kubernetesClient.CoreV1().Secrets(namespace).Delete(context.Background(), certSecretName(v.releaseName), metav1.DeleteOptions{})
			_ = v.kubernetesClient.CoreV1().Secrets(namespace).Delete(context.Background(), CASecretName(v.releaseName), metav1.DeleteOptions{})
		}
	})

	_, err = v.kubernetesClient.CoreV1().Secrets(namespace).Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      certSecretName(v.releaseName),
		},
		Data: map[string][]byte{
			corev1.TLSCertKey:       []byte(certPem),
			corev1.TLSPrivateKeyKey: []byte(keyPem),
		},
		Type: corev1.SecretTypeTLS,
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	_, err = v.kubernetesClient.CoreV1().Secrets(namespace).Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      CASecretName(v.releaseName),
			Namespace: namespace,
		},
		Data: map[string][]byte{
			corev1.TLSCertKey: []byte(caPem),
		},
		Type: corev1.SecretTypeOpaque,
	}, metav1.CreateOptions{})
	require.NoError(t, err)
}

func (v *VaultCluster) initAndUnseal(t *testing.T) {
	retrier := &retry.Timer{Timeout: 2 * time.Minute, Wait: 1 * time.Second}
	retry.RunWith(retrier, t, func(r *retry.R) {
		v.vaultClient = v.SetupVaultClient(t)
		initResp, err := v.vaultClient.Sys().Init(&vapi.InitRequest{
			SecretShares:    1,
			SecretThreshold: 1,
		})
		require.NoError(r, err)
		v.vaultClient.SetToken(initResp.RootToken)

		_, err = v.vaultClient.Sys().Unseal(initResp.KeysB64[0])
		require.NoError(r, err)
	})
}
