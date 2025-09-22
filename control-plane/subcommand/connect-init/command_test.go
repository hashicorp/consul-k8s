// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package connectinit

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/iptables"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/require"

	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/common"
)

const nodeName = "test-node"

func TestRun_FlagValidation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		flags  []string
		expErr string
	}{
		{
			flags:  []string{},
			expErr: "-pod-name must be set",
		},
		{
			flags:  []string{"-pod-name", testPodName},
			expErr: "-pod-namespace must be set",
		},
		{
			flags: []string{
				"-pod-name", testPodName,
				"-pod-namespace", testPodNamespace,
				"-auth-method-name", test.AuthMethod},
			expErr: "-service-account-name must be set when ACLs are enabled",
		},
		{
			flags: []string{
				"-pod-name", testPodName,
				"-pod-namespace", testPodNamespace},
			expErr: "-consul-node-name must be set",
		},
		{
			flags: []string{
				"-pod-name", testPodName,
				"-pod-namespace", testPodNamespace,
				"-auth-method-name", test.AuthMethod,
				"-service-account-name", "foo",
				"-log-level", "invalid",
				"-consul-node-name", "bar",
			},
			expErr: "unknown log level: invalid",
		},
	}
	for _, c := range cases {
		t.Run(c.expErr, func(t *testing.T) {
			ui := cli.NewMockUi()
			cmd := Command{
				UI: ui,
			}
			code := cmd.Run(c.flags)
			require.Equal(t, 1, code)
			require.Contains(t, ui.ErrorWriter.String(), c.expErr)
		})
	}
}

// TestRun_ConnectServices tests that the command can log in to Consul (if ACLs are enabled) using a kubernetes
// auth method and using the obtained token find the services for the provided pod name
// and namespace provided and write the proxy ID of the proxy service to a file.
func TestRun_ConnectServices(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name                       string
		aclsEnabled                bool
		serviceAccountName         string
		serviceName                string
		includeServiceAccountName  bool
		serviceAccountNameMismatch bool
		expFail                    bool
		multiport                  bool
	}{
		{
			name:               "service-name not provided",
			serviceAccountName: "counting",
		},
		{
			name:               "multi-port service",
			serviceAccountName: "counting-admin",
			serviceName:        "counting-admin",
			multiport:          true,
		},
		{
			name:               "acls enabled; service name annotation doesn't match service account name",
			aclsEnabled:        true,
			serviceAccountName: "not-a-match",
			serviceName:        "web",
			expFail:            true,
		},
		{
			name:               "acls enabled; K8s service name doesn't match service account name",
			aclsEnabled:        true,
			serviceAccountName: "not-a-match",
			serviceName:        "",
			expFail:            true,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			tokenFile := fmt.Sprintf("/tmp/%d1", rand.Int())
			proxyFile := fmt.Sprintf("/tmp/%d2", rand.Int())
			t.Cleanup(func() {
				_ = os.RemoveAll(proxyFile)
				_ = os.RemoveAll(tokenFile)
			})

			// Start Consul server with ACLs enabled and default deny policy.
			var serverCfg *testutil.TestServerConfig
			server, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
				serverCfg = c
			})
			require.NoError(t, err)
			defer server.Stop()
			server.WaitForLeader(t)
			cfg := &api.Config{
				Scheme:  "http",
				Address: server.HTTPAddr,
			}
			consulClient, err := api.NewClient(cfg)
			require.NoError(t, err)

			// Register Consul services.
			testConsulServices := []api.AgentService{consulCountingSvc, consulCountingSvcSidecar}
			if tt.multiport {
				testConsulServices = append(testConsulServices, consulCountingSvcMultiport, consulCountingSvcSidecarMultiport)
			}
			for _, svc := range testConsulServices {
				serviceRegistration := &api.CatalogRegistration{
					Node:    nodeName,
					Address: "127.0.0.1",
					Service: &svc,
				}
				_, err := consulClient.Catalog().Register(serviceRegistration, nil)
				require.NoError(t, err)
			}

			ui := cli.NewMockUi()
			cmd := Command{
				UI:                                 ui,
				serviceRegistrationPollingAttempts: 3,
			}

			// We build the consul-addr because normally it's defined by the init container setting
			// CONSUL_HTTP_ADDR when it processes the command template.
			flags := []string{"-pod-name", testPodName,
				"-pod-namespace", testPodNamespace,
				"-service-name", tt.serviceName,
				"-addresses", "127.0.0.1",
				"-http-port", strconv.Itoa(serverCfg.Ports.HTTP),
				"-grpc-port", strconv.Itoa(serverCfg.Ports.GRPC),
				"-proxy-id-file", proxyFile,
				"-multiport=" + strconv.FormatBool(tt.multiport),
				"-consul-node-name", nodeName,
			}
			if tt.aclsEnabled {
				flags = append(flags, "-auth-method-name", test.AuthMethod,
					"-service-account-name", tt.serviceAccountName,
					"-acl-token-sink", tokenFile)
			}

			// Run the command.
			code := cmd.Run(flags)
			if tt.expFail {
				require.Equal(t, 1, code)
				return
			}
			require.Equal(t, 0, code, ui.ErrorWriter.String())

			if tt.aclsEnabled {
				// Validate the ACL token was written.
				tokenData, err := os.ReadFile(tokenFile)
				require.NoError(t, err)
				require.NotEmpty(t, tokenData)

				// Check that the token has the metadata with pod name and pod namespace.
				consulClient, err = api.NewClient(&api.Config{Address: server.HTTPAddr, Token: string(tokenData)})
				require.NoError(t, err)
				token, _, err := consulClient.ACL().TokenReadSelf(nil)
				require.NoError(t, err)
				require.Equal(t, "token created via login: {\"pod\":\"default-ns/counting-pod\"}", token.Description)
			}

			// Validate contents of proxyFile.
			data, err := os.ReadFile(proxyFile)
			require.NoError(t, err)
			if tt.multiport {
				require.Contains(t, string(data), "counting-admin-sidecar-proxy-id")
			} else {
				require.Contains(t, string(data), "counting-counting-sidecar-proxy")
			}
		})
	}
}

