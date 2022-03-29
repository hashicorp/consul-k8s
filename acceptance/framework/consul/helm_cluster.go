package consul

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
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	policyv1beta "k8s.io/api/policy/v1beta1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// HelmCluster implements Cluster and uses Helm
// to create, destroy, and upgrade consul.
type HelmCluster struct {
	// ACLToken is an optional ACL token that will be used to create
	// a Consul API client. If not provided, we will attempt to read
	// a bootstrap token from a Kubernetes secret stored in the cluster.
	ACLToken string

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
) *HelmCluster {
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
	values := defaultValues()
	valuesFromConfig, err := cfg.HelmValuesFromConfig()
	require.NoError(t, err)

	// Merge all helm values
	helpers.MergeMaps(values, valuesFromConfig)
	helpers.MergeMaps(values, helmValues)

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
	helpers.CheckForPriorInstallations(t, h.kubernetesClient, h.helmOptions, "consul-helm", "chart=consul-helm")

	helm.Install(t, h.helmOptions, config.HelmChartPath, h.releaseName)

	k8s.WaitForAllPodsToBeReady(t, h.kubernetesClient, h.helmOptions.KubectlOptions.Namespace, fmt.Sprintf("release=%s", h.releaseName))
}

func (h *HelmCluster) Destroy(t *testing.T) {
	t.Helper()

	k8s.WritePodsDebugInfoIfFailed(t, h.helmOptions.KubectlOptions, h.debugDirectory, "release="+h.releaseName)

	// Ignore the error returned by the helm delete here so that we can
	// always idempotently clean up resources in the cluster.
	_ = helm.DeleteE(t, h.helmOptions, h.releaseName, false)

	// Force delete any pods that have h.releaseName in their name because sometimes
	// graceful termination takes a long time and since this is an uninstall
	// we don't care that they're stopped gracefully.
	pods, err := h.kubernetesClient.CoreV1().Pods(h.helmOptions.KubectlOptions.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: "release=" + h.releaseName})
	require.NoError(t, err)
	for _, pod := range pods.Items {
		if strings.Contains(pod.Name, h.releaseName) {
			var gracePeriod int64 = 0
			err := h.kubernetesClient.CoreV1().Pods(h.helmOptions.KubectlOptions.Namespace).Delete(context.Background(), pod.Name, metav1.DeleteOptions{GracePeriodSeconds: &gracePeriod})
			if !errors.IsNotFound(err) {
				require.NoError(t, err)
			}
		}
	}

	// Delete PVCs.
	err = h.kubernetesClient.CoreV1().PersistentVolumeClaims(h.helmOptions.KubectlOptions.Namespace).DeleteCollection(context.Background(), metav1.DeleteOptions{}, metav1.ListOptions{LabelSelector: "release=" + h.releaseName})
	require.NoError(t, err)

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

	helpers.MergeMaps(h.helmOptions.SetValues, helmValues)
	helm.Upgrade(t, h.helmOptions, config.HelmChartPath, h.releaseName)
	k8s.WaitForAllPodsToBeReady(t, h.kubernetesClient, h.helmOptions.KubectlOptions.Namespace, fmt.Sprintf("release=%s", h.releaseName))
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

		// If an ACL token is provided, we'll use that instead of trying to find it.
		if h.ACLToken != "" {
			config.Token = h.ACLToken
		} else {
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
	}

	serverPod := fmt.Sprintf("%s-consul-server-0", h.releaseName)
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

