package consul

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gruntwork-io/terratest/modules/helm"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	gwv1beta1 "github.com/hashicorp/consul-k8s/control-plane/gateway07/gateway-api-0.7.1-custom/apis/v1beta1"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func (h *HelmCluster) cleanupOpenShiftBeforeInstall(t *testing.T) {
	t.Helper()

	logger.Logf(t, "Cleaning stale Consul resources before Helm install in OpenShift namespace %s", h.helmOptions.KubectlOptions.Namespace)

	h.resetNamespacePSAEnforcementLabel(t)
	h.deleteStaleTestNamespaces(t)
	h.deleteStaleNamedSecretsForRelease(t, h.releaseName)
	h.deleteGatewayHookJobsIfExistsForRelease(t, h.releaseName)
	h.deleteStaleGatewayAndConsulAPIResources(t)
	if strings.HasPrefix(t.Name(), "TestAPIGateway") {
		logger.Logf(t, "Deleting stale Gateway API and Consul API resources before Helm install for API gateway test %s", t.Name())
		h.deleteStaleAPIGatewayTestClusterResources(t)
	}
	h.deleteStaleHelmReleases(t)
	h.deleteStaleConsulOwnedCRDs(t)
	h.deleteStaleStaticPrefixedResources(t)
	h.deleteStaleLabeledResources(t)
}

func (h *HelmCluster) resetNamespacePSAEnforcementLabel(t *testing.T) {
	t.Helper()
	logger.Logf(t, "Resetting stale PSA enforcement label on namespace %s before Helm install", h.helmOptions.KubectlOptions.Namespace)
	namespace := h.helmOptions.KubectlOptions.Namespace
	ns, err := h.kubernetesClient.CoreV1().Namespaces().Get(context.Background(), namespace, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		return
	}
	require.NoError(t, err)

	labels := ns.GetLabels()
	if labels == nil {
		return
	}

	if labels["pod-security.kubernetes.io/enforce"] != "restricted" {
		return
	}

	nsCopy := ns.DeepCopy()
	labelsCopy := nsCopy.GetLabels()
	if labelsCopy == nil {
		labelsCopy = map[string]string{}
	}
	labelsCopy["pod-security.kubernetes.io/enforce"] = "privileged"
	nsCopy.SetLabels(labelsCopy)

	logger.Logf(t, "Resetting stale PSA enforcement label on namespace %s from restricted to privileged before Helm install", namespace)
	_, err = h.kubernetesClient.CoreV1().Namespaces().Update(context.Background(), nsCopy, metav1.UpdateOptions{})
	require.NoError(t, err)
}

func (h *HelmCluster) deleteStaleAPIGatewayTestClusterResources(t *testing.T) {
	t.Helper()
	h.deleteStaleAPIGatewayTestSecrets(t)
	for _, name := range []string{"gateway-class-config", "controlled-gateway-class-config"} {
		h.deleteStaleGatewayClassConfig(t, name)
	}

	for _, name := range []string{"gateway-class", "controlled-gateway-class-one", "controlled-gateway-class-two", "uncontrolled-gateway-class"} {
		h.deleteStaleGatewayClass(t, name)
	}

	for _, name := range []string{"custom-gateway-class", "custom-controlled-gateway-class-one", "custom-controlled-gateway-class-two", "custom-uncontrolled-gateway-class"} {
		h.deleteCustomStaleGatewayClass(t, name)
	}
}

func (h *HelmCluster) deleteStaleAPIGatewayTestSecrets(t *testing.T) {
	t.Helper()

	namespace := h.helmOptions.KubectlOptions.Namespace
	secrets, err := h.kubernetesClient.CoreV1().Secrets(namespace).List(context.Background(), metav1.ListOptions{
		LabelSelector: "test-certificate=true",
	})
	require.NoError(t, err)

	for _, secret := range secrets.Items {
		logger.Logf(t, "Deleting stale API gateway test secret %s in namespace %s before Helm install", secret.Name, namespace)
		err := h.kubernetesClient.CoreV1().Secrets(namespace).Delete(context.Background(), secret.Name, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			require.NoError(t, err)
		}
	}
}

