// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package cloud

import (
	"strings"

	"github.com/hashicorp/serf/testutil/retry"
	"github.com/stretchr/testify/require"
	otlpcolmetrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	otlpcommon "go.opentelemetry.io/proto/otlp/common/v1"
	otlpmetrics "go.opentelemetry.io/proto/otlp/metrics/v1"
	"google.golang.org/protobuf/proto"
)

type metricValidations struct {
	disabled             bool
	expectedMetricName   string
	disallowedMetricName string
	expectedLabelKeys    []string
}

// validateMetrics ensure OTLP metrics as recorded by the Collector or Consul as expected.
func validateMetrics(r *retry.R, records []*RequestRecord, validations *metricValidations, since int64) {
	// If metrics are disabled, no metrics records should exist, and return early.
	if validations.disabled {
		require.Empty(r, records)
		return
	}

	// If metrics are not disabled, records should not be empty.
	require.NotEmpty(r, records)

	for _, record := range records {
		require.True(r, record.ValidRequest, "expected request to be valid")

		req := &otlpcolmetrics.ExportMetricsServiceRequest{}
		err := proto.Unmarshal(record.Body, req)
		require.NoError(r, err, "failed to extract metrics from body")

		// Basic validation that metrics are not empty.
		require.NotEmpty(r, req.GetResourceMetrics())
		require.NotEmpty(r, req.ResourceMetrics[0].GetScopeMetrics())
		require.NotEmpty(r, req.ResourceMetrics[0].ScopeMetrics[0].GetMetrics())

		// Verify expected key labels and metric names.
		labels := externalLabels(req, since)
		for _, key := range validations.expectedLabelKeys {
			require.Contains(r, labels, key)
		}
		validateMetricName(r, req, validations)
	}
}

// validateMetricName ensures an expected metric name has been recorded based on filters and disallowed metrics are not present.
func validateMetricName(t *retry.R, request *otlpcolmetrics.ExportMetricsServiceRequest, validations *metricValidations) {
	exists := false
	for _, metric := range request.ResourceMetrics[0].ScopeMetrics[0].GetMetrics() {
		require.NotContains(t, metric.Name, validations.disallowedMetricName)

		if strings.Contains(metric.Name, validations.expectedMetricName) {
			exists = true
		}
	}

	require.True(t, exists)
}

// externalLabels converts OTLP labels to a map[string]string format.
func externalLabels(request *otlpcolmetrics.ExportMetricsServiceRequest, since int64) map[string]string {
	// For the Consul Telemetry Collector, labels are contained at the higher level scope.
	attrs := request.ResourceMetrics[0].GetResource().GetAttributes()

	// For Consul server metrics, labels are contained with individual metrics, and must be extracted.
	if len(attrs) < 1 {
		attrs = getMetricLabel(request.ResourceMetrics[0].GetScopeMetrics(), since)
	}

	labels := make(map[string]string, len(attrs))
	for _, kv := range attrs {
		k := strings.ReplaceAll(kv.GetKey(), ".", "_")
		labels[k] = kv.GetValue().GetStringValue()
	}

	return labels
}

// getMetricLabel returns labels at each datapoint within a metric.
func getMetricLabel(scopeMetrics []*otlpmetrics.ScopeMetrics, since int64) []*otlpcommon.KeyValue {
	// The attributes field can only be accessed on the specific implementation (gauge, sum or hist).
	for _, metric := range scopeMetrics[0].Metrics {
		switch v := metric.Data.(type) {
		case *otlpmetrics.Metric_Gauge:
			for _, dp := range v.Gauge.GetDataPoints() {
				// When a refresh has occured, filter time since last refresh as older data points may not have latest labels.
				if dp.StartTimeUnixNano > uint64(since) {
					return dp.Attributes
				}
			}
		case *otlpmetrics.Metric_Histogram:
			for _, dp := range v.Histogram.GetDataPoints() {
				if dp.StartTimeUnixNano > uint64(since) {
					return dp.Attributes
				}
			}
		case *otlpmetrics.Metric_Sum:
			for _, dp := range v.Sum.GetDataPoints() {
				if dp.StartTimeUnixNano > uint64(since) {
					return dp.Attributes
				}
			}
		}
	}

	return []*otlpcommon.KeyValue{}
}
