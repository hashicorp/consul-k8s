// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package catalog

import (
	"context"
	"reflect"
	"testing"

	toconsul "github.com/hashicorp/consul-k8s/control-plane/catalog/to-consul"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/require"
)

// Test that the source works with services registered before hand.
func TestSource_initServices(t *testing.T) {
	t.Parallel()

	// Set up server, client
	testClient := test.TestServerWithMockConnMgrWatcher(t, nil)
	client := testClient.APIClient

	// Create services before the source is running
	_, err := client.Catalog().Register(testRegistration("hostA", "svcA", nil), nil)
	require.NoError(t, err)
	_, err = client.Catalog().Register(testRegistration("hostB", "svcA", nil), nil)
	require.NoError(t, err)
	_, err = client.Catalog().Register(testRegistration("hostB", "svcB", nil), nil)
	require.NoError(t, err)

	_, sink, closer := testSource(testClient.Cfg, testClient.Watcher)
	defer closer()

	var actual map[string]string
	retry.Run(t, func(r *retry.R) {
		sink.Lock()
		defer sink.Unlock()
		actual = sink.Services
		if len(actual) != 3 {
			r.Fatal("services not found")
		}
	})

	expected := map[string]string{
		"consul": "consul.service.test",
		"svcA":   "svcA.service.test",
		"svcB":   "svcB.service.test",
	}
	require.Equal(t, expected, actual)
}

// Test that we can specify a prefix to prepend to all destination services.
func TestSource_prefix(t *testing.T) {
	t.Parallel()

	// Set up server, client
	testClient := test.TestServerWithMockConnMgrWatcher(t, nil)
	client := testClient.APIClient

	_, sink, closer := testSourceWithConfig(testClient.Cfg, testClient.Watcher, func(s *Source) {
		s.Prefix = "foo-"
	})
	defer closer()

	// Create services before the source is running
	_, err := client.Catalog().Register(testRegistration("hostA", "svcA", nil), nil)
	require.NoError(t, err)
	_, err = client.Catalog().Register(testRegistration("hostB", "svcA", nil), nil)
	require.NoError(t, err)
	_, err = client.Catalog().Register(testRegistration("hostB", "svcB", nil), nil)
	require.NoError(t, err)

	var actual map[string]string
	retry.Run(t, func(r *retry.R) {
		sink.Lock()
		defer sink.Unlock()
		actual = sink.Services
		if len(actual) != 3 {
			r.Fatal("services not found")
		}
	})

	expected := map[string]string{
		"foo-consul": "consul.service.test",
		"foo-svcA":   "svcA.service.test",
		"foo-svcB":   "svcB.service.test",
	}
	require.Equal(t, expected, actual)
}

// Test that the source ignores K8S services.
func TestSource_ignoreK8S(t *testing.T) {
	t.Parallel()

	// Set up server, client
	testClient := test.TestServerWithMockConnMgrWatcher(t, nil)
	client := testClient.APIClient

	// Create services before the source is running
	_, err := client.Catalog().Register(testRegistration("hostA", "svcA", nil), nil)
	require.NoError(t, err)
	_, err = client.Catalog().Register(testRegistration("hostB", "svcA", nil), nil)
	require.NoError(t, err)
	_, err = client.Catalog().Register(testRegistration("hostB", "svcB", []string{toconsul.TestConsulK8STag}), nil)
	require.NoError(t, err)

	_, sink, closer := testSource(testClient.Cfg, testClient.Watcher)
	defer closer()

	var actual map[string]string
	retry.Run(t, func(r *retry.R) {
		sink.Lock()
		defer sink.Unlock()
		actual = sink.Services
		if len(actual) != 2 {
			r.Fatal("services not found")
		}
	})

	expected := map[string]string{
		"consul": "consul.service.test",
		"svcA":   "svcA.service.test",
	}
	require.Equal(t, expected, actual)
}