func (h *HelmCluster) deleteStaleGatewayClass(t *testing.T, name string) {
	t.Helper()
	logger.Logf(t, "Checking for stale stable GatewayClass %s before Helm install", name)
	ctx := context.Background()
	var gatewayClass gwv1.GatewayClass
	err := h.runtimeClient.Get(ctx, client.ObjectKey{Name: name}, &gatewayClass)
	if errors.IsNotFound(err) {
		return
	}
	if isMissingRuntimeKindError(err) {
		logger.Logf(t, "Skipping stale GatewayClass cleanup for %s because the kind is not available yet: %v", name, err)
		return
	}
	require.NoError(t, err)

	if len(gatewayClass.Finalizers) > 0 {
		gatewayClassCopy := gatewayClass.DeepCopy()
		gatewayClassCopy.Finalizers = nil
		err = h.runtimeClient.Update(ctx, gatewayClassCopy)
		if err != nil && !errors.IsNotFound(err) && !errors.IsConflict(err) {
			require.NoError(t, err)
		}
	}

	logger.Logf(t, "Deleting stale GatewayClass %s before Helm install", name)
	err = h.runtimeClient.Delete(ctx, &gatewayClass)
	if err != nil && !errors.IsNotFound(err) {
		require.NoError(t, err)
	}

	retry.RunWith(h.cleanupRetryCounter(), t, func(r *retry.R) {
		var liveGatewayClass gwv1.GatewayClass
		err := h.runtimeClient.Get(ctx, client.ObjectKey{Name: name}, &liveGatewayClass)
		if errors.IsNotFound(err) {
			return
		}
		if isMissingRuntimeKindError(err) {
			return
		}
		require.NoError(r, err)
		r.Errorf("gatewayclass %s still exists after cleanup", name)
	})
}

func (h *HelmCluster) deleteCustomStaleGatewayClass(t *testing.T, name string) {
	t.Helper()
	logger.Logf(t, "Checking for stale CustomGatewayClass %s before Helm install", name)
	ctx := context.Background()
	var gatewayClass gwv1beta1.CustomGatewayClass
	err := h.runtimeClient.Get(ctx, client.ObjectKey{Name: name}, &gatewayClass)
	if errors.IsNotFound(err) {
		return
	}
	if isMissingRuntimeKindError(err) {
		logger.Logf(t, "Skipping stale CustomGatewayClass cleanup for %s because the kind is not available yet: %v", name, err)
		return
	}
	require.NoError(t, err)

	if len(gatewayClass.Finalizers) > 0 {
		gatewayClassCopy := gatewayClass.DeepCopy()
		gatewayClassCopy.Finalizers = nil
		err = h.runtimeClient.Update(ctx, gatewayClassCopy)
		if err != nil && !errors.IsNotFound(err) && !errors.IsConflict(err) {
			require.NoError(t, err)
		}
	}

	logger.Logf(t, "Deleting stale CustomGatewayClass %s before Helm install", name)
	err = h.runtimeClient.Delete(ctx, &gatewayClass)
	if err != nil && !errors.IsNotFound(err) {
		require.NoError(t, err)
	}

	retry.RunWith(h.cleanupRetryCounter(), t, func(r *retry.R) {
		var liveGatewayClass gwv1beta1.CustomGatewayClass
		err := h.runtimeClient.Get(ctx, client.ObjectKey{Name: name}, &liveGatewayClass)
		if errors.IsNotFound(err) {
			return
		}
		if isMissingRuntimeKindError(err) {
			return
		}
		require.NoError(r, err)
		r.Errorf("customgatewayclass %s still exists after cleanup", name)
	})
}

