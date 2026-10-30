// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package metrics

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/common"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
)

// Config represents configuration common to connect-inject components related to metrics.
type Config struct {
	DefaultEnableMetrics        bool
	EnableGatewayMetrics        bool
	DefaultEnableMetricsMerging bool
	DefaultMergedMetricsPort    string
	DefaultPrometheusScrapePort string
	DefaultPrometheusScrapePath string
}

type metricsPorts struct {
	mergedPort string
	// servicePort and servicePath describe the single endpoint configured by
	// the service-metrics-port and service-metrics-path annotations.
	servicePort string
	servicePath string
	// serviceEndpoints holds the scrape targets configured by the
	// service-metrics-endpoints annotation. Nil when it is not set.
	serviceEndpoints []ServiceMetricsEndpoint
}

const (
	defaultServiceMetricsPath = "/metrics"
)

// MergedMetricsServerConfiguration is called when running a merged metrics server and used to return ports necessary to
// configure the merged metrics server.
func (mc Config) MergedMetricsServerConfiguration(pod corev1.Pod) (metricsPorts, error) {
	run, err := mc.ShouldRunMergedMetricsServer(pod)
	if err != nil {
		return metricsPorts{}, err
	}

	// This should never happen because we only call this function in the meshWebhook if
	// we need to run the metrics merging server. This check is here just in case.
	if !run {
		return metricsPorts{}, errors.New("metrics merging should be enabled in order to return the metrics server configuration")
	}

	// Configure consul sidecar with the appropriate metrics flags.
	mergedMetricsPort, err := mc.MergedMetricsPort(pod)
	if err != nil {
		return metricsPorts{}, err
	}

	// The service-metrics-endpoints annotation takes precedence, so when it is
	// set the legacy annotations are not read. Nil when it is not set.
	serviceEndpoints, err := mc.ServiceMetricsEndpoints(pod)
	if err != nil {
		return metricsPorts{}, err
	}

	if len(serviceEndpoints) > 0 {
		return metricsPorts{
			mergedPort: mergedMetricsPort,
			// servicePort and servicePath describe the first endpoint, for
			// callers that only understand a single one.
			servicePort:      serviceEndpoints[0].Port,
			servicePath:      serviceEndpoints[0].Path,
			serviceEndpoints: serviceEndpoints,
		}, nil
	}

	// Don't need to check the error since it's checked in the call to
	// mc.ShouldRunMergedMetricsServer() above.
	serviceMetricsPort, _ := mc.ServiceMetricsPort(pod)

	serviceMetricsPath := mc.ServiceMetricsPath(pod)

	metricsPorts := metricsPorts{
		mergedPort:  mergedMetricsPort,
		servicePort: serviceMetricsPort,
		servicePath: serviceMetricsPath,
	}
	return metricsPorts, nil
}

// EnableMetrics returns whether metrics are enabled either via the default value in the meshWebhook, or if it's been
// overridden via the annotation.
func (mc Config) EnableMetrics(pod corev1.Pod) (bool, error) {
	enabled := mc.DefaultEnableMetrics
	if raw, ok := pod.Annotations[constants.AnnotationEnableMetrics]; ok && raw != "" {
		enableMetrics, err := strconv.ParseBool(raw)
		if err != nil {
			return false, fmt.Errorf("%s annotation value of %s was invalid: %s", constants.AnnotationEnableMetrics, raw, err)
		}
		enabled = enableMetrics
	}
	return enabled, nil
}

// EnableMetricsMerging returns whether metrics merging functionality is enabled either via the default value in the
// meshWebhook, or if it's been overridden via the annotation.
func (mc Config) EnableMetricsMerging(pod corev1.Pod) (bool, error) {
	enabled := mc.DefaultEnableMetricsMerging
	if raw, ok := pod.Annotations[constants.AnnotationEnableMetricsMerging]; ok && raw != "" {
		enableMetricsMerging, err := strconv.ParseBool(raw)
		if err != nil {
			return false, fmt.Errorf("%s annotation value of %s was invalid: %s", constants.AnnotationEnableMetricsMerging, raw, err)
		}
		enabled = enableMetricsMerging
	}
	return enabled, nil
}

