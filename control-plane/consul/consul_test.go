// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package consul

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/consul-k8s/version"
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