func (h *HelmCluster) deleteStaleGatewayClassConfig(t *testing.T, name string) {
	t.Helper()
	logger.Logf(t, "Checking for stale GatewayClassConfig %s before Helm install", name)
	ctx := context.Background()
	var gatewayClassConfig v1alpha1.GatewayClassConfig
	err := h.runtimeClient.Get(ctx, client.ObjectKey{Name: name}, &gatewayClassConfig)
	if errors.IsNotFound(err) {
		return
	}
	if isMissingRuntimeKindError(err) {
		logger.Logf(t, "Skipping stale GatewayClassConfig cleanup for %s because the kind is not available yet: %v", name, err)
		return
	}
	require.NoError(t, err)

	if len(gatewayClassConfig.Finalizers) > 0 {
		gatewayClassConfigCopy := gatewayClassConfig.DeepCopy()
		gatewayClassConfigCopy.Finalizers = nil
		err = h.runtimeClient.Update(ctx, gatewayClassConfigCopy)
		if err != nil && !errors.IsNotFound(err) && !errors.IsConflict(err) {
			require.NoError(t, err)
		}
	}

	logger.Logf(t, "Deleting stale GatewayClassConfig %s before Helm install", name)
	err = h.runtimeClient.Delete(ctx, &gatewayClassConfig)
	if err != nil && !errors.IsNotFound(err) {
		require.NoError(t, err)
	}

	retry.RunWith(h.cleanupRetryCounter(), t, func(r *retry.R) {
		var liveGatewayClassConfig v1alpha1.GatewayClassConfig
		err := h.runtimeClient.Get(ctx, client.ObjectKey{Name: name}, &liveGatewayClassConfig)
		if errors.IsNotFound(err) {
			return
		}
		if isMissingRuntimeKindError(err) {
			return
		}
		require.NoError(r, err)
		r.Errorf("gatewayclassconfig %s still exists after cleanup", name)
	})
}

func isMissingRuntimeKindError(err error) bool {
	if err == nil {
		return false
	}
	if errors.IsNotFound(err) || apimeta.IsNoMatchError(err) {
		return true
	}

	errText := err.Error()
	return strings.Contains(errText, "no matches for kind") ||
		strings.Contains(errText, "no kind is registered for the type") ||
		strings.Contains(errText, "unable to retrieve the complete list of server APIs") ||
		strings.Contains(errText, "no matches for gateway.networking.k8s.io/") ||
		strings.Contains(errText, "the server could not find the requested resource")
}

func isTransientKubeAPIError(err error, output string) bool {
	if err == nil {
		return false
	}

	combined := err.Error()
	if output != "" {
		combined += "\n" + output
	}

	return strings.Contains(combined, "Unable to connect to the server") ||
		strings.Contains(combined, "TLS handshake timeout") ||
		strings.Contains(combined, "Client.Timeout exceeded") ||
		strings.Contains(combined, "EOF")
}

func (h *HelmCluster) deleteStaleTestNamespaces(t *testing.T) {
	t.Helper()

	for _, namespace := range []string{"ns1", "ns2"} {
		ns, err := h.kubernetesClient.CoreV1().Namespaces().Get(context.Background(), namespace, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			continue
		}
		require.NoError(t, err)

		if len(ns.Spec.Finalizers) > 0 {
			nsCopy := ns.DeepCopy()
			nsCopy.Spec.Finalizers = nil
			_, err = h.kubernetesClient.CoreV1().Namespaces().Finalize(context.Background(), nsCopy, metav1.UpdateOptions{})
			if err != nil && !errors.IsNotFound(err) && !errors.IsConflict(err) {
				require.NoError(t, err)
			}
		}

		logger.Logf(t, "Deleting stale test namespace %s before Helm install", namespace)
		err = h.kubernetesClient.CoreV1().Namespaces().Delete(context.Background(), namespace, h.cleanupDeleteOptions())
		if err != nil && !errors.IsNotFound(err) {
			require.NoError(t, err)
		}
		time.Sleep(15 * time.Second)
		retry.RunWith(h.cleanupRetryCounter(), t, func(r *retry.R) {
			_, err := h.kubernetesClient.CoreV1().Namespaces().Get(context.Background(), namespace, metav1.GetOptions{})
			if errors.IsNotFound(err) {
				return
			}
			require.NoError(r, err)
			r.Errorf("namespace %s still exists after cleanup", namespace)
		})
	}
}