// MergedMetricsPort returns the port to run the merged metrics server on, either via the default value in the meshWebhook,
// or if it's been overridden via the annotation. It also validates the port is in the unprivileged port range.
func (mc Config) MergedMetricsPort(pod corev1.Pod) (string, error) {
	return common.DetermineAndValidatePort(pod, constants.AnnotationMergedMetricsPort, mc.DefaultMergedMetricsPort, false)
}

// PrometheusScrapePort returns the port for Prometheus to scrape from, either via the default value in the meshWebhook, or
// if it's been overridden via the annotation. It also validates the port is in the unprivileged port range.
func (mc Config) PrometheusScrapePort(pod corev1.Pod) (string, error) {
	return common.DetermineAndValidatePort(pod, constants.AnnotationPrometheusScrapePort, mc.DefaultPrometheusScrapePort, false)
}

// PrometheusScrapePath returns the path for Prometheus to scrape from, either via the default value in the meshWebhook, or
// if it's been overridden via the annotation.
func (mc Config) PrometheusScrapePath(pod corev1.Pod) string {
	if raw, ok := pod.Annotations[constants.AnnotationPrometheusScrapePath]; ok && raw != "" {
		return raw
	}

	if mc.DefaultPrometheusScrapePath == "" {
		return defaultServiceMetricsPath
	}

	return mc.DefaultPrometheusScrapePath
}

// ServiceMetricsPort returns the port the service exposes metrics on. This will
// default to the port used to register the service with Consul, and can be
// overridden with the annotation if provided.
func (mc Config) ServiceMetricsPort(pod corev1.Pod) (string, error) {
	// The annotationPort is the port used to register the service with Consul.
	// If that has been set, it'll be used as the port for getting service
	// metrics as well, unless overridden by the service-metrics-port annotation.
	if raw, ok := pod.Annotations[constants.AnnotationPort]; ok && raw != "" {
		// The service metrics port can be privileged if the service author has
		// written their service in such a way that it expects to be able to use
		// privileged ports. So, the port metrics are exposed on the service can
		// be privileged.
		return common.DetermineAndValidatePort(pod, constants.AnnotationServiceMetricsPort, raw, true)
	}

	// If the annotationPort is not set, the serviceMetrics port will be 0
	// unless overridden by the service-metrics-port annotation. If the service
	// metrics port is 0, the consul sidecar will not run a merged metrics
	// server.
	return common.DetermineAndValidatePort(pod, constants.AnnotationServiceMetricsPort, "0", true)
}

// ServiceMetricsPath returns a default of /metrics, or overrides
// that with the annotation if provided.
func (mc Config) ServiceMetricsPath(pod corev1.Pod) string {
	if raw, ok := pod.Annotations[constants.AnnotationServiceMetricsPath]; ok && raw != "" {
		return raw
	}

	return defaultServiceMetricsPath
}

// ShouldRunMergedMetricsServer returns whether we need to run a merged metrics
// server. This is used to configure the consul sidecar command, and the init
// container, so it can pass appropriate arguments to the consul connect envoy
// command.
func (mc Config) ShouldRunMergedMetricsServer(pod corev1.Pod) (bool, error) {
	enableMetrics, err := mc.EnableMetrics(pod)
	if err != nil {
		return false, err
	}
	enableMetricsMerging, err := mc.EnableMetricsMerging(pod)
	if err != nil {
		return false, err
	}
	// The service-metrics-endpoints annotation takes precedence over
	// service-metrics-port and service-metrics-path, so it is resolved first.
	// When it provides the endpoints the legacy annotations are not read at
	// all, otherwise an unused and invalid service-metrics-port would reject a
	// Pod that is fully configured by service-metrics-endpoints.
	serviceEndpoints, err := mc.ServiceMetricsEndpoints(pod)
	if err != nil {
		return false, err
	}
	if len(serviceEndpoints) > 0 {
		return enableMetrics && enableMetricsMerging, nil
	}

	serviceMetricsPort, err := mc.ServiceMetricsPort(pod)
	if err != nil {
		return false, err
	}

	// Don't need to check error here since ServiceMetricsPort has been
	// validated by calling mc.ServiceMetricsPort above.
	smp, _ := strconv.Atoi(serviceMetricsPort)

	if enableMetrics && enableMetricsMerging && smp > 0 {
		return true, nil
	}
	return false, nil
}

