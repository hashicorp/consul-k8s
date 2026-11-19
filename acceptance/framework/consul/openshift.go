// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package consul

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gruntwork-io/terratest/modules/helm"
	terratestLogger "github.com/gruntwork-io/terratest/modules/logger"
	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	customgwv1beta1 "github.com/hashicorp/consul-k8s/control-plane/gateway07/gateway-api-0.7.1-custom/apis/v1beta1"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/hashicorp/go-version"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func isOpenShiftReleaseStorageTimeout(message string) bool {
	message = strings.ToLower(message)
	if !strings.Contains(message, "create: failed to create") || !strings.Contains(message, "post secrets") {
		return false
	}
	for _, marker := range []string{
		"unable to return a response in the time allotted",
		"etcdserver: request timed out",
		"i/o timeout",
		"tls handshake timeout",
		"context deadline exceeded",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func installOpenShiftWithStorageRetry(t *testing.T, options *helm.Options, releaseName string, install func() error) error {
	t.Helper()
	for attempt := 1; ; attempt++ {
		err := install()
		if err == nil {
			return nil
		}
		pendingAfterTimeout := attempt > 1 && strings.Contains(err.Error(), "another operation (install/upgrade/rollback) is in progress")
		if attempt == 3 || (!isOpenShiftReleaseStorageTimeout(err.Error()) && !pendingAfterTimeout) {
			return err
		}
		// Helm persists its pending release before applying any manifests. A
		// timed-out storage write can still complete and leave an orphaned lock.
		t.Logf("Helm release storage timed out; clearing only release %s before install attempt %d/3: %v", releaseName, attempt+1, err)
		time.Sleep(5 * time.Second)
		output, cleanupErr := helm.RunHelmCommandAndGetOutputE(t, options, "uninstall", releaseName, "--no-hooks", "--timeout", "2m")
		if cleanupErr != nil && !strings.Contains(output, "release: not found") {
			return fmt.Errorf("cleaning release %s after a storage timeout: %w\noutput: %s", releaseName, cleanupErr, output)
		}
	}
}

func runOpenShiftAPIOperation(ctx context.Context, operation func(context.Context) error) error {
	var lastError error
	err := wait.PollUntilContextCancel(ctx, openShiftAPIRetry().Wait, true, func(ctx context.Context) (bool, error) {
		requestContext, cancel := context.WithTimeout(ctx, environment.OpenShiftAPIRequestTimeout)
		defer cancel()
		err := operation(requestContext)
		if err == nil {
			return true, nil
		}
		if k8s.IsRetryableOpenShiftAPIError(err) || apierrors.IsConflict(err) {
			lastError = err
			return false, nil
		}
		return false, err
	})
	if err != nil && lastError != nil {
		return fmt.Errorf("OpenShift API operation failed (last error: %v): %w", lastError, err)
	}
	return err
}

func (h *HelmCluster) cleanupOpenShiftCustomGatewayClasses(t *testing.T) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), openShiftAPIRetry().Timeout)
	defer cancel()
	var classes customgwv1beta1.CustomGatewayClassList
	err := runOpenShiftAPIOperation(ctx, func(ctx context.Context) error {
		err := h.runtimeClient.List(ctx, &classes)
		if apierrors.IsNotFound(err) || apimeta.IsNoMatchError(err) {
			return nil
		}
		return err
	})
	if err != nil {
		return fmt.Errorf("listing OpenShift custom gateway classes: %w", err)
	}
	for _, gatewayClass := range classes.Items {
		if !h.ownsOpenShiftCustomGatewayClass(&gatewayClass) {
			continue
		}
		t.Logf("Cleaning custom gateway class %s before uninstalling release %s", gatewayClass.Name, h.releaseName)
		err := runOpenShiftAPIOperation(ctx, func(ctx context.Context) error {
			var current customgwv1beta1.CustomGatewayClass
			if err := h.runtimeClient.Get(ctx, client.ObjectKeyFromObject(&gatewayClass), &current); err != nil {
				return client.IgnoreNotFound(err)
			}
			if !h.ownsOpenShiftCustomGatewayClass(&current) {
				return nil
			}
			if len(current.Finalizers) != 0 {
				current.Finalizers = nil
				if err := h.runtimeClient.Update(ctx, &current); err != nil {
					return client.IgnoreNotFound(err)
				}
			}
			return client.IgnoreNotFound(h.runtimeClient.Delete(ctx, &current))
		})
		if err != nil {
			return fmt.Errorf("cleaning OpenShift custom gateway class %s: %w", gatewayClass.Name, err)
		}
	}
	return nil
}

func (h *HelmCluster) ownsOpenShiftCustomGatewayClass(gatewayClass *customgwv1beta1.CustomGatewayClass) bool {
	release := gatewayClass.Labels["release"]
	return release == h.releaseName || (release == "" &&
		gatewayClass.Labels["consul.hashicorp.com/acceptance-namespace"] == h.helmOptions.KubectlOptions.Namespace)
}

