// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package datadog

import (
	"context"
	"fmt"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"
	"time"

	"github.com/hashicorp/consul-k8s/acceptance/framework/config"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"

	"github.com/gruntwork-io/terratest/modules/helm"
	terratestk8s "github.com/gruntwork-io/terratest/modules/k8s"
	terratestLogger "github.com/gruntwork-io/terratest/modules/logger"
	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
	"k8s.io/client-go/kubernetes"
)

const (
	releaseLabel            = "app.kubernetes.io/name"
	OperatorReleaseName     = "datadog-operator"
	DefaultHelmChartVersion = "1.4.0"
	datadogSecretName       = "datadog-secret"
	datadogAPIKey           = "api-key"
	datadogAppKey           = "app-key"
	datadogFakeAPIKey       = "DD_FAKEAPIKEY"
	datadogFakeAPPKey       = "DD_FAKEAPPKEY"
)

type DatadogCluster struct {
	ctx environment.TestContext

	helmOptions *helm.Options
	releaseName string

	kubectlOptions *terratestk8s.KubectlOptions

	kubernetesClient kubernetes.Interface

	noCleanupOnFailure bool
	noCleanup          bool
	debugDirectory     string
	logger             terratestLogger.TestLogger
}

// releaseLabelSelector returns label selector that selects all pods
// from a Datadog installation.
func (d *DatadogCluster) releaseLabelSelector() string {
	return fmt.Sprintf("%s=%s", releaseLabel, d.releaseName)
}

func NewDatadogCluster(t *testing.T, ctx environment.TestContext, cfg *config.TestConfig, releaseName string, releaseNamespace string, helmValues map[string]string) *DatadogCluster {
	logger := terratestLogger.New(logger.TestLogger{})

	configureNamespace(t, ctx.KubernetesClient(t), cfg, releaseNamespace)

	createOrUpdateDatadogSecret(t, ctx.KubernetesClient(t), cfg, releaseNamespace)

	kopts := ctx.KubectlOptionsForNamespace(releaseNamespace)

	values := defaultHelmValues()

	ddHelmChartVersion := DefaultHelmChartVersion
	if cfg.DatadogHelmChartVersion != "" {
		ddHelmChartVersion = cfg.DatadogHelmChartVersion
	}

	helpers.MergeMaps(values, helmValues)
	datadogHelmOpts := &helm.Options{
		SetValues:      values,
		KubectlOptions: kopts,
		Logger:         logger,
		Version:        ddHelmChartVersion,
	}

	helm.AddRepo(t, datadogHelmOpts, "datadog", "https://helm.datadoghq.com")
	// Ignoring the error from `helm repo update` as it could fail due to stale cache or unreachable servers and we're
	// asserting a chart version on Install which would fail in an obvious way should this not succeed.
	_, err := helm.RunHelmCommandAndGetOutputE(t, &helm.Options{}, "repo", "update")
	if err != nil {
		logger.Logf(t, "Unable to update helm repository, proceeding anyway: %s.", err)
	}

	return &DatadogCluster{
		ctx:                ctx,
		helmOptions:        datadogHelmOpts,
		kubectlOptions:     kopts,
		kubernetesClient:   ctx.KubernetesClient(t),
		noCleanupOnFailure: cfg.NoCleanupOnFailure,
		noCleanup:          cfg.NoCleanup,
		debugDirectory:     cfg.DebugDirectory,
		logger:             logger,
		releaseName:        releaseName,
	}
}

func (d *DatadogCluster) Create(t *testing.T) {
	t.Helper()

	helpers.Cleanup(t, d.noCleanupOnFailure, d.noCleanup, func() {
		d.Destroy(t)
	})

	helm.Install(t, d.helmOptions, "datadog/datadog-operator", d.releaseName)
	// Wait for the datadog-operator to become ready
	k8s.WaitForAllPodsToBeReady(t, d.kubernetesClient, d.helmOptions.KubectlOptions.Namespace, d.releaseLabelSelector())
}

func (d *DatadogCluster) Destroy(t *testing.T) {
	t.Helper()

	k8s.WritePodsDebugInfoIfFailed(t, d.kubectlOptions, d.debugDirectory, d.releaseLabelSelector())
	// Ignore the error returned by the helm delete here so that we can
	// always idempotent clean up resources in the cluster.
	_ = helm.DeleteE(t, d.helmOptions, d.releaseName, true)
}

func defaultHelmValues() map[string]string {
	return map[string]string{
		"replicaCount":     "1",
		"image.tag":        DefaultHelmChartVersion,
		"image.repository": "gcr.io/datadoghq/operator",
	}
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

func createOrUpdateDatadogSecret(t *testing.T, client kubernetes.Interface, cfg *config.TestConfig, namespace string) {
	secretMap := map[string]string{
		datadogAPIKey: datadogFakeAPIKey,
		datadogAppKey: datadogFakeAPPKey,
	}
	createMultiKeyK8sSecret(t, client, cfg, namespace, datadogSecretName, secretMap)
}

func createMultiKeyK8sSecret(t *testing.T, client kubernetes.Interface, cfg *config.TestConfig, namespace, secretName string, secretMap map[string]string) {
	retry.RunWith(&retry.Counter{Wait: 2 * time.Second, Count: 15}, t, func(r *retry.R) {
		_, err := client.CoreV1().Secrets(namespace).Get(context.Background(), secretName, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			_, err := client.CoreV1().Secrets(namespace).Create(context.Background(), &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: secretName,
				},
				StringData: secretMap,
				Type:       corev1.SecretTypeOpaque,
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
