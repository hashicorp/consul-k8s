package catalog

import (
	"context"
	"github.com/hashicorp/consul/sdk/testutil"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

	s, closer := testConsulSyncer(client)
	defer closer()

	// Sync
	s.Sync([]*api.CatalogRegistration{
		testRegistration(ConsulSyncNodeName, "bar", "default"),
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

// Test that the syncer reaps individual invalid service instances.
func TestConsulSyncer_reapServiceInstance(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	a := agent.NewTestAgent(t, t.Name(), ``)
	defer a.Shutdown()
	testrpc.WaitForTestAgent(t, a.RPC, "dc1")
	client := a.Client()

	s, closer := testConsulSyncer(client)
	defer closer()

	// Sync
	s.Sync([]*api.CatalogRegistration{
		testRegistration(ConsulSyncNodeName, "bar", "default"),
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
	svc := testRegistration(ConsulSyncNodeName, "bar", "default")
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

// Test that the syncer reaps services not registered by us that are tagged
// with k8s.
func TestConsulSyncer_reapService(t *testing.T) {
	t.Parallel()

	sourceK8sNamespaceAnnotations := []string{"", "other", "default"}
	for _, k8sNS := range sourceK8sNamespaceAnnotations {
		t.Run(k8sNS, func(tt *testing.T) {
			a, err := testutil.NewTestServerT(tt)
			require.NoError(tt, err)
			defer a.Stop()
			client, err := api.NewClient(&api.Config{
				Address: a.HTTPAddr,
			})
			require.NoError(tt, err)
			s, closer := testConsulSyncer(client)
			defer closer()

			s.Sync([]*api.CatalogRegistration{
				testRegistration(ConsulSyncNodeName, "bar", "default"),
			})

			// Create a service directly in Consul. Since it was created directly we
			// expect it to be deleted.
			svc := testRegistration(ConsulSyncNodeName, "baz", "default")
			svc.Service.Meta[ConsulK8SNS] = k8sNS
			_, err = client.Catalog().Register(svc, nil)
			require.NoError(tt, err)

			retry.Run(tt, func(r *retry.R) {
				// Invalid service should be deleted.
				bazInstances, _, err := client.Catalog().Service("baz", "", nil)
				require.NoError(r, err)
				require.Len(r, bazInstances, 0)

				// Valid service should still be registered.
				barInstances, _, err := client.Catalog().Service("bar", "", nil)
				require.NoError(r, err)
				require.Len(r, barInstances, 1)
				service := barInstances[0]
				require.Equal(r, ConsulSyncNodeName, service.Node)
				require.Equal(r, "bar", service.ServiceName)
				require.Equal(r, "127.0.0.1", service.Address)
			})
		})
	}
}

// Test that when the syncer is stopped, we don't continue to call the Consul
// API. This test was added as a regression test after a bug was discovered
// that after the context was cancelled, we would continue to make API calls
// to the Consul API in a tight loop.
func TestConsulSyncer_stopsGracefully(t *testing.T) {
	t.Parallel()

	// We use a test http server here so we can count the number of calls.
	callCount := 0
	consulServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		// We need to respond with errors to trigger the bug. If we don't
		// then the code path is only encountered after a timeout which we
		// won't trigger in the test.
		w.WriteHeader(500)
	}))
	defer consulServer.Close()

	// Start the syncer.
	client, err := api.NewClient(&api.Config{
		Address: consulServer.URL,
	})
	require.NoError(t, err)
	s, closer := testConsulSyncer(client)
	s.Sync([]*api.CatalogRegistration{
		testRegistration("k8s-sync", "bar", "default"),
	})

	// Compare the call count before and after stopping the server.
	beforeStopAPICount := callCount
	closer()
	time.Sleep(100 * time.Millisecond)
	// Before the bugfix, the count would be >100.
	require.LessOrEqual(t, callCount-beforeStopAPICount, 2)
}

func testRegistration(node, service, k8sSrcNamespace string) *api.CatalogRegistration {
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
				ConsulK8SNS:     k8sSrcNamespace,
			},
		},
	}
}

func testConsulSyncer(client *api.Client) (*ConsulSyncer, func()) {
	return testConsulSyncerWithConfig(client, func(syncer *ConsulSyncer) {})
}

// testConsulSyncerWithConfig starts a consul syncer that can be configured
// prior to starting via the configurator method.
func testConsulSyncerWithConfig(client *api.Client, configurator func(*ConsulSyncer)) (*ConsulSyncer, func()) {
	s := &ConsulSyncer{
		Client:            client,
		Log:               hclog.Default(),
		SyncPeriod:        200 * time.Millisecond,
		ServicePollPeriod: 50 * time.Millisecond,
		ConsulK8STag:      TestConsulK8STag,
		ConsulNodeServicesClient: &ConsulPreNamespacesNodeServicesClient{
			Client: client,
		},
	}
	configurator(s)
	s.init()

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
