// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

//go:build enterprise

package configentries

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/go-logr/logr"
	logrtest "github.com/go-logr/logr/testing"
	capi "github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
)

const (
	kubeNS              = "default"
	partitionName       = "default"
	nonDefaultPartition = "non-default"
)

// NOTE: We're not testing each controller type here because that's mostly done in
// the OSS tests and it would result in too many permutations. Instead
// we're only testing with the ServiceDefaults and ProxyDefaults configentries which
// will exercise all the namespaces code for config entries that are namespaced and those that
// exist in the global namespace.
// We also test Enterprise only features like SamenessGroups.

func TestConfigEntryController_createsEntConfigEntry(t *testing.T) {
	t.Parallel()

	cases := []struct {
		kubeKind            string
		consulKind          string
		consulPrereqs       []capi.ConfigEntry
		configEntryResource common.ConfigEntryResource
		reconciler          func(client.Client, *consul.Config, consul.ServerConnectionManager, logr.Logger) testReconciler
		compare             func(t *testing.T, consul capi.ConfigEntry)
	}{
		{
			kubeKind:   "SamenessGroup",
			consulKind: capi.SamenessGroup,
			configEntryResource: &v1alpha1.SamenessGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: kubeNS,
				},
				Spec: v1alpha1.SamenessGroupSpec{
					DefaultForFailover: true,
					IncludeLocal:       true,
					Members: []v1alpha1.SamenessGroupMember{
						{
							Peer:      "dc1",
							Partition: "",
						},
					},
				},
			},
			reconciler: func(client client.Client, cfg *consul.Config, watcher consul.ServerConnectionManager, logger logr.Logger) testReconciler {
				return &SamenessGroupController{
					Client: client,
					Log:    logger,
					ConfigEntryController: &ConfigEntryController{
						ConsulClientConfig:  cfg,
						ConsulServerConnMgr: watcher,
						DatacenterName:      datacenterName,
						ConsulPartition:     partitionName,
					},
				}
			},
			compare: func(t *testing.T, consulEntry capi.ConfigEntry) {
				resource, ok := consulEntry.(*capi.SamenessGroupConfigEntry)
				require.True(t, ok, "cast error")
				require.Equal(t, true, resource.DefaultForFailover)
				require.Equal(t, true, resource.IncludeLocal)
				require.Equal(t, "dc1", resource.Members[0].Peer)
				require.Equal(t, "", resource.Members[0].Partition)
			},
		},
		{
			kubeKind:   "ControlPlaneRequestLimit",
			consulKind: capi.RateLimitIPConfig,
			configEntryResource: &v1alpha1.ControlPlaneRequestLimit{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: kubeNS,
				},
				Spec: v1alpha1.ControlPlaneRequestLimitSpec{
					Mode: "permissive",
					ReadWriteRatesConfig: v1alpha1.ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					ACL: &v1alpha1.ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					Catalog: &v1alpha1.ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					ConfigEntry: &v1alpha1.ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					ConnectCA: &v1alpha1.ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					Coordinate: &v1alpha1.ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					DiscoveryChain: &v1alpha1.ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					Health: &v1alpha1.ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					Intention: &v1alpha1.ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					KV: &v1alpha1.ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					Tenancy: &v1alpha1.ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					PreparedQuery: &v1alpha1.ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					Session: &v1alpha1.ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					Txn: &v1alpha1.ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
				},
			},
			reconciler: func(client client.Client, cfg *consul.Config, watcher consul.ServerConnectionManager, logger logr.Logger) testReconciler {
				return &ControlPlaneRequestLimitController{
					Client: client,
					Log:    logger,
					ConfigEntryController: &ConfigEntryController{
						ConsulClientConfig:  cfg,
						ConsulServerConnMgr: watcher,
						DatacenterName:      datacenterName,
						ConsulPartition:     partitionName,
					},
				}
			},
			compare: func(t *testing.T, consulEntry capi.ConfigEntry) {
				resource, ok := consulEntry.(*capi.RateLimitIPConfigEntry)
				require.True(t, ok, "cast error")
				require.Equal(t, "permissive", resource.Mode)
				require.Equal(t, 100.0, resource.ReadRate)
				require.Equal(t, 100.0, resource.WriteRate)
				require.Equal(t, 100.0, resource.ACL.ReadRate)
				require.Equal(t, 100.0, resource.ACL.WriteRate)
				require.Equal(t, 100.0, resource.Catalog.ReadRate)
				require.Equal(t, 100.0, resource.Catalog.WriteRate)
				require.Equal(t, 100.0, resource.ConfigEntry.ReadRate)
				require.Equal(t, 100.0, resource.ConfigEntry.WriteRate)
				require.Equal(t, 100.0, resource.ConnectCA.ReadRate)
				require.Equal(t, 100.0, resource.ConnectCA.WriteRate)
				require.Equal(t, 100.0, resource.Coordinate.ReadRate)
				require.Equal(t, 100.0, resource.Coordinate.WriteRate)
				require.Equal(t, 100.0, resource.DiscoveryChain.ReadRate)
				require.Equal(t, 100.0, resource.DiscoveryChain.WriteRate)
				require.Equal(t, 100.0, resource.Health.ReadRate)
				require.Equal(t, 100.0, resource.Health.WriteRate)
				require.Equal(t, 100.0, resource.Intention.ReadRate)
				require.Equal(t, 100.0, resource.Intention.WriteRate)
				require.Equal(t, 100.0, resource.KV.ReadRate)
				require.Equal(t, 100.0, resource.KV.WriteRate)
				require.Equal(t, 100.0, resource.Tenancy.ReadRate)
				require.Equal(t, 100.0, resource.Tenancy.WriteRate)
				require.Equal(t, 100.0, resource.PreparedQuery.ReadRate)
				require.Equal(t, 100.0, resource.PreparedQuery.WriteRate)
				require.Equal(t, 100.0, resource.Session.ReadRate)
				require.Equal(t, 100.0, resource.Session.WriteRate)
				require.Equal(t, 100.0, resource.Txn.ReadRate)
				require.Equal(t, 100.0, resource.Txn.WriteRate, 100.0)
			},
		},
	}

	for _, c := range cases {
		t.Run(c.kubeKind, func(t *testing.T) {
			req := require.New(t)
			ctx := context.Background()

			s := runtime.NewScheme()
			s.AddKnownTypes(v1alpha1.GroupVersion, c.configEntryResource)
			fakeClient := fake.NewClientBuilder().WithScheme(s).
				WithRuntimeObjects(c.configEntryResource).
				WithStatusSubresource(c.configEntryResource).
				Build()

			testClient := test.TestServerWithMockConnMgrWatcher(t, nil)
			testClient.TestServer.WaitForServiceIntentions(t)
			consulClient := testClient.APIClient

			for _, configEntry := range c.consulPrereqs {
				written, _, err := consulClient.ConfigEntries().Set(configEntry, nil)
				req.NoError(err)
				req.True(written)
			}

			r := c.reconciler(fakeClient, testClient.Cfg, testClient.Watcher, logrtest.NewTestLogger(t))
			namespacedName := types.NamespacedName{
				Namespace: kubeNS,
				Name:      c.configEntryResource.KubernetesName(),
			}
			resp, err := r.Reconcile(ctx, ctrl.Request{
				NamespacedName: namespacedName,
			})
			req.NoError(err)
			req.False(resp.Requeue)

			cfg, _, err := consulClient.ConfigEntries().Get(c.consulKind, c.configEntryResource.ConsulName(), nil)
			req.NoError(err)
			req.Equal(c.configEntryResource.ConsulName(), cfg.GetName())
			c.compare(t, cfg)

			// Check that the status is "synced".
			err = fakeClient.Get(ctx, namespacedName, c.configEntryResource)
			req.NoError(err)
			req.Equal(corev1.ConditionTrue, c.configEntryResource.SyncedConditionStatus())

			// Check that the finalizer is added.
			req.Contains(c.configEntryResource.Finalizers(), FinalizerName)
		})
	}
}

