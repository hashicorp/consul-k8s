// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package featuregateset

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/require"
)

const customerHosts = "exec=echo 10.52.4.128 10.52.4.129 10.52.4.130 10.52.4.131 10.52.4.132 10.52.4.133 10.52.4.134 10.52.4.135 10.52.4.124 10.52.4.125"

func TestServerAddresses(t *testing.T) {
	for _, tt := range []struct {
		name      string
		addresses string
		want      []string
		wantError string
	}{
		{
			name:      "customer exec returns all ten servers",
			addresses: customerHosts,
			want:      strings.Fields(strings.TrimPrefix(customerHosts, "exec=echo ")),
		},
		{name: "IPv6", addresses: "exec=echo 2001:db8::1 2001:db8::2", want: []string{"2001:db8::1", "2001:db8::2"}},
		{name: "IP", addresses: "127.0.0.1", want: []string{"127.0.0.1"}},
		{name: "DNS", addresses: "localhost"},
		{name: "failed executable", addresses: "exec=false", wantError: "executable failed with exit code"},
		{name: "empty output", addresses: "exec=true", wantError: "executable returned no output"},
		{name: "invalid output", addresses: "exec=echo invalid", wantError: "executable returned invalid IP address"},
		{name: "no shell evaluation", addresses: "exec=echo $(echo 127.0.0.1)", wantError: "executable returned invalid IP address"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &Command{UI: cli.NewMockUi(), log: hclog.NewNullLogger()}
			cmd.once.Do(cmd.init)
			cmd.consul.Addresses = tt.addresses
			addresses, err := cmd.serverAddresses(context.Background())
			if tt.wantError != "" {
				require.ErrorContains(t, err, tt.wantError)
			} else {
				require.NoError(t, err)
				if tt.want != nil {
					require.Equal(t, tt.want, addresses)
				} else {
					require.NotEmpty(t, addresses)
				}
			}
		})
	}
}

func TestRun(t *testing.T) {
	for _, tlsEnabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("TLS=%t", tlsEnabled), func(t *testing.T) {
			tokenFile := filepath.Join(t.TempDir(), "token")
			require.NoError(t, os.WriteFile(tokenFile, []byte("bootstrap-token"), 0600))
			var leaderChecks atomic.Int32
			var writes atomic.Int32
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "bootstrap-token", r.Header.Get("X-Consul-Token"))
				if tlsEnabled {
					require.NotNil(t, r.TLS)
					require.Equal(t, "example.com", r.TLS.ServerName)
				}
				switch r.URL.Path {
				case "/v1/status/leader":
					if leaderChecks.Add(1) == 1 {
						fmt.Fprint(w, `""`)
					} else {
						fmt.Fprint(w, `"127.0.0.1:8300"`)
					}
				case "/v1/operator/feature/consul-ai", "/v1/operator/feature/another-gate":
					require.Equal(t, http.MethodPut, r.Method)
					require.Equal(t, "external-partition", r.URL.Query().Get("partition"))
					var body struct{ Enabled bool }
					require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
					require.Equal(t, r.URL.Path == "/v1/operator/feature/consul-ai", body.Enabled)
					writes.Add(1)
					fmt.Fprint(w, `{"Applied":true}`)
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
					http.NotFound(w, r)
				}
			})
			server := httptest.NewUnstartedServer(handler)
			if tlsEnabled {
				server.StartTLS()
				t.Setenv("CONSUL_USE_TLS", "true")
				t.Setenv("CONSUL_CACERT_PEM", string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})))
				t.Setenv("CONSUL_TLS_SERVER_NAME", "example.com")
			} else {
				server.Start()
			}
			defer server.Close()
			_, port, err := net.SplitHostPort(server.Listener.Addr().String())
			require.NoError(t, err)
			t.Setenv("CONSUL_ADDRESSES", "exec=echo 127.0.0.1")
			t.Setenv("CONSUL_HTTP_PORT", port)
			t.Setenv("CONSUL_ACL_TOKEN_FILE", tokenFile)
			t.Setenv("CONSUL_PARTITION", "external-partition")
			t.Setenv("CONSUL_SKIP_SERVER_WATCH", "true")
			cmd := &Command{UI: cli.NewMockUi(), retryDuration: time.Millisecond}
			require.Equal(t, 0, cmd.Run([]string{"-feature-gate=consul-ai=true", "-feature-gate=another-gate=false", "-timeout=5s"}))
			require.GreaterOrEqual(t, leaderChecks.Load(), int32(2))
			require.Equal(t, int32(2), writes.Load())
		})
	}
}

func TestRunTriesOtherServers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/v1/status/leader", r.URL.Path)
		fmt.Fprint(w, `"127.0.0.1:8300"`)
	}))
	defer server.Close()
	_, port, err := net.SplitHostPort(server.Listener.Addr().String())
	require.NoError(t, err)
	cmd := &Command{UI: cli.NewMockUi()}
	require.Equal(t, 0, cmd.Run([]string{"-addresses=exec=echo 127.0.0.2 127.0.0.1", "-http-port=" + port, "-api-timeout=50ms", "-timeout=2s"}))
}

func TestRunFeatureSetError(t *testing.T) {
	for _, tt := range []struct {
		name   string
		status int
		body   string
	}{
		{name: "permission denied", status: http.StatusForbidden, body: "Permission denied"},
		{name: "not applied", status: http.StatusOK, body: `{"Applied":false}`},
		{name: "invalid response", status: http.StatusOK, body: "invalid"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var writes atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/status/leader" {
					fmt.Fprint(w, `"127.0.0.1:8300"`)
				} else {
					writes.Add(1)
					w.WriteHeader(tt.status)
					fmt.Fprint(w, tt.body)
				}
			}))
			defer server.Close()
			_, port, err := net.SplitHostPort(server.Listener.Addr().String())
			require.NoError(t, err)
			cmd := &Command{UI: cli.NewMockUi()}
			require.Equal(t, 1, cmd.Run([]string{"-addresses=127.0.0.1", "-http-port=" + port, "-feature-gate=consul-ai=true"}))
			require.Equal(t, int32(1), writes.Load())
		})
	}
}

func TestRunTimeout(t *testing.T) {
	for _, addresses := range []string{"exec=false", "127.0.0.1"} {
		t.Run(addresses, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, `""`)
			}))
			defer server.Close()
			_, port, err := net.SplitHostPort(server.Listener.Addr().String())
			require.NoError(t, err)
			cmd := &Command{UI: cli.NewMockUi(), retryDuration: time.Millisecond}
			require.Equal(t, 1, cmd.Run([]string{"-addresses=" + addresses, "-http-port=" + port, "-timeout=20ms"}))
		})
	}
}

func TestRunInvalidFlags(t *testing.T) {
	for _, args := range [][]string{
		{"-unknown"},
		{"unexpected"},
		{"-addresses="},
		{"-http-port=0"},
		{"-http-port=65536"},
		{"-api-timeout=0"},
		{"-timeout=0"},
		{"-addresses=exec= "},
		{"-feature-gate=invalid"},
		{"-feature-gate=consul-ai=invalid"},
		{"-feature-gate==true"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Setenv("CONSUL_ADDRESSES", "127.0.0.1")
			t.Setenv("CONSUL_HTTP_PORT", "8500")
			ui := cli.NewMockUi()
			cmd := &Command{UI: ui}
			require.Equal(t, 1, cmd.Run(args))
			require.NotEmpty(t, ui.ErrorWriter.String())
		})
	}
}
