// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package consul

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/gruntwork-io/terratest/modules/helm"
	terratestLogger "github.com/gruntwork-io/terratest/modules/logger"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	policyv1beta "k8s.io/api/policy/v1beta1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"

	"github.com/hashicorp/consul-k8s/acceptance/framework/config"
	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul-k8s/acceptance/framework/portforward"
)

// HelmCluster implements Cluster and uses Helm
// to create, destroy, and upgrade consul.
type HelmCluster struct {
	// ACLToken is an optional ACL token that will be used to create
	// a Consul API client. If not provided, we will attempt to read
	// a bootstrap token from a Kubernetes secret stored in the cluster.
	ACLToken string

	// SkipCheckForPreviousInstallations is a toggle for skipping the check
	// if there are any previous installations of this Helm chart in the cluster.
	SkipCheckForPreviousInstallations bool

	// ChartPath is an option field that allows consumers to change the default
	// chart path if so desired
	ChartPath string

	ctx                environment.TestContext
	helmOptions        *helm.Options
	releaseName        string
	runtimeClient      client.Client
	kubernetesClient   kubernetes.Interface
	noCleanupOnFailure bool
	noCleanup          bool
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
	if cfg.EnableRestrictedPSAEnforcement {
		configureNamespace(t, ctx.KubernetesClient(t), cfg, ctx.KubectlOptions(t).Namespace)
	}

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
		"install": {"--timeout", "30m"},
	}

	opts := &helm.Options{
		SetValues:      values,
		KubectlOptions: ctx.KubectlOptions(t),
		Logger:         logger,
		ExtraArgs:      extraArgs,
		Version:        cfg.HelmChartVersion,
	}
	return &HelmCluster{
		ctx:                ctx,
		helmOptions:        opts,
		releaseName:        releaseName,
		runtimeClient:      ctx.ControllerRuntimeClient(t),
		kubernetesClient:   ctx.KubernetesClient(t),
		noCleanupOnFailure: cfg.NoCleanupOnFailure,
		noCleanup:          cfg.NoCleanup,
		debugDirectory:     cfg.DebugDirectory,
		logger:             logger,
	}
}

