package datadog

import (
	"encoding/json"
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

func SearchMetricsAPIWithRetry(apiClient *DatadogClient, metricName string, maxRetries int, t *testing.T) (response datadogV1.MetricSearchResponse, fullResponse *http.Response, err error) {
	logger := terratestLogger.New(logger.TestLogger{})
	api := datadogV1.NewMetricsApi(apiClient.ApiClient)
	for attempt := 0; attempt < maxRetries; attempt++ {
		response, fullResponse, err = api.ListMetrics(apiClient.Ctx, metricName)
		if err == nil && responseContainsMetric(response, metricName) {
			return response, fullResponse, nil // Success
		}

		// Log the error and response details
		content, _ := json.MarshalIndent(response, "", "    ")
		logger.Logf(t, "Attempt %d: Error when calling MetricsApi.ListMetrics: %v", attempt+1, err)
		logger.Logf(t, "Attempt %d: Response: %v", attempt+1, string(content))

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
	backoff := math.Pow(10, float64(attempt)) * 100 // Base delay in milliseconds
	jitter := rand.Float64() * 100                  // Add jitter up to 100ms
	return time.Duration(backoff+jitter) * time.Millisecond
}

// responseContainsMetric checks if the specified metricName is present in the response.
func responseContainsMetric(response datadogV1.MetricSearchResponse, metricName string) bool {
	// Implement logic to check if `metricName` is present in `response`.
	// This is a placeholder implementation. You'll need to adjust it based on how the response structure is defined and how the metrics are listed in it.
	reg := regexp.MustCompile(".*" + metricName)
	for _, metric := range response.Results.Metrics {
		if reg.MatchString(metric) {
			return true
		}
	}
	return false
}
