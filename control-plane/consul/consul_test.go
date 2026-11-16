// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package consul

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/hashicorp/consul-k8s/version"
	"github.com/hashicorp/consul-server-connection-manager/discovery"
	capi "github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestNewClient(t *testing.T) {
	type APICall struct {
		Method          string
		Path            string
		UserAgentHeader string
	}

	var consulAPICalls []APICall
	consulServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Record all the API calls made.
		consulAPICalls = append(consulAPICalls, APICall{
			Method:          r.Method,
			Path:            r.URL.Path,
			UserAgentHeader: r.UserAgent(),
		})
		fmt.Fprintln(w, "\"leader\"")
	}))
	defer consulServer.Close()
	cfg := capi.DefaultConfig()
	cfg.Address = consulServer.URL
	client, err := NewClient(cfg, 0)
	require.NoError(t, err)
	leader, err := client.Status().Leader()
	require.NoError(t, err)
	require.Equal(t, "leader", leader)

	require.Len(t, consulAPICalls, 1)
	require.Equal(t, APICall{
		Method:          "GET",
		Path:            "/v1/status/leader",
		UserAgentHeader: fmt.Sprintf("consul-k8s/%s", version.GetHumanVersion()),
	}, consulAPICalls[0])
}

func TestNewClient_httpClientDefaultTimeout(t *testing.T) {
	client, err := NewClient(&capi.Config{Address: "http://126.0.0.1"}, 0)
	require.NoError(t, err)
	// arbitrarily calling /agent/checks.  This could be any call.  We are
	// really testing the unreachable address
	_, err = client.Agent().Checks()

	// using concat (+) instead of fmt.Sprintf because string has lots of %s in it that cause issues
	expectedErrorFragment := "Get \"http://126.0.0.1/v1/agent/checks\":"
	expectedErrorFragmentTwo := "(Client.Timeout exceeded while awaiting headers)"

	// Splitting this into two asserts on fragments of the error because the error thrown
	// can be either of the two below and matching on the whole string causes the test tobe flakey
	// "1 error occurred:\n\t* Get \"http://126.0.0.1/v1/agent/checks\": context deadline exceeded (Client.Timeout exceeded while awaiting headers)\n\n"
	// "1 error occurred:\n\t* Get \"http://126.0.0.1/v1/agent/checks\": dial tcp 126.0.0.1:31200: i/o timeout (Client.Timeout exceeded while awaiting headers)\n\n"
	require.Contains(t, err.Error(), expectedErrorFragment)
	require.Contains(t, err.Error(), expectedErrorFragmentTwo)
	require.Error(t, err, "Get \"http://126.0.0.1/v1/agent/checks\": context deadline exceeded (Client.Timeout exceeded while awaiting headers)")

}

func TestCloneConfigCopiesAPIClientConfig(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		APIClientConfig: &capi.Config{
			Address: "https://consul-server.default.svc",
			Token:   "original-token",
		},
		HTTPPort: 8500,
	}

	cloned := cloneConfig(cfg)

	require.NotSame(t, cfg, cloned)
	require.NotSame(t, cfg.APIClientConfig, cloned.APIClientConfig)
	require.Equal(t, "https://consul-server.default.svc", cfg.APIClientConfig.Address)
	require.Equal(t, "original-token", cfg.APIClientConfig.Token)

	cloned.APIClientConfig.Address = "https://10.0.0.1:8500"
	cloned.APIClientConfig.Token = "runtime-token"

	require.Equal(t, "https://consul-server.default.svc", cfg.APIClientConfig.Address)
	require.Equal(t, "original-token", cfg.APIClientConfig.Token)
}

