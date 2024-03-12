package datadog

import (
	"context"
	"fmt"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"testing"

	"github.com/hashicorp/consul-k8s/acceptance/framework/config"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"

	"github.com/DataDog/datadog-api-client-go/v2/api/datadog"
	"github.com/gruntwork-io/terratest/modules/helm"
	terratestk8s "github.com/gruntwork-io/terratest/modules/k8s"
	terratestLogger "github.com/gruntwork-io/terratest/modules/logger"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
	"k8s.io/client-go/kubernetes"
)

const (
	releaseLabel               = "app.kubernetes.io/name"
	DatadogOperatorReleaseName = "datadog-operator"
	DefaultHelmChartVersion    = "1.4.0"
)

type DatadogCluster struct {
	ctx environment.TestContext

	helmOptions   *helm.Options
	releaseName   string
	datadogClient *DatadogClient

	kubectlOptions *terratestk8s.KubectlOptions

	kubernetesClient kubernetes.Interface

	noCleanupOnFailure bool
	noCleanup          bool
	debugDirectory     string
	logger             terratestLogger.TestLogger
}

type DatadogClient struct {
	ApiClient *datadog.APIClient
	Ctx       context.Context
}

// releaseLabelSelector returns label selector that selects all pods
// from a Vault installation.
func (d *DatadogCluster) releaseLabelSelector() string {
	return fmt.Sprintf("%s=%s", releaseLabel, d.releaseName)
}

// DatadogClient returns datadog client
func (d *DatadogCluster) DatadogClient(*testing.T) *DatadogClient { return d.datadogClient }

// NewDatadogClient initializes and returns a DataDog client using the API key and Application key from environment variables.
func NewDatadogClient(cfg *config.TestConfig) (*DatadogClient, error) {
	// Retrieve DataDog API and Application keys from environment variables
	apiKey := cfg.DatadogAPIKey
	appKey := cfg.DatadogAppKey

	if apiKey == "" || appKey == "" {
		return nil, fmt.Errorf("DataDog API key or Application key is not set")
	}

	// Configuration for DataDog API client
	ctx := datadog.NewDefaultContext(context.Background())
	configuration := datadog.NewConfiguration()

	// Set API and Application keys in the context
	ctx = context.WithValue(ctx, datadog.ContextAPIKeys, map[string]datadog.APIKey{
		"apiKeyAuth": {
			Key: apiKey,
		},
		"appKeyAuth": {
			Key: appKey,
		},
	})

	// Create a DataDog API client
	client := datadog.NewAPIClient(configuration)

	// Return the client
	return &DatadogClient{
		ApiClient: client,
		Ctx:       ctx,
	}, nil
}

func NewDatadogCluster(t *testing.T, ctx environment.TestContext, cfg *config.TestConfig, releaseName string, releaseNamespace string, helmValues map[string]string) *DatadogCluster {
	logger := terratestLogger.New(logger.TestLogger{})

	kopts := ctx.KubectlOptionsForNamespace(releaseNamespace)

	values := defaultHelmValues()
	k8sClient := environment.KubernetesClientFromOptions(t, ctx.KubectlOptions(t))

	ddClient, err := NewDatadogClient(cfg)
	if err != nil {
		logger.Logf(t, "Failed to initialize Datadog API Client: %s.", err)
	}

	if cfg.DatadogAPIKey != "" || cfg.DatadogAppKey != "" {
		consul.CreateOrUpdateDatadogSecret(t, k8sClient, cfg, releaseNamespace)
	}

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
	_, err = helm.RunHelmCommandAndGetOutputE(t, &helm.Options{}, "repo", "update")
	if err != nil {
		logger.Logf(t, "Unable to update helm repository, proceeding anyway: %s.", err)
	}

	return &DatadogCluster{
		ctx:                ctx,
		datadogClient:      ddClient,
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
	// always idempotently clean up resources in the cluster.
	_ = helm.DeleteE(t, d.helmOptions, d.releaseName, true)
}

func defaultHelmValues() map[string]string {
	return map[string]string{
		"replicaCount":     "1",
		"image.tag":        DefaultHelmChartVersion,
		"image.repository": "gcr.io/datadoghq/operator",
	}
}