func (h *HelmCluster) Create(t *testing.T) {
	t.Helper()
	h.helmOptions.ExtraArgs = map[string][]string{
		"--wait":    nil,
		"--timeout": {"10m"},
	}
	// check and remove any CRDs with finalizers
	helpers.GetCRDRemoveFinalizers(t, h.helmOptions.KubectlOptions)

	// Make sure we delete the cluster if we receive an interrupt signal and
	// register cleanup so that we delete the cluster when test finishes.
	helpers.Cleanup(t, h.noCleanupOnFailure, h.noCleanup, func() {
		h.Destroy(t)
	})

	// Fail if there are any existing installations of the Helm chart.
	if !h.SkipCheckForPreviousInstallations {
		helpers.CheckForPriorInstallations(t, h.kubernetesClient, h.helmOptions, "consul-helm", "chart=consul-helm")
	}

	chartName := config.HelmChartPath
	if h.helmOptions.Version != config.HelmChartPath {
		chartName = "hashicorp/consul"
		helm.AddRepo(t, h.helmOptions, "hashicorp", "https://helm.releases.hashicorp.com")
		// Ignoring the error from `helm repo update` as it could fail due to stale cache or unreachable servers and we're
		// asserting a chart version on Install which would fail in an obvious way should this not succeed.
		_, err := helm.RunHelmCommandAndGetOutputE(t, &helm.Options{}, "repo", "update")
		if err != nil {
			logger.Logf(t, "Unable to update helm repository, proceeding anyway: %s.", err)
		}
	}
	if h.ChartPath != "" {
		chartName = h.ChartPath
	}
	t.Logf("================================= preinstall cluster state ================================= ")
	o, err := exec.Command("kubectl", "get", "ns", "--context", h.helmOptions.KubectlOptions.ContextName).CombinedOutput()
	t.Logf("Current namespaces in the cluster: with error: %s \noutput:\n %s", err, string(o))
	o, err = exec.Command("kubectl", "get", "pods", "-A", "-o", "wide", "--context", h.helmOptions.KubectlOptions.ContextName).CombinedOutput()
	t.Logf("Current pods in default the cluster: with error: %s \noutput:\n %s", err, string(o))
	t.Logf("================================= -------------------------------- ================================= ")
	// Retry the install in case previous tests have not finished cleaning up.
	retry.RunWith(&retry.Counter{Wait: 20 * time.Second, Count: 30}, t, func(r *retry.R) {
		err := helm.InstallE(r, h.helmOptions, chartName, h.releaseName)
		if err != nil {
			t.Logf("helm install failed with error %s, retrying...", err.Error())
		}
		require.NoError(r, err)
	})
	t.Logf("================================= post install cluster state ================================= ")
	o, err = exec.Command("kubectl", "get", "ns", "--context", h.helmOptions.KubectlOptions.ContextName).CombinedOutput()
	t.Logf("Current namespaces in the cluster: with error: %s \noutput:\n %s", err, string(o))
	o, err = exec.Command("kubectl", "get", "pods", "-A", "-o", "wide", "--context", h.helmOptions.KubectlOptions.ContextName).CombinedOutput()
	t.Logf("Current pods in default the cluster: with error: %s \noutput:\n %s", err, string(o))
	t.Logf("================================= -------------------------------- ================================= ")

	k8s.WaitForAllPodsToBeReady(t, h.kubernetesClient, h.helmOptions.KubectlOptions.Namespace, fmt.Sprintf("release=%s", h.releaseName))
}