// ServiceMetricsEndpoint is a single scrape target exposed by the service.
type ServiceMetricsEndpoint struct {
	// Port is the resolved port number the service exposes metrics on.
	Port string
	// Path is the HTTP path metrics are served from. Defaults to /metrics.
	Path string
}

// ServiceMetricsEndpoints returns the scrape targets declared by the
// service-metrics-endpoints annotation, which allows a single container to
// expose metrics on more than one port.
//
// It returns nil when the annotation is not set. It deliberately does not fall
// back to service-metrics-port/service-metrics-path so that the single-endpoint
// path remains exactly as it was; callers that need the legacy behaviour should
// keep using ServiceMetricsPort and ServiceMetricsPath.
func (mc Config) ServiceMetricsEndpoints(pod corev1.Pod) ([]ServiceMetricsEndpoint, error) {
	raw, ok := pod.Annotations[constants.AnnotationServiceMetricsEndpoints]
	if !ok || raw == "" {
		return nil, nil
	}
	return parseServiceMetricsEndpoints(pod, raw)
}

// parseServiceMetricsEndpoints parses the service-metrics-endpoints annotation
// value. Each entry is "port" or "port:path". Ports may be numeric or the name
// of a container port, and may be privileged since the service author controls
// which port their application listens on.
func parseServiceMetricsEndpoints(pod corev1.Pod, raw string) ([]ServiceMetricsEndpoint, error) {
	var endpoints []ServiceMetricsEndpoint
	seen := make(map[string]struct{})

	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}

		rawPort, path, hasPath := strings.Cut(entry, ":")
		rawPort = strings.TrimSpace(rawPort)
		path = strings.TrimSpace(path)

		if rawPort == "" {
			return nil, fmt.Errorf("%s annotation entry %q is missing a port", constants.AnnotationServiceMetricsEndpoints, entry)
		}

		port, err := common.PortValue(pod, rawPort)
		if err != nil {
			return nil, fmt.Errorf("%s annotation entry %q does not have a valid port: %s is not a port number or a named container port", constants.AnnotationServiceMetricsEndpoints, entry, rawPort)
		}
		if port < 1 || port > 65535 {
			return nil, fmt.Errorf("%s annotation entry %q has port %d which is not in the valid port range 1-65535", constants.AnnotationServiceMetricsEndpoints, entry, port)
		}

		if !hasPath || path == "" {
			path = defaultServiceMetricsPath
		}
		if !strings.HasPrefix(path, "/") {
			return nil, fmt.Errorf("%s annotation entry %q has path %q which must begin with '/'", constants.AnnotationServiceMetricsEndpoints, entry, path)
		}

		endpoint := ServiceMetricsEndpoint{Port: strconv.Itoa(int(port)), Path: path}

		// Scraping the same port and path twice would duplicate every metric in
		// the merged output, so drop exact repeats.
		key := endpoint.Port + endpoint.Path
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}

		endpoints = append(endpoints, endpoint)
	}

	if len(endpoints) == 0 {
		return nil, fmt.Errorf("%s annotation value of %q did not contain any endpoints", constants.AnnotationServiceMetricsEndpoints, raw)
	}

	return endpoints, nil
}