func (h *HelmCluster) deleteStaleStaticPrefixedResources(t *testing.T) {
	t.Helper()
	logger.Logf(t, "Deleting static resources in deployment, service, serviceaccount and rolebinding")
	namespace := h.helmOptions.KubectlOptions.Namespace
	resourceKinds := []string{"deployment", "service", "serviceaccount", "rolebinding"}

	for _, resourceKind := range resourceKinds {
		output, err := k8s.RunKubectlAndGetOutputE(
			t,
			h.helmOptions.KubectlOptions,
			"get",
			resourceKind,
			"-o",
			"name",
			"--ignore-not-found=true",
		)
		require.NoError(t, err)

		for _, resourceName := range splitNonEmptyLines(output) {
			parts := strings.SplitN(resourceName, "/", 2)
			if len(parts) != 2 || !strings.HasPrefix(parts[1], "static") {
				continue
			}

			logger.Logf(t, "Deleting stale %s resource %s in namespace %s before Helm install", resourceKind, resourceName, namespace)
			_, err = k8s.RunKubectlAndGetOutputE(
				t,
				h.helmOptions.KubectlOptions,
				"delete",
				resourceName,
				"--ignore-not-found=true",
				"--wait=false",
			)
			if err != nil && !strings.Contains(err.Error(), "not found") {
				require.NoError(t, err)
			}
		}
	}
}

func (h *HelmCluster) deleteStaleGatewayAndConsulAPIResources(t *testing.T) {
	t.Helper()
	logger.Logf(t, "Deleting stale Gateway API and Consul API resources before Helm install")
	apiGroups := []string{"gateway.networking.k8s.io", "consul.hashicorp.com"}

	for _, apiGroup := range apiGroups {
		resourcesOutput, err := k8s.RunKubectlAndGetOutputE(
			t,
			h.helmOptions.KubectlOptions,
			"api-resources",
			"--api-group="+apiGroup,
			"--verbs=list",
			"--namespaced=true",
			"-o",
			"name",
		)
		require.NoError(t, err)

		resources := splitNonEmptyLines(resourcesOutput)
		sort.Strings(resources)

		for _, resource := range resources {
			objectsOutput, err := k8s.RunKubectlAndGetOutputE(
				t,
				h.helmOptions.KubectlOptions,
				"get",
				resource,
				"-o",
				"name",
				"--ignore-not-found=true",
			)
			require.NoError(t, err)

			for _, objectName := range splitNonEmptyLines(objectsOutput) {
				logger.Logf(t, "Deleting stale %s resource %s before Helm install", apiGroup, objectName)

				_, _ = k8s.RunKubectlAndGetOutputE(
					t,
					h.helmOptions.KubectlOptions,
					"patch",
					objectName,
					"--type=merge",
					"-p",
					`{"metadata":{"finalizers":[]}}`,
				)

				_, err = k8s.RunKubectlAndGetOutputE(
					t,
					h.helmOptions.KubectlOptions,
					"delete",
					objectName,
					"--ignore-not-found=true",
					"--wait=false",
				)
				if err != nil && !strings.Contains(err.Error(), "not found") {
					require.NoError(t, err)
				}
			}
		}
	}
}

func splitNonEmptyLines(output string) []string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		result = append(result, line)
	}
	return result
}