// TestRun_Gateways tests that the command can log in to Consul (if ACLs are enabled) using a kubernetes
// auth method and using the obtained token find the service for the provided gateway
// and namespace provided and write the proxy ID of the gateway service to a file.
func TestRun_Gateways(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		gatewayKind  string
		agentService api.AgentService
		serviceName  string
		expFail      bool
	}{
		{
			name:        "mesh-gateway",
			gatewayKind: "mesh-gateway",
			agentService: api.AgentService{
				ID:      "mesh-gateway",
				Service: "mesh-gateway",
				Kind:    api.ServiceKindMeshGateway,
				Port:    4444,
				Address: "127.0.0.1",
				Meta: map[string]string{
					"component":    "mesh-gateway",
					metaKeyPodName: testGatewayName,
					metaKeyKubeNS:  "default-ns",
				},
			},
		},
		{
			name:        "ingress-gateway",
			gatewayKind: "ingress-gateway",
			agentService: api.AgentService{
				ID:      "ingress-gateway",
				Service: "ingress-gateway",
				Kind:    api.ServiceKindMeshGateway,
				Port:    4444,
				Address: "127.0.0.1",
				Meta: map[string]string{
					"component":    "ingress-gateway",
					metaKeyPodName: testGatewayName,
					metaKeyKubeNS:  "default-ns",
				},
			},
		},
		{
			name:        "terminating-gateway",
			gatewayKind: "terminating-gateway",
			agentService: api.AgentService{
				ID:      "terminating-gateway",
				Service: "terminating-gateway",
				Kind:    api.ServiceKindMeshGateway,
				Port:    4444,
				Address: "127.0.0.1",
				Meta: map[string]string{
					"component":    "terminating-gateway",
					metaKeyPodName: testGatewayName,
					metaKeyKubeNS:  "default-ns",
				},
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			proxyFile := fmt.Sprintf("/tmp/%d2", rand.Int())
			t.Cleanup(func() {
				_ = os.RemoveAll(proxyFile)
			})

			// Start Consul server with ACLs enabled and default deny policy.
			var serverCfg *testutil.TestServerConfig
			server, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
				serverCfg = c
			})
			require.NoError(t, err)
			defer server.Stop()
			server.WaitForLeader(t)
			cfg := &api.Config{
				Scheme:  "http",
				Address: server.HTTPAddr,
			}
			consulClient, err := api.NewClient(cfg)
			require.NoError(t, err)

			// Register Consul services.
			testConsulServices := []api.AgentService{tt.agentService}
			for _, svc := range testConsulServices {
				serviceRegistration := &api.CatalogRegistration{
					Node:    nodeName,
					Address: "127.0.0.1",
					Service: &svc,
				}
				_, err = consulClient.Catalog().Register(serviceRegistration, nil)
				require.NoError(t, err)
			}

			ui := cli.NewMockUi()
			cmd := Command{
				UI:                                 ui,
				serviceRegistrationPollingAttempts: 3,
			}

			// We build the http-addr because normally it's defined by the init container setting
			// CONSUL_HTTP_ADDR when it processes the command template.
			flags := []string{"-pod-name", testGatewayName,
				"-pod-namespace", testPodNamespace,
				"-gateway-kind", tt.gatewayKind,
				"-addresses", "127.0.0.1",
				"-http-port", strconv.Itoa(serverCfg.Ports.HTTP),
				"-grpc-port", strconv.Itoa(serverCfg.Ports.GRPC),
				"-proxy-id-file", proxyFile,
				"-consul-node-name", nodeName,
			}

			// Run the command.
			code := cmd.Run(flags)
			if tt.expFail {
				require.Equal(t, 1, code)
				return
			}
			require.Equal(t, 0, code, ui.ErrorWriter.String())

			// Validate contents of proxyFile.
			data, err := os.ReadFile(proxyFile)
			require.NoError(t, err)
			require.Contains(t, string(data), tt.gatewayKind)
		})
	}
}