func TestConfigEntryController_updatesEntConfigEntry(t *testing.T) {
	t.Parallel()
	kubeNS := "default"

	cases := []struct {
		kubeKind            string
		consulKind          string
		consulPrereqs       []capi.ConfigEntry
		configEntryResource common.ConfigEntryResource
		reconciler          func(client.Client, *consul.Config, consul.ServerConnectionManager, logr.Logger) testReconciler
		updateF             func(common.ConfigEntryResource)
		compare             func(t *testing.T, consul capi.ConfigEntry)
	}{
		{
			kubeKind:   "SamenessGroup",
			consulKind: capi.SamenessGroup,
			configEntryResource: &v1alpha1.SamenessGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: kubeNS,
				},
				Spec: v1alpha1.SamenessGroupSpec{
					DefaultForFailover: true,
					IncludeLocal:       true,
					Members: []v1alpha1.SamenessGroupMember{
						{
							Peer:      "dc1",
							Partition: "",
						},
					},
				},
			},
			reconciler: func(client client.Client, cfg *consul.Config, watcher consul.ServerConnectionManager, logger logr.Logger) testReconciler {
				return &SamenessGroupController{
					Client: client,
					Log:    logger,
					ConfigEntryController: &ConfigEntryController{
						ConsulClientConfig:  cfg,
						ConsulServerConnMgr: watcher,
						DatacenterName:      datacenterName,
						ConsulPartition:     partitionName,
					},
				}
			},
			updateF: func(resource common.ConfigEntryResource) {
				sg := resource.(*v1alpha1.SamenessGroup)
				sg.Spec.DefaultForFailover = false
			},
			compare: func(t *testing.T, consulEntry capi.ConfigEntry) {
				resource, ok := consulEntry.(*capi.SamenessGroupConfigEntry)
				require.True(t, ok, "cast error")
				require.Equal(t, false, resource.DefaultForFailover)
				require.Equal(t, true, resource.IncludeLocal)
				require.Equal(t, "dc1", resource.Members[0].Peer)
				require.Equal(t, "", resource.Members[0].Partition)
			},
		},
		{
			kubeKind:   "ControlPlaneRequestLimit",
			consulKind: capi.RateLimitIPConfig,
			configEntryResource: &v1alpha1.ControlPlaneRequestLimit{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: kubeNS,
				},
				Spec: v1alpha1.ControlPlaneRequestLimitSpec{
					Mode: "permissive",
					ReadWriteRatesConfig: v1alpha1.ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					ACL: &v1alpha1.ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					Catalog: &v1alpha1.ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					ConfigEntry: &v1alpha1.ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					ConnectCA: &v1alpha1.ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					Coordinate: &v1alpha1.ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					DiscoveryChain: &v1alpha1.ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					Health: &v1alpha1.ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					Intention: &v1alpha1.ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					KV: &v1alpha1.ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					Tenancy: &v1alpha1.ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					PreparedQuery: &v1alpha1.ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					Session: &v1alpha1.ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					Txn: &v1alpha1.ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
				},
			},
			reconciler: func(client client.Client, cfg *consul.Config, watcher consul.ServerConnectionManager, logger logr.Logger) testReconciler {
				return &ControlPlaneRequestLimitController{
					Client: client,
					Log:    logger,
					ConfigEntryController: &ConfigEntryController{
						ConsulClientConfig:  cfg,
						ConsulServerConnMgr: watcher,
						DatacenterName:      datacenterName,
						ConsulPartition:     partitionName,
					},
				}
			},
			updateF: func(resource common.ConfigEntryResource) {
				ipRateLimit := resource.(*v1alpha1.ControlPlaneRequestLimit)
				ipRateLimit.Spec.Mode = "enforcing"
			},
			compare: func(t *testing.T, consulEntry capi.ConfigEntry) {
				resource, ok := consulEntry.(*capi.RateLimitIPConfigEntry)
				require.True(t, ok, "cast error")
				require.Equal(t, "enforcing", resource.Mode)
				require.Equal(t, 100.0, resource.ReadRate)
				require.Equal(t, 100.0, resource.WriteRate)
				require.Equal(t, 100.0, resource.ACL.ReadRate)
				require.Equal(t, 100.0, resource.ACL.WriteRate)
				require.Equal(t, 100.0, resource.Catalog.ReadRate)
				require.Equal(t, 100.0, resource.Catalog.WriteRate)
				require.Equal(t, 100.0, resource.ConfigEntry.ReadRate)
				require.Equal(t, 100.0, resource.ConfigEntry.WriteRate)
				require.Equal(t, 100.0, resource.ConnectCA.ReadRate)
				require.Equal(t, 100.0, resource.ConnectCA.WriteRate)
				require.Equal(t, 100.0, resource.Coordinate.ReadRate)
				require.Equal(t, 100.0, resource.Coordinate.WriteRate)
				require.Equal(t, 100.0, resource.DiscoveryChain.ReadRate)
				require.Equal(t, 100.0, resource.DiscoveryChain.WriteRate)
				require.Equal(t, 100.0, resource.Health.ReadRate)
				require.Equal(t, 100.0, resource.Health.WriteRate)
				require.Equal(t, 100.0, resource.Intention.ReadRate)
				require.Equal(t, 100.0, resource.Intention.WriteRate)
				require.Equal(t, 100.0, resource.KV.ReadRate)
				require.Equal(t, 100.0, resource.KV.WriteRate)
				require.Equal(t, 100.0, resource.Tenancy.ReadRate)
				require.Equal(t, 100.0, resource.Tenancy.WriteRate)
				require.Equal(t, 100.0, resource.PreparedQuery.ReadRate)
				require.Equal(t, 100.0, resource.PreparedQuery.WriteRate)
				require.Equal(t, 100.0, resource.Session.ReadRate)
				require.Equal(t, 100.0, resource.Session.WriteRate)
				require.Equal(t, 100.0, resource.Txn.ReadRate)
				require.Equal(t, 100.0, resource.Txn.WriteRate)
			},
		},
	}

	for _, c := range cases {
		t.Run(c.kubeKind, func(t *testing.T) {
			req := require.New(t)
			ctx := context.Background()

			s := runtime.NewScheme()
			s.AddKnownTypes(v1alpha1.GroupVersion, c.configEntryResource)
			fakeClient := fake.NewClientBuilder().WithScheme(s).
				WithRuntimeObjects(c.configEntryResource).
				WithStatusSubresource(c.configEntryResource).
				Build()

			testClient := test.TestServerWithMockConnMgrWatcher(t, nil)
			testClient.TestServer.WaitForServiceIntentions(t)
			consulClient := testClient.APIClient

			// Create any prereqs.
			for _, configEntry := range c.consulPrereqs {
				written, _, err := consulClient.ConfigEntries().Set(configEntry, nil)
				req.NoError(err)
				req.True(written)
			}

			// We haven't run reconcile yet so we must create the config entry
			// in Consul ourselves.
			{
				written, _, err := consulClient.ConfigEntries().Set(c.configEntryResource.ToConsul(datacenterName), nil)
				req.NoError(err)
				req.True(written)
			}

			// Now run reconcile which should update the entry in Consul.
			{
				namespacedName := types.NamespacedName{
					Namespace: kubeNS,
					Name:      c.configEntryResource.KubernetesName(),
				}
				// First get it so we have the latest revision number.
				err := fakeClient.Get(ctx, namespacedName, c.configEntryResource)
				req.NoError(err)

				// Update the entry in Kube and run reconcile.
				c.updateF(c.configEntryResource)
				err = fakeClient.Update(ctx, c.configEntryResource)
				req.NoError(err)
				r := c.reconciler(fakeClient, testClient.Cfg, testClient.Watcher, logrtest.NewTestLogger(t))
				resp, err := r.Reconcile(ctx, ctrl.Request{
					NamespacedName: namespacedName,
				})
				req.NoError(err)
				req.False(resp.Requeue)

				// Now check that the object in Consul is as expected.
				cfg, _, err := consulClient.ConfigEntries().Get(c.consulKind, c.configEntryResource.ConsulName(), nil)
				req.NoError(err)
				req.Equal(c.configEntryResource.ConsulName(), cfg.GetName())
				c.compare(t, cfg)
			}
		})
	}
}

