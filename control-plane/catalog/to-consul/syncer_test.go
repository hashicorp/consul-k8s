// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package catalog

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/armon/go-metrics/prometheus"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/require"

	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
)

const (
	// ConsulSyncNodeName is the name of the node in Consul that we register
	// services on. It's not a real node backed by a Consul agent.
	ConsulSyncNodeName = "k8s-sync"
)

func TestConsulSyncer_register(t *testing.T) {
	t.Parallel()

	// Set up server, client, syncer
	// Create test consulServer server.
	testClient := test.TestServerWithMockConnMgrWatcher(t, nil)
	client := testClient.APIClient

	s, closer := testConsulSyncer(testClient)
	defer closer()

	// Sync
	s.Sync([]*api.CatalogRegistration{
		testRegistration(ConsulSyncNodeName, "bar", "default"),
	})

	// Read the service back out
	var service *api.CatalogService
	retry.Run(t, func(r *retry.R) {
		resp, meta, err := client.Catalog().Services(&api.QueryOptions{})
		require.NoError(t, err)

		fmt.Println("=======")
		fmt.Printf("Services: %+v\n", resp)
		fmt.Printf("Meta: %+v\n", meta)

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
	require.Equal(t, "k8s-sync", service.Node)
	require.Equal(t, "bar", service.ServiceName)
	require.Equal(t, "127.0.0.1", service.Address)
}

// Test that the syncer reaps individual invalid service instances.
func TestConsulSyncer_reapServiceInstance(t *testing.T) {
	t.Parallel()

	for _, node := range []string{ConsulSyncNodeName, "test-node"} {
		name := fmt.Sprintf("consul node name: %s", node)
		t.Run(name, func(t *testing.T) {
			// Set up server, client, syncer
			testClient := test.TestServerWithMockConnMgrWatcher(t, nil)
			client := testClient.APIClient

			s, closer := testConsulSyncer(testClient)
			defer closer()

			// Sync
			s.Sync([]*api.CatalogRegistration{
				testRegistration(node, "bar", "default"),
			})

			// Wait for the first service
			retry.Run(t, func(r *retry.R) {
				services, _, err := client.Catalog().Service("bar", s.ConsulK8STag, nil)
				if err != nil {
					r.Fatalf("err: %s", err)
				}
				if len(services) != 1 {
					r.Fatal("service not found or too many")
				}
			})

			// Create an invalid service directly in Consul
			svc := testRegistration(node, "bar", "default")
			svc.Service.ID = serviceID(node, "bar2")
			fmt.Println("invalid service id", svc.Service.ID)
			_, err := client.Catalog().Register(svc, nil)
			require.NoError(t, err)

			// Valid service should exist
			var service *api.CatalogService
			retry.Run(t, func(r *retry.R) {
				services, _, err := client.Catalog().Service("bar", s.ConsulK8STag, nil)
				if err != nil {
					r.Fatalf("err: %s", err)
				}
				if len(services) != 1 {
					r.Fatal("service not found or too many")
				}
				service = services[0]
			})

			// Verify the settings
			require.Equal(t, serviceID(node, "bar"), service.ServiceID)
			require.Equal(t, node, service.Node)
			require.Equal(t, "bar", service.ServiceName)
			require.Equal(t, "127.0.0.1", service.Address)
		})
	}
}

// Test that the syncer reaps services not registered by us that are tagged
// with k8s.
func TestConsulSyncer_reapService(t *testing.T) {
	t.Parallel()

	sourceK8sNamespaceAnnotations := []string{"", "other", "default"}
	for _, k8sNS := range sourceK8sNamespaceAnnotations {
		t.Run(k8sNS, func(tt *testing.T) {
			testClient := test.TestServerWithMockConnMgrWatcher(t, nil)
			client := testClient.APIClient

			s, closer := testConsulSyncer(testClient)
			defer closer()

			// Run the sync with a test service
			s.Sync([]*api.CatalogRegistration{
				testRegistration(ConsulSyncNodeName, "bar", "default"),
			})

			// Create a service directly in Consul. Since it was created directly we
			// expect it to be deleted.
			svc := testRegistration(ConsulSyncNodeName, "baz", "default")
			svc.Service.Meta[ConsulK8SNS] = k8sNS
			_, err := client.Catalog().Register(svc, nil)
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

// Test that the syncer doesn't reap any services until the initial sync has
// been performed.
func TestConsulSyncer_noReapingUntilInitialSync(t *testing.T) {
	t.Parallel()

	testClient := test.TestServerWithMockConnMgrWatcher(t, nil)
	client := testClient.APIClient
	s, closer := testConsulSyncerWithConfig(testClient, func(s *ConsulSyncer) {
		// Set the sync period to 5ms so we know it will have run at least once
		// after we wait 100ms.
		s.SyncPeriod = 5 * time.Millisecond
	})
	defer closer()

	// Create a service directly in Consul. Since it was created on the
	// synthetic sync node and has the sync-associated tag, we expect
	// it to be deleted but not until the initial sync is performed.
	svc := testRegistration(ConsulSyncNodeName, "baz", "default")
	_, err := client.Catalog().Register(svc, nil)
	require.NoError(t, err)

	// We wait until the syncer has had the time to delete the service.
	// Since we set the sync period to 5ms we know that it's run syncFull at
	// least once.
	time.Sleep(100 * time.Millisecond)

	// Invalid service should not be deleted.
	bazInstances, _, err := client.Catalog().Service("baz", "", nil)
	require.NoError(t, err)
	require.Len(t, bazInstances, 1)

	// Now we perform the initial sync.
	s.Sync(nil)
	// The service should get deleted.
	retry.Run(t, func(r *retry.R) {
		bazInstances, _, err = client.Catalog().Service("baz", "", nil)
		require.NoError(r, err)
		require.Len(r, bazInstances, 0)
	})
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

	parsedURL, err := url.Parse(consulServer.URL)
	require.NoError(t, err)

	port, err := strconv.Atoi(parsedURL.Port())
	require.NoError(t, err)

	testClient := &test.TestServerClient{
		Cfg:     &consul.Config{APIClientConfig: &api.Config{}, HTTPPort: port},
		Watcher: test.MockConnMgrForIPAndPort(t, parsedURL.Host, port, false),
	}

	// Start the syncer.
	s, closer := testConsulSyncer(testClient)

	// Sync
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

func testConsulSyncer(testClient *test.TestServerClient) (*ConsulSyncer, func()) {
	return testConsulSyncerWithConfig(testClient, func(syncer *ConsulSyncer) {})
}

// testConsulSyncerWithConfig starts a consul syncer that can be configured
// prior to starting via the configurator method.
func testConsulSyncerWithConfig(testClient *test.TestServerClient, configurator func(*ConsulSyncer)) (*ConsulSyncer, func()) {
	s := &ConsulSyncer{
		ConsulClientConfig:  testClient.Cfg,
		ConsulServerConnMgr: testClient.Watcher,
		Log:                 hclog.Default(),
		SyncPeriod:          200 * time.Millisecond,
		ServicePollPeriod:   50 * time.Millisecond,
		ConsulK8STag:        TestConsulK8STag,
		ConsulNodeName:      ConsulSyncNodeName,
		PrometheusSink:      &prometheus.PrometheusSink{},
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