func (h *HelmCluster) Destroy(t *testing.T) {
	t.Helper()

	k8s.WritePodsDebugInfoIfFailed(t, h.helmOptions.KubectlOptions, h.debugDirectory, "release="+h.releaseName)

	// Ignore the error returned by the helm delete here so that we can
	// always idempotently clean up resources in the cluster.
	h.helmOptions.ExtraArgs = map[string][]string{
		"--wait":    nil,
		"--timeout": {"10m"},
	}

	// Clean up any stuck gateway resources, note that we swallow all errors from
	// here down since the terratest helm installation may actually already be
	// deleted at this point, in which case these operations will fail on non-existent
	// CRD cleanups.
	requirement, err := labels.NewRequirement("release", selection.Equals, []string{h.releaseName})
	require.NoError(t, err)

	// Forcibly delete all gateway classes and remove their finalizers.
	_ = h.runtimeClient.DeleteAllOf(context.Background(), &gwv1beta1.GatewayClass{}, client.HasLabels{"release=" + h.releaseName})

	var gatewayClassList gwv1beta1.GatewayClassList
	if h.runtimeClient.List(context.Background(), &gatewayClassList, &client.ListOptions{
		LabelSelector: labels.NewSelector().Add(*requirement),
	}) == nil {
		for _, item := range gatewayClassList.Items {
			item.SetFinalizers([]string{})
			_ = h.runtimeClient.Update(context.Background(), &item)
		}
	}

	// Forcibly delete all gateway class configs and remove their finalizers.
	_ = h.runtimeClient.DeleteAllOf(context.Background(), &v1alpha1.GatewayClassConfig{}, client.HasLabels{"release=" + h.releaseName})

	var gatewayClassConfigList v1alpha1.GatewayClassConfigList
	if h.runtimeClient.List(context.Background(), &gatewayClassConfigList, &client.ListOptions{
		LabelSelector: labels.NewSelector().Add(*requirement),
	}) == nil {
		for _, item := range gatewayClassConfigList.Items {
			item.SetFinalizers([]string{})
			_ = h.runtimeClient.Update(context.Background(), &item)
		}
	}

	retry.RunWith(&retry.Counter{Wait: 2 * time.Minute, Count: 30}, t, func(r *retry.R) {
		t.Logf("======================================= predelete cluster state ======================================= ")
		o, err := exec.Command("kubectl", "get", "ns", "--context", h.helmOptions.KubectlOptions.ContextName).CombinedOutput()
		t.Logf("Current namespaces in the cluster: with error: %s \noutput:\n %s", err, string(o))
		o, err = exec.Command("kubectl", "get", "pods", "-A", "-o", "wide", "--context", h.helmOptions.KubectlOptions.ContextName).CombinedOutput()
		t.Logf("Current pods in default the cluster: with error: %s \noutput:\n %s", err, string(o))
		t.Logf("================================= -------------------------------- ================================= ")

		err = helm.DeleteE(r, h.helmOptions, h.releaseName, false)
		if err != nil {
			t.Logf("helm delete failed with error %s, retrying...", err.Error())
		}
		t.Logf("======================================= postdelete cluster state ======================================= ")

		o, err = exec.Command("kubectl", "get", "ns", "--context", h.helmOptions.KubectlOptions.ContextName).CombinedOutput()
		t.Logf("Current namespaces in the cluster: with error: %s \noutput:\n %s", err, string(o))
		o, err = exec.Command("kubectl", "get", "pods", "-A", "-o", "wide", "--context", h.helmOptions.KubectlOptions.ContextName).CombinedOutput()
		t.Logf("Current pods in default the cluster: with error: %s \noutput:\n %s", err, string(o))
		t.Logf("================================= -------------------------------- ================================= ")

		require.NoError(r, err)
	})

	// Retry because sometimes certain resources (like PVC) take time to delete
	// in cloud providers.
	retry.RunWith(&retry.Counter{Wait: 2 * time.Second, Count: 600}, t, func(r *retry.R) {

		// Force delete any pods that have h.releaseName in their name because sometimes
		// graceful termination takes a long time and since this is an uninstall
		// we don't care that they're stopped gracefully.
		pods, err := h.kubernetesClient.CoreV1().Pods(h.helmOptions.KubectlOptions.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: "release=" + h.releaseName})
		require.NoError(r, err)
		for _, pod := range pods.Items {
			if strings.Contains(pod.Name, h.releaseName) {
				var gracePeriod int64 = 0
				err := h.kubernetesClient.CoreV1().Pods(h.helmOptions.KubectlOptions.Namespace).Delete(context.Background(), pod.Name, metav1.DeleteOptions{GracePeriodSeconds: &gracePeriod})
				if !errors.IsNotFound(err) {
					require.NoError(r, err)
				}
			}
		}

		// Delete any deployments that have h.releaseName in their name.
		deployments, err := h.kubernetesClient.AppsV1().Deployments(h.helmOptions.KubectlOptions.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: "release=" + h.releaseName})
		require.NoError(r, err)
		for _, deployment := range deployments.Items {
			if strings.Contains(deployment.Name, h.releaseName) {
				err := h.kubernetesClient.AppsV1().Deployments(h.helmOptions.KubectlOptions.Namespace).Delete(context.Background(), deployment.Name, metav1.DeleteOptions{})
				if !errors.IsNotFound(err) {
					require.NoError(r, err)
				}
			}
		}

		// Delete any replicasets that have h.releaseName in their name.
		replicasets, err := h.kubernetesClient.AppsV1().ReplicaSets(h.helmOptions.KubectlOptions.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: "release=" + h.releaseName})
		require.NoError(r, err)
		for _, replicaset := range replicasets.Items {
			if strings.Contains(replicaset.Name, h.releaseName) {
				err := h.kubernetesClient.AppsV1().ReplicaSets(h.helmOptions.KubectlOptions.Namespace).Delete(context.Background(), replicaset.Name, metav1.DeleteOptions{})
				if !errors.IsNotFound(err) {
					require.NoError(r, err)
				}
			}
		}

		// Delete any statefulsets that have h.releaseName in their name.
		statefulsets, err := h.kubernetesClient.AppsV1().StatefulSets(h.helmOptions.KubectlOptions.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: "release=" + h.releaseName})
		require.NoError(r, err)
		for _, statefulset := range statefulsets.Items {
			if strings.Contains(statefulset.Name, h.releaseName) {
				err := h.kubernetesClient.AppsV1().StatefulSets(h.helmOptions.KubectlOptions.Namespace).Delete(context.Background(), statefulset.Name, metav1.DeleteOptions{})
				if !errors.IsNotFound(err) {
					require.NoError(r, err)
				}
			}
		}

		// Delete any daemonsets that have h.releaseName in their name.
		daemonsets, err := h.kubernetesClient.AppsV1().DaemonSets(h.helmOptions.KubectlOptions.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: "release=" + h.releaseName})
		require.NoError(r, err)
		for _, daemonset := range daemonsets.Items {
			if strings.Contains(daemonset.Name, h.releaseName) {
				err := h.kubernetesClient.AppsV1().DaemonSets(h.helmOptions.KubectlOptions.Namespace).Delete(context.Background(), daemonset.Name, metav1.DeleteOptions{})
				if !errors.IsNotFound(err) {
					require.NoError(r, err)
				}
			}
		}

		// Delete any services that have h.releaseName in their name.
		services, err := h.kubernetesClient.CoreV1().Services(h.helmOptions.KubectlOptions.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: "release=" + h.releaseName})
		require.NoError(r, err)
		for _, service := range services.Items {
			if strings.Contains(service.Name, h.releaseName) {
				err := h.kubernetesClient.CoreV1().Services(h.helmOptions.KubectlOptions.Namespace).Delete(context.Background(), service.Name, metav1.DeleteOptions{})
				if !errors.IsNotFound(err) {
					require.NoError(r, err)
				}
			}
		}

		// Delete PVCs.
		err = h.kubernetesClient.CoreV1().PersistentVolumeClaims(h.helmOptions.KubectlOptions.Namespace).DeleteCollection(context.Background(), metav1.DeleteOptions{}, metav1.ListOptions{LabelSelector: "release=" + h.releaseName})
		require.NoError(r, err)

		// Delete any serviceaccounts that have h.releaseName in their name.
		sas, err := h.kubernetesClient.CoreV1().ServiceAccounts(h.helmOptions.KubectlOptions.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: "release=" + h.releaseName})
		require.NoError(r, err)
		for _, sa := range sas.Items {
			if strings.Contains(sa.Name, h.releaseName) {
				err := h.kubernetesClient.CoreV1().ServiceAccounts(h.helmOptions.KubectlOptions.Namespace).Delete(context.Background(), sa.Name, metav1.DeleteOptions{})
				if !errors.IsNotFound(err) {
					require.NoError(r, err)
				}
			}
		}

		// Delete any roles that have h.releaseName in their name.
		roles, err := h.kubernetesClient.RbacV1().Roles(h.helmOptions.KubectlOptions.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: "release=" + h.releaseName})
		require.NoError(r, err)
		for _, role := range roles.Items {
			if strings.Contains(role.Name, h.releaseName) {
				err := h.kubernetesClient.RbacV1().Roles(h.helmOptions.KubectlOptions.Namespace).Delete(context.Background(), role.Name, metav1.DeleteOptions{})
				if !errors.IsNotFound(err) {
					require.NoError(r, err)
				}
			}
		}

		// Delete any rolebindings that have h.releaseName in their name.
		roleBindings, err := h.kubernetesClient.RbacV1().RoleBindings(h.helmOptions.KubectlOptions.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: "release=" + h.releaseName})
		require.NoError(r, err)
		for _, roleBinding := range roleBindings.Items {
			if strings.Contains(roleBinding.Name, h.releaseName) {
				err := h.kubernetesClient.RbacV1().RoleBindings(h.helmOptions.KubectlOptions.Namespace).Delete(context.Background(), roleBinding.Name, metav1.DeleteOptions{})
				if !errors.IsNotFound(err) {
					require.NoError(r, err)
				}
			}
		}

		// Delete any secrets that have h.releaseName in their name.
		secrets, err := h.kubernetesClient.CoreV1().Secrets(h.helmOptions.KubectlOptions.Namespace).List(context.Background(), metav1.ListOptions{})
		require.NoError(r, err)
		for _, secret := range secrets.Items {
			if strings.Contains(secret.Name, h.releaseName) {
				err := h.kubernetesClient.CoreV1().Secrets(h.helmOptions.KubectlOptions.Namespace).Delete(context.Background(), secret.Name, metav1.DeleteOptions{})
				if !errors.IsNotFound(err) {
					require.NoError(r, err)
				}
			}
		}

		// Delete any jobs that have h.releaseName in their name.
		jobs, err := h.kubernetesClient.BatchV1().Jobs(h.helmOptions.KubectlOptions.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: "release=" + h.releaseName})
		require.NoError(r, err)
		for _, job := range jobs.Items {
			if strings.Contains(job.Name, h.releaseName) {
				err := h.kubernetesClient.BatchV1().Jobs(h.helmOptions.KubectlOptions.Namespace).Delete(context.Background(), job.Name, metav1.DeleteOptions{})
				if !errors.IsNotFound(err) {
					require.NoError(r, err)
				}
			}
		}

		// Verify that all deployments have been deleted.
		deployments, err = h.kubernetesClient.AppsV1().Deployments(h.helmOptions.KubectlOptions.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: "release=" + h.releaseName})
		require.NoError(r, err)
		for _, deployment := range deployments.Items {
			if strings.Contains(deployment.Name, h.releaseName) {
				r.Errorf("Found deployment which should have been deleted: %s", deployment.Name)
			}
		}

		// Verify that all replicasets have been deleted.
		replicasets, err = h.kubernetesClient.AppsV1().ReplicaSets(h.helmOptions.KubectlOptions.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: "release=" + h.releaseName})
		require.NoError(r, err)
		for _, replicaset := range replicasets.Items {
			if strings.Contains(replicaset.Name, h.releaseName) {
				r.Errorf("Found replicaset which should have been deleted: %s", replicaset.Name)
			}
		}

		// Verify that all statefulets have been deleted.
		statefulsets, err = h.kubernetesClient.AppsV1().StatefulSets(h.helmOptions.KubectlOptions.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: "release=" + h.releaseName})
		require.NoError(r, err)
		for _, statefulset := range statefulsets.Items {
			if strings.Contains(statefulset.Name, h.releaseName) {
				r.Errorf("Found statefulset which should have been deleted: %s", statefulset.Name)
			}
		}

		// Verify that all daemonsets have been deleted.
		daemonsets, err = h.kubernetesClient.AppsV1().DaemonSets(h.helmOptions.KubectlOptions.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: "release=" + h.releaseName})
		require.NoError(r, err)
		for _, daemonset := range daemonsets.Items {
			if strings.Contains(daemonset.Name, h.releaseName) {
				r.Errorf("Found daemonset which should have been deleted: %s", daemonset.Name)
			}
		}

		// Verify that all services have been deleted.
		services, err = h.kubernetesClient.CoreV1().Services(h.helmOptions.KubectlOptions.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: "release=" + h.releaseName})
		require.NoError(r, err)
		for _, service := range services.Items {
			if strings.Contains(service.Name, h.releaseName) {
				r.Errorf("Found service which should have been deleted: %s", service.Name)
			}
		}

		// Verify all Consul Pods are deleted.
		pods, err = h.kubernetesClient.CoreV1().Pods(h.helmOptions.KubectlOptions.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: "release=" + h.releaseName})
		require.NoError(r, err)
		for _, pod := range pods.Items {
			if strings.Contains(pod.Name, h.releaseName) {
				r.Errorf("Found pod which should have been deleted: %s", pod.Name)
			}
		}

		// Verify all Consul PVCs are deleted.
		pvcs, err := h.kubernetesClient.CoreV1().PersistentVolumeClaims(h.helmOptions.KubectlOptions.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: "release=" + h.releaseName})
		require.NoError(r, err)
		require.Len(r, pvcs.Items, 0)

		// Verify all Consul Service Accounts are deleted.
		sas, err = h.kubernetesClient.CoreV1().ServiceAccounts(h.helmOptions.KubectlOptions.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: "release=" + h.releaseName})
		require.NoError(r, err)
		for _, sa := range sas.Items {
			if strings.Contains(sa.Name, h.releaseName) {
				r.Errorf("Found service account which should have been deleted: %s", sa.Name)
			}
		}

		// Verify all Consul Roles are deleted.
		roles, err = h.kubernetesClient.RbacV1().Roles(h.helmOptions.KubectlOptions.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: "release=" + h.releaseName})
		require.NoError(r, err)
		for _, role := range roles.Items {
			if strings.Contains(role.Name, h.releaseName) {
				r.Errorf("Found role which should have been deleted: %s", role.Name)
			}
		}

		// Verify all Consul Role Bindings are deleted.
		roleBindings, err = h.kubernetesClient.RbacV1().RoleBindings(h.helmOptions.KubectlOptions.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: "release=" + h.releaseName})
		require.NoError(r, err)
		for _, roleBinding := range roleBindings.Items {
			if strings.Contains(roleBinding.Name, h.releaseName) {
				r.Errorf("Found role binding which should have been deleted: %s", roleBinding.Name)
			}
		}

		// Verify all Consul Secrets are deleted.
		secrets, err = h.kubernetesClient.CoreV1().Secrets(h.helmOptions.KubectlOptions.Namespace).List(context.Background(), metav1.ListOptions{})
		require.NoError(r, err)
		for _, secret := range secrets.Items {
			if strings.Contains(secret.Name, h.releaseName) {
				r.Errorf("Found secret which should have been deleted: %s", secret.Name)
			}
		}

		// Verify all Consul Jobs are deleted.
		jobs, err = h.kubernetesClient.BatchV1().Jobs(h.helmOptions.KubectlOptions.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: "release=" + h.releaseName})
		require.NoError(r, err)
		for _, job := range jobs.Items {
			if strings.Contains(job.Name, h.releaseName) {
				r.Errorf("Found job which should have been deleted: %s", job.Name)
			}
		}
	})

	t.Logf("================================= before force delete cluster state ================================= ")
	o, err := exec.Command("kubectl", "get", "ns", "--context", h.helmOptions.KubectlOptions.ContextName).CombinedOutput()
	t.Logf("Current namespaces in the cluster: with error: %s \noutput:\n %s", err, string(o))
	o, err = exec.Command("kubectl", "get", "pods", "-A", "-o", "wide", "--context", h.helmOptions.KubectlOptions.ContextName).CombinedOutput()
	t.Logf("Current pods in default the cluster: with error: %s \noutput:\n %s", err, string(o))
	t.Logf("================================= -------------------------------- ================================= ")

	helm.DeleteE(t, h.helmOptions, h.releaseName, true)
	t.Logf("================================= after force delete cluster state ================================= ")
	o, err = exec.Command("kubectl", "get", "ns", "--context", h.helmOptions.KubectlOptions.ContextName).CombinedOutput()
	t.Logf("Current namespaces in the cluster: with error: %s \noutput:\n %s", err, string(o))
	o, err = exec.Command("kubectl", "get", "pods", "-A", "-o", "wide", "--context", h.helmOptions.KubectlOptions.ContextName).CombinedOutput()
	t.Logf("Current pods in default the cluster: with error: %s \noutput:\n %s", err, string(o))
	t.Logf("================================= -------------------------------- ================================= ")

}

