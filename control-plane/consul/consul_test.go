package consul

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/consul-k8s/control-plane/version"
	capi "github.com/hashicorp/consul/api"
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

	client, err := NewClient(&capi.Config{Address: consulServer.URL})
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
