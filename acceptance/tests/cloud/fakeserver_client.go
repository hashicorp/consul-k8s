// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package cloud

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
)

const (
	// recordsPathConsul and recordsPathCollector distinguish metrics for consul vs. collector when fetching records.
	recordsPathConsul    = "v1/metrics/consul"
	recordsPathCollector = "v1/metrics/collector"
)

var (
	errEncodingPayload = errors.New("failed to encode payload")
	errCreatingRequest = errors.New("failed to create HTTP request")
	errMakingRequest   = errors.New("failed to make request")
	errReadingBody     = errors.New("failed to read body")
	errClosingBody     = errors.New("failed to close body")
	errParsingBody     = errors.New("failed to parse body")
)

// fakeServerClient provides an interface to communicate with the fakesever (a fake HCP Telemetry Gateway) via HTTP.
type fakeServerClient struct {
	client *http.Client
	tunnel string
}

// modifyTelemetryConfigBody is a POST body that provides telemetry config changes to the fakeserver.
type modifyTelemetryConfigBody struct {
	Filters  []string          `json:"filters"`
	Labels   map[string]string `json:"labels"`
	Disabled bool              `json:"disabled"`
}

// TokenResponse is used to read a token response from the fakeserver.
type TokenResponse struct {
	Token string `json:"token"`
}

// RecordsResponse is used to read a /records response from the fakeserver.
type RecordsResponse struct {
	Records []*RequestRecord `json:"records"`
}

// RequestRecord holds info about a single request.
type RequestRecord struct {
	Method       string `json:"method"`
	Path         string `json:"path"`
	Body         []byte `json:"body"`
	ValidRequest bool   `json:"validRequest"`
	Timestamp    int64  `json:"timestamp"`
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

	resp, err := f.handleRequest(req)
	if err != nil {
		return "", err
	}

	tokenResponse := &TokenResponse{}
	err = json.Unmarshal(resp, tokenResponse)
	if err != nil {
		return "", fmt.Errorf("%w : %w", errParsingBody, err)
	}

	return tokenResponse.Token, nil
}

// modifyTelemetryConfig can update the telemetry config returned by the fakeserver.
// via the fakeserver's modify_telemetry_config endpoint.
func (f *fakeServerClient) modifyTelemetryConfig(payload *modifyTelemetryConfigBody) error {
	url := fmt.Sprintf("https://%s/modify_telemetry_config", f.tunnel)
	payloadBuf := new(bytes.Buffer)

	err := json.NewEncoder(payloadBuf).Encode(payload)
	if err != nil {
		return fmt.Errorf("%w:%w", errEncodingPayload, err)
	}

	req, err := http.NewRequest("POST", url, payloadBuf)
	if err != nil {
		return fmt.Errorf("%w: %w", errCreatingRequest, err)
	}

	_, err = f.handleRequest(req)

	return err
}

func (f *fakeServerClient) getRecordsForPath(path string, refreshTime int64) ([]*RequestRecord, error) {
	url := fmt.Sprintf("https://%s/records/%s", f.tunnel, path)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errCreatingRequest, err)
	}
	if refreshTime > 0 {
		q := req.URL.Query()
		q.Add("since", strconv.FormatInt(refreshTime, 10))
		req.URL.RawQuery = q.Encode()
	}

	resp, err := f.handleRequest(req)
	if err != nil {
		return nil, err
	}

	recordsResponse := &RecordsResponse{}
	err = json.Unmarshal(resp, recordsResponse)
	if err != nil {
		return nil, fmt.Errorf("%w : %w", errParsingBody, err)
	}

	return recordsResponse.Records, nil
}

// handleRequest returns the response body if the request is succesful.
func (f *fakeServerClient) handleRequest(req *http.Request) ([]byte, error) {
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w : %w", errMakingRequest, err)
	}
	body, err := io.ReadAll(resp.Body)
	cErr := resp.Body.Close()
	if cErr != nil {
		return nil, fmt.Errorf("%w : %w", errClosingBody, err)
	}
	if err != nil {
		return nil, fmt.Errorf("%w : %w", errReadingBody, err)
	}

	return body, nil
}
