package connectinject

import (
	"errors"
	"fmt"
	"strconv"

	corev1 "k8s.io/api/core/v1"
)

// MetricsConfig represents configuration common to connect-inject components related to metrics.
type MetricsConfig struct {
	DefaultEnableMetrics        bool
	EnableGatewayMetrics        bool
	DefaultEnableMetricsMerging bool
	DefaultMergedMetricsPort    string
	DefaultPrometheusScrapePort string
	DefaultPrometheusScrapePath string
}

type metricsPorts struct {
	mergedPort  string
	servicePort string
	servicePath string
}

const (
	defaultServiceMetricsPath = "/metrics"
)

// mergedMetricsServerConfiguration is called when running a merged metrics server and used to return ports necessary to
// configure the merged metrics server.
func (mc MetricsConfig) mergedMetricsServerConfiguration(pod corev1.Pod) (metricsPorts, error) {
	run, err := mc.shouldRunMergedMetricsServer(pod)
	if err != nil {
		return metricsPorts{}, err
	}

	// This should never happen because we only call this function in the meshWebhook if
	// we need to run the metrics merging server. This check is here just in case.
	if !run {
		return metricsPorts{}, errors.New("metrics merging should be enabled in order to return the metrics server configuration")
	}

	// Configure consul sidecar with the appropriate metrics flags.
	mergedMetricsPort, err := mc.mergedMetricsPort(pod)
	if err != nil {
		return metricsPorts{}, err
	}

	// Don't need to check the error since it's checked in the call to
	// mc.shouldRunMergedMetricsServer() above.
	serviceMetricsPort, _ := mc.serviceMetricsPort(pod)

	serviceMetricsPath := mc.serviceMetricsPath(pod)

	metricsPorts := metricsPorts{
		mergedPort:  mergedMetricsPort,
		servicePort: serviceMetricsPort,
		servicePath: serviceMetricsPath,
	}
	return metricsPorts, nil
}

// enableMetrics returns whether metrics are enabled either via the default value in the meshWebhook, or if it's been
// overridden via the annotation.
func (mc MetricsConfig) enableMetrics(pod corev1.Pod) (bool, error) {
	enabled := mc.DefaultEnableMetrics
	if raw, ok := pod.Annotations[annotationEnableMetrics]; ok && raw != "" {
		enableMetrics, err := strconv.ParseBool(raw)
		if err != nil {
			return false, fmt.Errorf("%s annotation value of %s was invalid: %s", annotationEnableMetrics, raw, err)
		}
		enabled = enableMetrics
	}
	return enabled, nil
}

// enableMetricsMerging returns whether metrics merging functionality is enabled either via the default value in the
// meshWebhook, or if it's been overridden via the annotation.
func (mc MetricsConfig) enableMetricsMerging(pod corev1.Pod) (bool, error) {
	enabled := mc.DefaultEnableMetricsMerging
	if raw, ok := pod.Annotations[annotationEnableMetricsMerging]; ok && raw != "" {
		enableMetricsMerging, err := strconv.ParseBool(raw)
		if err != nil {
			return false, fmt.Errorf("%s annotation value of %s was invalid: %s", annotationEnableMetricsMerging, raw, err)
		}
		enabled = enableMetricsMerging
	}
	return enabled, nil
}

// mergedMetricsPort returns the port to run the merged metrics server on, either via the default value in the meshWebhook,
// or if it's been overridden via the annotation. It also validates the port is in the unprivileged port range.
func (mc MetricsConfig) mergedMetricsPort(pod corev1.Pod) (string, error) {
	return determineAndValidatePort(pod, annotationMergedMetricsPort, mc.DefaultMergedMetricsPort, false)
}

// prometheusScrapePort returns the port for Prometheus to scrape from, either via the default value in the meshWebhook, or
// if it's been overridden via the annotation. It also validates the port is in the unprivileged port range.
func (mc MetricsConfig) prometheusScrapePort(pod corev1.Pod) (string, error) {
	return determineAndValidatePort(pod, annotationPrometheusScrapePort, mc.DefaultPrometheusScrapePort, false)
}

