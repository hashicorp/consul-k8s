package framework

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"testing"

	"github.com/gruntwork-io/terratest/modules/helm"
	"github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/gruntwork-io/terratest/modules/logger"
	"github.com/hashicorp/consul-helm/test/acceptance/helpers"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/freeport"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// The path to the helm chart.
// Note: this will need to be changed if this file is moved.
const helmChartPath = "../../../.."

// Cluster represents a consul cluster object
type Cluster interface {
	Create(t *testing.T)
	Destroy(t *testing.T)
	// Upgrade runs helm upgrade. It will merge the helm values from the
	// initial install with helmValues. Any keys that were previously set
	// will be overridden by the helmValues keys.
	Upgrade(t *testing.T, helmValues map[string]string)
	SetupConsulClient(t *testing.T, secure bool) *api.Client
}

// HelmCluster implements Cluster and uses Helm
// to create, destroy, and upgrade consul
type HelmCluster struct {
	helmOptions        *helm.Options
	releaseName        string
	kubernetesClient   kubernetes.Interface
	noCleanupOnFailure bool
}

func NewHelmCluster(
	t *testing.T,
	helmValues map[string]string,
	ctx TestContext,
	cfg *TestConfig,
	releaseName string) Cluster {

	// Deploy single-server cluster by default unless helmValues overwrites that
	values := map[string]string{
		"server.replicas":        "1",
		"server.bootstrapExpect": "1",
	}
	valuesFromConfig := cfg.HelmValuesFromConfig()

	// Merge all helm values
	mergeMaps(values, valuesFromConfig)
	mergeMaps(values, helmValues)

	opts := &helm.Options{
		SetValues:      values,
		KubectlOptions: ctx.KubectlOptions(),
		Logger:         logger.TestingT,
	}
	return &HelmCluster{
		helmOptions:        opts,
		releaseName:        releaseName,
		kubernetesClient:   ctx.KubernetesClient(t),
		noCleanupOnFailure: cfg.NoCleanupOnFailure,
	}
}

func (h *HelmCluster) Create(t *testing.T) {
	t.Helper()

	// Make sure we delete the cluster if we receive an interrupt signal and
	// register cleanup so that we delete the cluster when test finishes.
	helpers.Cleanup(t, h.noCleanupOnFailure, func() {
		h.Destroy(t)
	})

	// Fail if there are any existing installations of the Helm chart.
	h.checkForPriorInstallations(t)

	helm.Install(t, h.helmOptions, helmChartPath, h.releaseName)

	helpers.WaitForAllPodsToBeReady(t, h.kubernetesClient, h.helmOptions.KubectlOptions.Namespace, fmt.Sprintf("release=%s", h.releaseName))
}

func (h *HelmCluster) Destroy(t *testing.T) {
	t.Helper()

	helm.Delete(t, h.helmOptions, h.releaseName, false)

	// delete PVCs
	h.kubernetesClient.CoreV1().PersistentVolumeClaims(h.helmOptions.KubectlOptions.Namespace).DeleteCollection(&metav1.DeleteOptions{}, metav1.ListOptions{LabelSelector: "release=" + h.releaseName})

	// delete any secrets that have h.releaseName in their name
	secrets, err := h.kubernetesClient.CoreV1().Secrets(h.helmOptions.KubectlOptions.Namespace).List(metav1.ListOptions{})
	require.NoError(t, err)
	for _, secret := range secrets.Items {
		if strings.Contains(secret.Name, h.releaseName) {
			err := h.kubernetesClient.CoreV1().Secrets(h.helmOptions.KubectlOptions.Namespace).Delete(secret.Name, nil)
			require.NoError(t, err)
		}
	}

	// delete any serviceaccounts that have h.releaseName in their name
	sas, err := h.kubernetesClient.CoreV1().ServiceAccounts(h.helmOptions.KubectlOptions.Namespace).List(metav1.ListOptions{})
	require.NoError(t, err)
	for _, sa := range sas.Items {
		if strings.Contains(sa.Name, h.releaseName) {
			err := h.kubernetesClient.CoreV1().ServiceAccounts(h.helmOptions.KubectlOptions.Namespace).Delete(sa.Name, nil)
			require.NoError(t, err)
		}
	}
}

func (h *HelmCluster) Upgrade(t *testing.T, helmValues map[string]string) {
	mergeMaps(h.helmOptions.SetValues, helmValues)
	helm.Upgrade(t, h.helmOptions, helmChartPath, h.releaseName)
	helpers.WaitForAllPodsToBeReady(t, h.kubernetesClient, h.helmOptions.KubectlOptions.Namespace, fmt.Sprintf("release=%s", h.releaseName))
}

func (h *HelmCluster) SetupConsulClient(t *testing.T, secure bool) *api.Client {
	t.Helper()

	namespace := h.helmOptions.KubectlOptions.Namespace
	config := api.DefaultConfig()
	localPort := freeport.MustTake(1)[0]
	remotePort := 8500 // use non-secure by default

	if secure {
		// overwrite remote port to HTTPS
		remotePort = 8501

		// get the CA
		caSecret, err := h.kubernetesClient.CoreV1().Secrets(namespace).Get(h.releaseName+"-consul-ca-cert", metav1.GetOptions{})
		require.NoError(t, err)
		caFile, err := ioutil.TempFile("", "")
		require.NoError(t, err)
		helpers.Cleanup(t, h.noCleanupOnFailure, func() {
			require.NoError(t, os.Remove(caFile.Name()))
		})

		if caContents, ok := caSecret.Data["tls.crt"]; ok {
			_, err := caFile.Write(caContents)
			require.NoError(t, err)
		}

		// get the ACL token
		aclSecret, err := h.kubernetesClient.CoreV1().Secrets(namespace).Get(h.releaseName+"-consul-bootstrap-acl-token", metav1.GetOptions{})
		require.NoError(t, err)

		config.TLSConfig.CAFile = caFile.Name()
		config.Token = string(aclSecret.Data["token"])
		config.Scheme = "https"
	}

	tunnel := k8s.NewTunnel(h.helmOptions.KubectlOptions, k8s.ResourceTypePod, fmt.Sprintf("%s-consul-server-0", h.releaseName), localPort, remotePort)
	tunnel.ForwardPort(t)

	t.Cleanup(func() {
		tunnel.Close()
	})

	config.Address = fmt.Sprintf("127.0.0.1:%d", localPort)
	consulClient, err := api.NewClient(config)
	require.NoError(t, err)

	return consulClient
}

// checkForPriorInstallations checks if there is an existing Helm release
// for this Helm chart already installed. If there is, it fails the tests.
func (h *HelmCluster) checkForPriorInstallations(t *testing.T) {
	t.Helper()

	// check if there's an existing cluster and fail if there is
	output, err := helm.RunHelmCommandAndGetOutputE(t, h.helmOptions, "list", "--output", "json")
	require.NoError(t, err)

	var installedReleases []map[string]string

	err = json.Unmarshal([]byte(output), &installedReleases)
	require.NoError(t, err)

	for _, r := range installedReleases {
		require.NotContains(t, r["chart"], "consul", fmt.Sprintf("detected an existing installation of Consul %s, release name: %s", r["chart"], r["name"]))
	}
}

// mergeValues will merge the values in b with values in a and save in a.
// If there are conflicts, the values in b will overwrite the values in a.
func mergeMaps(a, b map[string]string) {
	for k, v := range b {
		a[k] = v
	}
}
