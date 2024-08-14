// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package envoy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"k8s.io/apimachinery/pkg/util/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCallLoggingEndpoint(t *testing.T) {
	t.Parallel()
	rawLogLevels, err := os.ReadFile("testdata/fetch_debug_levels.txt")
	require.NoError(t, err)
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(rawLogLevels)
	}))

	defer mockServer.Close()

	mpf := &mockPortForwarder{
		openBehavior: func(ctx context.Context) (string, error) {
			return strings.Replace(mockServer.URL, "http://", "", 1), nil
		},
	}
	logLevels, err := CallLoggingEndpoint(context.Background(), mpf, NewLoggerParams())
	require.NoError(t, err)
	require.Equal(t, testLogConfig(), logLevels)
}

const (
	testConfigDump = "test_config_dump.json"
	testClusters   = "test_clusters.json"
)

func TestUnmarshaling(t *testing.T) {
	var envoyConfig EnvoyConfig
	err := json.Unmarshal(rawEnvoyConfig(t), &envoyConfig)
	require.NoError(t, err)

	require.Equal(t, testEnvoyConfig.Clusters, envoyConfig.Clusters)
	require.Equal(t, testEnvoyConfig.Endpoints, envoyConfig.Endpoints)
	require.Equal(t, testEnvoyConfig.Listeners, envoyConfig.Listeners)
	require.Equal(t, testEnvoyConfig.Routes, envoyConfig.Routes)
	require.Equal(t, testEnvoyConfig.Secrets, envoyConfig.Secrets)
}

func TestJSON(t *testing.T) {
	raw, err := os.ReadFile(fmt.Sprintf("testdata/%s", testConfigDump))
	require.NoError(t, err)
	expected := bytes.TrimSpace(raw)

	var envoyConfig EnvoyConfig
	err = json.Unmarshal(raw, &envoyConfig)
	require.NoError(t, err)

	actual := envoyConfig.JSON()

	require.Equal(t, expected, actual)
}

func TestFetchConfig(t *testing.T) {
	configDump, err := os.ReadFile(fmt.Sprintf("testdata/%s", testConfigDump))
	require.NoError(t, err)

	clusters, err := os.ReadFile(fmt.Sprintf("testdata/%s", testClusters))
	require.NoError(t, err)

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/config_dump" {
			w.Write(configDump)
		}
		if r.URL.Path == "/clusters" {
			w.Write(clusters)
		}
	}))
	defer mockServer.Close()

	mpf := &mockPortForwarder{
		openBehavior: func(ctx context.Context) (string, error) {
			return strings.Replace(mockServer.URL, "http://", "", 1), nil
		},
	}

	envoyConfig, err := FetchConfig(context.Background(), mpf)

	require.NoError(t, err)

	require.Equal(t, testEnvoyConfig.Clusters, envoyConfig.Clusters)
	require.Equal(t, testEnvoyConfig.Endpoints, envoyConfig.Endpoints)
	require.Equal(t, testEnvoyConfig.Listeners, envoyConfig.Listeners)
	require.Equal(t, testEnvoyConfig.Routes, envoyConfig.Routes)
	require.Equal(t, testEnvoyConfig.Secrets, envoyConfig.Secrets)
}

