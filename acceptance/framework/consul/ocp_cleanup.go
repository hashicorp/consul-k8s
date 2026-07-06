package consul

import (
	"context"
	"encoding/json"
	"fmt"
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
	// Clear finalizers on all resources in the consul namespace first so that
	// subsequent deletes are not blocked by stuck finalizer handlers (e.g.
	// connect-inject webhook that is already gone).
	h.clearNamespaceResourceFinalizers(t, h.helmOptions.KubectlOptions.Namespace)
	// Kill any running gateway-resources / gateway-cleanup Jobs before we clean
	// up CRDs. These Jobs call kubectl-apply to install CRDs with the old release's
	// annotation and will undo our CRD cleanup if left running.
	h.deleteAllGatewayJobsInNamespace(t)
	h.deleteStaleTestNamespaces(t)
	h.deleteStaleNamedSecretsForRelease(t, h.releaseName)
	h.deleteGatewayHookJobsIfExistsForRelease(t, h.releaseName)
	h.deleteStaleGatewayAndConsulAPIResources(t)
	if strings.HasPrefix(t.Name(), "TestAPIGateway") {
		logger.Logf(t, "Deleting stale Gateway API and Consul API resources before Helm install for API gateway test %s", t.Name())
		h.deleteStaleAPIGatewayTestClusterResources(t)
	}
	h.deleteStaleHelmReleases(t)
	h.deleteStaleHelmManagedResources(t)
	h.deleteStaleConsulOwnedCRDs(t)
	h.deleteStaleStaticPrefixedResources(t)
	h.deleteStaleLabeledResources(t)
}