// configurePodSecurityPolicies creates a simple pod security policy, a cluster role to allow access to the PSP,
// and a role binding that binds the default service account in the helm installation namespace to the cluster role.
// We bind the default service account for tests that are spinning up pods without a service account set so that
// they will not be rejected by the kube pod security policy controller.
func configurePodSecurityPolicies(t *testing.T, client kubernetes.Interface, cfg *config.TestConfig, namespace string) {
	pspName := "test-psp"

	// Pod Security Policy
	{
		// Check if the pod security policy with this name already exists
		_, err := client.PolicyV1beta1().PodSecurityPolicies().Get(context.Background(), pspName, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			// This pod security policy can be used by any tests resources.
			// This policy is fairly simple and only prevents from running privileged containers.
			psp := &policyv1beta.PodSecurityPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-psp",
				},
				Spec: policyv1beta.PodSecurityPolicySpec{
					Privileged:          true,
					AllowedCapabilities: []corev1.Capability{"NET_ADMIN"},
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
		_, err := client.RbacV1().ClusterRoles().Get(context.Background(), pspName, metav1.GetOptions{})

		// If it doesn't exist, create the clusterrole.
		if errors.IsNotFound(err) {
			pspClusterRole := &rbacv1.ClusterRole{
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
		_, err := client.RbacV1().RoleBindings(namespace).Get(context.Background(), pspName, metav1.GetOptions{})

		if errors.IsNotFound(err) {
			pspRoleBinding := &rbacv1.RoleBinding{
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
		_ = client.PolicyV1beta1().PodSecurityPolicies().Delete(context.Background(), pspName, metav1.DeleteOptions{})
		_ = client.RbacV1().ClusterRoles().Delete(context.Background(), pspName, metav1.DeleteOptions{})
		_ = client.RbacV1().RoleBindings(namespace).Delete(context.Background(), pspName, metav1.DeleteOptions{})
	})
}

func createOrUpdateLicenseSecret(t *testing.T, client kubernetes.Interface, cfg *config.TestConfig, namespace string) {
	CreateK8sSecret(t, client, cfg, namespace, config.LicenseSecretName, config.LicenseSecretKey, cfg.EnterpriseLicense)
}

// configureSCCs creates RoleBindings that bind the default service account to cluster roles
// allowing access to the anyuid and privileged Security Context Constraints on OpenShift.
func configureSCCs(t *testing.T, client kubernetes.Interface, cfg *config.TestConfig, namespace string) {
	const anyuidClusterRole = "system:openshift:scc:anyuid"
	const privilegedClusterRole = "system:openshift:scc:privileged"
	anyuidRoleBinding := "anyuid-test"
	privilegedRoleBinding := "privileged-test"

	// A role binding to allow default service account in the installation namespace access to the SCCs.
	{
		for clusterRoleName, roleBindingName := range map[string]string{anyuidClusterRole: anyuidRoleBinding, privilegedClusterRole: privilegedRoleBinding} {
			// Check if this cluster role binding already exists.
			_, err := client.RbacV1().RoleBindings(namespace).Get(context.Background(), roleBindingName, metav1.GetOptions{})

			if errors.IsNotFound(err) {
				roleBinding := &rbacv1.RoleBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name: roleBindingName,
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
						Name: clusterRoleName,
					},
				}

				_, err = client.RbacV1().RoleBindings(namespace).Create(context.Background(), roleBinding, metav1.CreateOptions{})
				require.NoError(t, err)
			} else {
				require.NoError(t, err)
			}
		}
	}

	helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
		_ = client.RbacV1().RoleBindings(namespace).Delete(context.Background(), anyuidRoleBinding, metav1.DeleteOptions{})
		_ = client.RbacV1().RoleBindings(namespace).Delete(context.Background(), privilegedRoleBinding, metav1.DeleteOptions{})
	})
}

func defaultValues() map[string]string {
	values := map[string]string{
		"server.replicas":              "1",
		"server.bootstrapExpect":       "1",
		"connectInject.envoyExtraArgs": "--log-level debug",
		"connectInject.logLevel":       "debug",
		// Disable DNS since enabling it changes the policy for the anonymous token,
		// which could result in tests passing due to that token having privileges to read services
		// (false positive).
		"dns.enabled": "false",

		// Enable trace logs for servers and clients.
		"server.extraConfig": `"{\"log_level\": \"TRACE\"}"`,
		"client.extraConfig": `"{\"log_level\": \"TRACE\"}"`,
	}
	return values
}

func CreateK8sSecret(t *testing.T, client kubernetes.Interface, cfg *config.TestConfig, namespace, secretName, secretKey, secret string) {
	_, err := client.CoreV1().Secrets(namespace).Get(context.Background(), secretName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		_, err := client.CoreV1().Secrets(namespace).Create(context.Background(), &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: secretName,
			},
			StringData: map[string]string{
				secretKey: secret,
			},
			Type: corev1.SecretTypeOpaque,
		}, metav1.CreateOptions{})
		require.NoError(t, err)
	} else {
		require.NoError(t, err)
	}

	helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
		_ = client.CoreV1().Secrets(namespace).Delete(context.Background(), secretName, metav1.DeleteOptions{})
	})
}
