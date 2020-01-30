// +build enterprise

package catalog

import (
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestConsulSyncer_registerConsulNamespace(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	svr, err := testutil.NewTestServerT(t)
	require.NoError(err)
	defer svr.Stop()
	client, err := api.NewClient(&api.Config{
		Address:    svr.HTTPAddr,
	})
	require.NoError(err)

	s, closer := testConsulSyncer(t, client)
	defer closer()
	s.EnableNamespaces = true

	// Sync
	registration := testRegistration("k8s-sync", "bar", "default")
	registration.Service.Namespace = "newnamespace"
	s.Sync([]*api.CatalogRegistration{
		registration,
	})

	// Read the service back out
	var service *api.CatalogService
	retry.Run(t, func(r *retry.R) {
		services, _, err := client.Catalog().Service("bar", "", &api.QueryOptions{Namespace:"newnamespace"})
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