func TestConfigEntryController_deletesEntConfigEntry(t *testing.T) {
	t.Parallel()
	kubeNS := "default"

	cases := []struct {
		kubeKind                        string
		consulKind                      string
		consulPrereq                    []capi.ConfigEntry
		configEntryResourceWithDeletion common.ConfigEntryResource
		reconciler                      func(client.Client, *consul.Config, consul.ServerConnectionManager, logr.Logger) testReconciler
	}{
		{
			kubeKind:   "SamenessGroup",
			consulKind: capi.SamenessGroup,
			configEntryResourceWithDeletion: &v1alpha1.SamenessGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "foo",
					Namespace:         kubeNS,
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
					Finalizers:        []string{FinalizerName},
				},
				Spec: v1alpha1.SamenessGroupSpec{
					DefaultForFailover: true,
					IncludeLocal:       true,
					Members: []v1alpha1.SamenessGroupMember{
						{
							Peer:      "dc1",
							Partition: "",
						},
					},
				},
			},
			reconciler: func(client client.Client, cfg *consul.Config, watcher consul.ServerConnectionManager, logger logr.Logger) testReconciler {
				return &SamenessGroupController{
					Client: client,
					Log:    logger,
					ConfigEntryController: &ConfigEntryController{
						ConsulClientConfig:  cfg,
						ConsulServerConnMgr: watcher,
						DatacenterName:      datacenterName,
						ConsulPartition:     partitionName,
					},
				}
			},
		},
		{

			kubeKind:   "ControlPlaneRequestLimit",
			consulKind: capi.RateLimitIPConfig,
			configEntryResourceWithDeletion: &v1alpha1.ControlPlaneRequestLimit{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "foo",
					Namespace:         kubeNS,
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
					Finalizers:        []string{FinalizerName},
				},
				Spec: v1alpha1.ControlPlaneRequestLimitSpec{
					Mode: "permissive",
					ReadWriteRatesConfig: v1alpha1.ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					ACL: &v1alpha1.ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					Catalog: &v1alpha1.ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					ConfigEntry: &v1alpha1.ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					ConnectCA: &v1alpha1.ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					Coordinate: &v1alpha1.ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					DiscoveryChain: &v1alpha1.ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					Health: &v1alpha1.ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					Intention: &v1alpha1.ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					KV: &v1alpha1.ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					Tenancy: &v1alpha1.ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					PreparedQuery: &v1alpha1.ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					Session: &v1alpha1.ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
					Txn: &v1alpha1.ReadWriteRatesConfig{
						ReadRate:  100.0,
						WriteRate: 100.0,
					},
				},
			},
			reconciler: func(client client.Client, cfg *consul.Config, watcher consul.ServerConnectionManager, logger logr.Logger) testReconciler {
				return &ControlPlaneRequestLimitController{
					Client: client,
					Log:    logger,
					ConfigEntryController: &ConfigEntryController{
						ConsulClientConfig:  cfg,
						ConsulServerConnMgr: watcher,
						DatacenterName:      datacenterName,
						ConsulPartition:     partitionName,
					},
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.kubeKind, func(t *testing.T) {
			req := require.New(t)

			s := runtime.NewScheme()
			s.AddKnownTypes(v1alpha1.GroupVersion, c.configEntryResourceWithDeletion)
			fakeClient := fake.NewClientBuilder().WithScheme(s).
				WithRuntimeObjects(c.configEntryResourceWithDeletion).
				WithStatusSubresource(c.configEntryResourceWithDeletion).
				Build()

			testClient := test.TestServerWithMockConnMgrWatcher(t, nil)
			testClient.TestServer.WaitForServiceIntentions(t)
			consulClient := testClient.APIClient

			// Create any prereqs.
			for _, configEntry := range c.consulPrereq {
				written, _, err := consulClient.ConfigEntries().Set(configEntry, nil)
				req.NoError(err)
				req.True(written)
			}

			// We haven't run reconcile yet so we must create the config entry
			// in Consul ourselves.
			{
				written, _, err := consulClient.ConfigEntries().Set(c.configEntryResourceWithDeletion.ToConsul(datacenterName), nil)
				req.NoError(err)
				req.True(written)
			}

			// Now run reconcile. It's marked for deletion so this should delete it.
			{
				namespacedName := types.NamespacedName{
					Namespace: kubeNS,
					Name:      c.configEntryResourceWithDeletion.KubernetesName(),
				}
				r := c.reconciler(fakeClient, testClient.Cfg, testClient.Watcher, logrtest.NewTestLogger(t))
				resp, err := r.Reconcile(context.Background(), ctrl.Request{
					NamespacedName: namespacedName,
				})
				req.NoError(err)
				req.False(resp.Requeue)

				_, _, err = consulClient.ConfigEntries().Get(c.consulKind, c.configEntryResourceWithDeletion.ConsulName(), nil)
				req.EqualError(err,
					fmt.Sprintf("Unexpected response code: 404 (Config entry not found for %q / %q)",
						c.consulKind, c.configEntryResourceWithDeletion.ConsulName()))
			}
		})
	}
}

