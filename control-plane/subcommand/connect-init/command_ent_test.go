// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

//go:build enterprise

package connectinit

import (
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"testing"

	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/require"

	"github.com/hashicorp/consul-k8s/control-plane/namespaces"
)

func TestRun_WithNamespaces(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name                   string
		consulServiceNamespace string
	}{
		{
			name:                   "serviceNS=default",
			consulServiceNamespace: "default",
		},
		{
			name:                   "serviceNS=default-ns",
			consulServiceNamespace: "default-ns",
		},
		{
			name:                   "serviceNS=other",
			consulServiceNamespace: "other",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tokenFile := fmt.Sprintf("/tmp/%d1", rand.Int())
			proxyFile := fmt.Sprintf("/tmp/%d2", rand.Int())
			t.Cleanup(func() {
				_ = os.RemoveAll(proxyFile)
				_ = os.RemoveAll(tokenFile)
			})

			// Start Consul server with ACLs enabled and default deny policy.
			var serverCfg *testutil.TestServerConfig
			server, err := testutil.NewTestServerConfigT(t, func(cfg *testutil.TestServerConfig) {
				serverCfg = cfg
			})
			require.NoError(t, err)
			defer server.Stop()
			server.WaitForLeader(t)
			cfg := &api.Config{
				Address:   server.HTTPAddr,
				Namespace: c.consulServiceNamespace,
			}

			consulClient, err := api.NewClient(cfg)
			require.NoError(t, err)

			_, err = namespaces.EnsureExists(consulClient, c.consulServiceNamespace, "")
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
				serviceRegistrationPollingAttempts: 5,
			}
			// We build the consul-addr because normally it's defined by the init container setting
			// CONSUL_HTTP_ADDR when it processes the command template.
			flags := []string{"-pod-name", testPodName,
				"-pod-namespace", testPodNamespace,
				"-addresses", "127.0.0.1",
				"-http-port", strconv.Itoa(serverCfg.Ports.HTTP),
				"-grpc-port", strconv.Itoa(serverCfg.Ports.GRPC),
				"-namespace", c.consulServiceNamespace,
				"-proxy-id-file", proxyFile,
				"-consul-node-name", nodeName,
			}

			// Run the command.
			code := cmd.Run(flags)
			require.Equal(t, 0, code, ui.ErrorWriter.String())

			// Validate contents of proxyFile.
			data, err := os.ReadFile(proxyFile)
			require.NoError(t, err)
			require.Contains(t, string(data), "counting-counting-sidecar-proxy")
		})
	}
}