// TestRun_ConnectServices_Errors tests that when registered services could not be found,
// we error out.
func TestRun_ConnectServices_Errors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		services []api.AgentServiceRegistration
	}{
		{
			name: "only service is registered; proxy service is missing",
			services: []api.AgentServiceRegistration{
				{
					ID:      "counting-counting",
					Name:    "counting",
					Address: "127.0.0.1",
					Meta: map[string]string{
						metaKeyPodName: "counting-pod",
						metaKeyKubeNS:  "default-ns",
					},
				},
			},
		},
		{
			name: "only proxy is registered; service is missing",
			services: []api.AgentServiceRegistration{
				{
					ID:   "counting-counting-sidecar-proxy",
					Name: "counting-sidecar-proxy",
					Kind: "connect-proxy",
					Proxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "counting",
						DestinationServiceID:   "counting-counting",
					},
					Port:    9999,
					Address: "127.0.0.1",
					Meta: map[string]string{
						metaKeyPodName: "counting-pod",
						metaKeyKubeNS:  "default-ns",
					},
				},
			},
		},
		{
			name: "service and proxy without pod-name and k8s-namespace meta",
			services: []api.AgentServiceRegistration{
				{
					ID:      "counting-counting",
					Name:    "counting",
					Address: "127.0.0.1",
				},
				{
					ID:   "counting-counting-sidecar-proxy",
					Name: "counting-sidecar-proxy",
					Kind: "connect-proxy",
					Proxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "counting",
						DestinationServiceID:   "counting-counting",
					},
					Port:    9999,
					Address: "127.0.0.1",
				},
			},
		},
		{
			name: "service and proxy with pod-name meta but without k8s-namespace meta",
			services: []api.AgentServiceRegistration{
				{
					ID:      "counting-counting",
					Name:    "counting",
					Address: "127.0.0.1",
					Meta: map[string]string{
						metaKeyPodName: "counting-pod",
					},
				},
				{
					ID:   "counting-counting-sidecar-proxy",
					Name: "counting-sidecar-proxy",
					Kind: "connect-proxy",
					Proxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "counting",
						DestinationServiceID:   "counting-counting",
					},
					Port:    9999,
					Address: "127.0.0.1",
					Meta: map[string]string{
						metaKeyPodName: "counting-pod",
					},
				},
			},
		},
		{
			name: "service and proxy with k8s-namespace meta but pod-name meta",
			services: []api.AgentServiceRegistration{
				{
					ID:      "counting-counting",
					Name:    "counting",
					Address: "127.0.0.1",
					Meta: map[string]string{
						metaKeyKubeNS: "default-ns",
					},
				},
				{
					ID:   "counting-counting-sidecar-proxy",
					Name: "counting-sidecar-proxy",
					Kind: "connect-proxy",
					Proxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "counting",
						DestinationServiceID:   "counting-counting",
					},
					Port:    9999,
					Address: "127.0.0.1",
					Meta: map[string]string{
						metaKeyKubeNS: "default-ns",
					},
				},
			},
		},
		{
			name: "both services are non-proxy services",
			services: []api.AgentServiceRegistration{
				{
					ID:      "counting-counting",
					Name:    "counting",
					Address: "127.0.0.1",
					Meta: map[string]string{
						metaKeyPodName: "counting-pod",
						metaKeyKubeNS:  "default-ns",
					},
				},
				{
					ID:      "counting-counting-1",
					Name:    "counting",
					Address: "127.0.0.1",
					Meta: map[string]string{
						metaKeyPodName: "counting-pod",
						metaKeyKubeNS:  "default-ns",
					},
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			proxyFile := fmt.Sprintf("/tmp/%d", rand.Int())
			t.Cleanup(func() {
				os.RemoveAll(proxyFile)
			})

			// Start Consul server.
			var serverCfg *testutil.TestServerConfig
			server, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
				serverCfg = c
			})
			require.NoError(t, err)
			defer server.Stop()
			server.WaitForLeader(t)
			consulClient, err := api.NewClient(&api.Config{Address: server.HTTPAddr})
			require.NoError(t, err)

			// Register Consul services.
			for _, svc := range c.services {
				require.NoError(t, consulClient.Agent().ServiceRegister(&svc))
			}

			ui := cli.NewMockUi()
			cmd := Command{
				UI:                                 ui,
				serviceRegistrationPollingAttempts: 1,
			}
			flags := []string{
				"-addresses", "127.0.0.1",
				"-http-port", strconv.Itoa(serverCfg.Ports.HTTP),
				"-grpc-port", strconv.Itoa(serverCfg.Ports.GRPC),
				"-pod-name", testPodName,
				"-pod-namespace", testPodNamespace,
				"-proxy-id-file", proxyFile,
				"-consul-node-name", nodeName,
			}

			code := cmd.Run(flags)
			require.Equal(t, 1, code)
		})
	}
}