func isOpenShiftReleaseNotFound(releaseName, output string, err error) bool {
	if err == nil {
		return false
	}
	message := output + "\n" + err.Error()
	return strings.Contains(message, "uninstall: Release not loaded: "+releaseName+": release: not found") ||
		strings.Contains(message, "uninstall: Release not loaded: "+releaseName+": not found")
}

func uninstallOpenShiftRelease(t *testing.T, options *helm.Options, releaseName string) error {
	t.Helper()
	for attempt := 1; ; attempt++ {
		output, err := helm.RunHelmCommandAndGetOutputE(t, options, "uninstall", releaseName, "--keep-history", "--timeout", "2m", "--debug")
		if err == nil {
			return nil
		}
		if isOpenShiftReleaseNotFound(releaseName, output, err) {
			t.Logf("Release %s is already absent; continuing release-owned resource cleanup", releaseName)
			return nil
		}
		if attempt == 3 || (!k8s.IsRetryableOpenShiftAPIError(err) && !k8s.IsRetryableOpenShiftAPIError(errors.New(output))) {
			return fmt.Errorf("uninstalling OpenShift release %s: %w\noutput: %s", releaseName, err, output)
		}
		t.Logf("Transient OpenShift API error during release %s uninstall; retrying attempt %d/3: %v", releaseName, attempt+1, err)
		time.Sleep(openShiftAPIRetry().Wait)
	}
}

