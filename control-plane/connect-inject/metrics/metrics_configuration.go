// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package metrics

import (
	"errors"
	"fmt"
	"strconv"

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
	mergedPort  string
	servicePort string
	servicePath string
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
