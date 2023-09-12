package cloud

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

const (
	// Validation are used in the validationBody to distinguish metrics for consul vs. collector during validation.
	validationPathConsul    = "/v1/metrics/consul"
	validationPathCollector = "/v1/metrics/collector"
)

var (
	errCreatingRequest      = errors.New("failed to create HTTP request")
	errMakingRequest        = errors.New("failed to make request")
	errReadingBody          = errors.New("failed to read body")
	errParsingBody          = errors.New("failed to parse body")
	errValidation           = errors.New("failed validation")
	errUnexpectedStatusCode = errors.New("unexpected status code")
)

// fakeServerClient provides an interface to communicate with the fakesever (a fake HCP Telemetry Gateway) via HTTP.
type fakeServerClient struct {
	client *http.Client
	tunnel string
}

// TokenResponse is used to read a token response from the fakeserver.
type TokenResponse struct {
	Token string `json:"token"`
}

// errMsg is used to obtain the error trace of a valiation failure.
type errMsg struct {
	Error string `json:"error"`
}

// modifyTelemetryConfigBody is a POST body that provides telemetry config changes to the fakeserver.
type modifyTelemetryConfigBody struct {
	Filters  []string          `json:"filters"`
	Labels   map[string]string `json:"labels"`
	Disabled bool              `json:"disabled"`
}

// validationBody is a POST body that provides validation verifications to the fakeserver.
type validationBody struct {
	Path                 string   `json:"path"`
	ExpectedLabelKeys    []string `json:"expectedLabelKeys"`
	DisallowedMetricName string   `json:"disallowedMetricName"`
	ExpectedMetricName   string   `json:"expectedMetricName"`
	MetricsDisabled      bool     `json:"metricsDisabled"`
	FilterRecordsSince   int64    `json:"filterRecordsSince"`
}

// newfakeServerClient returns a fakeServerClient to be used in tests to communicate with the fake Telemetry Gateway.
func newfakeServerClient(tunnel string) *fakeServerClient {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	return &fakeServerClient{
		client: &http.Client{Transport: tr},
		tunnel: tunnel,
	}
}

// requestToken retrieves a token from the fakeserver's token endpoint.
func (f *fakeServerClient) requestToken() (string, error) {
	url := fmt.Sprintf("https://%s/token", f.tunnel)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("%w: %w", errCreatingRequest, err)
	}

	return f.handleTokenRequest(req)
}

// modifyTelemetryConfig can update the telemetry config returned by the fakeserver.
// via the fakeserver's modify_telemetry_config endpoint.
func (f *fakeServerClient) modifyTelemetryConfig(payload *modifyTelemetryConfigBody) error {
	url := fmt.Sprintf("https://%s/modify_telemetry_config", f.tunnel)
	payloadBuf := new(bytes.Buffer)
	json.NewEncoder(payloadBuf).Encode(payload)

	req, err := http.NewRequest("POST", url, payloadBuf)
	if err != nil {
		return fmt.Errorf("%w: %w", errCreatingRequest, err)
	}

	return f.handleRequest(req)
}

// validateMetrics queries the fakeserver's validation endpoint, which verifies metrics
// are exported successfully with the expected labels and filters.
func (f *fakeServerClient) validateMetrics(payload *validationBody) error {
	payloadBuf := new(bytes.Buffer)
	json.NewEncoder(payloadBuf).Encode(payload)

	url := fmt.Sprintf("https://%s/validation", f.tunnel)
	req, err := http.NewRequest("POST", url, payloadBuf)
	if err != nil {
		return fmt.Errorf("%w: %w", errCreatingRequest, err)
	}

	return f.handleRequest(req)
}

// handleTokenRequest returns a token if the request is succesful.
func (f *fakeServerClient) handleTokenRequest(req *http.Request) (string, error) {
	resp, err := f.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w : %w", errMakingRequest, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("%w : %w", errReadingBody, err)
	}

	var tokenResponse TokenResponse
	err = json.Unmarshal(body, &tokenResponse)
	if err != nil {
		return "", fmt.Errorf("%w : %w", errParsingBody, err)
	}

	return tokenResponse.Token, nil
}

// handleRequest makes a request to any endpoint and handles errors.
func (f *fakeServerClient) handleRequest(req *http.Request) error {
	resp, err := f.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w : %w", errMakingRequest, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusExpectationFailed {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("%w : %w", errReadingBody, err)
		}

		var message errMsg
		err = json.Unmarshal(body, &message)
		if err != nil {
			return fmt.Errorf("%w : %w", errParsingBody, err)
		}

		return errValidation
	}

	if resp.StatusCode != http.StatusOK {
		return errUnexpectedStatusCode
	}

	return nil
}
