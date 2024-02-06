// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package consul

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/hashicorp/consul-server-connection-manager/discovery"
	pbcatalog "github.com/hashicorp/consul/proto-public/pbcatalog/v2beta1"
	"github.com/hashicorp/consul/proto-public/pbdataplane"
	pbmesh "github.com/hashicorp/consul/proto-public/pbmesh/v2beta1"
	"github.com/hashicorp/consul/proto-public/pbresource"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/anypb"
)

func Test_NewDataplaneServiceClient(t *testing.T) {

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

	opts := hclog.LoggerOptions{Name: "dataplane-service-client"}
	logger := hclog.New(&opts)

	watcher, err := discovery.NewWatcher(context.Background(), discoverConfig, logger)
	require.NoError(t, err)
	require.NotNil(t, watcher)

	defer watcher.Stop()
	go watcher.Run()

	// Create a workload and create a proxyConfiguration
	createWorkload(t, watcher, "foo")
	pc := createProxyConfiguration(t, watcher, "foo")

	client, err := NewDataplaneServiceClient(watcher)
	require.NoError(t, err)
	require.NotNil(t, client)
	require.NotNil(t, watcher)

	req := &pbdataplane.GetEnvoyBootstrapParamsRequest{
		ProxyId:   "foo",
		Namespace: "default",
		Partition: "default",
	}

	res, err := client.GetEnvoyBootstrapParams(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Equal(t, "foo", res.GetIdentity())
	require.Equal(t, "default", res.GetNamespace())
	require.Equal(t, "default", res.GetPartition())

	if diff := cmp.Diff(pc.BootstrapConfig, res.GetBootstrapConfig(), protocmp.Transform()); diff != "" {
		t.Errorf("unexpected difference:\n%v", diff)
	}

	// NOTE: currently it isn't possible to test that the grpc connection responds to changes in the
	// discovery server. The discovery response only includes the IP address of the host, so all servers
	// for a local test are de-duplicated as a single entry.
}

func createWorkload(t *testing.T, watcher ServerConnectionManager, name string) {

	client, err := NewResourceServiceClient(watcher)
	require.NoError(t, err)

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

	id := &pbresource.ID{
		Name: name,
		Type: pbcatalog.WorkloadType,
		Tenancy: &pbresource.Tenancy{
			Partition: "default",
			Namespace: "default",
		},
	}

	proto, err := anypb.New(workload)
	require.NoError(t, err)

	req := &pbresource.WriteRequest{
		Resource: &pbresource.Resource{
			Id:   id,
			Data: proto,
		},
	}

	_, err = client.Write(context.Background(), req)
	require.NoError(t, err)

	resourceHasPersisted(t, client, id)
}

func createProxyConfiguration(t *testing.T, watcher ServerConnectionManager, name string) *pbmesh.ProxyConfiguration {

	client, err := NewResourceServiceClient(watcher)
	require.NoError(t, err)

	pc := &pbmesh.ProxyConfiguration{
		Workloads: &pbcatalog.WorkloadSelector{
			Names: []string{"foo"},
		},
		BootstrapConfig: &pbmesh.BootstrapConfig{
			StatsBindAddr: "127.0.0.2:1234",
			ReadyBindAddr: "127.0.0.3:5678",
		},
	}

	id := &pbresource.ID{
		Name: name,
		Type: pbmesh.ProxyConfigurationType,
		Tenancy: &pbresource.Tenancy{
			Partition: "default",
			Namespace: "default",
		},
	}

	proto, err := anypb.New(pc)
	require.NoError(t, err)

	req := &pbresource.WriteRequest{
		Resource: &pbresource.Resource{
			Id:   id,
			Data: proto,
		},
	}

	_, err = client.Write(context.Background(), req)
	require.NoError(t, err)

	resourceHasPersisted(t, client, id)
	return pc
}

// resourceHasPersisted checks that a recently written resource exists in the Consul
// state store with a valid version. This must be true before a resource is overwritten
// or deleted.
// TODO: refactor so that there isn't an import cycle when using test.ResourceHasPersisted.
func resourceHasPersisted(t *testing.T, client pbresource.ResourceServiceClient, id *pbresource.ID) {
	req := &pbresource.ReadRequest{Id: id}

	require.Eventually(t, func() bool {
		res, err := client.Read(context.Background(), req)
		if err != nil {
			return false
		}

		if res.GetResource().GetVersion() == "" {
			return false
		}

		return true
	}, 5*time.Second,
		time.Second)
}