func (h *HelmCluster) Upgrade(t *testing.T, helmValues map[string]string) {
	t.Helper()

	helpers.MergeMaps(h.helmOptions.SetValues, helmValues)
	chartName := "hashicorp/consul"
	if h.helmOptions.Version == config.HelmChartPath {
		chartName = config.HelmChartPath
	}
	helm.Upgrade(t, h.helmOptions, chartName, h.releaseName)
	k8s.WaitForAllPodsToBeReady(t, h.kubernetesClient, h.helmOptions.KubectlOptions.Namespace, fmt.Sprintf("release=%s", h.releaseName))
}

// CreatePortForwardTunnel returns the local address:port of a tunnel to the consul server pod in the given release.
func (h *HelmCluster) CreatePortForwardTunnel(t *testing.T, remotePort int, release ...string) string {
	releaseName := h.releaseName
	if len(release) > 0 {
		releaseName = release[0]
	}
	serverPod := fmt.Sprintf("%s-consul-server-0", releaseName)
	if releaseName == "" {
		serverPod = "consul-server-0"
	}
	return portforward.CreateTunnelToResourcePort(t, serverPod, remotePort, h.helmOptions.KubectlOptions, h.logger)
}

// For instances when namespace is being manually set by the test and needs to be overridden.
func (h *HelmCluster) SetNamespace(ns string) {
	h.helmOptions.KubectlOptions.Namespace = ns
}