// clearNamespaceResourceFinalizers removes finalizers from all resources in the
// given namespace so that subsequent deletes are not blocked by stale finalizer
// handlers (e.g. the connect-inject webhook may already be gone, leaving pods or
// services stuck with an unreachable finalizer). This is called at the very start
// of cleanupOpenShiftBeforeInstall, before any deletes are issued.
func (h *HelmCluster) clearNamespaceResourceFinalizers(t *testing.T, namespace string) {
	t.Helper()
	logger.Logf(t, "Clearing stale finalizers on resources in namespace %s before Helm install", namespace)
	ctx := context.Background()

	// Pods
	pods, err := h.kubernetesClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err == nil {
		for i := range pods.Items {
			if len(pods.Items[i].Finalizers) == 0 {
				continue
			}
			podCopy := pods.Items[i].DeepCopy()
			podCopy.Finalizers = nil
			if _, patchErr := h.kubernetesClient.CoreV1().Pods(namespace).Update(ctx, podCopy, metav1.UpdateOptions{}); patchErr != nil && !errors.IsNotFound(patchErr) && !errors.IsConflict(patchErr) {
				logger.Logf(t, "warning: failed to clear finalizers on pod %s: %s", podCopy.Name, patchErr)
			}
		}
	}

	// Services
	services, err := h.kubernetesClient.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
	if err == nil {
		for i := range services.Items {
			if len(services.Items[i].Finalizers) == 0 {
				continue
			}
			svcCopy := services.Items[i].DeepCopy()
			svcCopy.Finalizers = nil
			if _, patchErr := h.kubernetesClient.CoreV1().Services(namespace).Update(ctx, svcCopy, metav1.UpdateOptions{}); patchErr != nil && !errors.IsNotFound(patchErr) && !errors.IsConflict(patchErr) {
				logger.Logf(t, "warning: failed to clear finalizers on service %s: %s", svcCopy.Name, patchErr)
			}
		}
	}

	// ConfigMaps
	configMaps, err := h.kubernetesClient.CoreV1().ConfigMaps(namespace).List(ctx, metav1.ListOptions{})
	if err == nil {
		for i := range configMaps.Items {
			if len(configMaps.Items[i].Finalizers) == 0 {
				continue
			}
			cmCopy := configMaps.Items[i].DeepCopy()
			cmCopy.Finalizers = nil
			if _, patchErr := h.kubernetesClient.CoreV1().ConfigMaps(namespace).Update(ctx, cmCopy, metav1.UpdateOptions{}); patchErr != nil && !errors.IsNotFound(patchErr) && !errors.IsConflict(patchErr) {
				logger.Logf(t, "warning: failed to clear finalizers on configmap %s: %s", cmCopy.Name, patchErr)
			}
		}
	}

	// Secrets
	secrets, err := h.kubernetesClient.CoreV1().Secrets(namespace).List(ctx, metav1.ListOptions{})
	if err == nil {
		for i := range secrets.Items {
			if len(secrets.Items[i].Finalizers) == 0 {
				continue
			}
			secCopy := secrets.Items[i].DeepCopy()
			secCopy.Finalizers = nil
			if _, patchErr := h.kubernetesClient.CoreV1().Secrets(namespace).Update(ctx, secCopy, metav1.UpdateOptions{}); patchErr != nil && !errors.IsNotFound(patchErr) && !errors.IsConflict(patchErr) {
				logger.Logf(t, "warning: failed to clear finalizers on secret %s: %s", secCopy.Name, patchErr)
			}
		}
	}

	// Deployments
	deployments, err := h.kubernetesClient.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err == nil {
		for i := range deployments.Items {
			if len(deployments.Items[i].Finalizers) == 0 {
				continue
			}
			dCopy := deployments.Items[i].DeepCopy()
			dCopy.Finalizers = nil
			if _, patchErr := h.kubernetesClient.AppsV1().Deployments(namespace).Update(ctx, dCopy, metav1.UpdateOptions{}); patchErr != nil && !errors.IsNotFound(patchErr) && !errors.IsConflict(patchErr) {
				logger.Logf(t, "warning: failed to clear finalizers on deployment %s: %s", dCopy.Name, patchErr)
			}
		}
	}

	// StatefulSets
	statefulSets, err := h.kubernetesClient.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{})
	if err == nil {
		for i := range statefulSets.Items {
			if len(statefulSets.Items[i].Finalizers) == 0 {
				continue
			}
			ssCopy := statefulSets.Items[i].DeepCopy()
			ssCopy.Finalizers = nil
			if _, patchErr := h.kubernetesClient.AppsV1().StatefulSets(namespace).Update(ctx, ssCopy, metav1.UpdateOptions{}); patchErr != nil && !errors.IsNotFound(patchErr) && !errors.IsConflict(patchErr) {
				logger.Logf(t, "warning: failed to clear finalizers on statefulset %s: %s", ssCopy.Name, patchErr)
			}
		}
	}
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

	logger.Logf(t, "Unconditionally deleting all consul CRDs before Helm install to eliminate ownership annotation races")
	// On OpenShift tests use --skip-crds, so Helm never manages CRDs directly.
	// CRDs are installed/updated by the gateway-resources Job of each release.
	// Because the Job Pod races with cleanup (it may write the CRD annotation
	// AFTER we check it), annotation-based detection is unreliable. The safest
	// approach is to delete ALL consul CRDs unconditionally; the new install's
	// gateway-resources Job will recreate them with the correct release annotation.
	crds := helpers.OpenShiftCleanupCRDs(!h.isOpenShiftGTE419)

	var deletedCRDs []string
	for _, crd := range crds {
		// Clear any CRs for this CRD first to unblock deletion.
		h.clearStaleCRDObjectFinalizers(t, crd)

		out, err := k8s.RunKubectlAndGetOutputE(t, h.helmOptions.KubectlOptions,
			"delete", "crd", crd, "--ignore-not-found=true", "--wait=false")
		if err != nil && !strings.Contains(err.Error(), "not found") {
			logger.Logf(t, "warning: failed to delete CRD %s: %s (output: %s)", crd, err, out)
			continue
		}
		deletedCRDs = append(deletedCRDs, crd)
	}

	// Wait for every CRD to be fully gone before returning. A CRD still in
	// Terminating state when Helm runs will cause "invalid ownership metadata".
	for _, crd := range deletedCRDs {
		retry.RunWith(&retry.Timer{Timeout: 3 * time.Minute, Wait: 5 * time.Second}, t, func(r *retry.R) {
			out, err := k8s.RunKubectlAndGetOutputE(r, h.helmOptions.KubectlOptions,
				"get", "crd", crd, "--ignore-not-found=true", "-o", "name")
			if err != nil {
				r.Errorf("transient error waiting for CRD %s to be deleted: %s", crd, err)
				return
			}
			if strings.TrimSpace(out) != "" {
				// Still present or Terminating; clear finalizers and retry.
				h.clearStaleCRDObjectFinalizers(t, crd)
				r.Errorf("CRD %s still present, waiting for deletion to complete", crd)
			}
		})
		logger.Logf(t, "CRD %s fully deleted", crd)
	}
}

