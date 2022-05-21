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
	releaseLabel = "app.kubernetes.io/instance="
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
func NewVaultCluster(t *testing.T, ctx environment.TestContext, cfg *config.TestConfig, releaseName string, helmValues map[string]string) *VaultCluster {

	logger := terratestLogger.New(logger.TestLogger{})

	kopts := ctx.KubectlOptions(t)

	values := defaultHelmValues(releaseName)
	if cfg.EnablePodSecurityPolicies {
		values["global.psp.enable"] = "true"
	}

	helpers.MergeMaps(values, helmValues)
	vaultHelmOpts := &helm.Options{
		SetValues:      values,
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
func (v *VaultCluster) VaultClient(*testing.T) *vapi.Client { return v.vaultClient }

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
	// We don't need to verify TLS for localhost traffic.
	err := config.ConfigureTLS(&vapi.TLSConfig{Insecure: true})
	require.NoError(t, err)
	vaultClient, err := vapi.NewClient(config)
	require.NoError(t, err)
	return vaultClient
}

// bootstrap sets up Kubernetes auth method and enables secrets engines.
func (v *VaultCluster) bootstrap(t *testing.T, vaultNamespace string) {
	if !v.serverEnabled() {
		v.logger.Logf(t, "skipping bootstrapping Vault because Vault server is not enabled")
		return
	}
	v.vaultClient = v.SetupVaultClient(t)
	if vaultNamespace != "" {
		createNamespace(t, v.vaultClient, vaultNamespace)
		v.vaultClient.SetNamespace(vaultNamespace)
	}

	// Enable the KV-V2 Secrets engine.
	err := v.vaultClient.Sys().Mount("consul", &vapi.MountInput{
		Type:   "kv-v2",
		Config: vapi.MountConfigInput{},
	})
	require.NoError(t, err)

	namespace := v.helmOptions.KubectlOptions.Namespace
	vaultServerServiceAccountName := fmt.Sprintf("%s-vault", v.releaseName)
	v.ConfigureAuthMethod(t, v.vaultClient, "kubernetes", "https://kubernetes.default.svc", vaultServerServiceAccountName, namespace)
}

// ConfigureAuthMethod configures the auth method in Vault from the provided service account name and namespace,
// kubernetes host and auth path.
// We need to take vaultClient here in case this Vault cluster does not have a server to run API commands against.
func (v *VaultCluster) ConfigureAuthMethod(t *testing.T, vaultClient *vapi.Client, authPath, k8sHost, saName, saNS string) {
	v.logger.Logf(t, "enabling kubernetes auth method on %s path", authPath)
	err := vaultClient.Sys().EnableAuthWithOptions(authPath, &vapi.EnableAuthOptions{
		Type: "kubernetes",
	})
	require.NoError(t, err)

	// To configure the auth method, we need to read the token and the CA cert from the auth method's
	// service account token.
	// The JWT token and CA cert is what Vault server will use to validate service account token
	// with the Kubernetes API.
	var sa *corev1.ServiceAccount
	retry.Run(t, func(r *retry.R) {
		sa, err = v.kubernetesClient.CoreV1().ServiceAccounts(saNS).Get(context.Background(), saName, metav1.GetOptions{})
		require.NoError(r, err)
		require.Len(r, sa.Secrets, 1)
	})

	v.logger.Logf(t, "updating vault kubernetes auth config for %s auth path", authPath)
	tokenSecret, err := v.kubernetesClient.CoreV1().Secrets(saNS).Get(context.Background(), sa.Secrets[0].Name, metav1.GetOptions{})
	require.NoError(t, err)
	_, err = vaultClient.Logical().Write(fmt.Sprintf("auth/%s/config", authPath), map[string]interface{}{
		"token_reviewer_jwt": string(tokenSecret.Data["token"]),
		"kubernetes_ca_cert": string(tokenSecret.Data["ca.crt"]),
		"kubernetes_host":    k8sHost,
	})
	require.NoError(t, err)
}

// Create installs Vault via Helm and then calls bootstrap to initialize it.
func (v *VaultCluster) Create(t *testing.T, ctx environment.TestContext, vaultNamespace string) {
	t.Helper()

	// Make sure we delete the cluster if we receive an interrupt signal and
	// register cleanup so that we delete the cluster when test finishes.
	helpers.Cleanup(t, v.noCleanupOnFailure, func() {
		v.Destroy(t)
	})

	// Fail if there are any existing installations of the Helm chart.
	helpers.CheckForPriorInstallations(t, v.kubernetesClient, v.helmOptions, "", v.releaseLabelSelector())

	v.createTLSCerts(t)

	// Install Vault.
	helm.Install(t, v.helmOptions, "hashicorp/vault", v.releaseName)

	v.initAndUnseal(t)

	// Wait for the injector and vault server pods to become Ready.
	k8s.WaitForAllPodsToBeReady(t, v.kubernetesClient, v.helmOptions.KubectlOptions.Namespace, v.releaseLabelSelector())

	// Now call bootstrap().
	v.bootstrap(t, vaultNamespace)
}

// Destroy issues a helm delete and deletes the PVC + any helm secrets related to the release that are leftover.
func (v *VaultCluster) Destroy(t *testing.T) {
	t.Helper()

	k8s.WritePodsDebugInfoIfFailed(t, v.kubectlOptions, v.debugDirectory, v.releaseLabelSelector())
	// Ignore the error returned by the helm delete here so that we can
	// always idempotently clean up resources in the cluster.
	_ = helm.DeleteE(t, v.helmOptions, v.releaseName, true)

	err := v.kubernetesClient.CoreV1().PersistentVolumeClaims(v.helmOptions.KubectlOptions.Namespace).DeleteCollection(context.Background(),
		metav1.DeleteOptions{}, metav1.ListOptions{LabelSelector: v.releaseLabelSelector()})
	require.NoError(t, err)
}

func defaultHelmValues(releaseName string) map[string]string {
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
		"ui.enabled":                               "true",
	}
}

