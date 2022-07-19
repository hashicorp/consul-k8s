package read

import (
	"context"
	"embed"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

//go:embed test_config_dump.json
var fs embed.FS

func TestUnmarshaling(t *testing.T) {
	raw, err := fs.ReadFile("test_config_dump.json")
	require.NoError(t, err)

	var envoyConfig EnvoyConfig
	err = json.Unmarshal(raw, &envoyConfig)
	require.NoError(t, err)

	require.Equal(t, testEnvoyConfig.Clusters, envoyConfig.Clusters)
	require.Equal(t, testEnvoyConfig.Endpoints, envoyConfig.Endpoints)
	require.Equal(t, testEnvoyConfig.InboundListeners, envoyConfig.InboundListeners)
	require.Equal(t, testEnvoyConfig.OutboundListeners, envoyConfig.OutboundListeners)
	require.Equal(t, testEnvoyConfig.Routes, envoyConfig.Routes)
	require.Equal(t, testEnvoyConfig.Secrets, envoyConfig.Secrets)
}

func TestFetchConfig(t *testing.T) {
	configResponse, err := fs.ReadFile("test_config_dump.json")
	require.NoError(t, err)

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(configResponse)
	}))
	defer mockServer.Close()

	mpf := &mockPortForwarder{
		openBehavior: func(ctx context.Context) (string, error) {
			return strings.Replace(mockServer.URL, "http://", "", 1), nil
		},
	}

	configDump, err := FetchConfig(context.Background(), mpf)

	require.NoError(t, err)
	require.NotNil(t, configDump)
}

type mockPortForwarder struct {
	openBehavior func(context.Context) (string, error)
}

func (m *mockPortForwarder) Open(ctx context.Context) (string, error) { return m.openBehavior(ctx) }
func (m *mockPortForwarder) Close()                                   {}

// testEnvoyConfig is what we expect the config at `test_config_dump.json` to be.
var testEnvoyConfig = &EnvoyConfig{
	Clusters: []Cluster{
		{
			Name:                     "local_agent",
			FullyQualifiedDomainName: "local_agent",
			Endpoints:                []string{"192.168.79.187:8502"},
			Type:                     "STATIC",
			LastUpdated:              "2022-05-13T04:22:39.553Z",
		},
		{
			Name:                     "client",
			FullyQualifiedDomainName: "client.default.dc1.internal.bc3815c2-1a0f-f3ff-a2e9-20d791f08d00.consul",
			Type:                     "EDS",
			LastUpdated:              "2022-06-09T00:39:12.948Z",
		},
		{
			Name:                     "frontend",
			FullyQualifiedDomainName: "frontend.default.dc1.internal.bc3815c2-1a0f-f3ff-a2e9-20d791f08d00.consul",
			Type:                     "EDS",
			LastUpdated:              "2022-06-09T00:39:12.855Z",
		},
		{
			Name:                     "local_app",
			FullyQualifiedDomainName: "local_app",
			Endpoints:                []string{"127.0.0.1:8080"},
			Type:                     "STATIC",
			LastUpdated:              "2022-05-13T04:22:39.655Z",
		},
		{
			Name:                     "original-destination",
			FullyQualifiedDomainName: "original-destination",
			Type:                     "ORIGINAL_DST",
			LastUpdated:              "2022-05-13T04:22:39.743Z",
		},
		{
			Name:                     "server",
			FullyQualifiedDomainName: "server.default.dc1.internal.bc3815c2-1a0f-f3ff-a2e9-20d791f08d00.consul",
			Type:                     "EDS",
			LastUpdated:              "2022-06-09T00:39:12.754Z",
		},
	},
	Endpoints: []Endpoint{
		{
			Address: "192.168.79.187:8502",
			Cluster: "local_agent",
			Weight:  1,
			Status:  "HEALTHY",
		},
		{
			Address: "127.0.0.1:8080",
			Cluster: "local_app",
			Weight:  1,
			Status:  "HEALTHY",
		},
		{
			Address: "192.168.31.201:20000",
			Weight:  1,
			Status:  "HEALTHY",
		},
		{
			Address: "192.168.47.235:20000",
			Weight:  1,
			Status:  "HEALTHY",
		},
		{
			Address: "192.168.71.254:20000",
			Weight:  1,
			Status:  "HEALTHY",
		},
		{
			Address: "192.168.63.120:20000",
			Weight:  1,
			Status:  "HEALTHY",
		},
		{
			Address: "192.168.18.110:20000",
			Weight:  1,
			Status:  "HEALTHY",
		},
		{
			Address: "192.168.52.101:20000",
			Weight:  1,
			Status:  "HEALTHY",
		},
		{
			Address: "192.168.65.131:20000",
			Weight:  1,
			Status:  "HEALTHY",
		},
	},
	InboundListeners: []InboundListener{
		{
			Name:        "public_listener",
			Address:     "192.168.69.179:20000",
			LastUpdated: "2022-06-09T00:39:27.668Z",
		},
	},
	OutboundListeners: []OutboundListener{
		{
			Name:        "outbound_listener",
			Address:     "127.0.0.1:15001",
			LastUpdated: "2022-05-24T17:41:59.079Z",
		},
	},
	Routes:  []Route{},
	Secrets: []Secret{},
}
