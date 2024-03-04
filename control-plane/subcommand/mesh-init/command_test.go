// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package meshinit

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	pbcatalog "github.com/hashicorp/consul/proto-public/pbcatalog/v2beta1"
	pbmesh "github.com/hashicorp/consul/proto-public/pbmesh/v2beta1"
	"github.com/hashicorp/consul/proto-public/pbresource"
	"github.com/hashicorp/consul/sdk/iptables"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
)

func TestRun_FlagValidation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		flags  []string
		env    string
		expErr string
	}{
		{
			flags:  []string{},
			expErr: "-proxy-name must be set",
		},
		{
			flags: []string{
				"-proxy-name", testPodName,
				"-log-level", "invalid",
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

// TestRun_MeshServices tests that the command can log in to Consul (if ACLs are enabled) using a kubernetes
// auth method and, using the obtained token, make call to the dataplane GetBootstrapParams() RPC.
func TestRun_MeshServices(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name               string
		workload           *pbcatalog.Workload
		proxyConfiguration *pbmesh.ProxyConfiguration
		aclsEnabled        bool
		expFail            bool
	}{
		{
			name:     "basic workload bootstrap",
			workload: getWorkload(),
		},
		{
			name:               "workload and proxyconfiguration bootstrap",
			workload:           getWorkload(),
			proxyConfiguration: getProxyConfiguration(),
		},
		{
			name:    "missing workload",
			expFail: true,
		},
		// TODO: acls enabled
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			//tokenFile := fmt.Sprintf("/tmp/%d1", rand.Int())
			//t.Cleanup(func() {
			//	_ = os.RemoveAll(tokenFile)
			//})

			// Create test consulServer server.
			var serverCfg *testutil.TestServerConfig
			testClient := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
				c.Experiments = []string{"resource-apis"}
				serverCfg = c
			})

			loadResource(t, testClient.ResourceClient, getWorkloadID(testPodName, constants.DefaultConsulNS, constants.DefaultConsulPartition), tt.workload, nil)
			loadResource(t, testClient.ResourceClient, getProxyConfigurationID(testPodName, constants.DefaultConsulNS, constants.DefaultConsulPartition), tt.proxyConfiguration, nil)

			ui := cli.NewMockUi()
			cmd := Command{
				UI:                 ui,
				maxPollingAttempts: 3,
			}

			// We build the consul-addr because normally it's defined by the init container setting
			// CONSUL_HTTP_ADDR when it processes the command template.
			flags := []string{
				"-proxy-name", testPodName,
				"-addresses", "127.0.0.1",
				"-http-port", strconv.Itoa(serverCfg.Ports.HTTP),
				"-grpc-port", strconv.Itoa(serverCfg.Ports.GRPC),
			}
			//if tt.aclsEnabled {
			//	flags = append(flags, "-auth-method-name", test.AuthMethod,
			//		"-service-account-name", tt.serviceAccountName,
			//		"-acl-token-sink", tokenFile) //TODO: what happens if this is unspecified? We don't need this file
			//}

			// Run the command.
			code := cmd.Run(flags)
			if tt.expFail {
				require.Equal(t, 1, code)
				return
			}
			require.Equal(t, 0, code, ui.ErrorWriter.String())

			// TODO: Can we remove the tokenFile from this workflow?
			// consul-dataplane performs it's own login using the Serviceaccount bearer token
			//if tt.aclsEnabled {
			//	// Validate the ACL token was written.
			//	tokenData, err := os.ReadFile(tokenFile)
			//	require.NoError(t, err)
			//	require.NotEmpty(t, tokenData)
			//
			//	// Check that the token has the metadata with pod name and pod namespace.
			//	consulClient, err = api.NewClient(&api.Config{Address: server.HTTPAddr, Token: string(tokenData)})
			//	require.NoError(t, err)
			//	token, _, err := consulClient.ACL().TokenReadSelf(nil)
			//	require.NoError(t, err)
			//	require.Equal(t, "token created via login: {\"pod\":\"default-ns/counting-pod\"}", token.Description)
			//}
		})
	}
}