func TestConfigEntryController_createsConfigEntry_consulNamespaces(tt *testing.T) {
	tt.Parallel()

	cases := map[string]struct {
		Mirror       bool
		MirrorPrefix string
		SourceKubeNS string
		DestConsulNS string
		ExpConsulNS  string
	}{
		"SourceKubeNS=default, DestConsulNS=default": {
			SourceKubeNS: "default",
			DestConsulNS: "default",
			ExpConsulNS:  "default",
		},
		"SourceKubeNS=kube, DestConsulNS=default": {
			SourceKubeNS: "kube",
			DestConsulNS: "default",
			ExpConsulNS:  "default",
		},
		"SourceKubeNS=default, DestConsulNS=other": {
			SourceKubeNS: "default",
			DestConsulNS: "other",
			ExpConsulNS:  "other",
		},
		"SourceKubeNS=kube, DestConsulNS=other": {
			SourceKubeNS: "kube",
			DestConsulNS: "other",
			ExpConsulNS:  "other",
		},
		"SourceKubeNS=default, Mirror=true": {
			SourceKubeNS: "default",
			Mirror:       true,
			ExpConsulNS:  "default",
		},
		"SourceKubeNS=kube, Mirror=true": {
			SourceKubeNS: "kube",
			Mirror:       true,
			ExpConsulNS:  "kube",
		},
		"SourceKubeNS=default, Mirror=true, Prefix=prefix": {
			SourceKubeNS: "default",
			Mirror:       true,
			MirrorPrefix: "prefix-",
			ExpConsulNS:  "prefix-default",
		},
	}

	for name, c := range cases {
		configEntryKinds := map[string]struct {
			ConsulKind        string
			ConsulNamespace   string
			KubeResource      common.ConfigEntryResource
			GetController     func(client client.Client, logger logr.Logger, scheme *runtime.Scheme, cont *ConfigEntryController) reconcile.Reconciler
			AssertValidConfig func(entry capi.ConfigEntry) bool
		}{
			"namespaced": {
				ConsulKind: capi.ServiceDefaults,
				KubeResource: &v1alpha1.ServiceDefaults{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "foo",
						Namespace: c.SourceKubeNS,
					},
					Spec: v1alpha1.ServiceDefaultsSpec{
						Protocol: "http",
					},
				},
				GetController: func(client client.Client, logger logr.Logger, scheme *runtime.Scheme, cont *ConfigEntryController) reconcile.Reconciler {
					return &ServiceDefaultsController{
						Client:                client,
						Log:                   logger,
						Scheme:                scheme,
						ConfigEntryController: cont,
					}
				},
				AssertValidConfig: func(cfg capi.ConfigEntry) bool {
					configEntry, ok := cfg.(*capi.ServiceConfigEntry)
					if !ok {
						return false
					}
					return configEntry.Protocol == "http"
				},
				ConsulNamespace: c.ExpConsulNS,
			},
			"global": {
				ConsulKind: capi.ProxyDefaults,
				KubeResource: &v1alpha1.ProxyDefaults{
					ObjectMeta: metav1.ObjectMeta{
						Name:      common.Global,
						Namespace: c.SourceKubeNS,
					},
					Spec: v1alpha1.ProxyDefaultsSpec{
						MeshGateway: v1alpha1.MeshGateway{
							Mode: "remote",
						},
					},
				},
				GetController: func(client client.Client, logger logr.Logger, scheme *runtime.Scheme, cont *ConfigEntryController) reconcile.Reconciler {
					return &ProxyDefaultsController{
						Client:                client,
						Log:                   logger,
						Scheme:                scheme,
						ConfigEntryController: cont,
					}
				},
				AssertValidConfig: func(cfg capi.ConfigEntry) bool {
					configEntry, ok := cfg.(*capi.ProxyConfigEntry)
					if !ok {
						return false
					}
					return configEntry.MeshGateway.Mode == capi.MeshGatewayModeRemote
				},
				ConsulNamespace: common.DefaultConsulNamespace,
			},
			"intentions": {
				ConsulKind: capi.ServiceIntentions,
				KubeResource: &v1alpha1.ServiceIntentions{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "foo",
						Namespace: c.SourceKubeNS,
					},
					Spec: v1alpha1.ServiceIntentionsSpec{
						Destination: v1alpha1.IntentionDestination{
							Name:      "test",
							Namespace: c.ExpConsulNS,
						},
						Sources: v1alpha1.SourceIntentions{
							&v1alpha1.SourceIntention{
								Name:      "baz",
								Namespace: "bar",
								Action:    "allow",
							},
						},
					},
				},
				GetController: func(client client.Client, logger logr.Logger, scheme *runtime.Scheme, cont *ConfigEntryController) reconcile.Reconciler {
					return &ServiceIntentionsController{
						Client:                client,
						Log:                   logger,
						Scheme:                scheme,
						ConfigEntryController: cont,
					}
				},
				AssertValidConfig: func(cfg capi.ConfigEntry) bool {
					configEntry, ok := cfg.(*capi.ServiceIntentionsConfigEntry)
					if !ok {
						return false
					}
					return configEntry.Sources[0].Action == capi.IntentionActionAllow
				},
				ConsulNamespace: c.ExpConsulNS,
			},
		}

		for kind, in := range configEntryKinds {
			tt.Run(fmt.Sprintf("%s : %s", name, kind), func(t *testing.T) {
				req := require.New(t)
				s := runtime.NewScheme()
				s.AddKnownTypes(v1alpha1.GroupVersion, in.KubeResource)
				ctx := context.Background()

				testClient := test.TestServerWithMockConnMgrWatcher(t, nil)
				testClient.TestServer.WaitForServiceIntentions(t)
				consulClient := testClient.APIClient

				fakeClient := fake.NewClientBuilder().WithScheme(s).
					WithRuntimeObjects(in.KubeResource).
					WithStatusSubresource(in.KubeResource).
					Build()

				r := in.GetController(
					fakeClient,
					logrtest.NewTestLogger(t),
					s,
					&ConfigEntryController{
						ConsulClientConfig:         testClient.Cfg,
						ConsulServerConnMgr:        testClient.Watcher,
						EnableConsulNamespaces:     true,
						EnableNSMirroring:          c.Mirror,
						NSMirroringPrefix:          c.MirrorPrefix,
						ConsulDestinationNamespace: c.DestConsulNS,
					},
				)

				resp, err := r.Reconcile(ctx, ctrl.Request{
					NamespacedName: types.NamespacedName{
						Namespace: c.SourceKubeNS,
						Name:      in.KubeResource.KubernetesName(),
					},
				})
				req.NoError(err)
				req.False(resp.Requeue)

				cfg, _, err := consulClient.ConfigEntries().Get(in.ConsulKind, in.KubeResource.ConsulName(), &capi.QueryOptions{
					Namespace: in.ConsulNamespace,
				})
				req.NoError(err)

				result := in.AssertValidConfig(cfg)
				req.True(result)

				// Check that the status is "synced".
				err = fakeClient.Get(ctx, types.NamespacedName{
					Namespace: c.SourceKubeNS,
					Name:      in.KubeResource.KubernetesName(),
				}, in.KubeResource)
				req.NoError(err)
				conditionSynced := in.KubeResource.SyncedConditionStatus()
				req.Equal(conditionSynced, corev1.ConditionTrue)
			})
		}
	}
}

