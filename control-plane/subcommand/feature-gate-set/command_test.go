// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package featuregateset

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
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

func TestRunAuthMethod(t *testing.T) {
	for _, tc := range []struct {
		name         string
		env          bool
		loginStatus  int
		writeStatus  int
		logoutStatus int
	}{
		{name: "flag"},
		{name: "environment", env: true},
		{name: "login failure", loginStatus: http.StatusForbidden},
		{name: "write failure logs out", writeStatus: http.StatusForbidden},
		{name: "logout failure", logoutStatus: http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bearerFile := filepath.Join(t.TempDir(), "bearer")
			require.NoError(t, os.WriteFile(bearerFile, []byte(" bearer-token\n"), 0600))
			var logins, writes, logouts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v1/status/leader":
					fmt.Fprint(w, `"127.0.0.1:8300"`)
				case "/v1/acl/login":
					logins.Add(1)
					var body struct {
						AuthMethod, BearerToken string
						Meta                    map[string]string
					}
					require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
					require.Equal(t, "test-auth", body.AuthMethod)
					require.Equal(t, "bearer-token", body.BearerToken)
					require.Equal(t, "feature-gate-set", body.Meta["component"])
					require.Equal(t, "login-ns", r.URL.Query().Get("ns"))
					require.Equal(t, "login-partition", r.URL.Query().Get("partition"))
					require.Equal(t, "login-dc", r.URL.Query().Get("dc"))
					if tc.loginStatus != 0 {
						http.Error(w, "login denied", tc.loginStatus)
						return
					}
					fmt.Fprint(w, `{"SecretID":"login-token"}`)
				case "/v1/operator/feature/consul-ai":
					writes.Add(1)
					require.Equal(t, "login-token", r.Header.Get("X-Consul-Token"))
					if tc.writeStatus != 0 {
						http.Error(w, "write denied", tc.writeStatus)
						return
					}
					fmt.Fprint(w, `{"Applied":true}`)
				case "/v1/acl/logout":
					logouts.Add(1)
					require.Equal(t, "login-token", r.Header.Get("X-Consul-Token"))
					require.Equal(t, "login-partition", r.URL.Query().Get("partition"))
					if tc.logoutStatus != 0 {
						http.Error(w, "logout failed", tc.logoutStatus)
					}
				default:
					t.Errorf("unexpected path: %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			_, port, err := net.SplitHostPort(server.Listener.Addr().String())
			require.NoError(t, err)
			args := []string{
				"-addresses=exec=echo 127.0.0.1", "-http-port=" + port,
				"-consul-login-bearer-token-file=" + bearerFile,
				"-consul-login-namespace=login-ns", "-consul-login-partition=login-partition",
				"-consul-login-datacenter=login-dc", "-consul-login-meta=component=feature-gate-set",
				"-token=static-token", "-feature-gate=consul-ai=true",
			}
			t.Setenv("CONSUL_LOGIN_AUTH_METHOD", "")
			if tc.env {
				t.Setenv("CONSUL_LOGIN_AUTH_METHOD", "test-auth")
			} else {
				args = append(args, "-auth-method-name=test-auth")
			}
			ui := cli.NewMockUi()
			cmd := &Command{UI: ui}
			wantExit := 0
			if tc.loginStatus != 0 || tc.writeStatus != 0 || tc.logoutStatus != 0 {
				wantExit = 1
			}
			require.Equal(t, wantExit, cmd.Run(args))
			require.Equal(t, int32(1), logins.Load())
			if tc.loginStatus != 0 {
				require.Zero(t, writes.Load())
				require.Zero(t, logouts.Load())
			} else {
				require.Equal(t, int32(1), writes.Load())
				require.Equal(t, int32(1), logouts.Load())
			}
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

func TestProviderDiscoveryCancellation(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	defer func() {
		close(release)
		<-finished
	}()
	cmd := &Command{
		UI: cli.NewMockUi(),
		discoverServers: func(string) ([]string, error) {
			close(started)
			<-release
			close(finished)
			return []string{"127.0.0.1"}, nil
		},
	}
	cmd.once.Do(cmd.init)
	cmd.consul.Addresses = "provider=blocked"
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := cmd.serverAddresses(ctx)
		result <- err
	}()
	<-started
	select {
	case err := <-result:
		require.ErrorIs(t, err, context.DeadlineExceeded)
	case <-time.After(time.Second):
		t.Fatal("provider discovery did not return on cancellation")
	}
}

func TestProviderDiscoveryResult(t *testing.T) {
	for _, discoveryErr := range []error{nil, errors.New("provider unavailable")} {
		cmd := &Command{
			UI: cli.NewMockUi(),
			discoverServers: func(string) ([]string, error) {
				return []string{"127.0.0.1"}, discoveryErr
			},
		}
		cmd.once.Do(cmd.init)
		cmd.consul.Addresses = "provider=test"
		addresses, err := cmd.serverAddresses(context.Background())
		require.ErrorIs(t, err, discoveryErr)
		require.Equal(t, []string{"127.0.0.1"}, addresses)
	}
}

func TestInferredTLSServerName(t *testing.T) {
	for _, mode := range []string{"IP", "IPv6", "provider", "explicit"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, `"127.0.0.1:8300"`)
			}))
			if mode == "IPv6" {
				listener, err := net.Listen("tcp", "[::1]:0")
				if err != nil {
					server.Close()
					t.Skipf("IPv6 loopback unavailable: %s", err)
				}
				require.NoError(t, server.Listener.Close())
				server.Listener = listener
			}
			server.StartTLS()
			defer server.Close()
			host, port, err := net.SplitHostPort(server.Listener.Addr().String())
			require.NoError(t, err)
			cmd := &Command{UI: cli.NewMockUi()}
			cmd.once.Do(cmd.init)
			cmd.consul.Addresses = host
			if mode == "provider" || mode == "explicit" {
				cmd.consul.Addresses = "provider=test"
				cmd.discoverServers = func(string) ([]string, error) {
					return []string{host}, nil
				}
			}
			if mode == "explicit" {
				cmd.consul.TLSServerName = "example.com"
			}
			cmd.consul.UseTLS = true
			cmd.consul.CACertPEM = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
			args := []string{"-http-port=" + port, "-timeout=1s"}
			require.Equal(t, 0, cmd.Run(args))
		})
	}
}

func TestLeaderPollingClosesIdleConnections(t *testing.T) {
	var checks atomic.Int32
	var closed atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		checks.Add(1)
		fmt.Fprint(w, `""`)
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateClosed {
			closed.Add(1)
		}
	}
	server.Start()
	defer server.Close()
	_, port, err := net.SplitHostPort(server.Listener.Addr().String())
	require.NoError(t, err)
	cmd := &Command{UI: cli.NewMockUi(), retryDuration: time.Millisecond}
	require.Equal(t, 1, cmd.Run([]string{"-addresses=127.0.0.1", "-http-port=" + port, "-timeout=100ms"}))
	require.Greater(t, checks.Load(), int32(1))
	require.Eventually(t, func() bool { return closed.Load() >= checks.Load() }, time.Second, time.Millisecond)
}