func (h *HelmCluster) cleanupOpenShiftOrphanedCRDs(t *testing.T) error {
	t.Helper()
	namespace := h.helmOptions.KubectlOptions.Namespace
	ctx, cancel := context.WithTimeout(context.Background(), openShiftAPIRetry().Timeout)
	defer cancel()
	var testNamespace corev1.Namespace
	if err := runOpenShiftAPIOperation(ctx, func(ctx context.Context) error {
		return h.runtimeClient.Get(ctx, client.ObjectKey{Name: namespace}, &testNamespace)
	}); err != nil {
		return fmt.Errorf("checking OpenShift acceptance namespace %s: %w", namespace, err)
	}
	if testNamespace.Labels["consul.hashicorp.com/acceptance-tests"] != "true" ||
		namespace == metav1.NamespaceDefault || namespace == "openshift" || namespace == "dedicated-admin" ||
		strings.HasPrefix(namespace, "kube-") || strings.HasPrefix(namespace, "openshift-") {
		return nil
	}

	var pods corev1.PodList
	if err := runOpenShiftAPIOperation(ctx, func(ctx context.Context) error {
		return h.runtimeClient.List(ctx, &pods, client.MatchingLabels{"app": "consul"})
	}); err != nil {
		return fmt.Errorf("checking for active Consul installations before CRD recovery: %w", err)
	}
	if len(pods.Items) != 0 {
		t.Log("Skipping orphaned Consul CRD recovery because Consul pods still exist")
		return nil
	}

	var namespaces corev1.NamespaceList
	if err := runOpenShiftAPIOperation(ctx, func(ctx context.Context) error {
		return h.runtimeClient.List(ctx, &namespaces, client.MatchingLabels{"consul.hashicorp.com/acceptance-namespace": namespace})
	}); err != nil {
		return err
	}
	ownedNamespaces := map[string]bool{namespace: true}
	for _, auxiliary := range namespaces.Items {
		ownedNamespaces[auxiliary.Name] = true
	}
	var crds apiextensionsv1.CustomResourceDefinitionList
	if err := runOpenShiftAPIOperation(ctx, func(ctx context.Context) error {
		return h.runtimeClient.List(ctx, &crds)
	}); err != nil {
		return fmt.Errorf("listing orphaned OpenShift Consul CRDs: %w", err)
	}
	for _, crd := range crds.Items {
		owner := crd.Annotations["meta.helm.sh/release-name"]
		if (crd.Spec.Group != "consul.hashicorp.com" && crd.Spec.Group != "auth.consul.hashicorp.com") ||
			crd.Annotations["meta.helm.sh/release-namespace"] != namespace ||
			owner == "" || crd.Labels["release"] != owner {
			continue
		}
		var version string
		for _, candidate := range crd.Spec.Versions {
			if candidate.Served {
				version = candidate.Name
				if candidate.Storage {
					break
				}
			}
		}
		if version == "" {
			return fmt.Errorf("orphaned Consul CRD %s has no served version", crd.Name)
		}
		instances := &unstructured.UnstructuredList{}
		instances.SetGroupVersionKind(schema.GroupVersionKind{Group: crd.Spec.Group, Version: version, Kind: crd.Spec.Names.ListKind})
		if err := runOpenShiftAPIOperation(ctx, func(ctx context.Context) error {
			return h.runtimeClient.List(ctx, instances)
		}); err != nil {
			return fmt.Errorf("listing instances of orphaned CRD %s: %w", crd.Name, err)
		}
		for _, instance := range instances.Items {
			owned := ownedNamespaces[instance.GetNamespace()] ||
				instance.GetLabels()["release"] == owner ||
				instance.GetLabels()["consul.hashicorp.com/acceptance-namespace"] == namespace ||
				instance.GetAnnotations()["meta.helm.sh/release-namespace"] == namespace
			if !owned {
				return fmt.Errorf("refusing to recover CRD %s: instance %s/%s has no acceptance ownership", crd.Name, instance.GetNamespace(), instance.GetName())
			}
			for _, finalizer := range instance.GetFinalizers() {
				if !strings.HasSuffix(finalizer, ".consul.hashicorp.com") {
					return fmt.Errorf("refusing to recover CRD %s: instance %s/%s has foreign finalizer %s", crd.Name, instance.GetNamespace(), instance.GetName(), finalizer)
				}
			}
		}
		t.Logf("Removing orphaned Consul CRD %s from release %s in acceptance namespace %s", crd.Name, owner, namespace)
		for _, instance := range instances.Items {
			key := client.ObjectKeyFromObject(&instance)
			gvk := instance.GroupVersionKind()
			if err := runOpenShiftAPIOperation(ctx, func(ctx context.Context) error {
				current := &unstructured.Unstructured{}
				current.SetGroupVersionKind(gvk)
				if err := h.runtimeClient.Get(ctx, key, current); err != nil {
					return client.IgnoreNotFound(err)
				}
				finalizers := slices.DeleteFunc(current.GetFinalizers(), func(finalizer string) bool {
					return strings.HasSuffix(finalizer, ".consul.hashicorp.com")
				})
				current.SetFinalizers(finalizers)
				if err := h.runtimeClient.Update(ctx, current); err != nil {
					return client.IgnoreNotFound(err)
				}
				return client.IgnoreNotFound(h.runtimeClient.Delete(ctx, current))
			}); err != nil {
				return fmt.Errorf("cleaning orphaned CRD %s instance %s: %w", crd.Name, key, err)
			}
		}
		if err := runOpenShiftAPIOperation(ctx, func(ctx context.Context) error {
			return client.IgnoreNotFound(h.runtimeClient.Delete(ctx, &crd))
		}); err != nil {
			return fmt.Errorf("deleting orphaned Consul CRD %s: %w", crd.Name, err)
		}
		if err := wait.PollUntilContextCancel(ctx, time.Second, true, func(ctx context.Context) (bool, error) {
			var current apiextensionsv1.CustomResourceDefinition
			err := h.runtimeClient.Get(ctx, client.ObjectKey{Name: crd.Name}, &current)
			if apierrors.IsNotFound(err) {
				return true, nil
			}
			if k8s.IsRetryableOpenShiftAPIError(err) {
				return false, nil
			}
			return false, err
		}); err != nil {
			return fmt.Errorf("waiting for orphaned Consul CRD %s deletion: %w", crd.Name, err)
		}
	}
	return nil
}