// Test that the source deletes services properly.
func TestSource_deleteService(t *testing.T) {
	// Unable to be run in parallel with other tests that
	// check for the existence of `consul.service.test`

	// Set up server, client
	testClient := test.TestServerWithMockConnMgrWatcher(t, nil)
	client := testClient.APIClient

	// Create services before the source is running
	_, err := client.Catalog().Register(testRegistration("hostA", "svcA", nil), nil)
	require.NoError(t, err)
	_, err = client.Catalog().Register(testRegistration("hostB", "svcA", nil), nil)
	require.NoError(t, err)
	_, err = client.Catalog().Register(testRegistration("hostB", "svcB", nil), nil)
	require.NoError(t, err)

	_, sink, closer := testSource(testClient.Cfg, testClient.Watcher)
	defer closer()

	var actual map[string]string
	retry.Run(t, func(r *retry.R) {
		sink.Lock()
		defer sink.Unlock()
		actual = sink.Services
		if len(actual) != 3 {
			r.Fatal("services not found")
		}
	})

	// Delete the service
	_, err = client.Catalog().Deregister(&api.CatalogDeregistration{
		Node: "hostB", ServiceID: "svcB"}, nil)
	require.NoError(t, err)

	retry.Run(t, func(r *retry.R) {
		sink.Lock()
		defer sink.Unlock()
		svcs := sink.Services
		if reflect.DeepEqual(svcs, actual) {
			r.Fatal("services are the same")
		}

		actual = svcs
	})

	expected := map[string]string{
		"consul": "consul.service.test",
		"svcA":   "svcA.service.test",
	}
	require.Equal(t, expected, actual)
}

// Test that the source deletes services properly. This case tests
// deleting a single service instance, which shouldn't negatively affect
// anything.
func TestSource_deleteServiceInstance(t *testing.T) {
	t.Parallel()

	// Set up server, client
	testClient := test.TestServerWithMockConnMgrWatcher(t, nil)
	client := testClient.APIClient

	// Create services before the source is running
	_, err := client.Catalog().Register(testRegistration("hostA", "svcA", nil), nil)
	require.NoError(t, err)
	_, err = client.Catalog().Register(testRegistration("hostB", "svcA", nil), nil)
	require.NoError(t, err)
	_, err = client.Catalog().Register(testRegistration("hostB", "svcB", nil), nil)
	require.NoError(t, err)

	_, sink, closer := testSource(testClient.Cfg, testClient.Watcher)
	defer closer()

	var actual map[string]string
	retry.Run(t, func(r *retry.R) {
		sink.Lock()
		defer sink.Unlock()
		actual = sink.Services
		if len(actual) != 3 {
			r.Fatal("services not found")
		}
	})

	// Delete the service
	_, err = client.Catalog().Deregister(&api.CatalogDeregistration{
		Node: "hostB", ServiceID: "svcA"}, nil)
	require.NoError(t, err)

	retry.Run(t, func(r *retry.R) {
		sink.Lock()
		defer sink.Unlock()
		svcs := sink.Services
		if !reflect.DeepEqual(svcs, actual) {
			r.Fatal("services are not the same")
		}
	})
}

// testRegistration creates a Consul test registration.
func testRegistration(node, service string, tags []string) *api.CatalogRegistration {
	return &api.CatalogRegistration{
		Node:    node,
		Address: "127.0.0.1",
		Service: &api.AgentService{
			Service: service,
			Tags:    tags,
		},
	}
}

// testSource creates a Source and Sink for testing.
func testSource(clientCfg *consul.Config, connMgr consul.ServerConnectionManager) (*Source, *TestSink, func()) {
	return testSourceWithConfig(clientCfg, connMgr, func(source *Source) {})
}

// testSourceWithConfig starts a Source that can be configured
// prior to starting via the configurator method.
func testSourceWithConfig(clientCfg *consul.Config, connMgr consul.ServerConnectionManager, configurator func(*Source)) (*Source, *TestSink, func()) {
	sink := &TestSink{}
	s := &Source{
		ConsulClientConfig:  clientCfg,
		ConsulServerConnMgr: connMgr,
		Domain:              "test",
		Sink:                sink,
		Log:                 hclog.Default(),
		ConsulK8STag:        toconsul.TestConsulK8STag,
	}
	configurator(s)

	ctx, cancelF := context.WithCancel(context.Background())
	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		s.Run(ctx)
	}()

	return s, sink, func() {
		cancelF()
		<-doneCh
	}
}
