package read

import (
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

//go:embed test_config_dump.json
var fs embed.FS

func TestUnmarshaling(t *testing.T) {
	raw, err := fs.ReadFile("test_config_dump.json")
	require.NoError(t, err)

	var envoyConfig EnvoyConfig
	err = json.Unmarshal(raw, &envoyConfig)
	require.NoError(t, err)

	fmt.Println(envoyConfig.OutboundListeners)
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
	Clusters:          []Cluster{},
	Endpoints:         []Endpoint{},
	InboundListeners:  []InboundListener{},
	OutboundListeners: []OutboundListener{},
	Routes:            []Route{},
	Secrets:           []Secret{},
}