func (h *HelmCluster) waitForOpenShiftCRDDeletion(t *testing.T) {
	t.Helper()
	output, err := k8s.RunKubectlAndGetOutputE(t, h.helmOptions.KubectlOptions, "get", "crd", "--selector", "release="+h.releaseName, "-o", "json")
	require.NoError(t, err)
	var crds struct {
		Items []struct {
			Metadata struct {
				Name        string            `json:"name"`
				Annotations map[string]string `json:"annotations"`
			} `json:"metadata"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal([]byte(output), &crds))
	args := []string{"wait", "--for=delete", "--timeout=2m"}
	for _, crd := range crds.Items {
		if crd.Metadata.Annotations["meta.helm.sh/release-name"] == h.releaseName &&
			crd.Metadata.Annotations["meta.helm.sh/release-namespace"] == h.helmOptions.KubectlOptions.Namespace {
			args = append(args, "crd/"+crd.Metadata.Name)
		}
	}
	if len(args) > 3 {
		t.Logf("Waiting for CRDs from release %s to be deleted: %s", h.releaseName, strings.Join(args[3:], ", "))
		k8s.RunKubectl(t, h.helmOptions.KubectlOptions, args...)
	}
}

func openShiftHelmValues(t *testing.T, client kubernetes.Interface) map[string]string {
	t.Helper()

	var serverVersion string
	retry.RunWith(openShiftAPIRetry(), t, func(r *retry.R) {
		info, err := client.Discovery().ServerVersion()
		require.NoError(r, err, "detecting the OpenShift Kubernetes version")
		serverVersion = info.GitVersion
	})
	kubernetesVersion, err := version.NewVersion(serverVersion)
	require.NoError(t, err, "parsing the OpenShift Kubernetes version")
	isOCP419OrLater := kubernetesVersion.GreaterThanOrEqual(version.Must(version.NewVersion("1.32.0")))
	t.Logf("Detected OpenShift Kubernetes version %s (OCP 4.19+: %t)", serverVersion, isOCP419OrLater)

	return map[string]string{
		"global.openshift.enabled":                       "true",
		"global.openshift.isOcpGreaterthan4_18":          strconv.FormatBool(isOCP419OrLater),
		"global.openshift.crds.enableTcpRoute":           strconv.FormatBool(!isOCP419OrLater),
		"connectInject.apiGateway.manageExternalCRDs":    "false",
		"connectInject.apiGateway.manageNonStandardCRDs": "true",
		"server.affinity":                                "null",
	}
}

// openShiftReleasedChartImages returns overrides that pin a published chart's
// default images by their fully qualified names.
//
// Published charts default to short names such as hashicorp/consul. OpenShift
// resolves short names through registry aliases that send hashicorp/* to
// registry.connect.redhat.com, which does not carry those tags, so the release
// never becomes ready. Every other platform resolves the same names to
// docker.io, so qualifying them keeps the released images unchanged. Values
// that are already set, such as the enterprise Consul image, are left alone.
func openShiftReleasedChartImages(t *testing.T, chartVersion string, setValues map[string]string) map[string]string {
	t.Helper()

	var chartValues string
	retry.RunWith(&retry.Counter{Wait: 5 * time.Second, Count: 3}, t, func(r *retry.R) {
		var err error
		chartValues, err = helm.RunHelmCommandAndGetStdOutE(r, &helm.Options{Logger: terratestLogger.Discard},
			"show", "values", "consul", "--repo", "https://helm.releases.hashicorp.com", "--version", chartVersion)
		require.NoError(r, err, "reading the default values of chart %s", chartVersion)
	})
	images, err := qualifyReleasedChartImages([]byte(chartValues), setValues)
	require.NoError(t, err)
	t.Logf("Qualified the short image names of chart %s for OpenShift: %v", chartVersion, images)
	return images
}

// qualifyReleasedChartImages returns docker.io overrides for the global image
// defaults in chartValues that are short names and not already in setValues.
func qualifyReleasedChartImages(chartValues []byte, setValues map[string]string) (map[string]string, error) {
	var values struct {
		Global map[string]interface{} `yaml:"global"`
	}
	if err := yaml.Unmarshal(chartValues, &values); err != nil {
		return nil, fmt.Errorf("parsing chart values: %w", err)
	}
	images := map[string]string{}
	for _, key := range []string{"image", "imageK8S", "imageEnvoy", "imageConsulDataplane"} {
		image, _ := values.Global[key].(string)
		if _, set := setValues["global."+key]; set || image == "" || !isShortImageName(image) {
			continue
		}
		images["global."+key] = "docker.io/" + image
	}
	return images, nil
}

// isShortImageName reports whether the container runtime has to choose the
// registry for image. As in image reference parsing, the first path component
// is a registry only if it contains "." or ":" or is "localhost".
func isShortImageName(image string) bool {
	registry, _, found := strings.Cut(image, "/")
	return !found || (!strings.ContainsAny(registry, ".:") && registry != "localhost")
}

func (h *HelmCluster) isReleaseSecret(secret corev1.Secret) bool {
	if !h.cfg.EnableOpenshift && !h.cfg.UseOpenshift {
		return strings.Contains(secret.Name, h.releaseName)
	}
	if h.releaseName == "" {
		return false
	}
	if release, ok := secret.Labels["release"]; ok {
		return release == h.releaseName
	}
	if release, ok := secret.Annotations["meta.helm.sh/release-name"]; ok {
		return release == h.releaseName &&
			secret.Annotations["meta.helm.sh/release-namespace"] == h.helmOptions.KubectlOptions.Namespace
	}
	// OpenShift recreates dockercfg secrets while their service accounts exist.
	// A release named "consul" must not match another release's "*-consul-*" secrets.
	return strings.HasPrefix(secret.Name, h.releaseName+"-") ||
		strings.HasPrefix(secret.Name, "sh.helm.release.v1."+h.releaseName+".")
}

func ensureNamespaceExists(t *testing.T, client kubernetes.Interface, namespace string) {
	t.Helper()
	k8s.EnsureOpenShiftNamespace(t, client, namespace)
}

// openShiftAPIRetry rides out transient managed OpenShift API errors such as
// TLS handshake timeouts.
func openShiftAPIRetry() *retry.Timer {
	return &retry.Timer{Timeout: 2 * time.Minute, Wait: 5 * time.Second}
}