func (h *HelmCluster) deleteStaleConsulOwnedCRDs(t *testing.T) {
	t.Helper()

	logger.Logf(t, "Delete stale Gateway API CRDs with Helm ownership annotations to prevent install conflicts")
	// These cluster-scoped CRDs can keep stale Helm ownership annotations from
	// earlier acceptance releases. Limit cleanup to Consul-owned CRDs and only
	// include Gateway API CRDs on OpenShift versions where tests install them.
	crds := helpers.OpenShiftCleanupCRDs(!h.isOpenShiftGTE419)
	//sort.Strings(crds)

	for _, crd := range crds {
		var ownerRelease string
		retry.RunWith(h.cleanupRetryCounter(), t, func(r *retry.R) {
			var output string
			var err error
			output, err = k8s.RunKubectlAndGetOutputE(
				r,
				h.helmOptions.KubectlOptions,
				"get", "crd", crd,
				"--ignore-not-found=true",
				"-o", "jsonpath={.metadata.annotations.meta\\.helm\\.sh/release-name}",
			)
			if err != nil {
				if isTransientKubeAPIError(err, output) {
					r.Errorf("transient kube API error checking stale CRD %s ownership: %s", crd, strings.TrimSpace(err.Error()+"\n"+output))
					return
				}
				require.NoError(r, err)
			}

			ownerRelease = strings.TrimSpace(output)
		})

		if ownerRelease != "" && ownerRelease != h.releaseName {
			logger.Logf(t, "Deleting stale CRD %s owned by release %s before installing release %s", crd, ownerRelease, h.releaseName)
			_, err := k8s.RunKubectlAndGetOutputE(t, h.helmOptions.KubectlOptions, "delete", "crd", crd, "--ignore-not-found=true")
			require.NoError(t, err)
		}
	}
}

func (h *HelmCluster) deleteStaleHelmReleases(t *testing.T) {
	t.Helper()

	output, err := helm.RunHelmCommandAndGetOutputE(t, h.helmOptions, "list", "--all", "--output", "json")
	require.NoError(t, err)

	var releases []struct {
		Name  string `json:"name"`
		Chart string `json:"chart"`
	}
	require.NoError(t, json.Unmarshal([]byte(output), &releases))

	for _, release := range releases {
		if !strings.Contains(release.Chart, "consul") {
			continue
		}

		logger.Logf(t, "Deleting stale Helm release %s in namespace %s before install", release.Name, h.helmOptions.KubectlOptions.Namespace)
		err := h.uninstallReleaseNoHooks(t, release.Name)
		if err != nil && isGatewayCleanupAlreadyExistsError(err) {
			h.deleteGatewayCleanupJobIfExistsForRelease(t, release.Name)
			err = h.uninstallReleaseNoHooks(t, release.Name)
		}
		if err != nil && isGatewayResourcesAlreadyExistsError(err) {
			h.deleteGatewayResourcesJobIfExistsForRelease(t, release.Name)
			err = h.uninstallReleaseNoHooks(t, release.Name)
		}
		if err != nil && !strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "already deleted") {
			require.NoError(t, err)
		}
	}
}

func (h *HelmCluster) uninstallReleaseNoHooks(t *testing.T, releaseName string) error {
	_, err := helm.RunHelmCommandAndGetOutputE(t, h.helmOptions,
		"uninstall", releaseName,
		"--no-hooks",
		"--timeout", "30s",
	)
	return err
}

func fastDeleteOptions() metav1.DeleteOptions {
	var gracePeriod int64 = 0
	background := metav1.DeletePropagationBackground
	return metav1.DeleteOptions{
		GracePeriodSeconds: &gracePeriod,
		PropagationPolicy:  &background,
	}
}

func (h *HelmCluster) cleanupDeleteOptions() metav1.DeleteOptions {
	if h.enableOpenshift {
		return fastDeleteOptions()
	}
	return metav1.DeleteOptions{}
}

func (h *HelmCluster) cleanupRetryCounter() *retry.Counter {
	if h.enableOpenshift {
		// OpenShift interrupt cleanup should be best-effort and quick.
		return &retry.Counter{Wait: openShiftCleanupWait, Count: openShiftCleanupCount}
	}
	return &retry.Counter{Wait: retryWaitDuration, Count: retryMaxCount}
}

