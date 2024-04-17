package datadog

import (
	"fmt"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"testing"

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