func TestIsEnterpriseDistribution(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name                 string
		validLicense         bool
		stateFailures        int
		failedRequests       int
		failureStatusCode    int
		expectedEnterprise   bool
		expectedRequests     int
		expectedErrorMessage string
	}{
		{
			name:               "valid license",
			validLicense:       true,
			expectedEnterprise: true,
			expectedRequests:   1,
		},
		{
			name:             "invalid license is not retried",
			expectedRequests: 1,
		},
		{
			name:               "connection manager becomes ready",
			validLicense:       true,
			stateFailures:      1,
			expectedEnterprise: true,
			expectedRequests:   1,
		},
		{
			name:               "license check succeeds on the fifth retry",
			validLicense:       true,
			failedRequests:     5,
			expectedEnterprise: true,
			expectedRequests:   6,
		},
		{
			name:                 "license check exhausts five retries",
			failedRequests:       6,
			expectedRequests:     6,
			expectedErrorMessage: "Unexpected response code: 503",
		},
		{
			name:               "429 Too Many Requests is retried",
			validLicense:       true,
			failedRequests:     1,
			failureStatusCode:  http.StatusTooManyRequests,
			expectedEnterprise: true,
			expectedRequests:   2,
		},
		{
			name:               "408 Request Timeout is retried",
			validLicense:       true,
			failedRequests:     1,
			failureStatusCode:  http.StatusRequestTimeout,
			expectedEnterprise: true,
			expectedRequests:   2,
		},
		{
			name:                 "404 Not Found fails fast without retries",
			failedRequests:       6,
			failureStatusCode:    http.StatusNotFound,
			expectedRequests:     1,
			expectedErrorMessage: "Unexpected response code: 404",
		},
		{
			name:                 "403 Forbidden fails fast without retries",
			failedRequests:       6,
			failureStatusCode:    http.StatusForbidden,
			expectedRequests:     1,
			expectedErrorMessage: "Unexpected response code: 403",
		},
		{
			name:                 "401 Unauthorized fails fast without retries",
			failedRequests:       6,
			failureStatusCode:    http.StatusUnauthorized,
			expectedRequests:     1,
			expectedErrorMessage: "Unexpected response code: 401",
		},
		{
			name:                 "400 Bad Request fails fast without retries",
			failedRequests:       6,
			failureStatusCode:    http.StatusBadRequest,
			expectedRequests:     1,
			expectedErrorMessage: "Unexpected response code: 400",
		},
		{
			name:                 "501 Not Implemented fails fast without retries",
			failedRequests:       6,
			failureStatusCode:    http.StatusNotImplemented,
			expectedRequests:     1,
			expectedErrorMessage: "Unexpected response code: 501",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				var requests int
				cfg := capi.DefaultConfig()
				cfg.Transport = &http.Transport{}
				cfg.Transport.RegisterProtocol("http", roundTripperFunc(func(r *http.Request) (*http.Response, error) {
					require.Equal(t, http.MethodGet, r.Method)
					require.Equal(t, "/v1/operator/license", r.URL.Path)
					requests++
					status := http.StatusOK
					body := fmt.Sprintf(`{"Valid":%t}`, tc.validLicense)
					if requests <= tc.failedRequests {
						status = http.StatusServiceUnavailable
						if tc.failureStatusCode != 0 {
							status = tc.failureStatusCode
						}
						body = "Consul server error"
					}
					return &http.Response{
						StatusCode: status,
						Header:     make(http.Header),
						Body:       io.NopCloser(strings.NewReader(body)),
					}, nil
				}))

				watcher := NewMockServerConnectionManager(t)
				if tc.stateFailures > 0 {
					watcher.On("State").
						Return(discovery.State{}, errors.New("Consul servers are not ready")).
						Times(tc.stateFailures)
				}
				watcher.On("State").Return(discovery.State{
					Address: discovery.Addr{TCPAddr: net.TCPAddr{IP: net.ParseIP("127.0.0.1")}},
					Token:   "runtime-token",
				}, nil).Times(tc.expectedRequests)

				originalAddress := cfg.Address
				originalToken := cfg.Token
				isEnterprise, err := IsEnterpriseDistribution(context.Background(), &Config{
					APIClientConfig: cfg,
					HTTPPort:        8500,
				}, watcher)

				if tc.expectedErrorMessage != "" {
					require.ErrorContains(t, err, tc.expectedErrorMessage)
				} else {
					require.NoError(t, err)
				}
				require.Equal(t, tc.expectedEnterprise, isEnterprise)
				require.Equal(t, tc.expectedRequests, requests)
				require.Equal(t, originalAddress, cfg.Address)
				require.Equal(t, originalToken, cfg.Token)
			})
		})
	}
}

func TestIsEnterpriseDistributionRetryBackoffs(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		var attempts []time.Time
		stateErr := errors.New("Consul servers are not ready")
		watcher := NewMockServerConnectionManager(t)
		watcher.On("State").Return(discovery.State{}, stateErr).
			Run(func(mock.Arguments) {
				attempts = append(attempts, time.Now())
			}).Times(6)

		isEnterprise, err := IsEnterpriseDistribution(context.Background(), &Config{}, watcher)

		require.ErrorIs(t, err, stateErr)
		require.False(t, isEnterprise)
		require.Len(t, attempts, 6)
		require.Equal(t, start, attempts[0])

		for i := range 5 {
			require.Equal(t, 30*time.Second, attempts[i+1].Sub(attempts[i]))
		}
		require.Equal(t, 2*time.Minute+30*time.Second, time.Since(start))
	})
}

func TestIsEnterpriseDistributionRetriesAfterTimeout(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var requests int
		cfg := capi.DefaultConfig()
		cfg.Transport = &http.Transport{}
		cfg.Transport.RegisterProtocol("http", roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			requests++
			if requests == 1 {
				<-r.Context().Done()
				return nil, r.Context().Err()
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"Valid":true}`)),
			}, nil
		}))

		start := time.Now()
		watcher := MockConnMgrForIPAndPort(t, "127.0.0.1", 8500, false)
		isEnterprise, err := IsEnterpriseDistribution(context.Background(), &Config{
			APIClientConfig: cfg,
			HTTPPort:        8500,
			APITimeout:      time.Second,
		}, watcher)

		require.NoError(t, err)
		require.True(t, isEnterprise)
		require.Equal(t, 2, requests)
		require.Equal(t, 31*time.Second, time.Since(start))
		watcher.AssertNumberOfCalls(t, "State", 2)
	})
}

func TestIsEnterpriseDistributionContextCancelled(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		stateErr := errors.New("Consul servers are not ready")
		watcher := NewMockServerConnectionManager(t)
		watcher.On("State").Return(discovery.State{}, stateErr).Once()

		start := time.Now()
		isEnterprise, err := IsEnterpriseDistribution(ctx, &Config{}, watcher)

		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.ErrorContains(t, err, stateErr.Error())
		require.False(t, isEnterprise)
		require.Equal(t, 5*time.Second, time.Since(start))
		watcher.AssertNumberOfCalls(t, "State", 1)
	})
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
