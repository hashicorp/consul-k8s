package read

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

//go:embed test_config_dump.json test_clusters.json
var fs embed.FS

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
	raw, err := fs.ReadFile(testConfigDump)
	require.NoError(t, err)
	expected := bytes.TrimSpace(raw)

	var envoyConfig EnvoyConfig
	err = json.Unmarshal(raw, &envoyConfig)
	require.NoError(t, err)

	actual := envoyConfig.JSON()

	require.Equal(t, expected, actual)
}

func TestFetchConfig(t *testing.T) {
	configDump, err := fs.ReadFile(testConfigDump)
	require.NoError(t, err)

	clusters, err := fs.ReadFile(testClusters)
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

type mockPortForwarder struct {
	openBehavior func(context.Context) (string, error)
}

func (m *mockPortForwarder) Open(ctx context.Context) (string, error) { return m.openBehavior(ctx) }
func (m *mockPortForwarder) Close()                                   {}

func rawEnvoyConfig(t *testing.T) []byte {
	configDump, err := fs.ReadFile(testConfigDump)
	require.NoError(t, err)

	clusters, err := fs.ReadFile(testClusters)
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
		{Name: "public_listener", Address: "192.168.69.179:20000", FilterChain: []FilterChain{{Filters: []string{"* to local_app/"}, FilterChainMatch: "Any"}}, Direction: "INBOUND", LastUpdated: "2022-08-10T12:30:47.142Z"},
		{Name: "outbound_listener", Address: "127.0.0.1:15001", FilterChain: []FilterChain{
			{Filters: []string{"to client.default.dc1.internal.bc3815c2-1a0f-f3ff-a2e9-20d791f08d00.consul"}, FilterChainMatch: "10.100.134.173/32, 240.0.0.3/32"},
			{Filters: []string{"to frontend.default.dc1.internal.bc3815c2-1a0f-f3ff-a2e9-20d791f08d00.consul"}, FilterChainMatch: "10.100.31.2/32, 240.0.0.5/32"},
			{Filters: []string{"to original-destination"}, FilterChainMatch: "Any"},
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
