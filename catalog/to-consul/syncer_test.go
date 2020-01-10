package catalog

import (
	"context"
	"testing"
	"time"

	"github.com/deckarep/golang-set"
	"github.com/hashicorp/consul/agent"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/hashicorp/consul/testrpc"
	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/require"
)

func TestConsulSyncer_register(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	a := agent.NewTestAgent(t, t.Name(), ``)
	defer a.Shutdown()
	testrpc.WaitForTestAgent(t, a.RPC, "dc1")
	client := a.Client()

	s, closer := testConsulSyncer(t, client)
	defer closer()

	// Sync
	s.Sync([]*api.CatalogRegistration{
		testRegistration("k8s-sync", "bar", "default"),
	})

	// Read the service back out
	var service *api.CatalogService
	retry.Run(t, func(r *retry.R) {
		services, _, err := client.Catalog().Service("bar", "", nil)
		if err != nil {
			r.Fatalf("err: %s", err)
		}
		if len(services) == 0 {
			r.Fatal("service not found")
		}
		service = services[0]
	})

	// Verify the settings
	require.Equal("k8s-sync", service.Node)
	require.Equal("bar", service.ServiceName)
	require.Equal("127.0.0.1", service.Address)
}

// Test that the syncer reaps invalid services
func TestConsulSyncer_reapService(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	a := agent.NewTestAgent(t, t.Name(), ``)
	defer a.Shutdown()
	testrpc.WaitForTestAgent(t, a.RPC, "dc1")
	client := a.Client()

	s, closer := testConsulSyncer(t, client)
	defer closer()

	// Sync
	s.Sync([]*api.CatalogRegistration{
		testRegistration("k8s-sync", "bar", "default"),
	})

	// Create an invalid service directly in Consul
	_, err := client.Catalog().Register(testRegistration("k8s-sync", "baz", "default"), nil)
	require.NoError(err)

	// Reaped service should not exist
	retry.Run(t, func(r *retry.R) {
		services, _, err := client.Catalog().Service("baz", "", nil)
		if err != nil {
			r.Fatalf("err: %s", err)
		}
		if len(services) > 0 {
			r.Fatal("service still exists")
		}
	})

	// Valid service should exist
	var service *api.CatalogService
	retry.Run(t, func(r *retry.R) {
		services, _, err := client.Catalog().Service("bar", "", nil)
		if err != nil {
			r.Fatalf("err: %s", err)
		}
		if len(services) == 0 {
			r.Fatal("service not found")
		}
		service = services[0]
	})

	// Verify the settings
	require.Equal("k8s-sync", service.Node)
	require.Equal("bar", service.ServiceName)
	require.Equal("127.0.0.1", service.Address)
}

// Test that the syncer reaps invalid services by instance
func TestConsulSyncer_reapServiceInstance(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	a := agent.NewTestAgent(t, t.Name(), ``)
	defer a.Shutdown()
	testrpc.WaitForTestAgent(t, a.RPC, "dc1")
	client := a.Client()

	s, closer := testConsulSyncer(t, client)
	defer closer()

	// Sync
	s.Sync([]*api.CatalogRegistration{
		testRegistration("k8s-sync", "bar", "default"),
	})

	// Wait for the first service
	retry.Run(t, func(r *retry.R) {
		services, _, err := client.Catalog().Service("bar", "", nil)
		if err != nil {
			r.Fatalf("err: %s", err)
		}
		if len(services) != 1 {
			r.Fatal("service not found or too many")
		}
	})

	// Create an invalid service directly in Consul
	svc := testRegistration("k8s-sync", "bar", "default")
	svc.Service.ID = serviceID("k8s-sync", "bar2")
	_, err := client.Catalog().Register(svc, nil)
	require.NoError(err)

	// Valid service should exist
	var service *api.CatalogService
	retry.Run(t, func(r *retry.R) {
		services, _, err := client.Catalog().Service("bar", "", nil)
		if err != nil {
			r.Fatalf("err: %s", err)
		}
		if len(services) != 1 {
			r.Fatal("service not found or too many")
		}
		service = services[0]
	})

	// Verify the settings
	require.Equal(serviceID("k8s-sync", "bar"), service.ServiceID)
	require.Equal("k8s-sync", service.Node)
	require.Equal("bar", service.ServiceName)
	require.Equal("127.0.0.1", service.Address)
}

