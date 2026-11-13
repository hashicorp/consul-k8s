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

	// Only the IP of the connection manager state is used to build the client address; the port comes from the
	// Config. Reserve a loopback port and close it so that nothing is listening on it. Connecting to it fails
	// immediately with "connection refused", without depending on how the network handles packets sent to a
	// routable address (which can time out instead).
	closedListener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	closedPort := closedListener.Addr().(*net.TCPAddr).Port
	require.NoError(t, closedListener.Close())

	state := discovery.State{
		Address: discovery.Addr{
			TCPAddr: net.TCPAddr{
				IP:   serverIP,
				Port: port,
			},
		},
	}
	connMgr.On("State").Return(state, nil)

	cfg := capi.DefaultConfig()
	client, err := NewDynamicClientFromConnMgr(&Config{APIClientConfig: cfg, HTTPPort: closedPort, GRPCPort: closedPort}, connMgr)
	require.NoError(t, err)

	// Make a request to the closed port
	_, err = client.ConsulClient.Status().Leader()
	require.Error(t, err)
	require.Contains(t, err.Error(), "connection refused")

	// Point the config at the real server port, refresh the client and make a call to the server
	client.Config.HTTPPort = port
	client.Config.GRPCPort = port
	err = client.RefreshClient()
	require.NoError(t, err)
	leader, err := client.ConsulClient.Status().Leader()
	require.NoError(t, err)
	require.Equal(t, "leader", leader)
}