func TestConfigEntryController_updatesConfigEntry_consulNamespaces(tt *testing.T) {
	tt.Parallel()

	cases := map[string]struct {
		Mirror       bool
		MirrorPrefix string
		SourceKubeNS string
		DestConsulNS string
		ExpConsulNS  string
	}{
		"SourceKubeNS=default, DestConsulNS=default": {
			SourceKubeNS: "default",
			DestConsulNS: "default",
			ExpConsulNS:  "default",
		},
		"SourceKubeNS=kube, DestConsulNS=default": {
			SourceKubeNS: "kube",
			DestConsulNS: "default",
			ExpConsulNS:  "default",
		},
		"SourceKubeNS=default, DestConsulNS=other": {
			SourceKubeNS: "default",
			DestConsulNS: "other",
			ExpConsulNS:  "other",
		},
		"SourceKubeNS=kube, DestConsulNS=other": {
			SourceKubeNS: "kube",
			DestConsulNS: "other",
			ExpConsulNS:  "other",
		},
		"SourceKubeNS=default, Mirror=true": {
			SourceKubeNS: "default",
			Mirror:       true,
			ExpConsulNS:  "default",
		},
		"SourceKubeNS=kube, Mirror=true": {
			SourceKubeNS: "kube",
			Mirror:       true,
			ExpConsulNS:  "kube",
		},
		"SourceKubeNS=default, Mirror=true, Prefix=prefix": {
			SourceKubeNS: "default",
			Mirror:       true,
			MirrorPrefix: "prefix-",
			ExpConsulNS:  "prefix-default",
		},
	}

	for name, c := range cases {
		configEntryKinds := map[string]struct {
			ConsulKind            string
			ConsulNamespace       string
			KubeResource          common.ConfigEntryResource
			GetControllerFunc     func(client client.Client, logger logr.Logger, scheme *runtime.Scheme, cont *ConfigEntryController) reconcile.Reconciler
			AssertValidConfigFunc func(entry capi.ConfigEntry) bool
			WriteConfigEntryFunc  func(consulClient *capi.Client, namespace string) error
			UpdateResourceFunc    func(client client.Client, ctx context.Context, in common.ConfigEntryResource) error
		}{
			"namespaced": {
				ConsulKind: capi.ServiceDefaults,
				KubeResource: &v1alpha1.ServiceDefaults{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "foo",
						Namespace:  c.SourceKubeNS,
						Finalizers: []string{FinalizerName},
					},
					Spec: v1alpha1.ServiceDefaultsSpec{
						Protocol: "http",
					},
				},
				ConsulNamespace: c.ExpConsulNS,
				GetControllerFunc: func(client client.Client, logger logr.Logger, scheme *runtime.Scheme, cont *ConfigEntryController) reconcile.Reconciler {
					return &ServiceDefaultsController{
						Client:                client,
						Log:                   logger,
						Scheme:                scheme,
						ConfigEntryController: cont,
					}
				},
				WriteConfigEntryFunc: func(consulClient *capi.Client, namespace string) error {
					_, _, err := consulClient.ConfigEntries().Set(&capi.ServiceConfigEntry{
						Kind:     capi.ServiceDefaults,
						Name:     "foo",
						Protocol: "http",
					}, &capi.WriteOptions{Namespace: namespace})
					return err
				},
				UpdateResourceFunc: func(client client.Client, ctx context.Context, in common.ConfigEntryResource) error {
					svcDefault := in.(*v1alpha1.ServiceDefaults)
					svcDefault.Spec.Protocol = "tcp"
					return client.Update(ctx, svcDefault)
				},
				AssertValidConfigFunc: func(cfg capi.ConfigEntry) bool {
					configEntry, ok := cfg.(*capi.ServiceConfigEntry)
					if !ok {
						return false
					}
					return configEntry.Protocol == "tcp"
				},
			},
			"global": {
				ConsulKind: capi.ProxyDefaults,
				KubeResource: &v1alpha1.ProxyDefaults{
					ObjectMeta: metav1.ObjectMeta{
						Name:       common.Global,
						Namespace:  c.SourceKubeNS,
						Finalizers: []string{FinalizerName},
					},
					Spec: v1alpha1.ProxyDefaultsSpec{
						MeshGateway: v1alpha1.MeshGateway{
							Mode: "remote",
						},
					},
				},
				ConsulNamespace: common.DefaultConsulNamespace,
				GetControllerFunc: func(client client.Client, logger logr.Logger, scheme *runtime.Scheme, cont *ConfigEntryController) reconcile.Reconciler {
					return &ProxyDefaultsController{
						Client:                client,
						Log:                   logger,
						Scheme:                scheme,
						ConfigEntryController: cont,
					}
				},
				WriteConfigEntryFunc: func(consulClient *capi.Client, namespace string) error {
					_, _, err := consulClient.ConfigEntries().Set(&capi.ProxyConfigEntry{
						Kind: capi.ProxyDefaults,
						Name: common.Global,
						MeshGateway: capi.MeshGatewayConfig{
							Mode: capi.MeshGatewayModeRemote,
						},
					}, &capi.WriteOptions{Namespace: namespace})
					return err
				},
				UpdateResourceFunc: func(client client.Client, ctx context.Context, in common.ConfigEntryResource) error {
					proxyDefaults := in.(*v1alpha1.ProxyDefaults)
					proxyDefaults.Spec.MeshGateway.Mode = "local"
					return client.Update(ctx, proxyDefaults)
				},
				AssertValidConfigFunc: func(cfg capi.ConfigEntry) bool {
					configEntry, ok := cfg.(*capi.ProxyConfigEntry)
					if !ok {
						return false
					}
					return configEntry.MeshGateway.Mode == capi.MeshGatewayModeLocal
				},
			},
			"intentions": {
				ConsulKind: capi.ServiceIntentions,
				KubeResource: &v1alpha1.ServiceIntentions{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "test",
						Namespace:  c.SourceKubeNS,
						Finalizers: []string{FinalizerName},
					},
					Spec: v1alpha1.ServiceIntentionsSpec{
						Destination: v1alpha1.IntentionDestination{
							Name:      "foo",
							Namespace: c.ExpConsulNS,
						},
						Sources: v1alpha1.SourceIntentions{
							&v1alpha1.SourceIntention{
								Name:      "bar",
								Namespace: "baz",
								Action:    "deny",
							},
						},
					},
				},
				ConsulNamespace: c.ExpConsulNS,
				GetControllerFunc: func(client client.Client, logger logr.Logger, scheme *runtime.Scheme, cont *ConfigEntryController) reconcile.Reconciler {
					return &ServiceIntentionsController{
						Client:                client,
						Log:                   logger,
						Scheme:                scheme,
						ConfigEntryController: cont,
					}
				},
				WriteConfigEntryFunc: func(consulClient *capi.Client, namespace string) error {
					_, _, err := consulClient.ConfigEntries().Set(&capi.ServiceIntentionsConfigEntry{
						Kind: capi.ServiceIntentions,
						Name: "foo",
						Sources: []*capi.SourceIntention{
							{
								Name:      "bar",
								Namespace: "baz",
								Action:    capi.IntentionActionDeny,
							},
						},
					}, &capi.WriteOptions{Namespace: namespace})
					return err
				},
				UpdateResourceFunc: func(client client.Client, ctx context.Context, in common.ConfigEntryResource) error {
					svcIntention := in.(*v1alpha1.ServiceIntentions)
					svcIntention.Spec.Sources[0].Action = "allow"
					return client.Update(ctx, svcIntention)
				},
				AssertValidConfigFunc: func(cfg capi.ConfigEntry) bool {
					configEntry, ok := cfg.(*capi.ServiceIntentionsConfigEntry)
					if !ok {
						return false
					}
					return configEntry.Sources[0].Action == capi.IntentionActionAllow
				},
			},
		}
		for kind, in := range configEntryKinds {
			tt.Run(fmt.Sprintf("%s : %s", name, kind), func(t *testing.T) {
				req := require.New(t)
				s := runtime.NewScheme()
				s.AddKnownTypes(v1alpha1.GroupVersion, in.KubeResource)
				ctx := context.Background()

				testClient := test.TestServerWithMockConnMgrWatcher(t, nil)
				testClient.TestServer.WaitForServiceIntentions(t)
				consulClient := testClient.APIClient

				fakeClient := fake.NewClientBuilder().WithScheme(s).
					WithRuntimeObjects(in.KubeResource).
					WithStatusSubresource(in.KubeResource).
					Build()

				r := in.GetControllerFunc(
					fakeClient,
					logrtest.NewTestLogger(t),
					s,
					&ConfigEntryController{
						ConsulClientConfig:         testClient.Cfg,
						ConsulServerConnMgr:        testClient.Watcher,
						EnableConsulNamespaces:     true,
						EnableNSMirroring:          c.Mirror,
						NSMirroringPrefix:          c.MirrorPrefix,
						ConsulDestinationNamespace: c.DestConsulNS,
					},
				)

				// We haven't run reconcile yet so ensure it's created in Consul.
				{
					if in.ConsulNamespace != "default" {
						_, _, err := consulClient.Namespaces().Create(&capi.Namespace{
							Name: in.ConsulNamespace,
						}, nil)
						req.NoError(err)
					}

					err := in.WriteConfigEntryFunc(consulClient, in.ConsulNamespace)
					req.NoError(err)
				}

				// Now update it.
				{
					// First get it so we have the latest revision number.
					err := fakeClient.Get(ctx, types.NamespacedName{
						Namespace: c.SourceKubeNS,
						Name:      in.KubeResource.KubernetesName(),
					}, in.KubeResource)
					req.NoError(err)

					// Update the resource.
					err = in.UpdateResourceFunc(fakeClient, ctx, in.KubeResource)
					req.NoError(err)

					resp, err := r.Reconcile(ctx, ctrl.Request{
						NamespacedName: types.NamespacedName{
							Namespace: c.SourceKubeNS,
							Name:      in.KubeResource.KubernetesName(),
						},
					})
					req.NoError(err)
					req.False(resp.Requeue)

					cfg, _, err := consulClient.ConfigEntries().Get(in.ConsulKind, in.KubeResource.ConsulName(), &capi.QueryOptions{
						Namespace: in.ConsulNamespace,
					})
					req.NoError(err)
					req.True(in.AssertValidConfigFunc(cfg))
				}
			})
		}
	}
}