func (h *HelmCluster) SetupConsulClient(t *testing.T, secure bool, release ...string) (client *api.Client, configAddress string) {
	t.Helper()

	releaseName := h.releaseName
	if len(release) > 0 {
		releaseName = release[0]
	}

	namespace := h.helmOptions.KubectlOptions.Namespace
	config := api.DefaultConfig()
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
			retry.RunWith(&retry.Counter{Wait: 2 * time.Second, Count: 600}, t, func(r *retry.R) {
				// Get the ACL token. First, attempt to read it from the bootstrap token (this will be true in primary Consul servers).
				// If the bootstrap token doesn't exist, it means we are running against a secondary cluster
				// and will try to read the replication token from the federation secret.
				// In secondary servers, we don't create a bootstrap token since ACLs are only bootstrapped in the primary.
				// Instead, we provide a replication token that serves the role of the bootstrap token.
				aclSecretName := releaseName + "-consul-bootstrap-acl-token"
				if releaseName == "" {
					aclSecretName = "consul-bootstrap-acl-token"
				}
				aclSecret, err := h.kubernetesClient.CoreV1().Secrets(namespace).Get(context.Background(), aclSecretName, metav1.GetOptions{})
				if err != nil && errors.IsNotFound(err) {
					federationSecretName := fmt.Sprintf("%s-consul-federation", releaseName)
					if releaseName == "" {
						federationSecretName = "consul-federation"
					}
					aclSecret, err = h.kubernetesClient.CoreV1().Secrets(namespace).Get(context.Background(), federationSecretName, metav1.GetOptions{})
					require.NoError(r, err)
					config.Token = string(aclSecret.Data["replicationToken"])
				} else if err == nil {
					config.Token = string(aclSecret.Data["token"])
				} else {
					require.NoError(r, err)
				}
			})
		}
	}

	config.Address = h.CreatePortForwardTunnel(t, remotePort, release...)
	consulClient, err := api.NewClient(config)
	require.NoError(t, err)

	return consulClient, config.Address
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

	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		_ = client.PolicyV1beta1().PodSecurityPolicies().Delete(context.Background(), pspName, metav1.DeleteOptions{})
		_ = client.RbacV1().ClusterRoles().Delete(context.Background(), pspName, metav1.DeleteOptions{})
		_ = client.RbacV1().RoleBindings(namespace).Delete(context.Background(), pspName, metav1.DeleteOptions{})
	})
}

