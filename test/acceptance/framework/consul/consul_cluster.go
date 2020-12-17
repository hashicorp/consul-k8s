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
	"github.com/hashicorp/consul-helm/test/acceptance/framework/config"
	"github.com/hashicorp/consul-helm/test/acceptance/framework/environment"
	"github.com/hashicorp/consul-helm/test/acceptance/framework/helpers"
	"github.com/hashicorp/consul-helm/test/acceptance/framework/k8s"
	"github.com/hashicorp/consul-helm/test/acceptance/framework/logger"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	policyv1beta "k8s.io/api/policy/v1beta1"
	rbacv1 "k8s.io/api/rbac/v1"
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
	cfg                config.TestConfig
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
	releaseName string,
) Cluster {

	if cfg.EnablePodSecurityPolicies {
		configurePodSecurityPolicies(t, ctx.KubernetesClient(t), cfg, ctx.KubectlOptions(t).Namespace)
	}

	// Deploy with the following defaults unless helmValues overwrites it.
	values := map[string]string{
		"server.replicas":              "1",
		"server.bootstrapExpect":       "1",
		"connectInject.envoyExtraArgs": "--log-level debug",
		"connectInject.logLevel":       "debug",
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
	secrets, err := h.kubernetesClient.CoreV1().Secrets(h.helmOptions.KubectlOptions.Namespace).List(context.Background(), metav1.ListOptions{})
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

// checkForPriorInstallations checks if there is an existing Helm release
// for this Helm chart already installed. If there is, it fails the tests.
func (h *HelmCluster) checkForPriorInstallations(t *testing.T) {
	t.Helper()

	var helmListOutput string
	// Check if there's an existing cluster and fail if there is one.
	// We may need to retry since this is the first command run once the Kube
	// cluster is created and sometimes the API server returns errors.
	retry.RunWith(&retry.Counter{Wait: 1 * time.Second, Count: 3}, t, func(r *retry.R) {
		var err error
		// NOTE: It's okay to pass in `t` to RunHelmCommandAndGetOutputE despite being in a retry
		// because we're using RunHelmCommandAndGetOutputE (not RunHelmCommandAndGetOutput) so the `t` won't
		// get used to fail the test, just for logging.
		helmListOutput, err = helm.RunHelmCommandAndGetOutputE(t, h.helmOptions, "list", "--output", "json")
		require.NoError(r, err)
	})

	var installedReleases []map[string]string

	err := json.Unmarshal([]byte(helmListOutput), &installedReleases)
	require.NoError(t, err, "unmarshalling %q", helmListOutput)

	for _, r := range installedReleases {
		require.NotContains(t, r["chart"], "consul", fmt.Sprintf("detected an existing installation of Consul %s, release name: %s", r["chart"], r["name"]))
	}
}

// configurePodSecurityPolicies creates a simple pod security policy, a cluster role to allow access to the PSP,
// and a role binding that binds the default service account in the helm installation namespace to the cluster role.
// We bind the default service account for tests that are spinning up pods without a service account set so that
// they will not be rejected by the kube pod security policy controller.
func configurePodSecurityPolicies(t *testing.T, client kubernetes.Interface, cfg *config.TestConfig, namespace string) {
	pspName := "test-psp"

	// Pod Security Policy
	{
		// Check if the pod security policy with this name already exists
		psp, err := client.PolicyV1beta1().PodSecurityPolicies().Get(context.Background(), pspName, metav1.GetOptions{})
		// If it doesn't exist, create it.
		if errors.IsNotFound(err) {
			// This pod security policy can be used by any tests resources.
			// This policy is fairly simple and only prevents from running privileged containers.
			psp = &policyv1beta.PodSecurityPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-psp",
				},
				Spec: policyv1beta.PodSecurityPolicySpec{
					Privileged: false,
					SELinux: policyv1beta.SELinuxStrategyOptions{
						Rule: policyv1beta.SELinuxStrategyRunAsAny,
					},
					SupplementalGroups: policyv1beta.SupplementalGroupsStrategyOptions{
						Rule: policyv1beta.SupplementalGroupsStrategyRunAsAny,
					},
					RunAsUser: policyv1beta.RunAsUserStrategyOptions{
						Rule: policyv1beta.RunAsUserStrategyRunAsAny,
					},
					FSGroup: policyv1beta.FSGroupStrategyOptions{
						Rule: policyv1beta.FSGroupStrategyRunAsAny,
					},
					Volumes: []policyv1beta.FSType{policyv1beta.All},
				},
			}
			_, err = client.PolicyV1beta1().PodSecurityPolicies().Create(context.Background(), psp, metav1.CreateOptions{})
			require.NoError(t, err)
		} else {
			require.NoError(t, err)
		}
	}

	// Cluster role for the PSP.
	{
		// Check if we have a cluster role that authorizes the use of the pod security policy.
		pspClusterRole, err := client.RbacV1().ClusterRoles().Get(context.Background(), pspName, metav1.GetOptions{})

		// If it doesn't exist, create the clusterrole.
		if errors.IsNotFound(err) {
			pspClusterRole = &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{
					Name: pspName,
				},
				Rules: []rbacv1.PolicyRule{
					{
						Verbs:         []string{"use"},
						APIGroups:     []string{"policy"},
						Resources:     []string{"podsecuritypolicies"},
						ResourceNames: []string{pspName},
					},
				},
			}
			_, err = client.RbacV1().ClusterRoles().Create(context.Background(), pspClusterRole, metav1.CreateOptions{})
			require.NoError(t, err)
		} else {
			require.NoError(t, err)
		}
	}

	// A role binding to allow default service account in the installation namespace access to the PSP.
	{
		// Check if this cluster role binding already exists.
		pspRoleBinding, err := client.RbacV1().RoleBindings(namespace).Get(context.Background(), pspName, metav1.GetOptions{})

		if errors.IsNotFound(err) {
			pspRoleBinding = &rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: pspName,
				},
				Subjects: []rbacv1.Subject{
					{
						Kind:      rbacv1.ServiceAccountKind,
						Name:      "default",
						Namespace: namespace,
					},
				},
				RoleRef: rbacv1.RoleRef{
					Kind: "ClusterRole",
					Name: pspName,
				},
			}

			_, err = client.RbacV1().RoleBindings(namespace).Create(context.Background(), pspRoleBinding, metav1.CreateOptions{})
			require.NoError(t, err)
		} else {
			require.NoError(t, err)
		}
	}

	helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
		client.PolicyV1beta1().PodSecurityPolicies().Delete(context.Background(), pspName, metav1.DeleteOptions{})
		client.RbacV1().ClusterRoles().Delete(context.Background(), pspName, metav1.DeleteOptions{})
		client.RbacV1().RoleBindings(namespace).Delete(context.Background(), pspName, metav1.DeleteOptions{})
	})
}

// mergeValues will merge the values in b with values in a and save in a.
// If there are conflicts, the values in b will overwrite the values in a.
func mergeMaps(a, b map[string]string) {
	for k, v := range b {
		a[k] = v
	}
}