func TestConfigEntryController_deletesConfigEntry_consulNamespaces(tt *testing.T) {
	tt.Parallel()

	cases := map[string]struct {
		Mirror       bool
		MirrorPrefix string
		SourceKubeNS string
		DestConsulNS string
		ExpConsulNS  string
	}{
		"SourceKubeNS=default, DestConsulNS=default": {
			SourceKubeNS: "default",
			DestConsulNS: "default",
			ExpConsulNS:  "default",
		},
		"SourceKubeNS=kube, DestConsulNS=default": {
			SourceKubeNS: "kube",
			DestConsulNS: "default",
			ExpConsulNS:  "default",
		},
		"SourceKubeNS=default, DestConsulNS=other": {
			SourceKubeNS: "default",
			DestConsulNS: "other",
			ExpConsulNS:  "other",
		},
		"SourceKubeNS=kube, DestConsulNS=other": {
			SourceKubeNS: "kube",
			DestConsulNS: "other",
			ExpConsulNS:  "other",
		},
		"SourceKubeNS=default, Mirror=true": {
			SourceKubeNS: "default",
			Mirror:       true,
			ExpConsulNS:  "default",
		},
		"SourceKubeNS=kube, Mirror=true": {
			SourceKubeNS: "kube",
			Mirror:       true,
			ExpConsulNS:  "kube",
		},
		"SourceKubeNS=default, Mirror=true, Prefix=prefix": {
			SourceKubeNS: "default",
			Mirror:       true,
			MirrorPrefix: "prefix-",
			ExpConsulNS:  "prefix-default",
		},
	}

	for name, c := range cases {
		configEntryKinds := map[string]struct {
			ConsulKind           string
			ConsulNamespace      string
			KubeResource         common.ConfigEntryResource
			GetControllerFunc    func(client client.Client, logger logr.Logger, scheme *runtime.Scheme, cont *ConfigEntryController) reconcile.Reconciler
			WriteConfigEntryFunc func(consulClient *capi.Client, namespace string) error
		}{
			"namespaced": {
				ConsulKind: capi.ServiceDefaults,
				// Create it with the deletion timestamp set to mimic that it's already
				// been marked for deletion.
				KubeResource: &v1alpha1.ServiceDefaults{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "foo",
						Namespace:         c.SourceKubeNS,
						Finalizers:        []string{FinalizerName},
						DeletionTimestamp: &metav1.Time{Time: time.Now()},
					},
					Spec: v1alpha1.ServiceDefaultsSpec{
						Protocol: "http",
					},
				},
				ConsulNamespace: c.ExpConsulNS,
				GetControllerFunc: func(client client.Client, logger logr.Logger, scheme *runtime.Scheme, cont *ConfigEntryController) reconcile.Reconciler {
					return &ServiceDefaultsController{
						Client:                client,
						Log:                   logger,
						Scheme:                scheme,
						ConfigEntryController: cont,
					}
				},
				WriteConfigEntryFunc: func(consulClient *capi.Client, namespace string) error {
					_, _, err := consulClient.ConfigEntries().Set(&capi.ServiceConfigEntry{
						Kind:     capi.ServiceDefaults,
						Name:     "foo",
						Protocol: "http",
					}, &capi.WriteOptions{Namespace: namespace})
					return err
				},
			},
			"global": {
				ConsulKind: capi.ProxyDefaults,
				// Create it with the deletion timestamp set to mimic that it's already
				// been marked for deletion.
				KubeResource: &v1alpha1.ProxyDefaults{
					ObjectMeta: metav1.ObjectMeta{
						Name:              common.Global,
						Namespace:         c.SourceKubeNS,
						Finalizers:        []string{FinalizerName},
						DeletionTimestamp: &metav1.Time{Time: time.Now()},
					},
					Spec: v1alpha1.ProxyDefaultsSpec{
						MeshGateway: v1alpha1.MeshGateway{
							Mode: "remote",
						},
					},
				},
				ConsulNamespace: common.DefaultConsulNamespace,
				GetControllerFunc: func(client client.Client, logger logr.Logger, scheme *runtime.Scheme, cont *ConfigEntryController) reconcile.Reconciler {
					return &ProxyDefaultsController{
						Client:                client,
						Log:                   logger,
						Scheme:                scheme,
						ConfigEntryController: cont,
					}
				},
				WriteConfigEntryFunc: func(consulClient *capi.Client, namespace string) error {
					_, _, err := consulClient.ConfigEntries().Set(&capi.ProxyConfigEntry{
						Kind: capi.ProxyDefaults,
						Name: common.Global,
						MeshGateway: capi.MeshGatewayConfig{
							Mode: capi.MeshGatewayModeRemote,
						},
					}, &capi.WriteOptions{Namespace: namespace})
					return err
				},
			},
			"intentions": {
				ConsulKind: capi.ServiceIntentions,
				// Create it with the deletion timestamp set to mimic that it's already
				// been marked for deletion.
				KubeResource: &v1alpha1.ServiceIntentions{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "foo",
						Namespace:         c.SourceKubeNS,
						Finalizers:        []string{FinalizerName},
						DeletionTimestamp: &metav1.Time{Time: time.Now()},
					},
					Spec: v1alpha1.ServiceIntentionsSpec{
						Destination: v1alpha1.IntentionDestination{
							Name:      "test",
							Namespace: c.ExpConsulNS,
						},
						Sources: v1alpha1.SourceIntentions{
							&v1alpha1.SourceIntention{
								Name:      "bar",
								Namespace: "baz",
								Action:    "deny",
							},
						},
					},
				},
				ConsulNamespace: c.ExpConsulNS,
				GetControllerFunc: func(client client.Client, logger logr.Logger, scheme *runtime.Scheme, cont *ConfigEntryController) reconcile.Reconciler {
					return &ServiceIntentionsController{
						Client:                client,
						Log:                   logger,
						Scheme:                scheme,
						ConfigEntryController: cont,
					}
				},
				WriteConfigEntryFunc: func(consulClient *capi.Client, namespace string) error {
					_, _, err := consulClient.ConfigEntries().Set(&capi.ServiceIntentionsConfigEntry{
						Kind: capi.ServiceIntentions,
						Name: "test",
						Sources: []*capi.SourceIntention{
							{
								Name:      "bar",
								Namespace: "baz",
								Action:    capi.IntentionActionDeny,
							},
						},
					}, &capi.WriteOptions{Namespace: namespace})
					return err
				},
			},
		}
		for kind, in := range configEntryKinds {
			tt.Run(fmt.Sprintf("%s : %s", name, kind), func(t *testing.T) {
				req := require.New(t)

				s := runtime.NewScheme()
				s.AddKnownTypes(v1alpha1.GroupVersion, in.KubeResource)

				testClient := test.TestServerWithMockConnMgrWatcher(t, nil)
				testClient.TestServer.WaitForServiceIntentions(t)
				consulClient := testClient.APIClient

				fakeClient := fake.NewClientBuilder().WithScheme(s).
					WithRuntimeObjects(in.KubeResource).
					WithStatusSubresource(in.KubeResource).
					Build()

				r := in.GetControllerFunc(
					fakeClient,
					logrtest.NewTestLogger(t),
					s,
					&ConfigEntryController{
						ConsulClientConfig:         testClient.Cfg,
						ConsulServerConnMgr:        testClient.Watcher,
						EnableConsulNamespaces:     true,
						EnableNSMirroring:          c.Mirror,
						NSMirroringPrefix:          c.MirrorPrefix,
						ConsulDestinationNamespace: c.DestConsulNS,
					},
				)

				// We haven't run reconcile yet so ensure it's created in Consul.
				{
					if in.ConsulNamespace != "default" {
						_, _, err := consulClient.Namespaces().Create(&capi.Namespace{
							Name: in.ConsulNamespace,
						}, nil)
						req.NoError(err)
					}

					err := in.WriteConfigEntryFunc(consulClient, in.ConsulNamespace)
					req.NoError(err)
				}

				// Now run reconcile. It's marked for deletion so this should delete it.
				{
					resp, err := r.Reconcile(context.Background(), ctrl.Request{
						NamespacedName: types.NamespacedName{
							Namespace: c.SourceKubeNS,
							Name:      in.KubeResource.KubernetesName(),
						},
					})
					req.NoError(err)
					req.False(resp.Requeue)

					_, _, err = consulClient.ConfigEntries().Get(in.ConsulKind, in.KubeResource.ConsulName(), &capi.QueryOptions{
						Namespace: in.ConsulNamespace,
					})
					req.EqualError(err, fmt.Sprintf(`Unexpected response code: 404 (Config entry not found for "%s" / "%s")`, in.ConsulKind, in.KubeResource.ConsulName()))
				}
			})
		}
	}
}

