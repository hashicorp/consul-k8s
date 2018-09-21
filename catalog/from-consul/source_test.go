package catalog

import (
	"context"
	"reflect"
	"testing"

	"github.com/hashicorp/consul/agent"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/testrpc"
	"github.com/hashicorp/consul/testutil/retry"
	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/require"
)

// Test that the source works with services registered before hand.
func TestSource_initServices(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	a := agent.NewTestAgent(t.Name(), ``)
	defer a.Shutdown()
	testrpc.WaitForTestAgent(t, a.RPC, "dc1")
	client := a.Client()

	// Create services before the source is running
	_, err := client.Catalog().Register(testRegistration("hostA", "svcA", nil), nil)
	require.NoError(err)
	_, err = client.Catalog().Register(testRegistration("hostB", "svcA", nil), nil)
	require.NoError(err)
	_, err = client.Catalog().Register(testRegistration("hostB", "svcB", nil), nil)
	require.NoError(err)

	_, sink, closer := testSource(t, client)
	defer closer()

	var actual map[string]string
	retry.Run(t, func(r *retry.R) {
		sink.Lock()
		defer sink.Unlock()
		actual = sink.Services
		if len(actual) == 0 {
			r.Fatal("services not found")
		}
	})

	expected := map[string]string{
		"consul": "consul.service.test.",
		"svcA":   "svcA.service.test.",
		"svcB":   "svcB.service.test.",
	}
	require.Equal(expected, actual)
}

// Test that the source deletes services properly.
func TestSource_deleteService(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	a := agent.NewTestAgent(t.Name(), ``)
	defer a.Shutdown()
	testrpc.WaitForTestAgent(t, a.RPC, "dc1")
	client := a.Client()

	// Create services before the source is running
	_, err := client.Catalog().Register(testRegistration("hostA", "svcA", nil), nil)
	require.NoError(err)
	_, err = client.Catalog().Register(testRegistration("hostB", "svcA", nil), nil)
	require.NoError(err)
	_, err = client.Catalog().Register(testRegistration("hostB", "svcB", nil), nil)
	require.NoError(err)

	_, sink, closer := testSource(t, client)
	defer closer()

	var actual map[string]string
	retry.Run(t, func(r *retry.R) {
		sink.Lock()
		defer sink.Unlock()
		actual = sink.Services
		if len(actual) == 0 {
			r.Fatal("services not found")
		}
	})

	// Delete the service
	_, err = client.Catalog().Deregister(&api.CatalogDeregistration{
		Node: "hostB", ServiceID: "svcB"}, nil)
	require.NoError(err)

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
		"consul": "consul.service.test.",
		"svcA":   "svcA.service.test.",
	}
	require.Equal(expected, actual)
}

// Test that the source deletes services properly. This case tests
// deleting a single service instance, which shouldn't negatively affect
// anything.
func TestSource_deleteServiceInstance(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	a := agent.NewTestAgent(t.Name(), ``)
	defer a.Shutdown()
	testrpc.WaitForTestAgent(t, a.RPC, "dc1")
	client := a.Client()

	// Create services before the source is running
	_, err := client.Catalog().Register(testRegistration("hostA", "svcA", nil), nil)
	require.NoError(err)
	_, err = client.Catalog().Register(testRegistration("hostB", "svcA", nil), nil)
	require.NoError(err)
	_, err = client.Catalog().Register(testRegistration("hostB", "svcB", nil), nil)
	require.NoError(err)

	_, sink, closer := testSource(t, client)
	defer closer()

	var actual map[string]string
	retry.Run(t, func(r *retry.R) {
		sink.Lock()
		defer sink.Unlock()
		actual = sink.Services
		if len(actual) == 0 {
			r.Fatal("services not found")
		}
	})

	// Delete the service
	_, err = client.Catalog().Deregister(&api.CatalogDeregistration{
		Node: "hostB", ServiceID: "svcA"}, nil)
	require.NoError(err)

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
func testSource(t *testing.T, client *api.Client) (*Source, *TestSink, func()) {
	sink := &TestSink{}
	s := &Source{
		Client: client,
		Domain: "test",
		Sink:   sink,
		Log:    hclog.Default(),
	}

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
