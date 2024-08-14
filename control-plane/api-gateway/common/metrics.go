// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"strconv"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

const (
	defaultScrapePort = 20200
	defaultScrapePath = "/metrics"
)

type MetricsConfig struct {
	Enabled bool
	Path    string
	Port    int
}

func gatewayMetricsEnabled(gateway gwv1beta1.Gateway, gcc v1alpha1.GatewayClassConfig, config HelmConfig) bool {
	// first check our annotations, if something is there, then it means we've explicitly
	// annotated metrics collection
	if scrape, isSet := gateway.Annotations[constants.AnnotationEnableMetrics]; isSet {
		enabled, err := strconv.ParseBool(scrape)
		if err == nil {
			return enabled
		}
		// TODO: log an error
		// we fall through to the other metrics enabled checks
	}

	// if it's not set on the annotation, then we check to see if it's set on the GatewayClassConfig
	if gcc.Spec.Metrics.Enabled != nil {
		return *gcc.Spec.Metrics.Enabled
	}

	// otherwise, fallback to the global helm setting
	return config.EnableGatewayMetrics
}

func fetchPortString(gateway gwv1beta1.Gateway, gcc v1alpha1.GatewayClassConfig, config HelmConfig) string {
	// first check our annotations, if something is there, then it means we've explicitly
	// annotated metrics collection
	if portString, isSet := gateway.Annotations[constants.AnnotationPrometheusScrapePort]; isSet {
		return portString
	}

	// if it's not set on the annotation, then we check to see if it's set on the GatewayClassConfig
	if gcc.Spec.Metrics.Port != nil {
		return strconv.Itoa(int(*gcc.Spec.Metrics.Port))
	}

	// otherwise, fallback to the global helm setting
	return config.DefaultPrometheusScrapePort
}

func gatewayMetricsPort(gateway gwv1beta1.Gateway, gcc v1alpha1.GatewayClassConfig, config HelmConfig) int {
	portString := fetchPortString(gateway, gcc, config)

	port, err := strconv.Atoi(portString)
	if err != nil {
		// if we can't parse the port string, just use the default
		// TODO: log an error
		return defaultScrapePort
	}

	if port < 1024 || port > 65535 {
		// if we requested a privileged port, use the default
		// TODO: log an error
		return defaultScrapePort
	}

	return port
}

func gatewayMetricsPath(gateway gwv1beta1.Gateway, gcc v1alpha1.GatewayClassConfig, config HelmConfig) string {
	// first check our annotations, if something is there, then it means we've explicitly
	// annotated metrics collection
	if path, isSet := gateway.Annotations[constants.AnnotationPrometheusScrapePath]; isSet {
		return path
	}

	// if it's not set on the annotation, then we check to see if it's set on the GatewayClassConfig
	if gcc.Spec.Metrics.Path != nil {
		return *gcc.Spec.Metrics.Path
	}

	// otherwise, fallback to the global helm setting
	return config.DefaultPrometheusScrapePath
}

func GatewayMetricsConfig(gateway gwv1beta1.Gateway, gcc v1alpha1.GatewayClassConfig, config HelmConfig) MetricsConfig {
	return MetricsConfig{
		Enabled: gatewayMetricsEnabled(gateway, gcc, config),
		Path:    gatewayMetricsPath(gateway, gcc, config),
		Port:    gatewayMetricsPort(gateway, gcc, config),
	}
}