// certSecretName returns the Kubernetes secret name of the certificate and key
// for the Vault server.
func certSecretName(releaseName string) string {
	return fmt.Sprintf("%s-vault-server-tls", releaseName)
}

// CASecretName returns the Kubernetes secret name of the CA for the Vault server.
func CASecretName(releaseName string) string {
	return fmt.Sprintf("%s-vault-ca", releaseName)
}

// Address is the in-cluster API address of the Vault server.
func (v *VaultCluster) Address() string {
	return fmt.Sprintf("https://%s-vault:8200", v.releaseName)
}

// releaseLabelSelector returns label selector that selects all pods
// from a Vault installation.
func (v *VaultCluster) releaseLabelSelector() string {
	return fmt.Sprintf("%s=%s", releaseLabel, v.releaseName)
}

// serverEnabled returns true if this Vault cluster has a server.
func (v *VaultCluster) serverEnabled() bool {
	serverEnabled, ok := v.helmOptions.SetValues["server.enabled"]
	// Server is enabled by default in the Vault Helm chart, so it's enabled either when that helm value is
	// not provided or when it's not explicitly disabled.
	return !ok || serverEnabled != "false"
}

// createTLSCerts generates a self-signed CA and uses it to generate
// certificate and key for the Vault server. It then saves those as
// Kubernetes secrets.
func (v *VaultCluster) createTLSCerts(t *testing.T) {
	if !v.serverEnabled() {
		v.logger.Logf(t, "skipping generating Vault TLS certificates because Vault server is not enabled")
		return
	}

	v.logger.Logf(t, "generating Vault TLS certificates")

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
			// We're ignoring error here because secret deletion is best-effort.
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

// initAndUnseal initializes and unseals Vault.
// Once initialized, it saves the Vault root token into a Kubernetes secret.
func (v *VaultCluster) initAndUnseal(t *testing.T) {
	if !v.serverEnabled() {
		v.logger.Logf(t, "skipping initializing and unsealing Vault because Vault server is not enabled")
		return
	}

	v.logger.Logf(t, "initializing and unsealing Vault")
	namespace := v.helmOptions.KubectlOptions.Namespace
	retrier := &retry.Timer{Timeout: 2 * time.Minute, Wait: 1 * time.Second}
	retry.RunWith(retrier, t, func(r *retry.R) {
		// Wait for vault server pod to be running so that we can create Vault client without errors.
		serverPod, err := v.kubernetesClient.CoreV1().Pods(namespace).Get(context.Background(), fmt.Sprintf("%s-vault-0", v.releaseName), metav1.GetOptions{})
		require.NoError(r, err)
		require.Equal(r, serverPod.Status.Phase, corev1.PodRunning)

		// Set up the client so that we can make API calls to initialize and unseal.
		v.vaultClient = v.SetupVaultClient(t)

		// Initialize Vault with 1 secret share. We don't need to
		// more key shares for this test installation.
		initResp, err := v.vaultClient.Sys().Init(&vapi.InitRequest{
			SecretShares:    1,
			SecretThreshold: 1,
		})
		require.NoError(r, err)
		v.vaultClient.SetToken(initResp.RootToken)

		// Unseal Vault with the unseal key we got when initialized it.
		// There should be one unseal key since we're only using one secret share.
		_, err = v.vaultClient.Sys().Unseal(initResp.KeysB64[0])
		require.NoError(r, err)
	})

	v.logger.Logf(t, "successfully initialized and unsealed Vault")

	rootTokenSecret := fmt.Sprintf("%s-vault-root-token", v.releaseName)
	v.logger.Logf(t, "saving Vault root token to %q Kubernetes secret", rootTokenSecret)

	helpers.Cleanup(t, v.noCleanupOnFailure, func() {
		_ = v.kubernetesClient.CoreV1().Secrets(namespace).Delete(context.Background(), rootTokenSecret, metav1.DeleteOptions{})
	})
	_, err := v.kubernetesClient.CoreV1().Secrets(namespace).Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rootTokenSecret,
			Namespace: namespace,
		},
		Data: map[string][]byte{
			"token": []byte(v.vaultClient.Token()),
		},
		Type: corev1.SecretTypeOpaque,
	}, metav1.CreateOptions{})
	require.NoError(t, err)
}

// CreateNamespace creates a Vault namespace given the namespacePath.
func createNamespace(t *testing.T, vaultClient *vapi.Client, namespacePath string) {
	_, err := vaultClient.Logical().Write(fmt.Sprintf("sys/namespaces/%s", namespacePath), nil)
	require.NoError(t, err)
}
