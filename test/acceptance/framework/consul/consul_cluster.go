package consul

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/gruntwork-io/terratest/modules/helm"
	terratestk8s "github.com/gruntwork-io/terratest/modules/k8s"
	terratestLogger "github.com/gruntwork-io/terratest/modules/logger"
	"github.com/hashicorp/consul-helm/test/acceptance/framework/config"
	"github.com/hashicorp/consul-helm/test/acceptance/framework/environment"
	"github.com/hashicorp/consul-helm/test/acceptance/framework/helpers"
	"github.com/hashicorp/consul-helm/test/acceptance/framework/k8s"
	"github.com/hashicorp/consul-helm/test/acceptance/framework/logger"
	"github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

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
	ctx                environment.TestContext
	helmOptions        *helm.Options
	releaseName        string
	kubernetesClient   kubernetes.Interface
	noCleanupOnFailure bool
	debugDirectory     string
	logger             terratestLogger.TestLogger
}

func NewHelmCluster(
	t *testing.T,
	helmValues map[string]string,
	ctx environment.TestContext,
	cfg *config.TestConfig,
	releaseName string) Cluster {

	// Deploy single-server cluster by default unless helmValues overwrites that
	values := map[string]string{
		"server.replicas":        "1",
		"server.bootstrapExpect": "1",
	}
	valuesFromConfig, err := cfg.HelmValuesFromConfig()
	require.NoError(t, err)

	// Merge all helm values
	mergeMaps(values, valuesFromConfig)
	mergeMaps(values, helmValues)

	logger := terratestLogger.New(logger.TestLogger{})

	opts := &helm.Options{
		SetValues:      values,
		KubectlOptions: ctx.KubectlOptions(t),
		Logger:         logger,
	}
	return &HelmCluster{
		ctx:                ctx,
		helmOptions:        opts,
		releaseName:        releaseName,
		kubernetesClient:   ctx.KubernetesClient(t),
		noCleanupOnFailure: cfg.NoCleanupOnFailure,
		debugDirectory:     cfg.DebugDirectory,
		logger:             logger,
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

	helm.Install(t, h.helmOptions, config.HelmChartPath, h.releaseName)

	helpers.WaitForAllPodsToBeReady(t, h.kubernetesClient, h.helmOptions.KubectlOptions.Namespace, fmt.Sprintf("release=%s", h.releaseName))
}

func (h *HelmCluster) Destroy(t *testing.T) {
	t.Helper()

	k8s.WritePodsDebugInfoIfFailed(t, h.helmOptions.KubectlOptions, h.debugDirectory, "release="+h.releaseName)

	// Ignore the error returned by the helm delete here so that we can
	// always idempotently clean up resources in the cluster.
	helm.DeleteE(t, h.helmOptions, h.releaseName, false)

	// Delete PVCs.
	h.kubernetesClient.CoreV1().PersistentVolumeClaims(h.helmOptions.KubectlOptions.Namespace).DeleteCollection(context.Background(), metav1.DeleteOptions{}, metav1.ListOptions{LabelSelector: "release=" + h.releaseName})

	// Delete any serviceaccounts that have h.releaseName in their name.
	sas, err := h.kubernetesClient.CoreV1().ServiceAccounts(h.helmOptions.KubectlOptions.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: "release=" + h.releaseName})
	require.NoError(t, err)
	for _, sa := range sas.Items {
		if strings.Contains(sa.Name, h.releaseName) {
			err := h.kubernetesClient.CoreV1().ServiceAccounts(h.helmOptions.KubectlOptions.Namespace).Delete(context.Background(), sa.Name, metav1.DeleteOptions{})
			if !errors.IsNotFound(err) {
				require.NoError(t, err)
			}
		}
	}

	// Delete any roles that have h.releaseName in their name.
	roles, err := h.kubernetesClient.RbacV1().Roles(h.helmOptions.KubectlOptions.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: "release=" + h.releaseName})
	require.NoError(t, err)
	for _, role := range roles.Items {
		if strings.Contains(role.Name, h.releaseName) {
			err := h.kubernetesClient.RbacV1().Roles(h.helmOptions.KubectlOptions.Namespace).Delete(context.Background(), role.Name, metav1.DeleteOptions{})
			if !errors.IsNotFound(err) {
				require.NoError(t, err)
			}
		}
	}

	// Delete any rolebindings that have h.releaseName in their name.
	roleBindings, err := h.kubernetesClient.RbacV1().RoleBindings(h.helmOptions.KubectlOptions.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: "release=" + h.releaseName})
	require.NoError(t, err)
	for _, roleBinding := range roleBindings.Items {
		if strings.Contains(roleBinding.Name, h.releaseName) {
			err := h.kubernetesClient.RbacV1().RoleBindings(h.helmOptions.KubectlOptions.Namespace).Delete(context.Background(), roleBinding.Name, metav1.DeleteOptions{})
			if !errors.IsNotFound(err) {
				require.NoError(t, err)
			}
		}
	}

	// Delete any secrets that have h.releaseName in their name.
	secrets, err := h.kubernetesClient.CoreV1().Secrets(h.helmOptions.KubectlOptions.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: "release=" + h.releaseName})
	require.NoError(t, err)
	for _, secret := range secrets.Items {
		if strings.Contains(secret.Name, h.releaseName) {
			err := h.kubernetesClient.CoreV1().Secrets(h.helmOptions.KubectlOptions.Namespace).Delete(context.Background(), secret.Name, metav1.DeleteOptions{})
			if !errors.IsNotFound(err) {
				require.NoError(t, err)
			}
		}
	}

	// Delete any jobs that have h.releaseName in their name.
	jobs, err := h.kubernetesClient.BatchV1().Jobs(h.helmOptions.KubectlOptions.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: "release=" + h.releaseName})
	require.NoError(t, err)
	for _, job := range jobs.Items {
		if strings.Contains(job.Name, h.releaseName) {
			err := h.kubernetesClient.BatchV1().Jobs(h.helmOptions.KubectlOptions.Namespace).Delete(context.Background(), job.Name, metav1.DeleteOptions{})
			if !errors.IsNotFound(err) {
				require.NoError(t, err)
			}
		}
	}
}