// Test that the syncer does not reap services in another NS.
// func TestConsulSyncer_reapServiceOtherNamespace(t *testing.T) {
// 	t.Parallel()
// 	require := require.New(t)

// 	a := agent.NewTestAgent(t, t.Name(), ``)
// 	defer a.Shutdown()
// 	testrpc.WaitForTestAgent(t, a.RPC, "dc1")
// 	client := a.Client()

// 	s, closer := testConsulSyncer(t, client)
// 	// Restrict namespace allow list to a single namespace
// 	allowSet := mapset.NewSet("namespace")
// 	s.AllowK8sNamespacesSet = allowSet
// 	defer closer()

// 	// Sync
// 	s.Sync([]*api.CatalogRegistration{
// 		testRegistration("foo", "bar", "namespace"),
// 	})

// 	// Create an invalid service directly in Consul
// 	svc := testRegistration("foo", "baz")
// 	svc.Service.Meta[ConsulK8SNS] = "other"
// 	_, err := client.Catalog().Register(svc, nil)
// 	require.NoError(err)

// 	// Sleep for a bit
// 	time.Sleep(500 * time.Millisecond)

// 	// Valid service should exist
// 	services, _, err := client.Catalog().Service("baz", "", nil)
// 	require.NoError(err)
// 	require.Len(services, 1)
// }

// Test that the syncer reaps services with no NS set.
func TestConsulSyncer_reapServiceSameNamespace(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	a := agent.NewTestAgent(t, t.Name(), ``)
	defer a.Shutdown()
	testrpc.WaitForTestAgent(t, a.RPC, "dc1")
	client := a.Client()

	s, closer := testConsulSyncer(t, client)
	defer closer()

	// Sync
	s.Sync([]*api.CatalogRegistration{
		testRegistration("k8s-sync", "bar", "default"),
	})

	// Create an invalid service directly in Consul
	svc := testRegistration("k8s-sync", "baz", "")
	_, err := client.Catalog().Register(svc, nil)
	require.NoError(err)

	// Reaped service should not exist
	retry.Run(t, func(r *retry.R) {
		services, _, err := client.Catalog().Service("baz", "", nil)
		if err != nil {
			r.Fatalf("err: %s", err)
		}
		if len(services) > 0 {
			r.Fatal("service still exists")
		}
	})

	// Valid service should exist
	var service *api.CatalogService
	retry.Run(t, func(r *retry.R) {
		services, _, err := client.Catalog().Service("bar", "", nil)
		if err != nil {
			r.Fatalf("err: %s", err)
		}
		if len(services) == 0 {
			r.Fatal("service not found")
		}
		service = services[0]
	})

	// Verify the settings
	require.Equal("k8s-sync", service.Node)
	require.Equal("bar", service.ServiceName)
	require.Equal("127.0.0.1", service.Address)
}

func testRegistration(node, service, namespace string) *api.CatalogRegistration {
	return &api.CatalogRegistration{
		Node:           node,
		Address:        "127.0.0.1",
		NodeMeta:       map[string]string{ConsulSourceKey: TestConsulK8STag},
		SkipNodeUpdate: true,
		Service: &api.AgentService{
			ID:      serviceID(node, service),
			Service: service,
			Tags:    []string{TestConsulK8STag},
			Meta: map[string]string{
				ConsulSourceKey: TestConsulK8STag,
				ConsulK8SNS:     namespace,
			},
		},
	}
}

func testConsulSyncer(t *testing.T, client *api.Client) (*ConsulSyncer, func()) {
	// Set up required allow and deny sets
	allowSet := mapset.NewSet("*")
	denySet := mapset.NewSet()

	s := &ConsulSyncer{
		Client:                client,
		Log:                   hclog.Default(),
		SyncPeriod:            200 * time.Millisecond,
		ServicePollPeriod:     50 * time.Millisecond,
		AllowK8sNamespacesSet: allowSet,
		DenyK8sNamespacesSet:  denySet,
		ConsulK8STag:          TestConsulK8STag,
	}

	ctx, cancelF := context.WithCancel(context.Background())
	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		s.Run(ctx)
	}()

	return s, func() {
		cancelF()
		<-doneCh
	}
}
