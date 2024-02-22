package datadog

import (
	"encoding/json"
	"fmt"
	"github.com/DataDog/datadog-api-client-go/v2/api/datadogV1"
	terratestLogger "github.com/gruntwork-io/terratest/modules/logger"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"math"
	"math/rand"
	"net/http"
	"regexp"
	"testing"
	"time"
)

type MetricsResponseWrapper struct {
	QueryResponse  *datadogV1.MetricsQueryResponse
	SearchResponse *datadogV1.MetricSearchResponse
}

func ApiWithRetry(t *testing.T, apiClient *DatadogClient, endpoint, testTags, query string, maxRetries int) (response MetricsResponseWrapper, fullResponse *http.Response, err error) {
	logger := terratestLogger.New(logger.TestLogger{})
	api := datadogV1.NewMetricsApi(apiClient.ApiClient)
	// api.ListMetrics: /api/v1/search
	// api.QueryMetrics: /api/v1/query
	for attempt := 0; attempt < maxRetries; attempt++ {
		switch endpoint {
		case MetricsListQuery:
			var searchResponse datadogV1.MetricSearchResponse
			searchResponse, fullResponse, err = api.ListMetrics(apiClient.Ctx, query)
			response.SearchResponse = &searchResponse
		case MetricTimeSeriesQuery:
			var queryResponse datadogV1.MetricsQueryResponse
			fullQueryString := fmt.Sprintf("avg:%s{%s}", query, testTags)
			queryResponse, fullResponse, err = api.QueryMetrics(apiClient.Ctx, time.Now().Add(-1*time.Minute).Unix(), time.Now().Unix(), fullQueryString)
			response.QueryResponse = &queryResponse
		default:
			var searchResponse datadogV1.MetricSearchResponse
			searchResponse, fullResponse, err = api.ListMetrics(apiClient.Ctx, query)
			response.SearchResponse = &searchResponse
		}
		if err == nil && responseContainsMetric(response, query) {
			return response, fullResponse, nil // Success
		}

		// Log the error and response details
		content, _ := json.MarshalIndent(response.SearchResponse, "", "    ")
		if endpoint == "time-series" {
			content, _ = json.MarshalIndent(response.QueryResponse, "", "    ")
		}
		if err != nil {
			logger.Logf(t, "Attempt %d of %d: Error received when calling Datadog API MetricsApi.ListMetrics (/v1/search) | query=%s | error=%v", attempt+1, maxRetries, query, err)
		} else {
			logger.Logf(t, "Attempt %d of %d: No match received when calling Datadog API MetricsApi.ListMetrics (/v1/search) | query=%s", attempt+1, maxRetries, query)
		}
		logger.Logf(t, "Response: %v", string(content))

		// Exponential backoff with jitter
		waitTime := getBackoffDuration(attempt)
		time.Sleep(waitTime)
	}

	// Return the last error if all retries fail
	return response, fullResponse, err
}

// getBackoffDuration calculates the time to wait before the next retry attempt with an exponential backoff and jitter
func getBackoffDuration(attempt int) time.Duration {
	// Exponential backoff factor
	backoff := math.Pow(7, float64(attempt)) * 100 // Base delay in milliseconds
	jitter := rand.Float64() * 100                 // Add jitter up to 100ms
	return time.Duration(backoff+jitter) * time.Millisecond
}

// responseContainsMetric checks if the specified metricName is present in the response.
func responseContainsMetric(response MetricsResponseWrapper, query string) bool {
	// Implement logic to check if `query` is present in `response`.
	reg := regexp.MustCompile(".*" + query)
	if response.SearchResponse != nil {
		for _, metric := range response.SearchResponse.Results.Metrics {
			if reg.MatchString(metric) {
				return true
			}
		}
	} else if response.QueryResponse != nil {
		if _, ok := response.QueryResponse.GetStatusOk(); ok {
			for _, series := range response.QueryResponse.Series {
				if reg.MatchString(series.GetDisplayName()) {
					return true
				}
			}
		}
	}
	return false
}
