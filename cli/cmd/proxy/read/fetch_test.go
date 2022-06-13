package read

import (
	"context"
	"embed"
	_ "embed"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type mockPortForwarder struct {
	openBehavior func(context.Context) (string, error)
}

func (m *mockPortForwarder) Open(ctx context.Context) (string, error) { return m.openBehavior(ctx) }
func (m *mockPortForwarder) Close()                                   {}

//go:embed test_config_dump.json
var fs embed.FS

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
	require.Equal(t, configResponse, configDump)
}
