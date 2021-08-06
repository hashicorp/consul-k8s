package catalog

import (
	"testing"

	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/stretchr/testify/require"
)

func TestPreNamespacesNodeServicesClient_NodeServices(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		ConsulServices []api.CatalogRegistration
		Exp            []ConsulService
	}{
		"no services": {
			ConsulServices: nil,
			Exp:            nil,
		},
		"no services on k8s node": {
			ConsulServices: []api.CatalogRegistration{
				{
					Node:    "not-k8s",
					Address: "127.0.0.1",
					Service: &api.AgentService{
						ID:      "svc-id",
						Service: "svc",
					},
				},
			},
			Exp: nil,
		},
		"service with k8s tag on different node": {
			ConsulServices: []api.CatalogRegistration{
				{
					Node:    "not-k8s",
					Address: "127.0.0.1",
					Service: &api.AgentService{
						ID:      "svc-id",
						Service: "svc",
						Tags:    []string{"k8s"},
					},
				},
			},
			Exp: nil,
		},
		"service on k8s node without any tags": {
			ConsulServices: []api.CatalogRegistration{
				{
					Node:    ConsulSyncNodeName,
					Address: "127.0.0.1",
					Service: &api.AgentService{
						ID:      "svc-id",
						Service: "svc",
						Tags:    nil,
					},
				},
			},
			Exp: nil,
		},
		"service on k8s node without k8s tag": {
			ConsulServices: []api.CatalogRegistration{
				{
					Node:    ConsulSyncNodeName,
					Address: "127.0.0.1",
					Service: &api.AgentService{
						ID:      "svc-id",
						Service: "svc",
						Tags:    []string{"not-k8s", "foo"},
					},
				},
			},
			Exp: nil,
		},
		"service on k8s node with k8s tag": {
			ConsulServices: []api.CatalogRegistration{
				{
					Node:    ConsulSyncNodeName,
					Address: "127.0.0.1",
					Service: &api.AgentService{
						ID:      "svc-id",
						Service: "svc",
						Tags:    []string{"k8s"},
					},
				},
			},
			Exp: []ConsulService{
				{
					Namespace: "",
					Name:      "svc",
				},
			},
		},
		"multiple services": {
			ConsulServices: []api.CatalogRegistration{
				{
					Node:    ConsulSyncNodeName,
					Address: "127.0.0.1",
					Service: &api.AgentService{
						ID:      "svc1-id",
						Service: "svc1",
						Tags:    []string{"k8s"},
					},
				},
				{
					Node:    ConsulSyncNodeName,
					Address: "127.0.0.1",
					Service: &api.AgentService{
						ID:      "svc2-id2",
						Service: "svc2",
						Tags:    []string{"k8s"},
					},
				},
			},
			Exp: []ConsulService{
				{
					Namespace: "",
					Name:      "svc1",
				},
				{
					Namespace: "",
					Name:      "svc2",
				},
			},
		},
		"multiple service instances": {
			ConsulServices: []api.CatalogRegistration{
				{
					Node:    ConsulSyncNodeName,
					Address: "127.0.0.1",
					Service: &api.AgentService{
						ID:      "svc-id1",
						Service: "svc",
						Tags:    []string{"k8s"},
					},
				},
				{
					Node:    ConsulSyncNodeName,
					Address: "127.0.0.1",
					Service: &api.AgentService{
						ID:      "svc-id2",
						Service: "svc",
						Tags:    []string{"k8s"},
					},
				},
			},
			Exp: []ConsulService{
				{
					Namespace: "",
					Name:      "svc",
				},
			},
		},
	}

	for name, c := range cases {
		t.Run(name, func(tt *testing.T) {
			require := require.New(tt)
			svr, err := testutil.NewTestServerConfigT(tt, nil)
			require.NoError(err)
			defer svr.Stop()

			consulClient, err := api.NewClient(&api.Config{
				Address: svr.HTTPAddr,
			})
			require.NoError(err)
			for _, registration := range c.ConsulServices {
				_, err = consulClient.Catalog().Register(&registration, nil)
				require.NoError(err)
			}

			client := PreNamespacesNodeServicesClient{
				Client: consulClient,
			}
			svcs, _, err := client.NodeServices("k8s", ConsulSyncNodeName, api.QueryOptions{})
			require.NoError(err)
			require.Len(svcs, len(c.Exp))
			for _, expSvc := range c.Exp {
				require.Contains(svcs, expSvc)
			}
		})
	}
}