// clearStaleCRDObjectFinalizers removes finalizers from any lingering custom
// resources of the given CRD so that deleting the CRD does not hang on the
// apiextensions customresourcecleanup finalizer. deleteStaleGatewayAndConsulAPIResources
// only clears finalizers on namespaced resources, so cluster-scoped CRs (such as
// customgatewayclasses) would otherwise leave the CRD stuck in Terminating.
func (h *HelmCluster) clearStaleCRDObjectFinalizers(t *testing.T, crd string) {
	t.Helper()

	// List custom resources for this CRD. Cluster-scoped resources (e.g.
	// customgatewayclasses) are returned regardless of namespace; namespaced
	// resources are scoped to the install namespace, matching how
	// deleteStaleGatewayAndConsulAPIResources operates.
	output, err := k8s.RunKubectlAndGetOutputE(
		t,
		h.helmOptions.KubectlOptions,
		"get", crd,
		"-o", "name",
		"--ignore-not-found=true",
	)
	if err != nil {
		// The CRD may already be gone or have no objects; nothing to clear.
		return
	}

	for _, objectName := range splitNonEmptyLines(output) {
		logger.Logf(t, "Clearing finalizers on %s before deleting stale CRD %s", objectName, crd)
		_, _ = k8s.RunKubectlAndGetOutputE(
			t,
			h.helmOptions.KubectlOptions,
			"patch", objectName,
			"--type=merge",
			"-p", `{"metadata":{"finalizers":[]}}`,
		)
	}
}

// deleteStaleHelmManagedResources deletes ConfigMaps and Secrets in the consul namespace
// that carry a "meta.helm.sh/release-name" annotation belonging to a *different* Helm
// release than the current one. These are left behind when a previous test's Helm release
// was in a non-deployed state (failed, pending-install, etc.) and therefore was not found
// by "helm list" (which only returns deployed releases when --all is unsupported). Their
// presence causes the next "helm install" to fail with "invalid ownership metadata".
func (h *HelmCluster) deleteStaleHelmManagedResources(t *testing.T) {
	t.Helper()
	namespace := h.helmOptions.KubectlOptions.Namespace

	configMaps, err := h.kubernetesClient.CoreV1().ConfigMaps(namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		logger.Logf(t, "warning: failed to list ConfigMaps for stale Helm resource cleanup: %s", err)
	} else {
		for _, cm := range configMaps.Items {
			relName, ok := cm.Annotations["meta.helm.sh/release-name"]
			if !ok || relName == h.releaseName {
				continue
			}
			logger.Logf(t, "Deleting stale Helm-managed ConfigMap %s (owned by release %s, current is %s)", cm.Name, relName, h.releaseName)
			delErr := h.kubernetesClient.CoreV1().ConfigMaps(namespace).Delete(context.Background(), cm.Name, metav1.DeleteOptions{})
			if delErr != nil && !errors.IsNotFound(delErr) {
				logger.Logf(t, "warning: failed to delete stale ConfigMap %s: %s", cm.Name, delErr)
			}
		}
	}

	secrets, err := h.kubernetesClient.CoreV1().Secrets(namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		logger.Logf(t, "warning: failed to list Secrets for stale Helm resource cleanup: %s", err)
	} else {
		for _, sec := range secrets.Items {
			relName, ok := sec.Annotations["meta.helm.sh/release-name"]
			if !ok || relName == h.releaseName {
				continue
			}
			logger.Logf(t, "Deleting stale Helm-managed Secret %s (owned by release %s, current is %s)", sec.Name, relName, h.releaseName)
			delErr := h.kubernetesClient.CoreV1().Secrets(namespace).Delete(context.Background(), sec.Name, metav1.DeleteOptions{})
			if delErr != nil && !errors.IsNotFound(delErr) {
				logger.Logf(t, "warning: failed to delete stale Secret %s: %s", sec.Name, delErr)
			}
		}
	}
}

