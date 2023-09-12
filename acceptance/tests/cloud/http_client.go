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

type httpClient struct {
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
	RefreshTime          int64    `json:"refreshTime"`
}

func newHttpClient(tunnel string) *httpClient {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	return &httpClient{
		client: &http.Client{Transport: tr},
		tunnel: tunnel,
	}
}

// The fake-server has a requestToken endpoint to retrieve the token.
func (h *httpClient) requestToken(endpoint string) (string, error) {
	url := fmt.Sprintf("https://%s/token", endpoint)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		fmt.Println("Error creating request:", err)
		return "", errors.New("error creating request")
	}

	// Perform request
	resp, err := h.client.Do(req)
	if err != nil {
		fmt.Println("Error sending request:", err)
		return "", errors.New("error making request")
	}
	defer resp.Body.Close()

	return handleTokenResponse(resp)
}

func (h *httpClient) modifyTelemetryConfig(payload *modifyTelemetryConfigBody) error {
	url := fmt.Sprintf("https://%s/modify_telemetry_config", h.tunnel)
	payloadBuf := new(bytes.Buffer)
	json.NewEncoder(payloadBuf).Encode(payload)

	req, err := http.NewRequest("POST", url, payloadBuf)
	if err != nil {
		fmt.Println("Error creating request:", err)
		return errors.New("error creating modify_telemetry_config request")
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return errors.New("error making modify_telemetry_config request")
	}

	return handleResponse(resp)
}

func (h *httpClient) validateMetrics(payload *validationBody) error {
	payloadBuf := new(bytes.Buffer)
	json.NewEncoder(payloadBuf).Encode(payload)

	url := fmt.Sprintf("https://%s/validation", h.tunnel)
	req, err := http.NewRequest("POST", url, payloadBuf)
	if err != nil {
		return errors.New("error creating validation request")
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return errors.New("error making validation request")
	}

	return handleResponse(resp)
}

func handleTokenResponse(resp *http.Response) (string, error) {
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

func handleResponse(resp *http.Response) error {
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