// TestRun_RetryServicePolling runs the command but does not register the consul service
// for 2 seconds and then asserts the command exits successfully.
func TestRun_RetryServicePolling(t *testing.T) {
	t.Parallel()

	// Start Consul server.
	var serverCfg *testutil.TestServerConfig
	testClient := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
		c.Experiments = []string{"resource-apis"}
		serverCfg = c
	})

	// Start the consul service registration in a go func and delay it so that it runs
	// after the cmd.Run() starts.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Wait a moment, this ensures that we are already in the retry logic.
		time.Sleep(time.Second * 2)
		// Register counting service.
		loadResource(t, testClient.ResourceClient, getWorkloadID(testPodName, constants.DefaultConsulNS, constants.DefaultConsulPartition), getWorkload(), nil)
	}()

	ui := cli.NewMockUi()
	cmd := Command{
		UI:                 ui,
		maxPollingAttempts: 10,
	}

	flags := []string{
		"-proxy-name", testPodName,
		"-addresses", "127.0.0.1",
		"-http-port", strconv.Itoa(serverCfg.Ports.HTTP),
		"-grpc-port", strconv.Itoa(serverCfg.Ports.GRPC),
	}
	code := cmd.Run(flags)
	wg.Wait()
	require.Equal(t, 0, code)
}

func TestRun_TrafficRedirection(t *testing.T) {
	cases := map[string]struct {
		registerProxyConfiguration bool
		expIptablesParamsFunc      func(actual iptables.Config) error
	}{
		"no proxyConfiguration provided": {
			expIptablesParamsFunc: func(actual iptables.Config) error {
				if len(actual.ExcludeInboundPorts) != 0 {
					return fmt.Errorf("ExcludeInboundPorts in iptables.Config was %v, but should be empty", actual.ExcludeInboundPorts)
				}
				if actual.ProxyInboundPort != 20000 {
					return fmt.Errorf("ProxyInboundPort in iptables.Config was %d, but should be [20000]", actual.ProxyOutboundPort)
				}
				if actual.ProxyOutboundPort != 15001 {
					return fmt.Errorf("ProxyOutboundPort in iptables.Config was %d, but should be [15001]", actual.ProxyOutboundPort)
				}
				return nil
			},
		},
		"stats bind port is provided in proxyConfiguration": {
			registerProxyConfiguration: true,
			expIptablesParamsFunc: func(actual iptables.Config) error {
				if len(actual.ExcludeInboundPorts) != 1 || actual.ExcludeInboundPorts[0] != "9090" {
					return fmt.Errorf("ExcludeInboundPorts in iptables.Config was %v, but should be [9090, 1234]", actual.ExcludeInboundPorts)
				}
				if actual.ProxyInboundPort != 20000 {
					return fmt.Errorf("ProxyInboundPort in iptables.Config was %d, but should be [20000]", actual.ProxyOutboundPort)
				}
				if actual.ProxyOutboundPort != 15001 {
					return fmt.Errorf("ProxyOutboundPort in iptables.Config was %d, but should be [15001]", actual.ProxyOutboundPort)
				}
				return nil
			},
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			// Start Consul server.
			var serverCfg *testutil.TestServerConfig
			testClient := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
				c.Experiments = []string{"resource-apis"}
				serverCfg = c
			})

			// Add additional proxy configuration either to a config entry or to the service itself.
			if c.registerProxyConfiguration {
				loadResource(t, testClient.ResourceClient, getProxyConfigurationID(testPodName, constants.DefaultConsulNS, constants.DefaultConsulPartition), getProxyConfiguration(), nil)
			}

			// Register Consul workload.
			loadResource(t, testClient.ResourceClient, getWorkloadID(testPodName, constants.DefaultConsulNS, constants.DefaultConsulPartition), getWorkload(), nil)

			iptablesProvider := &fakeIptablesProvider{}
			iptablesCfg := iptables.Config{
				ProxyUserID:       "5995",
				ProxyInboundPort:  20000,
				ProxyOutboundPort: 15001,
			}

			ui := cli.NewMockUi()
			cmd := Command{
				UI:                 ui,
				maxPollingAttempts: 3,
				iptablesProvider:   iptablesProvider,
			}
			iptablesCfgJSON, err := json.Marshal(iptablesCfg)
			require.NoError(t, err)

			flags := []string{
				"-proxy-name", testPodName,
				"-addresses", "127.0.0.1",
				"-http-port", strconv.Itoa(serverCfg.Ports.HTTP),
				"-grpc-port", strconv.Itoa(serverCfg.Ports.GRPC),
				"-redirect-traffic-config", string(iptablesCfgJSON),
			}
			code := cmd.Run(flags)
			require.Equal(t, 0, code, ui.ErrorWriter.String())
			require.Truef(t, iptablesProvider.applyCalled, "redirect traffic rules were not applied")
			if c.expIptablesParamsFunc != nil {
				errMsg := c.expIptablesParamsFunc(cmd.iptablesConfig)
				require.NoError(t, errMsg)
			}
		})
	}
}