func (h *HelmCluster) deleteStaleHelmReleases(t *testing.T) {
	t.Helper()

	output, err := helm.RunHelmCommandAndGetOutputE(t, h.helmOptions, "list", "--all", "--output", "json")
	if err != nil && strings.Contains(err.Error(), "unknown flag: --all") {
		// Some Helm versions do not support the --all long flag; fall back to listing deployed releases only.
		logger.Logf(t, "helm list --all not supported, falling back to listing deployed releases only")
		output, err = helm.RunHelmCommandAndGetOutputE(t, h.helmOptions, "list", "--output", "json")
	}
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

func (h *HelmCluster) deleteGatewayCleanupJobIfExistsForRelease(t require.TestingT, releaseName string) {
	namespace := h.helmOptions.KubectlOptions.Namespace
	jobName := fmt.Sprintf("%s-consul-gateway-cleanup", releaseName)

	err := h.kubernetesClient.BatchV1().Jobs(namespace).Delete(context.Background(), jobName, h.cleanupDeleteOptions())
	if err != nil && !errors.IsNotFound(err) {
		require.NoError(t, err)
	}
}

func (h *HelmCluster) deleteGatewayResourcesJobIfExistsForRelease(t require.TestingT, releaseName string) {
	namespace := h.helmOptions.KubectlOptions.Namespace
	jobName := fmt.Sprintf("%s-consul-gateway-resources", releaseName)

	err := h.kubernetesClient.BatchV1().Jobs(namespace).Delete(context.Background(), jobName, h.cleanupDeleteOptions())
	if err != nil && !errors.IsNotFound(err) {
		require.NoError(t, err)
	}
}

func (h *HelmCluster) deleteServerACLInitCleanupJobIfExistsForRelease(t require.TestingT, releaseName string) {
	namespace := h.helmOptions.KubectlOptions.Namespace
	jobName := fmt.Sprintf("%s-consul-server-acl-init-cleanup", releaseName)

	err := h.kubernetesClient.BatchV1().Jobs(namespace).Delete(context.Background(), jobName, h.cleanupDeleteOptions())
	if err != nil && !errors.IsNotFound(err) {
		require.NoError(t, err)
	}
}

func (h *HelmCluster) deleteGatewayHookJobsIfExistsForRelease(t require.TestingT, releaseName string) {
	h.deleteGatewayCleanupJobIfExistsForRelease(t, releaseName)
	h.deleteGatewayResourcesJobIfExistsForRelease(t, releaseName)
	h.deleteServerACLInitCleanupJobIfExistsForRelease(t, releaseName)
}

func isGatewayCleanupAlreadyExistsError(err error) bool {
	if err == nil {
		return false
	}
	errText := err.Error()
	return strings.Contains(errText, "gateway-cleanup") && strings.Contains(errText, "already exists")
}

func isGatewayResourcesAlreadyExistsError(err error) bool {
	if err == nil {
		return false
	}
	errText := err.Error()
	return strings.Contains(errText, "gateway-resources") && strings.Contains(errText, "already exists")
}

func isServerACLInitCleanupAlreadyExistsError(err error) bool {
	if err == nil {
		return false
	}
	errText := err.Error()
	return strings.Contains(errText, "server-acl-init-cleanup") && strings.Contains(errText, "already exists")
}

func isHelmOwnershipConflictError(err error) bool {
	if err == nil {
		return false
	}
	errText := err.Error()
	return strings.Contains(errText, "cannot be imported into the current release") ||
		strings.Contains(errText, "invalid ownership metadata")
}

// deleteAllGatewayJobsInNamespace deletes every gateway-resources and
// gateway-cleanup Job in the consul namespace, regardless of which Helm release
// owns them. These Jobs run kubectl-apply to install CRDs and will undo any
// stale-CRD cleanup if left running in the background. This must be called both
// in pre-install cleanup (so no Job starts writing after cleanup) and inside the
// install-retry loop when an ownership conflict is detected.
func (h *HelmCluster) deleteAllGatewayJobsInNamespace(t *testing.T) {
	t.Helper()
	namespace := h.helmOptions.KubectlOptions.Namespace
	gatewayJobPatterns := []string{
		"consul-gateway-resources",
		"consul-gateway-cleanup",
		"consul-gateway-resources-custom",
		"consul-gateway-cleanup-custom",
	}

	jobs, err := h.kubernetesClient.BatchV1().Jobs(namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		logger.Logf(t, "warning: failed to list Jobs for gateway job cleanup: %s", err)
		return
	}
	for _, job := range jobs.Items {
		for _, pattern := range gatewayJobPatterns {
			if strings.Contains(job.Name, pattern) {
				logger.Logf(t, "Deleting stale gateway Job %s in namespace %s to stop it from recreating CRDs", job.Name, namespace)
				_ = h.kubernetesClient.BatchV1().Jobs(namespace).Delete(
					context.Background(), job.Name, fastDeleteOptions(),
				)
				// Also delete any Pods created by this Job so they stop running kubectl-apply.
				pods, podErr := h.kubernetesClient.CoreV1().Pods(namespace).List(
					context.Background(),
					metav1.ListOptions{LabelSelector: "job-name=" + job.Name},
				)
				if podErr == nil {
					for _, pod := range pods.Items {
						_ = h.kubernetesClient.CoreV1().Pods(namespace).Delete(
							context.Background(), pod.Name, fastDeleteOptions(),
						)
					}
				}
				break
			}
		}
	}
}

func isRetryableHelmInstallError(err error) bool {
	if err == nil {
		return false
	}

	errText := strings.ToLower(err.Error())
	retryableSubstrings := []string{
		"tls handshake timeout",
		"connection reset by peer",
		"connection refused",
		"i/o timeout",
		"context deadline exceeded",
		"unexpected eof",
		"http2: client connection lost",
		// Helm returns this when --timeout fires (helm upgrade --install --timeout Nm)
		"timed out waiting for the condition",
		// Server-Side Apply field manager conflicts arise when CRDs or other
		// resources carry managed fields from a previous Helm release name.
		// These are transient w.r.t. the test lifecycle and are resolved by
		// the --force-conflicts flag on subsequent retries.
		"conflict occurred while applying object",
		"apply failed with",
	}

	for _, s := range retryableSubstrings {
		if strings.Contains(errText, s) {
			return true
		}
	}

	return false
}
