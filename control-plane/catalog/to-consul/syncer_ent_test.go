// +build enterprise

package catalog

import (
	"testing"

	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
)

// Test that the syncer registers services in Consul namespaces.
func TestConsulSyncer_ConsulNamespaces(t *testing.T) {
	t.Parallel()

	a, err := testutil.NewTestServerConfigT(t, nil)
	require.NoError(t, err)
	defer a.Stop()

	client, err := api.NewClient(&api.Config{
		Address: a.HTTPAddr,
	})
	require.NoError(t, err)

	s, closer := testConsulSyncerWithConfig(client, func(s *ConsulSyncer) {
		s.EnableNamespaces = true
		s.ConsulNodeServicesClient = &NamespacesNodeServicesClient{
			Client: client,
		}
	})
	defer closer()

	// We expect services to be created in the default and foo namespaces.
	namespaces := []string{"default", "foo"}
	var registrations []*api.CatalogRegistration
	for _, ns := range namespaces {
		registrations = append(registrations,
			// The services will be named the same as their namespaces.
			testRegistrationNS(ConsulSyncNodeName, ns, ns, ns))
	}
	s.Sync(registrations)

	retry.Run(t, func(r *retry.R) {
		for _, ns := range namespaces {
			svcInstances, _, err := client.Catalog().Service(ns, "k8s", &api.QueryOptions{
				Namespace: ns,
			})
			require.NoError(r, err)
			require.Len(r, svcInstances, 1)
			instance := svcInstances[0]
			require.Equal(r, ConsulSyncNodeName, instance.Node)
			require.Equal(r, "127.0.0.1", instance.Address)
			require.Equal(r, map[string]string{ConsulSourceKey: "k8s"}, instance.NodeMeta)
			require.Equal(r, map[string]string{
				ConsulSourceKey: "k8s",
				ConsulK8SNS:     ns,
			}, instance.ServiceMeta)
		}
	})
}

// Test the syncer reaps services that weren't registered by us
// across all Consul namespaces.
func TestConsulSyncer_ReapConsulNamespace(t *testing.T) {
	t.Parallel()

	a, err := testutil.NewTestServerConfigT(t, nil)
	require.NoError(t, err)
	defer a.Stop()

	client, err := api.NewClient(&api.Config{
		Address: a.HTTPAddr,
	})
	require.NoError(t, err)

	s, closer := testConsulSyncerWithConfig(client, func(s *ConsulSyncer) {
		s.EnableNamespaces = true
		s.ConsulNodeServicesClient = &NamespacesNodeServicesClient{
			Client: client,
		}
	})
	defer closer()

	// We expect services to be created in the default and foo namespaces.
	s.Sync([]*api.CatalogRegistration{
		testRegistrationNS(ConsulSyncNodeName, "default", "default", "default"),
		testRegistrationNS(ConsulSyncNodeName, "foo", "foo", "foo"),
	})

	// We create services we expect to be deleted in the bar and baz namespaces.
	expEmptiedNamespaces := []string{"bar", "baz"}
	for _, ns := range expEmptiedNamespaces {
		svc := testRegistrationNS(ConsulSyncNodeName, ns, ns, ns)
		_, _, err := client.Namespaces().Create(&api.Namespace{
			Name: ns,
		}, nil)
		require.NoError(t, err)
		_, err = client.Catalog().Register(svc, &api.WriteOptions{
			Namespace: ns,
		})
		require.NoError(t, err)
	}

	retry.Run(t, func(r *retry.R) {
		// Invalid services should be deleted.
		for _, ns := range expEmptiedNamespaces {
			svcs, _, err := client.Catalog().Services(&api.QueryOptions{
				Namespace: ns,
			})
			require.NoError(r, err)
			require.Len(r, svcs, 0)
		}

		// The services in the foo and default namespaces should still exist.
		for _, ns := range []string{"default", "foo"} {
			svcs, _, err := client.Catalog().Services(&api.QueryOptions{
				Namespace: ns,
			})
			require.NoError(r, err)
			// The default namespace should have the consul service registered
			// so its count should be 2.
			if ns == "default" {
				require.Len(r, svcs, 2)
			} else {
				require.Len(r, svcs, 1)
			}
		}
	})
}

// Test that the syncer reaps individual invalid service instances when
// namespaces are enabled.
func TestConsulSyncer_reapServiceInstanceNamespacesEnabled(t *testing.T) {
	t.Parallel()

	a, err := testutil.NewTestServerConfigT(t, nil)
	require.NoError(t, err)
	defer a.Stop()

	client, err := api.NewClient(&api.Config{
		Address: a.HTTPAddr,
	})
	s, closer := testConsulSyncerWithConfig(client, func(s *ConsulSyncer) {
		s.EnableNamespaces = true
		s.ConsulNodeServicesClient = &NamespacesNodeServicesClient{
			Client: client,
		}
	})
	defer closer()

	// We'll create one service in the foo namespace. It should only have one
	// instance.
	s.Sync([]*api.CatalogRegistration{
		testRegistrationNS(ConsulSyncNodeName, "foo", "foo", "foo"),
	})

	// Create an invalid instance service directly in Consul.
	_, _, err = client.Namespaces().Create(&api.Namespace{
		Name: "foo",
	}, nil)
	require.NoError(t, err)
	svc := testRegistrationNS(ConsulSyncNodeName, "foo", "foo", "foo")
	svc.Service.ID = serviceID("k8s-sync", "foo2")
	_, err = client.Catalog().Register(svc, nil)
	require.NoError(t, err)

	// Test that the invalid instance is reaped.
	retry.Run(t, func(r *retry.R) {
		services, _, err := client.Catalog().Service("foo", "", &api.QueryOptions{
			Namespace: "foo",
		})
		require.NoError(r, err)
		require.Len(r, services, 1)
		require.Equal(r, "foo", services[0].ServiceName)
	})
}

func testRegistrationNS(node, service, k8sSrcNS, consulDestNS string) *api.CatalogRegistration {
	r := testRegistration(node, service, k8sSrcNS)
	r.Service.Namespace = consulDestNS
	return r
}