// prometheusScrapePath returns the path for Prometheus to scrape from, either via the default value in the meshWebhook, or
// if it's been overridden via the annotation.
func (mc MetricsConfig) prometheusScrapePath(pod corev1.Pod) string {
	if raw, ok := pod.Annotations[annotationPrometheusScrapePath]; ok && raw != "" {
		return raw
	}

	return mc.DefaultPrometheusScrapePath
}

// serviceMetricsPort returns the port the service exposes metrics on. This will
// default to the port used to register the service with Consul, and can be
// overridden with the annotation if provided.
func (mc MetricsConfig) serviceMetricsPort(pod corev1.Pod) (string, error) {
	// The annotationPort is the port used to register the service with Consul.
	// If that has been set, it'll be used as the port for getting service
	// metrics as well, unless overridden by the service-metrics-port annotation.
	if raw, ok := pod.Annotations[annotationPort]; ok && raw != "" {
		// The service metrics port can be privileged if the service author has
		// written their service in such a way that it expects to be able to use
		// privileged ports. So, the port metrics are exposed on the service can
		// be privileged.
		return determineAndValidatePort(pod, annotationServiceMetricsPort, raw, true)
	}

	// If the annotationPort is not set, the serviceMetrics port will be 0
	// unless overridden by the service-metrics-port annotation. If the service
	// metrics port is 0, the consul sidecar will not run a merged metrics
	// server.
	return determineAndValidatePort(pod, annotationServiceMetricsPort, "0", true)
}

// serviceMetricsPath returns a default of /metrics, or overrides
// that with the annotation if provided.
func (mc MetricsConfig) serviceMetricsPath(pod corev1.Pod) string {
	if raw, ok := pod.Annotations[annotationServiceMetricsPath]; ok && raw != "" {
		return raw
	}

	return defaultServiceMetricsPath
}

// shouldRunMergedMetricsServer returns whether we need to run a merged metrics
// server. This is used to configure the consul sidecar command, and the init
// container, so it can pass appropriate arguments to the consul connect envoy
// command.
func (mc MetricsConfig) shouldRunMergedMetricsServer(pod corev1.Pod) (bool, error) {
	enableMetrics, err := mc.enableMetrics(pod)
	if err != nil {
		return false, err
	}
	enableMetricsMerging, err := mc.enableMetricsMerging(pod)
	if err != nil {
		return false, err
	}
	serviceMetricsPort, err := mc.serviceMetricsPort(pod)
	if err != nil {
		return false, err
	}

	// Don't need to check error here since serviceMetricsPort has been
	// validated by calling mc.serviceMetricsPort above.
	smp, _ := strconv.Atoi(serviceMetricsPort)

	if enableMetrics && enableMetricsMerging && smp > 0 {
		return true, nil
	}
	return false, nil
}

// determineAndValidatePort behaves as follows:
// If the annotation exists, validate the port and return it.
// If the annotation does not exist, return the default port.
// If the privileged flag is true, it will allow the port to be in the
// privileged port range of 1-1023. Otherwise, it will only allow ports in the
// unprivileged range of 1024-65535.
func determineAndValidatePort(pod corev1.Pod, annotation string, defaultPort string, privileged bool) (string, error) {
	if raw, ok := pod.Annotations[annotation]; ok && raw != "" {
		port, err := portValue(pod, raw)
		if err != nil {
			return "", fmt.Errorf("%s annotation value of %s is not a valid integer", annotation, raw)
		}

		if privileged && (port < 1 || port > 65535) {
			return "", fmt.Errorf("%s annotation value of %d is not in the valid port range 1-65535", annotation, port)
		} else if !privileged && (port < 1024 || port > 65535) {
			return "", fmt.Errorf("%s annotation value of %d is not in the unprivileged port range 1024-65535", annotation, port)
		}

		// If the annotation exists, return the validated port.
		return fmt.Sprint(port), nil
	}

	// If the annotation does not exist, return the default.
	if defaultPort != "" {
		port, err := portValue(pod, defaultPort)
		if err != nil {
			return "", fmt.Errorf("%s is not a valid port on the pod %s", defaultPort, pod.Name)
		}
		return fmt.Sprint(port), nil
	}
	return "", nil
}