func (h *HelmCluster) deleteStaleLabeledResources(t *testing.T) {
	t.Helper()
	logger.Logf(t, "Deleting stale Consul resources with label selector %s before Helm install", staleConsulLabelSelector)

	deleteList := func(err error) {
		if err != nil && !errors.IsNotFound(err) {
			require.NoError(t, err)
		}
	}

	listOptions := metav1.ListOptions{LabelSelector: staleConsulLabelSelector}
	namespace := h.helmOptions.KubectlOptions.Namespace

	var gracePeriod int64 = 0
	deleteList(h.kubernetesClient.CoreV1().Pods(namespace).DeleteCollection(context.Background(), metav1.DeleteOptions{GracePeriodSeconds: &gracePeriod}, listOptions))
	deleteList(h.kubernetesClient.AppsV1().Deployments(namespace).DeleteCollection(context.Background(), metav1.DeleteOptions{}, listOptions))
	deleteList(h.kubernetesClient.AppsV1().ReplicaSets(namespace).DeleteCollection(context.Background(), metav1.DeleteOptions{}, listOptions))
	deleteList(h.kubernetesClient.AppsV1().StatefulSets(namespace).DeleteCollection(context.Background(), metav1.DeleteOptions{}, listOptions))
	deleteList(h.kubernetesClient.AppsV1().DaemonSets(namespace).DeleteCollection(context.Background(), metav1.DeleteOptions{}, listOptions))
	deleteList(h.kubernetesClient.CoreV1().PersistentVolumeClaims(namespace).DeleteCollection(context.Background(), metav1.DeleteOptions{}, listOptions))
	deleteList(h.kubernetesClient.CoreV1().ServiceAccounts(namespace).DeleteCollection(context.Background(), metav1.DeleteOptions{}, listOptions))
	deleteList(h.kubernetesClient.RbacV1().Roles(namespace).DeleteCollection(context.Background(), metav1.DeleteOptions{}, listOptions))
	deleteList(h.kubernetesClient.RbacV1().RoleBindings(namespace).DeleteCollection(context.Background(), metav1.DeleteOptions{}, listOptions))
	deleteList(h.kubernetesClient.BatchV1().Jobs(namespace).DeleteCollection(context.Background(), h.cleanupDeleteOptions(), listOptions))
	deleteList(h.kubernetesClient.CoreV1().ConfigMaps(namespace).DeleteCollection(context.Background(), metav1.DeleteOptions{}, listOptions))
	deleteList(h.kubernetesClient.CoreV1().Secrets(namespace).DeleteCollection(context.Background(), metav1.DeleteOptions{}, listOptions))
	deleteList(h.kubernetesClient.RbacV1().ClusterRoles().DeleteCollection(context.Background(), metav1.DeleteOptions{}, listOptions))
	deleteList(h.kubernetesClient.RbacV1().ClusterRoleBindings().DeleteCollection(context.Background(), metav1.DeleteOptions{}, listOptions))
	deleteList(h.kubernetesClient.AdmissionregistrationV1().MutatingWebhookConfigurations().DeleteCollection(context.Background(), metav1.DeleteOptions{}, listOptions))
	deleteList(h.kubernetesClient.AdmissionregistrationV1().ValidatingWebhookConfigurations().DeleteCollection(context.Background(), metav1.DeleteOptions{}, listOptions))

	services, err := h.kubernetesClient.CoreV1().Services(namespace).List(context.Background(), listOptions)
	require.NoError(t, err)
	for _, service := range services.Items {
		deleteList(h.deleteServiceWithFinalizerCleanup(context.Background(), namespace, &service, h.cleanupDeleteOptions()))
	}

	err = h.runtimeClient.DeleteAllOf(context.Background(), &gwv1.GatewayClass{}, client.MatchingLabels{"chart": "consul-helm"})
	if err != nil && !isMissingRuntimeKindError(err) {
		require.NoError(t, err)
	}
	err = h.runtimeClient.DeleteAllOf(context.Background(), &v1alpha1.GatewayClassConfig{}, client.MatchingLabels{"chart": "consul-helm"})
	if err != nil && !isMissingRuntimeKindError(err) {
		require.NoError(t, err)
	}

	mutatingWebhooks, err := h.kubernetesClient.AdmissionregistrationV1().MutatingWebhookConfigurations().List(context.Background(), listOptions)
	require.NoError(t, err)
	for _, webhook := range mutatingWebhooks.Items {
		webhook.SetFinalizers(nil)
		_, err := h.kubernetesClient.AdmissionregistrationV1().MutatingWebhookConfigurations().Update(context.Background(), &webhook, metav1.UpdateOptions{})
		deleteList(err)
	}

	validatingWebhooks, err := h.kubernetesClient.AdmissionregistrationV1().ValidatingWebhookConfigurations().List(context.Background(), listOptions)
	require.NoError(t, err)
	for _, webhook := range validatingWebhooks.Items {
		webhook.SetFinalizers(nil)
		_, err := h.kubernetesClient.AdmissionregistrationV1().ValidatingWebhookConfigurations().Update(context.Background(), &webhook, metav1.UpdateOptions{})
		deleteList(err)
	}
	retry.RunWith(&retry.Counter{Wait: 2 * time.Second, Count: 60}, t, func(r *retry.R) {
		pods, err := h.kubernetesClient.CoreV1().Pods(namespace).List(context.Background(), listOptions)
		require.NoError(r, err)
		if len(pods.Items) > 0 {
			var podNames []string
			for _, pod := range pods.Items {
				podNames = append(podNames, pod.Name)
			}
			r.Errorf("stale Consul pods still present after cleanup: %s", strings.Join(podNames, ", "))
		}
	})
}