// TestRun_Gateways_Errors tests that when registered services could not be found,
// we error out.
func TestRun_Gateways_Errors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		services []api.AgentServiceRegistration
	}{
		{
			name: "gateway without pod-name or k8s-namespace meta",
			services: []api.AgentServiceRegistration{
				{
					ID:      "mesh-gateway",
					Name:    "mesh-gateway",
					Kind:    "mesh-gateway",
					Port:    9999,
					Address: "127.0.0.1",
				},
			},
		},
		{
			name: "gateway with pod-name meta but without k8s-namespace meta",
			services: []api.AgentServiceRegistration{
				{
					ID:      "mesh-gateway",
					Name:    "mesh-gateway",
					Kind:    "mesh-gateway",
					Port:    9999,
					Address: "127.0.0.1",
					Meta: map[string]string{
						metaKeyPodName: "mesh-gateway",
					},
				},
			},
		},
		{
			name: "service and proxy with k8s-namespace meta but pod-name meta",
			services: []api.AgentServiceRegistration{
				{
					ID:      "mesh-gateway",
					Name:    "mesh-gateway",
					Kind:    "mesh-gateway",
					Port:    9999,
					Address: "127.0.0.1",
					Meta: map[string]string{
						metaKeyKubeNS: "default-ns",
					},
				},
			}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			proxyFile := fmt.Sprintf("/tmp/%d", rand.Int())
			t.Cleanup(func() {
				os.RemoveAll(proxyFile)
			})

			// Start Consul server.
			server, err := testutil.NewTestServerConfigT(t, nil)
			require.NoError(t, err)
			defer server.Stop()
			server.WaitForLeader(t)
			consulClient, err := api.NewClient(&api.Config{Address: server.HTTPAddr})
			require.NoError(t, err)

			// Register Consul services.
			for _, svc := range c.services {
				require.NoError(t, consulClient.Agent().ServiceRegister(&svc))
			}

			ui := cli.NewMockUi()
			cmd := Command{
				UI:                                 ui,
				serviceRegistrationPollingAttempts: 1,
			}
			flags := []string{
				"-http-addr", server.HTTPAddr,
				"-gateway-kind", "mesh-gateway",
				"-pod-name", testPodName,
				"-pod-namespace", testPodNamespace,
				"-proxy-id-file", proxyFile,
				"-consul-api-timeout", "10s",
				"-consul-node-name", nodeName,
			}

			code := cmd.Run(flags)
			require.Equal(t, 1, code)
		})
	}
}

