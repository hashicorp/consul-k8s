// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package consul

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gruntwork-io/terratest/modules/helm"
	terratestLogger "github.com/gruntwork-io/terratest/modules/logger"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	customgwv1beta1 "github.com/hashicorp/consul-k8s/control-plane/gateway07/gateway-api-0.7.1-custom/apis/v1beta1"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/hashicorp/go-version"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
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

func (h *HelmCluster) cleanupOpenShiftCustomGatewayClasses(t *testing.T) {
	t.Helper()
	var classes customgwv1beta1.CustomGatewayClassList
	err := h.runtimeClient.List(context.Background(), &classes)
	if apierrors.IsNotFound(err) || apimeta.IsNoMatchError(err) {
		return
	}
	require.NoError(t, err)
	for _, gatewayClass := range classes.Items {
		release := gatewayClass.Labels["release"]
		ownedFixture := release == "" && gatewayClass.Labels["consul.hashicorp.com/acceptance-namespace"] == h.helmOptions.KubectlOptions.Namespace
		if release != h.releaseName && !ownedFixture {
			continue
		}
		t.Logf("Cleaning custom gateway class %s before uninstalling release %s", gatewayClass.Name, h.releaseName)
		gatewayClass.Finalizers = nil
		if err := h.runtimeClient.Update(context.Background(), &gatewayClass); err != nil && !apierrors.IsNotFound(err) {
			require.NoError(t, err)
		}
		if err := h.runtimeClient.Delete(context.Background(), &gatewayClass); err != nil && !apierrors.IsNotFound(err) {
			require.NoError(t, err)
		}
	}
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

func ensureNamespaceExists(t *testing.T, client kubernetes.Interface, namespace string) {
	t.Helper()

	retry.RunWith(openShiftAPIRetry(), t, func(r *retry.R) {
		_, err := client.CoreV1().Namespaces().Get(context.Background(), namespace, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			_, err = client.CoreV1().Namespaces().Create(context.Background(), &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: namespace},
			}, metav1.CreateOptions{})
			if apierrors.IsAlreadyExists(err) {
				return
			}
		}
		require.NoError(r, err)
	})
}

// openShiftAPIRetry rides out transient managed OpenShift API errors such as
// TLS handshake timeouts.
func openShiftAPIRetry() *retry.Timer {
	return &retry.Timer{Timeout: 2 * time.Minute, Wait: 5 * time.Second}
}