func createOrUpdateLicenseSecret(t *testing.T, client kubernetes.Interface, cfg *config.TestConfig, namespace string) {
	CreateK8sSecret(t, client, cfg, namespace, config.LicenseSecretName, config.LicenseSecretKey, cfg.EnterpriseLicense)
}

func configureNamespace(t *testing.T, client kubernetes.Interface, cfg *config.TestConfig, namespace string) {
	ctx := context.Background()

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   namespace,
			Labels: map[string]string{},
		},
	}
	if cfg.EnableRestrictedPSAEnforcement {
		ns.ObjectMeta.Labels["pod-security.kubernetes.io/enforce"] = "restricted"
		ns.ObjectMeta.Labels["pod-security.kubernetes.io/enforce-version"] = "latest"
	}

	_, createErr := client.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if createErr == nil {
		logger.Logf(t, "Created namespace %s", namespace)
		return
	}

	_, updateErr := client.CoreV1().Namespaces().Update(ctx, ns, metav1.UpdateOptions{})
	if updateErr == nil {
		logger.Logf(t, "Updated namespace %s", namespace)
		return
	}

	require.Failf(t, "Failed to create or update namespace", "Namespace=%s, CreateError=%s, UpdateError=%s", namespace, createErr, updateErr)
}

// configureSCCs creates RoleBindings that bind the default service account to cluster roles
// allowing access to the privileged Security Context Constraints on OpenShift.
func configureSCCs(t *testing.T, client kubernetes.Interface, cfg *config.TestConfig, namespace string) {
	const privilegedClusterRole = "system:openshift:scc:privileged"
	privilegedRoleBinding := "privileged-test"

	// A role binding to allow default service account in the installation namespace access to the SCCs.
	// Check if this cluster role binding already exists.
	_, err := client.RbacV1().RoleBindings(namespace).Get(context.Background(), privilegedRoleBinding, metav1.GetOptions{})

	if errors.IsNotFound(err) {
		roleBinding := &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: privilegedRoleBinding,
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
				Name: privilegedClusterRole,
			},
		}

		_, err = client.RbacV1().RoleBindings(namespace).Create(context.Background(), roleBinding, metav1.CreateOptions{})
		require.NoError(t, err)
	} else {
		require.NoError(t, err)
	}

	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		_ = client.RbacV1().RoleBindings(namespace).Delete(context.Background(), privilegedRoleBinding, metav1.DeleteOptions{})
	})
}

