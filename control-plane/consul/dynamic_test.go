// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package consul

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/hashicorp/consul-server-connection-manager/discovery"
	capi "github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
)

func TestRefreshDynamicClient(t *testing.T) {
	// Create a server
	consulServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "\"leader\"")
	}))
	defer consulServer.Close()

	serverURL, err := url.Parse(consulServer.URL)
	require.NoError(t, err)
	serverIP := net.ParseIP(serverURL.Hostname())

	port, err := strconv.Atoi(serverURL.Port())
	require.NoError(t, err)

	connMgr := &MockServerConnectionManager{}

	// Use a bad IP so that the client call fails
	badState := discovery.State{
		Address: discovery.Addr{
			TCPAddr: net.TCPAddr{
				IP:   net.ParseIP("126.0.0.1"),
				Port: port,
			},
		},
	}

	goodState := discovery.State{
		Address: discovery.Addr{
			TCPAddr: net.TCPAddr{
				IP:   serverIP,
				Port: port,
			},
		},
	}

	// testify/mock has a weird behaviour when returning function calls. You cannot update On("State") to return
	// something different but instead need to load up the returns. Here we are simulating a bad consul server manager
	// state and then a good one
	connMgr.On("State").Return(badState, nil).Once()
	connMgr.On("State").Return(goodState, nil).Once()

	cfg := capi.DefaultConfig()
	client, err := NewDynamicClientFromConnMgr(&Config{APIClientConfig: cfg, HTTPPort: port, GRPCPort: port}, connMgr)
	require.NoError(t, err)

	// Make a request to the bad ip of the server
	_, err = client.ConsulClient.Status().Leader()
	require.Error(t, err)
	require.Contains(t, err.Error(), "connection refused")

	// Refresh the client and make a call to the server now that consul-server-connection-manager state is good
	err = client.RefreshClient()
	require.NoError(t, err)
	leader, err := client.ConsulClient.Status().Leader()
	require.NoError(t, err)
	require.Equal(t, "leader", leader)
}