const (
	testPodName = "foo"
)

type fakeIptablesProvider struct {
	applyCalled bool
	rules       []string
}

func loadResource(t *testing.T, client pbresource.ResourceServiceClient, id *pbresource.ID, proto proto.Message, owner *pbresource.ID) {
	if id == nil || !proto.ProtoReflect().IsValid() {
		return
	}

	data, err := anypb.New(proto)
	require.NoError(t, err)

	resource := &pbresource.Resource{
		Id:    id,
		Data:  data,
		Owner: owner,
	}

	req := &pbresource.WriteRequest{Resource: resource}
	_, err = client.Write(context.Background(), req)
	require.NoError(t, err)
	test.ResourceHasPersisted(t, context.Background(), client, id)
}

func getWorkloadID(name, namespace, partition string) *pbresource.ID {
	return &pbresource.ID{
		Name: name,
		Type: pbcatalog.WorkloadType,
		Tenancy: &pbresource.Tenancy{
			Partition: partition,
			Namespace: namespace,
		},
	}
}

// getWorkload creates a proxyConfiguration that matches the pod from createPod,
// assuming that metrics, telemetry, and overwrite probes are enabled separately.
func getWorkload() *pbcatalog.Workload {
	return &pbcatalog.Workload{
		Addresses: []*pbcatalog.WorkloadAddress{
			{Host: "10.0.0.1", Ports: []string{"public", "admin", "mesh"}},
		},
		Ports: map[string]*pbcatalog.WorkloadPort{
			"public": {
				Port:     80,
				Protocol: pbcatalog.Protocol_PROTOCOL_UNSPECIFIED,
			},
			"admin": {
				Port:     8080,
				Protocol: pbcatalog.Protocol_PROTOCOL_UNSPECIFIED,
			},
			"mesh": {
				Port:     constants.ProxyDefaultInboundPort,
				Protocol: pbcatalog.Protocol_PROTOCOL_MESH,
			},
		},
		NodeName: "k8s-node-0",
		Identity: testPodName,
	}
}

func getProxyConfigurationID(name, namespace, partition string) *pbresource.ID {
	return &pbresource.ID{
		Name: name,
		Type: pbmesh.ProxyConfigurationType,
		Tenancy: &pbresource.Tenancy{
			Partition: partition,
			Namespace: namespace,
		},
	}
}

// getProxyConfiguration creates a proxyConfiguration that matches the pod from createWorkload.
func getProxyConfiguration() *pbmesh.ProxyConfiguration {
	return &pbmesh.ProxyConfiguration{
		Workloads: &pbcatalog.WorkloadSelector{
			Names: []string{testPodName},
		},
		DynamicConfig: &pbmesh.DynamicConfig{
			Mode: pbmesh.ProxyMode_PROXY_MODE_TRANSPARENT,
			ExposeConfig: &pbmesh.ExposeConfig{
				ExposePaths: []*pbmesh.ExposePath{
					{
						ListenerPort:  20400,
						LocalPathPort: 2001,
						Path:          "/livez",
					},
					{
						ListenerPort:  20300,
						LocalPathPort: 2000,
						Path:          "/readyz",
					},
					{
						ListenerPort:  20500,
						LocalPathPort: 2002,
						Path:          "/startupz",
					},
				},
			},
		},
		BootstrapConfig: &pbmesh.BootstrapConfig{
			StatsBindAddr:      "0.0.0.0:9090",
			PrometheusBindAddr: "0.0.0.0:21234", // This gets added to the iptables exclude directly in the webhook
		},
	}
}

func (f *fakeIptablesProvider) AddRule(_ string, args ...string) {
	f.rules = append(f.rules, strings.Join(args, " "))
}

func (f *fakeIptablesProvider) ApplyRules() error {
	f.applyCalled = true
	return nil
}

func (f *fakeIptablesProvider) Rules() []string {
	return f.rules
}