func defaultValues() map[string]string {
	values := map[string]string{
		"global.logLevel": "debug",
		"server.replicas": "1",
		// Disable DNS since enabling it changes the policy for the anonymous token,
		// which could result in tests passing due to that token having privileges to read services
		// (false positive).
		"dns.enabled": "false",

		// Adjust the default value from 30s to 1s since we have several tests that verify tokens are cleaned up,
		// and many of them are using the default retryer (7s max).
		"connectInject.sidecarProxy.lifecycle.defaultShutdownGracePeriodSeconds": "1",

		// Enable trace logs for servers and clients.
		"server.extraConfig": `"{\"log_level\": \"TRACE\"}"`,
		"client.extraConfig": `"{\"log_level\": \"TRACE\"}"`,
	}
	return values
}

func CreateK8sSecret(t *testing.T, client kubernetes.Interface, cfg *config.TestConfig, namespace, secretName, secretKey, secret string) {
	retry.RunWith(&retry.Counter{Wait: 2 * time.Second, Count: 15}, t, func(r *retry.R) {
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
			require.NoError(r, err)
		} else {
			require.NoError(r, err)
		}
	})

	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		_ = client.CoreV1().Secrets(namespace).Delete(context.Background(), secretName, metav1.DeleteOptions{})
	})
}