// TestRun_RetryServicePolling runs the command but does not register the consul service
// for 2 seconds and then asserts that the proxyid file gets written correctly.
func TestRun_RetryServicePolling(t *testing.T) {
	t.Parallel()
	proxyFile := common.WriteTempFile(t, "")

	// Start Consul server.
	var serverCfg *testutil.TestServerConfig
	server, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
		serverCfg = c
	})
	require.NoError(t, err)
	defer server.Stop()
	server.WaitForLeader(t)
	consulClient, err := api.NewClient(&api.Config{Address: server.HTTPAddr})
	require.NoError(t, err)

	// Start the consul service registration in a go func and delay it so that it runs
	// after the cmd.Run() starts.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Wait a moment, this ensures that we are already in the retry logic.
		time.Sleep(time.Second * 2)
		// Register counting service.
		serviceRegistration := &api.CatalogRegistration{
			Node:    nodeName,
			Address: "127.0.0.1",
			Service: &consulCountingSvc,
		}
		_, err = consulClient.Catalog().Register(serviceRegistration, nil)
		require.NoError(t, err)
		// Register proxy sidecar service.
		serviceRegistration.Service = &consulCountingSvcSidecar
		_, err = consulClient.Catalog().Register(serviceRegistration, nil)
		require.NoError(t, err)
	}()

	ui := cli.NewMockUi()
	cmd := Command{
		UI:                                 ui,
		serviceRegistrationPollingAttempts: 10,
	}
	flags := []string{
		"-pod-name", testPodName,
		"-pod-namespace", testPodNamespace,
		"-addresses", "127.0.0.1",
		"-http-port", strconv.Itoa(serverCfg.Ports.HTTP),
		"-grpc-port", strconv.Itoa(serverCfg.Ports.GRPC),
		"-proxy-id-file", proxyFile,
		"-consul-node-name", nodeName,
	}
	code := cmd.Run(flags)
	wg.Wait()
	require.Equal(t, 0, code)

	// Validate contents of proxyFile.
	data, err := os.ReadFile(proxyFile)
	require.NoError(t, err)
	require.Contains(t, string(data), "counting-counting-sidecar-proxy")
}

// TestRun_InvalidProxyFile validates that we correctly fail in case the proxyid file
// is not writable. This functions as coverage for both ACL and non-ACL codepaths.
func TestRun_InvalidProxyFile(t *testing.T) {
	t.Parallel()
	// This is the output file for the proxyid.
	randFileName := fmt.Sprintf("/foo/%d/%d", rand.Int(), rand.Int())

	// Start Consul server.
	var serverCfg *testutil.TestServerConfig
	server, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
		serverCfg = c
	})
	require.NoError(t, err)
	defer server.Stop()
	server.WaitForLeader(t)
	consulClient, err := api.NewClient(&api.Config{Address: server.HTTPAddr})
	require.NoError(t, err)

	// Register Consul services.
	testConsulServices := []api.AgentService{consulCountingSvc, consulCountingSvcSidecar}
	for _, svc := range testConsulServices {
		serviceRegistration := &api.CatalogRegistration{
			Node:    nodeName,
			Address: "127.0.0.1",
			Service: &svc,
		}
		_, err = consulClient.Catalog().Register(serviceRegistration, nil)
		require.NoError(t, err)
	}
	ui := cli.NewMockUi()
	cmd := Command{
		UI:                                 ui,
		serviceRegistrationPollingAttempts: 3,
	}
	flags := []string{
		"-pod-name", testPodName,
		"-pod-namespace", testPodNamespace,
		"-addresses", "127.0.0.1",
		"-http-port", strconv.Itoa(serverCfg.Ports.HTTP),
		"-grpc-port", strconv.Itoa(serverCfg.Ports.GRPC),
		"-proxy-id-file", randFileName,
		"-consul-api-timeout", "10s",
	}
	code := cmd.Run(flags)
	require.Equal(t, 1, code)
	_, err = os.Stat(randFileName)
	require.Error(t, err)
}