func TestConfigEntryController_createsConfigEntry_consulPartitions(t *testing.T) {
	t.Parallel()

	svcDefaults := &v1alpha1.ServiceDefaults{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "foo",
			Namespace:  kubeNS,
			Finalizers: []string{FinalizerName},
		},
		Spec: v1alpha1.ServiceDefaultsSpec{Protocol: "http"},
	}

	run := func(t *testing.T, obj common.ConfigEntryResource) {
		t.Helper()
		req := require.New(t)

		scheme := runtime.NewScheme()
		scheme.AddKnownTypes(v1alpha1.GroupVersion, obj)

		fclient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithRuntimeObjects(obj).
			WithStatusSubresource(obj).
			Build()

		testClient := test.TestServerWithMockConnMgrWatcher(t, nil)
		consulClient := testClient.APIClient
		consulClient.Partitions().Create(
			context.Background(),
			&capi.Partition{Name: nonDefaultPartition},
			nil,
		)

		ceCtrl := &ConfigEntryController{
			ConsulClientConfig:          testClient.Cfg,
			ConsulServerConnMgr:         testClient.Watcher,
			DatacenterName:              datacenterName,
			EnableConsulAdminPartitions: true,
			ConsulPartition:             nonDefaultPartition,
		}

		r := &ServiceDefaultsController{
			Client:                fclient,
			Log:                   logrtest.NewTestLogger(t),
			Scheme:                scheme,
			ConfigEntryController: ceCtrl,
		}

		_, err := r.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{
				Namespace: obj.GetObjectMeta().Namespace,
				Name:      obj.KubernetesName(),
			},
		})
		req.NoError(err)

		// Verify object landed in the correct partition.
		got, _, err := consulClient.ConfigEntries().Get(
			obj.ToConsul(datacenterName).GetKind(),
			obj.ConsulName(),
			&capi.QueryOptions{Partition: nonDefaultPartition},
		)
		req.NoError(err)
		req.Equal(obj.ConsulName(), got.GetName())

		// Status should be Synced.
		err = fclient.Get(context.Background(),
			types.NamespacedName{Namespace: obj.GetObjectMeta().Namespace, Name: obj.KubernetesName()},
			obj,
		)
		req.NoError(err)
		req.Equal(corev1.ConditionTrue, obj.SyncedConditionStatus())
	}

	t.Run("ServiceDefaults", func(t *testing.T) { run(t, svcDefaults) })
}

