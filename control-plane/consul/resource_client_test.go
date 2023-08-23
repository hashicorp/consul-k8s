package consul

import (
	"context"
	"testing"

	"github.com/hashicorp/consul-server-connection-manager/discovery"
	pbcatalog "github.com/hashicorp/consul/proto-public/pbcatalog/v1alpha1"
	"github.com/hashicorp/consul/proto-public/pbresource"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/anypb"
)

func Test_NewResourceServiceClient(t *testing.T) {

	var serverConfig *testutil.TestServerConfig
	server, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
		c.Experiments = []string{"resource-apis"}
		serverConfig = c
	})
	require.NoError(t, err)
	defer server.Stop()

	server.WaitForLeader(t)
	server.WaitForActiveCARoot(t)

	t.Logf("server grpc address on %d", serverConfig.Ports.GRPC)

	// Create discovery configuration
	discoverConfig := discovery.Config{
		Addresses: "127.0.0.1",
		GRPCPort:  serverConfig.Ports.GRPC,
	}

	opts := hclog.LoggerOptions{Name: "resource-service-client"}
	logger := hclog.New(&opts)
	client, watcher, err := NewResourceServiceClient(context.Background(), discoverConfig, logger, serverConfig.Ports.GRPCTLS)
	require.NoError(t, err)
	require.NotNil(t, client)
	require.NotNil(t, watcher)

	defer watcher.Stop()

	req := createWriteRequest(t, "foo")
	res, err := client.Write(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Equal(t, "foo", res.GetResource().GetId().GetName())

	// NOTE: currently it isn't possible to test that the grpc connection responds to changes in the
	// discovery server. The discovery response only includes the IP address of the host, so all servers
	// for a local test are de-duplicated as a single entry.
}

func createWriteRequest(t *testing.T, name string) *pbresource.WriteRequest {

	workload := &pbcatalog.Workload{
		Addresses: []*pbcatalog.WorkloadAddress{
			{Host: "10.0.0.1", Ports: []string{"public", "admin", "mesh"}},
		},
		Ports: map[string]*pbcatalog.WorkloadPort{
			"public": {
				Port:     80,
				Protocol: pbcatalog.Protocol_PROTOCOL_TCP,
			},
			"admin": {
				Port:     8080,
				Protocol: pbcatalog.Protocol_PROTOCOL_TCP,
			},
			"mesh": {
				Port:     20000,
				Protocol: pbcatalog.Protocol_PROTOCOL_MESH,
			},
		},
		NodeName: "k8s-node-0-virtual",
		Identity: name,
	}

	proto, err := anypb.New(workload)
	require.NoError(t, err)

	req := &pbresource.WriteRequest{
		Resource: &pbresource.Resource{
			Id: &pbresource.ID{
				Name: name,
				Type: &pbresource.Type{
					Group:        "catalog",
					GroupVersion: "v1alpha1",
					Kind:         "Workload",
				},
				Tenancy: &pbresource.Tenancy{
					Partition: "default",
					Namespace: "default",
				},
			},
			Data: proto,
		},
	}
	return req
}