func TestRun_TrafficRedirection(t *testing.T) {
	cases := map[string]struct {
		proxyConfig           map[string]interface{}
		tproxyConfig          api.TransparentProxyConfig
		registerProxyDefaults bool
		expIptablesParamsFunc func(actual iptables.Config) (bool, string)
	}{
		"no extra proxy config provided": {},
		"envoy bind port is provided in service proxy config": {
			proxyConfig: map[string]interface{}{"bind_port": "21000"},
			expIptablesParamsFunc: func(actual iptables.Config) (bool, string) {
				if actual.ProxyInboundPort == 21000 {
					return true, ""
				} else {
					return false, fmt.Sprintf("ProxyInboundPort in iptables.Config was %d, but should be 21000", actual.ProxyInboundPort)
				}
			},
		},
		// This test is to make sure that we use merge-central-config parameter when we query the service
		// so that we get all config merged into the proxy configuration on the service.
		"envoy bind port is provided in a config entry": {
			proxyConfig:           map[string]interface{}{"bind_port": "21000"},
			registerProxyDefaults: true,
			expIptablesParamsFunc: func(actual iptables.Config) (bool, string) {
				if actual.ProxyInboundPort == 21000 {
					return true, ""
				} else {
					return false, fmt.Sprintf("ProxyInboundPort in iptables.Config was %d, but should be 21000", actual.ProxyInboundPort)
				}
			},
		},
		"tproxy outbound listener port is provided in service proxy config": {
			tproxyConfig: api.TransparentProxyConfig{OutboundListenerPort: 16000},
			expIptablesParamsFunc: func(actual iptables.Config) (bool, string) {
				if actual.ProxyOutboundPort == 16000 {
					return true, ""
				} else {
					return false, fmt.Sprintf("ProxyOutboundPort in iptables.Config was %d, but should be 16000", actual.ProxyOutboundPort)
				}
			},
		},
		"tproxy outbound listener port is provided in a config entry": {
			tproxyConfig:          api.TransparentProxyConfig{OutboundListenerPort: 16000},
			registerProxyDefaults: true,
			expIptablesParamsFunc: func(actual iptables.Config) (bool, string) {
				if actual.ProxyOutboundPort == 16000 {
					return true, ""
				} else {
					return false, fmt.Sprintf("ProxyOutboundPort in iptables.Config was %d, but should be 16000", actual.ProxyOutboundPort)
				}
			},
		},
		"envoy stats addr is provided in service proxy config": {
			proxyConfig: map[string]interface{}{"envoy_stats_bind_addr": "0.0.0.0:9090"},
			expIptablesParamsFunc: func(actual iptables.Config) (bool, string) {
				if len(actual.ExcludeInboundPorts) == 1 && actual.ExcludeInboundPorts[0] == "9090" {
					return true, ""
				} else {
					return false, fmt.Sprintf("ExcludeInboundPorts in iptables.Config was %v, but should be [9090]", actual.ExcludeInboundPorts)
				}
			},
		},
		"envoy stats addr is provided in a config entry": {
			proxyConfig:           map[string]interface{}{"envoy_stats_bind_addr": "0.0.0.0:9090"},
			registerProxyDefaults: true,
			expIptablesParamsFunc: func(actual iptables.Config) (bool, string) {
				if len(actual.ExcludeInboundPorts) == 1 && actual.ExcludeInboundPorts[0] == "9090" {
					return true, ""
				} else {
					return false, fmt.Sprintf("ExcludeInboundPorts in iptables.Config was %v, but should be [9090]", actual.ExcludeInboundPorts)
				}
			},
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			proxyFile := fmt.Sprintf("/tmp/%d", rand.Int())
			t.Cleanup(func() {
				_ = os.RemoveAll(proxyFile)
			})

			// Start Consul server.
			var serverCfg *testutil.TestServerConfig
			server, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
				serverCfg = c
			})
			require.NoError(t, err)
			t.Cleanup(func() {
				_ = server.Stop()
			})
			server.WaitForLeader(t)
			consulClient, err := api.NewClient(&api.Config{Address: server.HTTPAddr})
			require.NoError(t, err)

			// Add additional proxy configuration either to a config entry or to the service itself.
			if c.registerProxyDefaults {
				_, _, err = consulClient.ConfigEntries().Set(&api.ProxyConfigEntry{
					Name:             api.ProxyConfigGlobal,
					Kind:             api.ProxyDefaults,
					TransparentProxy: &c.tproxyConfig,
					Config:           c.proxyConfig,
				}, nil)
				require.NoError(t, err)
			} else {
				consulCountingSvcSidecar.Proxy.TransparentProxy = &c.tproxyConfig
				consulCountingSvcSidecar.Proxy.Config = c.proxyConfig
			}
			// Register Consul services.
			testConsulServices := []api.AgentService{consulCountingSvc, consulCountingSvcSidecar}
			for _, svc := range testConsulServices {
				serviceRegistration := &api.CatalogRegistration{
					Node:    nodeName,
					Address: "127.0.0.1",
					Service: &svc,
				}
				_, err = consulClient.Catalog().Register(serviceRegistration, nil)
				require.NoError(t, err)
			}
			ui := cli.NewMockUi()

			iptablesProvider := &fakeIptablesProvider{}
			iptablesCfg := iptables.Config{
				ProxyUserID:      "5995",
				ProxyInboundPort: 20000,
			}
			cmd := Command{
				UI:                                 ui,
				serviceRegistrationPollingAttempts: 3,
				iptablesProvider:                   iptablesProvider,
			}
			iptablesCfgJSON, err := json.Marshal(iptablesCfg)
			require.NoError(t, err)
			flags := []string{
				"-pod-name", testPodName,
				"-pod-namespace", testPodNamespace,
				"-consul-node-name", nodeName,
				"-addresses", "127.0.0.1",
				"-http-port", strconv.Itoa(serverCfg.Ports.HTTP),
				"-grpc-port", strconv.Itoa(serverCfg.Ports.GRPC),
				"-proxy-id-file", proxyFile,
				"-redirect-traffic-config", string(iptablesCfgJSON),
			}
			code := cmd.Run(flags)
			require.Equal(t, 0, code, ui.ErrorWriter.String())
			require.Truef(t, iptablesProvider.applyCalled, "redirect traffic rules were not applied")
			if c.expIptablesParamsFunc != nil {
				actualIptablesConfigParamsEqualExpected, errMsg := c.expIptablesParamsFunc(cmd.iptablesConfig)
				require.Truef(t, actualIptablesConfigParamsEqualExpected, errMsg)
			}
		})
	}
}

