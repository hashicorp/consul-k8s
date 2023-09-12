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
	validationPathConsul    = "/v1/metrics/consul"
	validationPathCollector = "/v1/metrics/collector"
)

type fakeServerClient struct {
	client *http.Client
	tunnel string
}

type errMsg struct {
	Error string `json:"error"`
}

type TokenResponse struct {
	Token string `json:"token"`
}

type modifyTelemetryConfigBody struct {
	Filters  []string          `json:"filters"`
	Labels   map[string]string `json:"labels"`
	Disabled bool              `json:"disabled"`
	Time     int64             `json:"time"`
}

type validationBody struct {
	Path                 string   `json:"path"`
	ExpectedLabelKeys    []string `json:"expectedLabelKeys"`
	DisallowedMetricName string   `json:"disallowedMetricName"`
	ExpectedMetricName   string   `json:"expectedMetricName"`
	MetricsDisabled      bool     `json:"metricsDisabled"`
	FilterRecordsSince   int64    `json:"filterRecordsSince"`
}

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
func (f *fakeServerClient) requestToken(endpoint string) (string, error) {
	url := fmt.Sprintf("https://%s/token", endpoint)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		fmt.Println("Error creating request:", err)
		return "", errors.New("error creating /token request")
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
		fmt.Println("Error creating request:", err)
		return errors.New("error creating modify_telemetry_config request")
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
		return errors.New("error creating /validation request")
	}

	return f.handleRequest(req)
}

// handleTokenRequest returns a token if the request is succesful.
func (f *fakeServerClient) handleTokenRequest(req *http.Request) (string, error) {
	// Perform request
	resp, err := f.client.Do(req)
	if err != nil {
		fmt.Println("Error sending request:", err)
		return "", errors.New("error making request")
	}
	defer resp.Body.Close()

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("Error reading response:", err)
		return "", errors.New("error reading body")
	}

	var tokenResponse TokenResponse
	err = json.Unmarshal(body, &tokenResponse)
	if err != nil {
		fmt.Println("Error parsing response:", err)
		return "", errors.New("error parsing body")
	}

	return tokenResponse.Token, nil
}

// handleRequest makes a request to any endpoint and handles errors.
func (f *fakeServerClient) handleRequest(req *http.Request) error {
	resp, err := f.client.Do(req)
	if err != nil {
		return errors.New("error making request")
	}

	if resp.StatusCode == http.StatusExpectationFailed {
		// Read the response body
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			fmt.Println("Error reading response:", err)
			return errors.New("error reading body")
		}
		var message errMsg
		err = json.Unmarshal(body, &message)
		if err != nil {
			fmt.Println("Error parsing response:", err)
			return errors.New("error parsing body")
		}

		return fmt.Errorf("failed validation: %s", message)
	}

	if resp.StatusCode != http.StatusOK {
		return errors.New("unexpected status code response from failure")
	}

	return nil
}