func (h *HelmCluster) deleteServiceWithFinalizerCleanup(ctx context.Context, namespace string, service *corev1.Service, deleteOpts metav1.DeleteOptions) error {
	if service == nil {
		return nil
	}

	serviceName := service.Name
	liveService, err := h.kubernetesClient.CoreV1().Services(namespace).Get(ctx, serviceName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	if len(liveService.Finalizers) > 0 {
		serviceCopy := liveService.DeepCopy()
		serviceCopy.Finalizers = nil
		if _, err := h.kubernetesClient.CoreV1().Services(namespace).Update(ctx, serviceCopy, metav1.UpdateOptions{}); err != nil {
			if isIgnorableServiceCleanupError(err) {
				return nil
			}
			return err
		}
	}

	err = h.kubernetesClient.CoreV1().Services(namespace).Delete(ctx, serviceName, deleteOpts)
	if err != nil {
		if isIgnorableServiceCleanupError(err) {
			return nil
		}
		return err
	}
	return nil
}

func isIgnorableServiceCleanupError(err error) bool {
	if err == nil {
		return false
	}

	if errors.IsNotFound(err) || errors.IsConflict(err) || errors.IsInvalid(err) {
		return true
	}

	errText := err.Error()
	return strings.Contains(errText, "StorageError: invalid object") || strings.Contains(errText, "Precondition failed: UID in precondition")
}

func (h *HelmCluster) deleteStaleNamedSecretsForRelease(t require.TestingT, releaseName string) {
	namespace := h.helmOptions.KubectlOptions.Namespace
	for _, secretName := range staleSecretNamesForRelease(releaseName) {
		err := h.kubernetesClient.CoreV1().Secrets(namespace).Delete(context.Background(), secretName, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			require.NoError(t, err)
		}
	}
}

func staleSecretNamesForRelease(releaseName string) []string {
	if releaseName == "" {
		return []string{
			"consul-bootstrap-acl-token",
			"consul-enterprise-license-acl-token",
		}
	}

	return []string{
		releaseName + "-consul-bootstrap-acl-token",
		releaseName + "-consul-enterprise-license-acl-token",
	}
}