const (
	metaKeyPodName         = "pod-name"
	metaKeyKubeNS          = "k8s-namespace"
	metaKeyKubeServiceName = "k8s-service-name"
	testPodNamespace       = "default-ns"
	testPodName            = "counting-pod"
	testGatewayName        = "gateway-pod"
)

var (
	consulCountingSvc = api.AgentService{
		ID:      "counting-counting",
		Service: "counting",
		Address: "127.0.0.1",
		Meta: map[string]string{
			metaKeyPodName:         "counting-pod",
			metaKeyKubeNS:          "default-ns",
			metaKeyKubeServiceName: "counting",
		},
	}
	consulCountingSvcSidecar = api.AgentService{
		ID:      "counting-counting-sidecar-proxy",
		Service: "counting-sidecar-proxy",
		Kind:    "connect-proxy",
		Proxy: &api.AgentServiceConnectProxyConfig{
			DestinationServiceName: "counting",
			DestinationServiceID:   "counting-counting",
		},
		Port:    9999,
		Address: "127.0.0.1",
		Meta: map[string]string{
			metaKeyPodName:         "counting-pod",
			metaKeyKubeNS:          "default-ns",
			metaKeyKubeServiceName: "counting",
		},
	}
	consulCountingSvcMultiport = api.AgentService{
		ID:      "counting-admin-id",
		Service: "counting-admin",
		Address: "127.0.0.1",
		Meta: map[string]string{
			metaKeyPodName:         "counting-pod",
			metaKeyKubeNS:          "default-ns",
			metaKeyKubeServiceName: "counting-admin",
		},
	}
	consulCountingSvcSidecarMultiport = api.AgentService{
		ID:      "counting-admin-sidecar-proxy-id",
		Service: "counting-admin-sidecar-proxy",
		Kind:    "connect-proxy",
		Proxy: &api.AgentServiceConnectProxyConfig{
			DestinationServiceName: "counting-admin",
			DestinationServiceID:   "counting-admin-id",
		},
		Port:    9999,
		Address: "127.0.0.1",
		Meta: map[string]string{
			metaKeyPodName:         "counting-pod",
			metaKeyKubeNS:          "default-ns",
			metaKeyKubeServiceName: "counting-admin",
		},
	}
)

type fakeIptablesProvider struct {
	applyCalled bool
	rules       []string
}

func (f *fakeIptablesProvider) AddRule(_ string, args ...string) {
	f.rules = append(f.rules, strings.Join(args, " "))
}

func (f *fakeIptablesProvider) ApplyRules(command string) error {
	f.applyCalled = true
	return nil
}

func (f *fakeIptablesProvider) Rules() []string {
	return f.rules
}

func (f *fakeIptablesProvider) ClearAllRules() {
	f.rules = nil
}