func TestConfigEntryController_updatesConfigEntry_consulPartitions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	obj := &v1alpha1.ServiceDefaults{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "foo",
			Namespace:  kubeNS,
			Finalizers: []string{FinalizerName},
		},
		Spec: v1alpha1.ServiceDefaultsSpec{Protocol: "http"},
	}

	scheme := runtime.NewScheme()
	scheme.AddKnownTypes(v1alpha1.GroupVersion, obj)
	fclient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(obj).
		WithStatusSubresource(obj).Build()

	testClient := test.TestServerWithMockConnMgrWatcher(t, nil)
	consulClient := testClient.APIClient
	consulClient.Partitions().Create(
		context.Background(),
		&capi.Partition{Name: nonDefaultPartition},
		nil,
	)
	r := &ServiceDefaultsController{
		Client: fclient,
		Log:    logrtest.NewTestLogger(t),
		Scheme: scheme,
		ConfigEntryController: &ConfigEntryController{
			ConsulClientConfig:          testClient.Cfg,
			ConsulServerConnMgr:         testClient.Watcher,
			DatacenterName:              datacenterName,
			EnableConsulAdminPartitions: true,
			ConsulPartition:             nonDefaultPartition,
		},
	}

	// First reconcile → create
	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: kubeNS, Name: obj.KubernetesName()},
	})
	require.NoError(t, err)

	// ---------------------------------------------------------------------
	// FETCH A FRESH COPY (so we have the current resourceVersion)
	// ---------------------------------------------------------------------
	var fresh v1alpha1.ServiceDefaults
	require.NoError(t, fclient.Get(ctx,
		types.NamespacedName{
			Namespace: kubeNS,
			Name:      obj.KubernetesName(),
		}, &fresh))

	// Mutate & update using the fresh object
	fresh.Spec.Protocol = "tcp"
	require.NoError(t, fclient.Update(ctx, &fresh))

	// Second reconcile → should perform the update path
	_, err = r.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: kubeNS,
			Name:      obj.KubernetesName(),
		},
	})
	require.NoError(t, err)

	// Assert the change reached Consul in the *part-1* partition
	ce, _, err := consulClient.ConfigEntries().Get(
		capi.ServiceDefaults,
		obj.ConsulName(),
		&capi.QueryOptions{Partition: nonDefaultPartition},
	)
	require.NoError(t, err)
	require.Equal(t, "tcp", ce.(*capi.ServiceConfigEntry).Protocol)
}

func TestConfigEntryController_deletesConfigEntry_consulPartitions(t *testing.T) {
	t.Parallel()

	// K8s object that is already marked for deletion.
	obj := &v1alpha1.ServiceDefaults{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "foo",
			Namespace:         kubeNS,
			Finalizers:        []string{FinalizerName},
			DeletionTimestamp: &metav1.Time{Time: time.Now()},
		},
		Spec: v1alpha1.ServiceDefaultsSpec{
			Protocol: "http",
		},
	}

	// ---------------------------------------------------------------------
	// Fake k8s client & scheme
	// ---------------------------------------------------------------------
	scheme := runtime.NewScheme()
	scheme.AddKnownTypes(v1alpha1.GroupVersion, obj)
	fclient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(obj).
		WithStatusSubresource(obj).
		Build()

	// ---------------------------------------------------------------------
	// Consul test server (create partition + seed entry)
	// ---------------------------------------------------------------------
	tc := test.TestServerWithMockConnMgrWatcher(t, nil)

	// create the partition so CRUD calls succeed
	consulClient := tc.APIClient
	consulClient.Partitions().Create(
		context.Background(),
		&capi.Partition{Name: nonDefaultPartition},
		nil,
	)

	// seed the entry inside that partition
	_, _, err := consulClient.ConfigEntries().Set(&capi.ServiceConfigEntry{
		Kind:      capi.ServiceDefaults,
		Name:      obj.KubernetesName(),
		Partition: nonDefaultPartition,
		Protocol:  "http",
		Meta: map[string]string{
			common.DatacenterKey: datacenterName,
		},
	}, &capi.WriteOptions{Partition: nonDefaultPartition})
	require.NoError(t, err)

	// ---------------------------------------------------------------------
	// Controller under test
	// ---------------------------------------------------------------------
	r := &ServiceDefaultsController{
		Client: fclient,
		Log:    logrtest.NewTestLogger(t),
		Scheme: scheme,
		ConfigEntryController: &ConfigEntryController{
			ConsulClientConfig:          tc.Cfg,
			ConsulServerConnMgr:         tc.Watcher,
			DatacenterName:              datacenterName,
			EnableConsulAdminPartitions: true,
			ConsulPartition:             nonDefaultPartition,
		},
	}

	// ---------------------------------------------------------------------
	// Reconcile – should delete the entry from Consul
	// ---------------------------------------------------------------------
	_, err = r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: obj.Namespace,
			Name:      obj.KubernetesName(),
		},
	})
	require.NoError(t, err)

	// Verify the config-entry is gone from the partition.
	_, _, err = consulClient.ConfigEntries().Get(
		capi.ServiceDefaults,
		obj.KubernetesName(),
		&capi.QueryOptions{Partition: nonDefaultPartition},
	)
	require.Error(t, err) // 404 expected – entry should no longer exist
}
