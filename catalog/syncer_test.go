package catalog

import (
	"context"
	"testing"
	"time"

	"github.com/hashicorp/consul/agent"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/testutil/retry"
	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/require"
)

func TestConsulSyncer_register(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	a := agent.NewTestAgent(t.Name(), ``)
	defer a.Shutdown()
	client := a.Client()

	s, closer := testConsulSyncer(t, client)
	defer closer()

	// Sync
	s.Sync([]*api.CatalogRegistration{
		testRegistration("foo", "bar"),
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
	require.Equal("foo", service.Node)
	require.Equal("bar", service.ServiceName)
	require.Equal("127.0.0.1", service.Address)
}

func testRegistration(node, service string) *api.CatalogRegistration {
	return &api.CatalogRegistration{
		Node:    node,
		Address: "127.0.0.1",
		Service: &api.AgentService{
			Service: service,
		},
	}
}

func testConsulSyncer(t *testing.T, client *api.Client) (*ConsulSyncer, func()) {
	s := &ConsulSyncer{
		Client:          client,
		Log:             hclog.Default(),
		ReconcilePeriod: 200 * time.Millisecond,
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
