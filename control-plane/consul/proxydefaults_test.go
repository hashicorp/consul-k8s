package consul

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/hashicorp/consul-server-connection-manager/discovery"
	capi "github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestFetchProxyDefaultsFromConsul(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		config           *Config
		serverConnMgr    ServerConnectionManager
		setupMockServer  func() *httptest.Server
		expectedEntry    *capi.ProxyConfigEntry
		expectError      bool
		expectedErrorMsg string
	}{
		"successfully fetches proxy-defaults with access logs": {
			setupMockServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/v1/config/proxy-defaults/global" {
						response := map[string]interface{}{
							"Kind":      "proxy-defaults",
							"Name":      "global",
							"Namespace": "default",
							"Config": map[string]interface{}{
								"envoy_dogstatsd_url":   "udp://127.0.0.1:9125",
								"envoy_access_log_path": "/var/log/consul/access.log",
							},
						}
						w.Header().Set("Content-Type", "application/json")
						json.NewEncoder(w).Encode(response)
						return
					}
					w.WriteHeader(http.StatusNotFound)
				}))
			},
			expectedEntry: &capi.ProxyConfigEntry{
				Kind: capi.ProxyDefaults,
				Name: capi.ProxyConfigGlobal,
				Config: map[string]interface{}{
					"envoy_dogstatsd_url":   "udp://127.0.0.1:9125",
					"envoy_access_log_path": "/var/log/consul/access.log",
				},
			},
			expectError: false,
		},
		"successfully fetches proxy-defaults without access logs": {
			setupMockServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/v1/config/proxy-defaults/global" {
						response := map[string]interface{}{
							"Kind":      "proxy-defaults",
							"Name":      "global",
							"Namespace": "default",
							"Config": map[string]interface{}{
								"envoy_dogstatsd_url": "udp://127.0.0.1:9125",
							},
						}
						w.Header().Set("Content-Type", "application/json")
						json.NewEncoder(w).Encode(response)
						return
					}
					w.WriteHeader(http.StatusNotFound)
				}))
			},
			expectedEntry: &capi.ProxyConfigEntry{
				Kind: capi.ProxyDefaults,
				Name: capi.ProxyConfigGlobal,
				Config: map[string]interface{}{
					"envoy_dogstatsd_url": "udp://127.0.0.1:9125",
				},
			},
			expectError: false,
		},
		"returns nil when proxy-defaults not found (404)": {
			setupMockServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/v1/config/proxy-defaults/global" {
						w.WriteHeader(http.StatusNotFound)
						json.NewEncoder(w).Encode(map[string]string{
							"error": "Config entry not found",
						})
						return
					}
					w.WriteHeader(http.StatusNotFound)
				}))
			},
			expectedEntry: nil,
			expectError:   false,
		},
		"returns error when config is nil": {
			config:           nil,
			expectedEntry:    nil,
			expectError:      true,
			expectedErrorMsg: "consul config is not defined",
		},
		"returns error when consul server returns 500": {
			setupMockServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/v1/config/proxy-defaults/global" {
						w.WriteHeader(http.StatusInternalServerError)
						json.NewEncoder(w).Encode(map[string]string{
							"error": "Internal server error",
						})
						return
					}
					w.WriteHeader(http.StatusNotFound)
				}))
			},
			expectedEntry:    nil,
			expectError:      true,
			expectedErrorMsg: "error fetching global proxy-defaults",
		},
		"successfully fetches with ServerConnectionManager": {
			setupMockServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/v1/config/proxy-defaults/global" {
						response := map[string]interface{}{
							"Kind":      "proxy-defaults",
							"Name":      "global",
							"Namespace": "default",
							"Config": map[string]interface{}{
								"protocol": "http",
							},
						}
						w.Header().Set("Content-Type", "application/json")
						json.NewEncoder(w).Encode(response)
						return
					}
					w.WriteHeader(http.StatusNotFound)
				}))
			},
			expectedEntry: &capi.ProxyConfigEntry{
				Kind: capi.ProxyDefaults,
				Name: capi.ProxyConfigGlobal,
				Config: map[string]interface{}{
					"protocol": "http",
				},
			},
			expectError: false,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var config *Config
			var serverConnMgr ServerConnectionManager

			// Setup mock server if provided
			var mockServer *httptest.Server
			if tc.setupMockServer != nil {
				mockServer = tc.setupMockServer()
				defer mockServer.Close()

				// Parse server URL
				parsedURL, err := url.Parse(mockServer.URL)
				require.NoError(t, err)

				host := strings.Split(parsedURL.Host, ":")[0]
				port, err := strconv.Atoi(parsedURL.Port())
				require.NoError(t, err)

				// Create config
				config = &Config{
					APIClientConfig: &capi.Config{
						Address: mockServer.URL,
						Scheme:  "http",
					},
					HTTPPort: port,
				}

				// Setup ServerConnectionManager if needed
				if strings.Contains(name, "ServerConnectionManager") {
					serverConnMgr = MockConnMgrForIPAndPort(t, host, port, false)
				}
			}

			// Override config if test case provides it
			if tc.config != nil {
				config = tc.config
			}
			if tc.serverConnMgr != nil {
				serverConnMgr = tc.serverConnMgr
			}

			// Call the function
			result, err := FetchProxyDefaultsFromConsul(config, serverConnMgr)

			// Assert results
			if tc.expectError {
				require.Error(t, err)
				if tc.expectedErrorMsg != "" {
					require.Contains(t, err.Error(), tc.expectedErrorMsg)
				}
				require.Nil(t, result)
			} else {
				require.NoError(t, err)
				if tc.expectedEntry == nil {
					require.Nil(t, result)
				} else {
					require.NotNil(t, result)
					require.Equal(t, tc.expectedEntry.Kind, result.Kind)
					require.Equal(t, tc.expectedEntry.Name, result.Name)
					require.Equal(t, tc.expectedEntry.Config, result.Config)
				}
			}
		})
	}
}

func MockConnMgrForIPAndPort(t *testing.T, ip string, port int, enableGRPCConn bool) *MockServerConnectionManager {
	parsedIP := net.ParseIP(ip)
	connMgr := &MockServerConnectionManager{}

	mockState := discovery.State{
		Address: discovery.Addr{
			TCPAddr: net.TCPAddr{
				IP:   parsedIP,
				Port: port,
			},
		},
	}

	// If the connection is enabled, some tests will receive extra HTTP API calls where
	// the server is being dialed.
	if enableGRPCConn {
		conn, err := grpc.DialContext(
			context.Background(),
			net.JoinHostPort(parsedIP.String(), strconv.Itoa(port)),
			grpc.WithTransportCredentials(insecure.NewCredentials()))
		require.NoError(t, err)
		mockState.GRPCConn = conn
	}
	connMgr.On("State").Return(mockState, nil)
	connMgr.On("Run").Return(nil)
	connMgr.On("Stop").Return(nil)
	return connMgr
}
