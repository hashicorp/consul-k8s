package metrics

import (
	"strconv"

	"github.com/armon/go-metrics"
	metricsutil "github.com/hashicorp/consul-k8s/control-plane/subcommand/common"
)

const (
	defaultScrapePort = 20300
	defaultScrapePath = "/metrics"
)

type Config struct {
	// EnableSyncCatalogMetrics indicates whether or not SyncCatalog metrics should be enabled
	// by default on a deployed consul-sync-catalog, passed from the helm chart via command-line flags to our controller.
	EnableSyncCatalogMetrics bool

	// The default path to use for scraping prometheus metrics, passed from the helm chart via command-line flags to our controller.
	DefaultPrometheusScrapePath string

	// The default port to use for scraping prometheus metrics, passed from the helm chart via command-line flags to our controller.
	DefaultPrometheusScrapePort int

	// Configures the retention time for metrics in the metrics store, passed from the helm chart via command-line flags to our controller.
	PrometheusMetricsRetentionTime string
}

func syncCatalogMetricsPort(portString string) int {
	port, err := strconv.Atoi(portString)
	if err != nil {
		return defaultScrapePort
	}

	if port < 1024 || port > 65535 {
		// if we requested a privileged port, use the default
		return defaultScrapePort
	}

	return port
}

func syncCatalogMetricsPath(path string) string {
	if path, isSet := metricsutil.GetScrapePath(path); isSet {
		return path
	}

	// otherwise, fallback to the global helm setting
	return defaultScrapePath
}

func SyncCatalogMetricsConfig(enableMetrics bool, metricsPort, metricsPath string) Config {
	return Config{
		EnableSyncCatalogMetrics:    enableMetrics,
		DefaultPrometheusScrapePort: syncCatalogMetricsPort(metricsPort),
		DefaultPrometheusScrapePath: syncCatalogMetricsPath(metricsPath),
	}
}

func ServiceNameLabel(serviceName string) []metrics.Label {
	return []metrics.Label{
		{Name: "service_name", Value: serviceName},
	}
}