func (h *HelmCluster) Upgrade(t *testing.T, helmValues map[string]string) {
	t.Helper()

	mergeMaps(h.helmOptions.SetValues, helmValues)
	helm.Upgrade(t, h.helmOptions, config.HelmChartPath, h.releaseName)
	helpers.WaitForAllPodsToBeReady(t, h.kubernetesClient, h.helmOptions.KubectlOptions.Namespace, fmt.Sprintf("release=%s", h.releaseName))
}

func (h *HelmCluster) SetupConsulClient(t *testing.T, secure bool) *api.Client {
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
		aclSecret, err := h.kubernetesClient.CoreV1().Secrets(namespace).Get(context.Background(), h.releaseName+"-consul-bootstrap-acl-token", metav1.GetOptions{})
		if err != nil && errors.IsNotFound(err) {
			federationSecret := fmt.Sprintf("%s-consul-federation", h.releaseName)
			aclSecret, err = h.kubernetesClient.CoreV1().Secrets(namespace).Get(context.Background(), federationSecret, metav1.GetOptions{})
			require.NoError(t, err)
			config.Token = string(aclSecret.Data["replicationToken"])
		} else if err == nil {
			config.Token = string(aclSecret.Data["token"])
		} else {
			require.NoError(t, err)
		}
	}

	tunnel := terratestk8s.NewTunnelWithLogger(
		h.helmOptions.KubectlOptions,
		terratestk8s.ResourceTypePod,
		fmt.Sprintf("%s-consul-server-0", h.releaseName),
		localPort,
		remotePort,
		h.logger)
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