// There are many protobuf types for filter extensions. This test ensures
// that the different types are formatted correctly.
func TestFormatFilters(t *testing.T) {
	cases := map[string]struct {
		filter   filter
		expected string
	}{
		"Connection Limit": {
			filter: filter{
				TypedConfig: filterTypedConfig{
					Type:           "type.googleapis.com/envoy.extensions.filters.network.connection_limit.v3.ConnectionLimit",
					MaxConnections: 42,
					Delay:          "200s",
				},
			},
			expected: "Connection limit: 42 max connections with 200s delay",
		},
		"Direct Response with file": {
			filter: filter{
				TypedConfig: filterTypedConfig{
					Type: "type.googleapis.com/envoy.extensions.filters.network.direct_response.v3.Config",
					Response: filterResponse{
						Filename: "cat-photo.jpg",
					},
				},
			},
			expected: "Direct response: -> file:cat-photo.jpg",
		},
		"Direct Response with bytes": {
			filter: filter{
				TypedConfig: filterTypedConfig{
					Type: "type.googleapis.com/envoy.extensions.filters.network.direct_response.v3.Config",
					Response: filterResponse{
						InlineBytes: []byte("abcd"),
					},
				},
			},
			expected: "Direct response: -> bytes:abcd",
		},
		"Direct Response with lots of bytes": {
			filter: filter{
				TypedConfig: filterTypedConfig{
					Type: "type.googleapis.com/envoy.extensions.filters.network.direct_response.v3.Config",
					Response: filterResponse{
						InlineBytes: []byte("abcdefghijklmnopqrstuvwxyz"),
					},
				},
			},
			expected: "Direct response: -> bytes:abcdefghijklmnopqrstuvwx...",
		},
		"Direct Response with string": {
			filter: filter{
				TypedConfig: filterTypedConfig{
					Type: "type.googleapis.com/envoy.extensions.filters.network.direct_response.v3.Config",
					Response: filterResponse{
						InlineString: "efgh",
					},
				},
			},
			expected: "Direct response: -> string:efgh",
		},
		"Direct Response with long string": {
			filter: filter{
				TypedConfig: filterTypedConfig{
					Type: "type.googleapis.com/envoy.extensions.filters.network.direct_response.v3.Config",
					Response: filterResponse{
						InlineString: "<!DOCTYPE html><html lang=\"en\"><head><meta name=\"viewport\" content=\"width=device-width\"/>",
					},
				},
			},
			expected: "Direct response: -> string:<!DOCTYPE html><html lan...",
		},
		"Direct Response with environment variable": {
			filter: filter{
				TypedConfig: filterTypedConfig{
					Type: "type.googleapis.com/envoy.extensions.filters.network.direct_response.v3.Config",
					Response: filterResponse{
						EnvironmentVariable: "POSTGRESS_CONNECTION_STRING",
					},
				},
			},
			expected: "Direct response: -> env:POSTGRESS_CONNECTION_STRING",
		},
		"Echo": {
			filter: filter{
				TypedConfig: filterTypedConfig{
					Type: "type.googleapis.com/envoy.extensions.filters.network.echo.v3.Echo",
				},
			},
			expected: "Echo: upstream will respond with the data it receives.",
		},
		"External Authorization using Envoy gRPC": {
			filter: filter{
				TypedConfig: filterTypedConfig{
					Type: "type.googleapis.com/envoy.extensions.filters.network.ext_authz.v3.ExtAuthz",
					GrpcService: filterGrpcService{
						EnvoyGrpc: filterEnvoyGrpc{
							ClusterName: "auth-server",
						},
					},
				},
			},
			expected: "External authorization: auth-server",
		},
		"External Authorization using Google gRPC": {
			filter: filter{
				TypedConfig: filterTypedConfig{
					Type: "type.googleapis.com/envoy.extensions.filters.network.ext_authz.v3.ExtAuthz",
					GrpcService: filterGrpcService{
						GoogleGrpc: filterGoogleGrpc{
							TargetUri: "auth.endpoint",
						},
					},
				},
			},
			expected: "External authorization: auth.endpoint",
		},
		"External Authorization unset": {
			filter: filter{
				TypedConfig: filterTypedConfig{
					Type: "type.googleapis.com/envoy.extensions.filters.network.ext_authz.v3.ExtAuthz",
				},
			},
			expected: "External authorization: No upstream configured.",
		},
		"HTTP Connection Manager": {
			filter: filter{
				TypedConfig: filterTypedConfig{
					Type: "type.googleapis.com/envoy.extensions.filters.network.http_connection_manager.v3.HttpConnectionManager",
					RouteConfig: filterRouteConfig{
						Name: "public_listener",
						VirtualHosts: []filterVirtualHost{
							{
								Name:    "public_listener",
								Domains: []string{"*"},
								Routes: []filterRoute{
									{
										Match: filterMatch{
											Prefix: "/",
										},
										Route: filterRouteCluster{
											Cluster: "local_app",
										},
									},
								},
							},
						},
					},
				},
			},
			expected: "HTTP: * -> local_app/",
		},
		"Local Ratelimit": {
			filter: filter{
				TypedConfig: filterTypedConfig{
					Type: "type.googleapis.com/envoy.extensions.filters.network.local_ratelimit.v3.LocalRateLimit",
					TokenBucket: filterTokenBucket{
						MaxTokens:     24,
						TokensPerFill: 2,
						FillInterval:  "20s",
					},
				},
			},
			expected: "Local rate limit: tokens: max 24 per-fill 2, interval: 20s",
		},
		"Ratelimit with descriptors": {
			filter: filter{
				TypedConfig: filterTypedConfig{
					Type:   "type.googleapis.com/envoy.extensions.filters.network.ratelimit.v3.RateLimit",
					Domain: "database-ratelimit",
					Descriptors: []filterRateLimitDescriptor{
						{
							Entries: []filterRateLimitDescriptorEntry{
								{
									Key:   "PATH",
									Value: "/users",
								},
							},
							Limit: filterRateLimitOverride{
								RequestsPerUnit: 42,
								Unit:            "MINUTE",
							},
						},
					},
				},
			},
			expected: "Rate limit: database-ratelimit PATH:/users 42 req per minute",
		},
		"Ratelimit with EnvoyGrpc definted limiter allowing failures": {
			filter: filter{
				TypedConfig: filterTypedConfig{
					Type:            "type.googleapis.com/envoy.extensions.filters.network.ratelimit.v3.RateLimit",
					Domain:          "database-ratelimit",
					FailureModeDeny: true,
					RateLimitService: filterRateLimitServiceConfig{
						GrpcService: filterGrpcService{
							EnvoyGrpc: filterEnvoyGrpc{
								ClusterName: "ratelimiter.service",
							},
						},
					},
				},
			},
			expected: "Rate limit: database-ratelimit using ratelimiter.service will deny if unreachable",
		},
		"Ratelimit with GoogleGrpc defined limiter": {
			filter: filter{
				TypedConfig: filterTypedConfig{
					Type:   "type.googleapis.com/envoy.extensions.filters.network.ratelimit.v3.RateLimit",
					Domain: "database-ratelimit",
					RateLimitService: filterRateLimitServiceConfig{
						GrpcService: filterGrpcService{
							GoogleGrpc: filterGoogleGrpc{
								TargetUri: "ratelimiter.service",
							},
						},
					},
				},
			},
			expected: "Rate limit: database-ratelimit using ratelimiter.service",
		},
		"RBAC": {
			filter: filter{
				TypedConfig: filterTypedConfig{
					Type: "type.googleapis.com/envoy.extensions.filters.network.rbac.v3.RBAC",
					Rules: filterRules{
						Action: "DENY",
						Policies: filterHttpTypedConfigPolicies{
							ConsulIntentions: filterHttpTypedConfigConsulIntentions{
								Principals: []principal{
									{
										Authenticated: authenticated{
											PrincipalName: principalName{
												SafeRegex: safeRegex{
													Regex: "^spiffe://[^/]+/ns/default/dc/[^/]+/svc/client$",
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			expected: "RBAC: DENY ^spiffe://[^/]+/ns/default/dc/[^/]+/svc/client$",
		},
		"SNI Cluster": {
			filter: filter{
				TypedConfig: filterTypedConfig{
					Type: "type.googleapis.com/envoy.extensions.filters.network.sni_cluster.v3.SniCluster",
				},
			},
			expected: "SNI: Upstream cluster name set by SNI field in TLS connection.",
		},
		"TCP Proxy with cluster": {
			filter: filter{
				TypedConfig: filterTypedConfig{
					Type:    "type.googleapis.com/envoy.extensions.filters.network.tcp_proxy.v3.TcpProxy",
					Cluster: "server.default.dc1.internal.bc3815c2-1a0f-f3ff-a2e9-20d791f08d00.consul",
				},
			},
			expected: "TCP: -> server",
		},
		"TCP Proxy without cluster": {
			filter: filter{
				TypedConfig: filterTypedConfig{
					Type: "type.googleapis.com/envoy.extensions.filters.network.tcp_proxy.v3.TcpProxy",
				},
			},
			expected: "TCP: No upstream cluster configured.",
		},
		"Unknown format": {
			filter: filter{
				TypedConfig: filterTypedConfig{
					Type: "type.googleapis.com/envoy.extensions.filters.network.NewFormat",
				},
			},
			expected: "Unknown filter: type.googleapis.com/envoy.extensions.filters.network.NewFormat",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			actual := formatFilters([]filter{tc.filter})[0]
			require.Equal(t, tc.expected, actual)
		})
	}
}

// TestClusterParsingEndpoints checks that the parseClusters function correctly
// maps endpoints from the config dump and cluster mapping into a config object.
// This includes omitting endpoints that are defined by domain names as seen in
// Logical DNS clusters.
func TestClusterParsingEndpoints(t *testing.T) {
	expected := []Cluster{
		{
			Name:                     "logical_dns_cluster",
			FullyQualifiedDomainName: "logical_dns_cluster.service.host",
			Endpoints:                []string{"192.168.18.110:20000", "192.168.52.101:20000", "192.168.65.131:20000"},
			Type:                     "LOGICAL_DNS",
			LastUpdated:              "2022-08-30T12:31:03.354Z",
		},
	}

	rawCfg := map[string]interface{}{
		"dynamic_active_clusters": []map[string]interface{}{
			{
				"cluster": map[string]interface{}{
					"name": "logical_dns_cluster.service.host",
					"type": "LOGICAL_DNS",
					"load_assignment": map[string]interface{}{
						"endpoints": []map[string]interface{}{
							{
								"lb_endpoints": []map[string]interface{}{
									{
										"endpoint": map[string]interface{}{
											"address": map[string]interface{}{
												"socket_address": map[string]interface{}{
													"address":    "192.168.18.110",
													"port_value": 20000,
												},
											},
										},
									},
									{
										"endpoints": map[string]interface{}{
											"address": map[string]interface{}{
												"socket_address": map[string]interface{}{
													"address":    "192.168.52.101",
													"port_value": 20000,
												},
											},
										},
									},
									{
										"endpoints": map[string]interface{}{
											"address": map[string]interface{}{
												"socket_address": map[string]interface{}{
													"address":    "domain-name",
													"port_value": 20000,
												},
											},
										},
									},
								},
							},
						},
					},
				},
				"last_updated": "2022-08-30T12:31:03.354Z",
			},
		},
	}
	clusterMapping := map[string][]string{
		"logical_dns_cluster.service.host": {"192.168.52.101:20000", "192.168.65.131:20000"},
	}

	actual, err := parseClusters(rawCfg, clusterMapping)
	require.NoError(t, err)

	require.Equal(t, expected, actual)
}

func rawEnvoyConfig(t *testing.T) []byte {
	configDump, err := os.ReadFile(fmt.Sprintf("testdata/%s", testConfigDump))
	require.NoError(t, err)

	clusters, err := os.ReadFile(fmt.Sprintf("testdata/%s", testClusters))
	require.NoError(t, err)

	return []byte(fmt.Sprintf("{\n\"config_dump\":%s,\n\"clusters\":%s}", string(configDump), string(clusters)))
}

// testEnvoyConfig is what we expect the config at `test_config_dump.json` to be.
var testEnvoyConfig = &EnvoyConfig{
	Clusters: []Cluster{
		{Name: "local_agent", FullyQualifiedDomainName: "local_agent", Endpoints: []string{"192.168.79.187:8502"}, Type: "STATIC", LastUpdated: "2022-05-13T04:22:39.553Z"},
		{Name: "client", FullyQualifiedDomainName: "client.default.dc1.internal.bc3815c2-1a0f-f3ff-a2e9-20d791f08d00.consul", Endpoints: []string{"192.168.18.110:20000", "192.168.52.101:20000", "192.168.65.131:20000"}, Type: "EDS", LastUpdated: "2022-08-10T12:30:32.326Z"},
		{Name: "frontend", FullyQualifiedDomainName: "frontend.default.dc1.internal.bc3815c2-1a0f-f3ff-a2e9-20d791f08d00.consul", Endpoints: []string{"192.168.63.120:20000"}, Type: "EDS", LastUpdated: "2022-08-10T12:30:32.233Z"},
		{Name: "local_app", FullyQualifiedDomainName: "local_app", Endpoints: []string{"127.0.0.1:8080"}, Type: "STATIC", LastUpdated: "2022-05-13T04:22:39.655Z"},
		{Name: "original-destination", FullyQualifiedDomainName: "original-destination", Endpoints: []string{}, Type: "ORIGINAL_DST", LastUpdated: "2022-05-13T04:22:39.743Z"},
	},
	Endpoints: []Endpoint{
		{Address: "192.168.79.187:8502", Cluster: "local_agent", Weight: 1, Status: "HEALTHY"},
		{Address: "192.168.18.110:20000", Cluster: "client", Weight: 1, Status: "HEALTHY"},
		{Address: "192.168.52.101:20000", Cluster: "client", Weight: 1, Status: "HEALTHY"},
		{Address: "192.168.65.131:20000", Cluster: "client", Weight: 1, Status: "HEALTHY"},
		{Address: "192.168.63.120:20000", Cluster: "frontend", Weight: 1, Status: "HEALTHY"},
		{Address: "127.0.0.1:8080", Cluster: "local_app", Weight: 1, Status: "HEALTHY"},
	},
	Listeners: []Listener{
		{Name: "public_listener", Address: "192.168.69.179:20000", FilterChain: []FilterChain{{Filters: []string{"HTTP: * -> local_app/"}, FilterChainMatch: "Any"}}, Direction: "INBOUND", LastUpdated: "2022-08-10T12:30:47.142Z"},
		{Name: "outbound_listener", Address: "127.0.0.1:15001", FilterChain: []FilterChain{
			{Filters: []string{"TCP: -> client"}, FilterChainMatch: "10.100.134.173/32, 240.0.0.3/32"},
			{Filters: []string{"TCP: -> frontend"}, FilterChainMatch: "10.100.31.2/32, 240.0.0.5/32"},
			{Filters: []string{"TCP: -> original-destination"}, FilterChainMatch: "Any"},
		}, Direction: "OUTBOUND", LastUpdated: "2022-07-18T15:31:03.246Z"},
	},
	Routes: []Route{
		{
			Name:               "public_listener",
			DestinationCluster: "local_app/",
			LastUpdated:        "2022-08-10T12:30:47.141Z",
		},
	},
	Secrets: []Secret{
		{
			Name:        "default",
			Type:        "Dynamic Active",
			LastUpdated: "2022-05-24T17:41:59.078Z",
		},
		{
			Name:        "ROOTCA",
			Type:        "Dynamic Warming",
			LastUpdated: "2022-03-15T05:14:22.868Z",
		},
	},
}

type mockPortForwarder struct {
	openBehavior func(context.Context) (string, error)
}

func (m *mockPortForwarder) Open(ctx context.Context) (string, error) { return m.openBehavior(ctx) }
func (m *mockPortForwarder) Close()                                   {}
func (m *mockPortForwarder) GetLocalPort() int                        { return int(rand.Int63nRange(0, 65535)) }

func testLogConfig() map[string]string {
	cfg := make(map[string]string, len(EnvoyLoggers))
	for k := range EnvoyLoggers {
		cfg[k] = "debug"
	}
	return cfg
}
