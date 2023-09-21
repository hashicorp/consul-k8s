// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package controllersv2

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/go-logr/logr"
	logrtest "github.com/go-logr/logr/testr"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	capi "github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
)

const datacenterName = "datacenter"

type testReconciler interface {
	Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error)
}

func TestConfigEntryControllers_createsConfigEntry(t *testing.T) {
	t.Parallel()
	kubeNS := "default"

	cases := []struct {
		kubeKind            string
		consulKind          string
		consulPrereqs       []capi.ConfigEntry
		configEntryResource common.ConfigEntryResource
		reconciler          func(client.Client, *consul.Config, consul.ServerConnectionManager, logr.Logger) testReconciler
		compare             func(t *testing.T, consul capi.ConfigEntry)
	}{
		{
			kubeKind:   "ServiceIntentions",
			consulKind: capi.ServiceIntentions,
			consulPrereqs: []capi.ConfigEntry{
				&capi.ServiceConfigEntry{
					Kind:     capi.ServiceDefaults,
					Name:     "foo",
					Protocol: "http",
				},
				&capi.ServiceConfigEntry{
					Kind:     capi.ServiceDefaults,
					Name:     "bar",
					Protocol: "http",
				},
				&capi.ServiceConfigEntry{
					Kind:     capi.ServiceDefaults,
					Name:     "baz",
					Protocol: "http",
				},
			},
			configEntryResource: &v1alpha1.ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "some-name",
					Namespace: kubeNS,
				},
				Spec: v1alpha1.ServiceIntentionsSpec{
					Destination: v1alpha1.IntentionDestination{
						Name: "foo",
					},
					Sources: v1alpha1.SourceIntentions{
						&v1alpha1.SourceIntention{
							Name:   "bar",
							Action: "allow",
						},
						&v1alpha1.SourceIntention{
							Name:   "baz",
							Action: "deny",
						},
						&v1alpha1.SourceIntention{
							Name: "bax",
							Permissions: v1alpha1.IntentionPermissions{
								&v1alpha1.IntentionPermission{
									Action: "allow",
									HTTP: &v1alpha1.IntentionHTTPPermission{
										PathExact: "/path",
										Header: v1alpha1.IntentionHTTPHeaderPermissions{
											v1alpha1.IntentionHTTPHeaderPermission{
												Name:    "auth",
												Present: true,
											},
										},
										Methods: []string{
											"PUT",
											"GET",
										},
									},
								},
							},
						},
					},
				},
			},
			reconciler: func(client client.Client, cfg *consul.Config, watcher consul.ServerConnectionManager, logger logr.Logger) testReconciler {
				return &TrafficPermissionsController{
					Client: client,
					Log:    logger,
					MeshConfigController: &MeshConfigController{
						ConsulClientConfig:  cfg,
						ConsulServerConnMgr: watcher,
						DatacenterName:      datacenterName,
					},
				}
			},
			compare: func(t *testing.T, consulEntry capi.ConfigEntry) {
				svcIntentions, ok := consulEntry.(*capi.ServiceIntentionsConfigEntry)
				require.True(t, ok, "cast error")
				require.Equal(t, "foo", svcIntentions.Name)
				require.Equal(t, "bar", svcIntentions.Sources[0].Name)
				require.Equal(t, capi.IntentionActionAllow, svcIntentions.Sources[0].Action)
				require.Equal(t, "baz", svcIntentions.Sources[1].Name)
				require.Equal(t, capi.IntentionActionDeny, svcIntentions.Sources[1].Action)
				require.Equal(t, "bax", svcIntentions.Sources[2].Name)
				require.Equal(t, capi.IntentionActionAllow, svcIntentions.Sources[2].Permissions[0].Action)
				require.Equal(t, "/path", svcIntentions.Sources[2].Permissions[0].HTTP.PathExact)
			},
		},
	}

	for _, c := range cases {
		t.Run(c.kubeKind, func(t *testing.T) {
			req := require.New(t)
			ctx := context.Background()

			s := runtime.NewScheme()
			s.AddKnownTypes(v1alpha1.GroupVersion, c.configEntryResource)
			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(c.configEntryResource).Build()

			testClient := test.TestServerWithMockConnMgrWatcher(t, nil)
			testClient.TestServer.WaitForServiceIntentions(t)
			consulClient := testClient.APIClient

			for _, configEntry := range c.consulPrereqs {
				written, _, err := consulClient.ConfigEntries().Set(configEntry, nil)
				req.NoError(err)
				req.True(written)
			}

			r := c.reconciler(fakeClient, testClient.Cfg, testClient.Watcher, logrtest.New(t))
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
		})
	}
}

func TestConfigEntryControllers_updatesConfigEntry(t *testing.T) {
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
			kubeKind:   "ServiceIntentions",
			consulKind: capi.ServiceIntentions,
			consulPrereqs: []capi.ConfigEntry{
				&capi.ServiceConfigEntry{
					Kind:     capi.ServiceDefaults,
					Name:     "foo",
					Protocol: "http",
				},
				&capi.ServiceConfigEntry{
					Kind:     capi.ServiceDefaults,
					Name:     "bar",
					Protocol: "http",
				},
			},
			configEntryResource: &v1alpha1.ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-name",
					Namespace: kubeNS,
				},
				Spec: v1alpha1.ServiceIntentionsSpec{
					Destination: v1alpha1.IntentionDestination{
						Name: "foo",
					},
					Sources: v1alpha1.SourceIntentions{
						&v1alpha1.SourceIntention{
							Name:   "bar",
							Action: "allow",
						},
						&v1alpha1.SourceIntention{
							Name: "baz",
							Permissions: v1alpha1.IntentionPermissions{
								&v1alpha1.IntentionPermission{
									Action: "allow",
									HTTP: &v1alpha1.IntentionHTTPPermission{
										PathExact: "/path",
										Header: v1alpha1.IntentionHTTPHeaderPermissions{
											v1alpha1.IntentionHTTPHeaderPermission{
												Name:    "auth",
												Present: true,
											},
										},
										Methods: []string{
											"PUT",
											"GET",
										},
									},
								},
							},
						},
					},
				},
			},
			reconciler: func(client client.Client, cfg *consul.Config, watcher consul.ServerConnectionManager, logger logr.Logger) testReconciler {
				return &TrafficPermissionsController{
					Client: client,
					Log:    logger,
					MeshConfigController: &MeshConfigController{
						ConsulClientConfig:  cfg,
						ConsulServerConnMgr: watcher,
						DatacenterName:      datacenterName,
					},
				}
			},
			updateF: func(resource common.ConfigEntryResource) {
				svcIntentions := resource.(*v1alpha1.ServiceIntentions)
				svcIntentions.Spec.Sources[0].Action = "deny"
				svcIntentions.Spec.Sources[1].Permissions[0].Action = "deny"
			},
			compare: func(t *testing.T, consulEntry capi.ConfigEntry) {
				configEntry, ok := consulEntry.(*capi.ServiceIntentionsConfigEntry)
				require.True(t, ok, "cast error")
				require.Equal(t, capi.IntentionActionDeny, configEntry.Sources[0].Action)
				require.Equal(t, capi.IntentionActionDeny, configEntry.Sources[1].Permissions[0].Action)
			},
		},
	}

	for _, c := range cases {
		t.Run(c.kubeKind, func(t *testing.T) {
			req := require.New(t)
			ctx := context.Background()

			s := runtime.NewScheme()
			s.AddKnownTypes(v1alpha1.GroupVersion, c.configEntryResource)
			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(c.configEntryResource).Build()

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
				r := c.reconciler(fakeClient, testClient.Cfg, testClient.Watcher, logrtest.New(t))
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

func TestConfigEntryControllers_deletesConfigEntry(t *testing.T) {
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
			kubeKind:   "ServiceIntentions",
			consulKind: capi.ServiceIntentions,
			consulPrereq: []capi.ConfigEntry{
				&capi.ServiceConfigEntry{
					Kind:     capi.ServiceDefaults,
					Name:     "foo",
					Protocol: "http",
				},
				&capi.ServiceConfigEntry{
					Kind:     capi.ServiceDefaults,
					Name:     "bar",
					Protocol: "http",
				},
			},
			configEntryResourceWithDeletion: &v1alpha1.ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-name",
					Namespace:         kubeNS,
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
				},
				Spec: v1alpha1.ServiceIntentionsSpec{
					Destination: v1alpha1.IntentionDestination{
						Name: "foo",
					},
					Sources: v1alpha1.SourceIntentions{
						&v1alpha1.SourceIntention{
							Name:   "bar",
							Action: "allow",
						},
						&v1alpha1.SourceIntention{
							Name: "baz",
							Permissions: v1alpha1.IntentionPermissions{
								&v1alpha1.IntentionPermission{
									Action: "allow",
									HTTP: &v1alpha1.IntentionHTTPPermission{
										PathExact: "/path",
										Header: v1alpha1.IntentionHTTPHeaderPermissions{
											v1alpha1.IntentionHTTPHeaderPermission{
												Name:    "auth",
												Present: true,
											},
										},
										Methods: []string{
											"PUT",
											"GET",
										},
									},
								},
							},
						},
					},
				},
			},
			reconciler: func(client client.Client, cfg *consul.Config, watcher consul.ServerConnectionManager, logger logr.Logger) testReconciler {
				return &TrafficPermissionsController{
					Client: client,
					Log:    logger,
					MeshConfigController: &MeshConfigController{
						ConsulClientConfig:  cfg,
						ConsulServerConnMgr: watcher,
						DatacenterName:      datacenterName,
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
			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(c.configEntryResourceWithDeletion).Build()

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
				r := c.reconciler(fakeClient, testClient.Cfg, testClient.Watcher, logrtest.New(t))
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

func TestConfigEntryControllers_errorUpdatesSyncStatus(t *testing.T) {
	t.Parallel()
	kubeNS := "default"

	req := require.New(t)
	ctx := context.Background()
	svcDefaults := &v1alpha1.ServiceDefaults{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: kubeNS,
		},
		Spec: v1alpha1.ServiceDefaultsSpec{
			Protocol: "http",
		},
	}

	s := runtime.NewScheme()
	s.AddKnownTypes(v1alpha1.GroupVersion, svcDefaults)
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(svcDefaults).Build()

	testClient := test.TestServerWithMockConnMgrWatcher(t, nil)
	testClient.TestServer.WaitForServiceIntentions(t)

	// Get watcher state to make sure we can get a healthy address.
	_, err := testClient.Watcher.State()
	require.NoError(t, err)
	// Stop the server before calling reconcile imitating a server that's not running.
	_ = testClient.TestServer.Stop()

	reconciler := &TrafficPermissionsController{
		Client: fakeClient,
		Log:    logrtest.New(t),
		MeshConfigController: &MeshConfigController{
			ConsulClientConfig:  testClient.Cfg,
			ConsulServerConnMgr: testClient.Watcher,
			DatacenterName:      datacenterName,
		},
	}

	// ReconcileEntry should result in an error.
	namespacedName := types.NamespacedName{
		Namespace: kubeNS,
		Name:      svcDefaults.KubernetesName(),
	}
	resp, err := reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: namespacedName,
	})
	req.Error(err)

	expErr := fmt.Sprintf("Get \"http://127.0.0.1:%d/v1/config/%s/%s\": dial tcp 127.0.0.1:%d: connect: connection refused",
		testClient.Cfg.HTTPPort, capi.ServiceDefaults, svcDefaults.ConsulName(), testClient.Cfg.HTTPPort)
	req.Contains(err.Error(), expErr)
	req.False(resp.Requeue)

	// Check that the status is "synced=false".
	err = fakeClient.Get(ctx, namespacedName, svcDefaults)
	req.NoError(err)
	status, reason, errMsg := svcDefaults.SyncedCondition()
	req.Equal(corev1.ConditionFalse, status)
	req.Equal("ConsulAgentError", reason)
	req.Contains(errMsg, expErr)
}

// Test that if the config entry hasn't changed in Consul but our resource
// synced status isn't set to true then we update its status.
func TestConfigEntryControllers_setsSyncedToTrue(t *testing.T) {
	t.Parallel()
	kubeNS := "default"
	req := require.New(t)
	ctx := context.Background()
	s := runtime.NewScheme()
	svcDefaults := &v1alpha1.ServiceDefaults{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: kubeNS,
		},
		Spec: v1alpha1.ServiceDefaultsSpec{
			Protocol: "http",
		},
		Status: v1alpha1.Status{
			Conditions: v1alpha1.Conditions{
				{
					Type:   v1alpha1.ConditionSynced,
					Status: corev1.ConditionUnknown,
				},
			},
		},
	}
	s.AddKnownTypes(v1alpha1.GroupVersion, svcDefaults)

	// The config entry exists in kube but its status will be nil.
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(svcDefaults).Build()

	testClient := test.TestServerWithMockConnMgrWatcher(t, nil)
	testClient.TestServer.WaitForServiceIntentions(t)
	consulClient := testClient.APIClient
	reconciler := &TrafficPermissionsController{
		Client: fakeClient,
		Log:    logrtest.New(t),
		MeshConfigController: &MeshConfigController{
			ConsulClientConfig:  testClient.Cfg,
			ConsulServerConnMgr: testClient.Watcher,
			DatacenterName:      datacenterName,
		},
	}

	// Create the resource in Consul to mimic that it was created
	// successfully (but its status hasn't been updated).
	_, _, err := consulClient.ConfigEntries().Set(svcDefaults.ToConsul(datacenterName), nil)
	require.NoError(t, err)

	namespacedName := types.NamespacedName{
		Namespace: kubeNS,
		Name:      svcDefaults.KubernetesName(),
	}
	resp, err := reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: namespacedName,
	})
	req.NoError(err)
	req.False(resp.Requeue)

	// Check that the status is now "synced".
	err = fakeClient.Get(ctx, namespacedName, svcDefaults)
	req.NoError(err)
	req.Equal(corev1.ConditionTrue, svcDefaults.SyncedConditionStatus())
}

// Test that if the config entry exists in Consul but is not managed by the
// controller, creating/updating the resource fails.
func TestConfigEntryControllers_doesNotCreateUnownedConfigEntry(t *testing.T) {
	t.Parallel()
	kubeNS := "default"

	cases := []struct {
		datacenterAnnotation string
		expErr               string
	}{
		{
			datacenterAnnotation: "",
			expErr:               "config entry already exists in Consul",
		},
		{
			datacenterAnnotation: "other-datacenter",
			expErr:               "config entry managed in different datacenter: \"other-datacenter\"",
		},
	}

	for _, c := range cases {
		t.Run(fmt.Sprintf("datacenter: %q", c.datacenterAnnotation), func(t *testing.T) {
			req := require.New(t)
			ctx := context.Background()

			s := runtime.NewScheme()
			svcDefaults := &v1alpha1.ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: kubeNS,
				},
				Spec: v1alpha1.ServiceDefaultsSpec{
					Protocol: "http",
				},
			}
			s.AddKnownTypes(v1alpha1.GroupVersion, svcDefaults)
			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(svcDefaults).Build()

			testClient := test.TestServerWithMockConnMgrWatcher(t, nil)
			testClient.TestServer.WaitForServiceIntentions(t)
			consulClient := testClient.APIClient

			// We haven't run reconcile yet. We must create the config entry
			// in Consul ourselves in a different datacenter.
			{
				written, _, err := consulClient.ConfigEntries().Set(svcDefaults.ToConsul(c.datacenterAnnotation), nil)
				req.NoError(err)
				req.True(written)
			}

			// Now run reconcile which should **not** update the entry in Consul.
			{
				namespacedName := types.NamespacedName{
					Namespace: kubeNS,
					Name:      svcDefaults.KubernetesName(),
				}
				// First get it so we have the latest revision number.
				err := fakeClient.Get(ctx, namespacedName, svcDefaults)
				req.NoError(err)

				// Attempt to create the entry in Kube and run reconcile.
				reconciler := TrafficPermissionsController{
					Client: fakeClient,
					Log:    logrtest.New(t),
					MeshConfigController: &MeshConfigController{
						ConsulClientConfig:  testClient.Cfg,
						ConsulServerConnMgr: testClient.Watcher,
						DatacenterName:      datacenterName,
					},
				}
				resp, err := reconciler.Reconcile(ctx, ctrl.Request{
					NamespacedName: namespacedName,
				})
				req.EqualError(err, c.expErr)
				req.False(resp.Requeue)

				// Now check that the object in Consul is as expected.
				cfg, _, err := consulClient.ConfigEntries().Get(capi.ServiceDefaults, svcDefaults.ConsulName(), nil)
				req.NoError(err)
				req.Equal(cfg.GetMeta()[common.DatacenterKey], c.datacenterAnnotation)

				// Check that the status is "synced=false".
				err = fakeClient.Get(ctx, namespacedName, svcDefaults)
				req.NoError(err)
				status, reason, errMsg := svcDefaults.SyncedCondition()
				req.Equal(corev1.ConditionFalse, status)
				req.Equal("ExternallyManagedConfigError", reason)
				req.Equal(errMsg, c.expErr)
			}
		})
	}
}

// Test that if the config entry exists in Consul but is not managed by the
// controller, deleting the resource does not delete the Consul config entry.
func TestConfigEntryControllers_doesNotDeleteUnownedConfig(t *testing.T) {
	t.Parallel()
	kubeNS := "default"

	// Test against the metadata being empty or set. Both should not trigger
	// deletion in Consul.
	cases := []string{"", "other-datacenter"}
	for _, datacenter := range cases {
		t.Run(datacenter, func(t *testing.T) {
			req := require.New(t)
			ctx := context.Background()

			s := runtime.NewScheme()
			svcDefaultsWithDeletion := &v1alpha1.ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "foo",
					Namespace:         kubeNS,
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
				},
				Spec: v1alpha1.ServiceDefaultsSpec{
					Protocol: "http",
				},
			}
			s.AddKnownTypes(v1alpha1.GroupVersion, svcDefaultsWithDeletion)
			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(svcDefaultsWithDeletion).Build()

			testClient := test.TestServerWithMockConnMgrWatcher(t, nil)
			testClient.TestServer.WaitForServiceIntentions(t)
			consulClient := testClient.APIClient
			reconciler := &TrafficPermissionsController{
				Client: fakeClient,
				Log:    logrtest.New(t),
				MeshConfigController: &MeshConfigController{
					ConsulClientConfig:  testClient.Cfg,
					ConsulServerConnMgr: testClient.Watcher,
					DatacenterName:      datacenterName,
				},
			}

			// We haven't run reconcile yet so we must create the config entry
			// in Consul ourselves.
			{
				// Create the resource with different datacenter on metadata
				written, _, err := consulClient.ConfigEntries().Set(svcDefaultsWithDeletion.ToConsul(datacenter), nil)
				req.NoError(err)
				req.True(written)
			}

			// Now run reconcile. It's marked for deletion so this should delete the kubernetes resource
			// but not the consul config entry.
			{
				namespacedName := types.NamespacedName{
					Namespace: kubeNS,
					Name:      svcDefaultsWithDeletion.KubernetesName(),
				}
				resp, err := reconciler.Reconcile(ctx, ctrl.Request{
					NamespacedName: namespacedName,
				})
				req.NoError(err)
				req.False(resp.Requeue)

				entry, _, err := consulClient.ConfigEntries().Get(capi.ServiceDefaults, svcDefaultsWithDeletion.ConsulName(), nil)
				req.NoError(err)
				req.Equal(entry.GetMeta()[common.DatacenterKey], datacenter)

				// Check that the resource is deleted from cluster.
				svcDefault := &v1alpha1.ServiceDefaults{}
				_ = fakeClient.Get(ctx, namespacedName, svcDefault)
			}
		})
	}
}

func TestConfigEntryControllers_updatesStatusWhenDeleteFails(t *testing.T) {
	ctx := context.Background()
	kubeNS := "default"

	s := runtime.NewScheme()
	s.AddKnownTypes(v1alpha1.GroupVersion, &v1alpha1.ServiceDefaults{}, &v1alpha1.ServiceSplitter{})

	defaults := &v1alpha1.ServiceDefaults{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "service",
			Namespace: "default",
		},
		Spec: v1alpha1.ServiceDefaultsSpec{
			Protocol: "http",
		},
	}

	splitter := &v1alpha1.ServiceSplitter{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "service",
			Namespace: "default",
		},
		Spec: v1alpha1.ServiceSplitterSpec{
			Splits: v1alpha1.ServiceSplits{
				{
					Weight:  100,
					Service: "service",
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(defaults, splitter).Build()

	testClient := test.TestServerWithMockConnMgrWatcher(t, nil)
	testClient.TestServer.WaitForServiceIntentions(t)

	logger := logrtest.New(t)

	svcDefaultsReconciler := TrafficPermissionsController{
		Client: fakeClient,
		Log:    logger,
		MeshConfigController: &MeshConfigController{
			ConsulClientConfig:  testClient.Cfg,
			ConsulServerConnMgr: testClient.Watcher,
			DatacenterName:      datacenterName,
		},
	}

	defaultsNamespacedName := types.NamespacedName{
		Namespace: kubeNS,
		Name:      defaults.Name,
	}

	// Create config entries for service-defaults and service-splitter.
	resp, err := svcDefaultsReconciler.Reconcile(ctx, ctrl.Request{NamespacedName: defaultsNamespacedName})
	require.NoError(t, err)
	require.False(t, resp.Requeue)

	err = fakeClient.Get(ctx, defaultsNamespacedName, defaults)
	require.NoError(t, err)

	// Update service-defaults with deletion timestamp so that it attempts deletion on reconcile.
	defaults.ObjectMeta.DeletionTimestamp = &metav1.Time{Time: time.Now()}
	err = fakeClient.Update(ctx, defaults)
	require.NoError(t, err)

	// Reconcile should fail as the service-splitter still required the service-defaults causing the delete operation on Consul to fail.
	resp, err = svcDefaultsReconciler.Reconcile(ctx, ctrl.Request{NamespacedName: defaultsNamespacedName})
	require.EqualError(t, err, "deleting config entry from consul: Unexpected response code: 500 (discovery chain \"service\" uses a protocol \"tcp\" that does not permit advanced routing or splitting behavior)")
	require.False(t, resp.Requeue)

	err = fakeClient.Get(ctx, defaultsNamespacedName, defaults)
	require.NoError(t, err)

	// Ensure the status of the resource is updated to display failure reason.
	syncCondition := defaults.GetCondition(v1alpha1.ConditionSynced)
	expectedCondition := &v1alpha1.Condition{
		Type:    v1alpha1.ConditionSynced,
		Status:  corev1.ConditionFalse,
		Reason:  ConsulAgentError,
		Message: "deleting config entry from consul: Unexpected response code: 500 (discovery chain \"service\" uses a protocol \"tcp\" that does not permit advanced routing or splitting behavior)",
	}
	require.True(t, cmp.Equal(syncCondition, expectedCondition, cmpopts.IgnoreFields(v1alpha1.Condition{}, "LastTransitionTime")))
}

// Test that if the resource already exists in Consul but the Kube resource
// has the "migrate-entry" annotation then we let the Kube resource sync to Consul.
func TestConfigEntryController_Migration(t *testing.T) {
	kubeNS := "default"
	protocol := "http"
	cfgEntryName := "service"

	cases := map[string]struct {
		KubeResource   v1alpha1.ServiceDefaults
		ConsulResource capi.ServiceConfigEntry
		ExpErr         string
	}{
		"identical resources should be migrated successfully": {
			KubeResource: v1alpha1.ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cfgEntryName,
					Namespace: kubeNS,
					Annotations: map[string]string{
						common.MigrateEntryKey: "true",
					},
				},
				Spec: v1alpha1.ServiceDefaultsSpec{
					Protocol: protocol,
				},
			},
			ConsulResource: capi.ServiceConfigEntry{
				Kind:     capi.ServiceDefaults,
				Name:     cfgEntryName,
				Protocol: protocol,
			},
		},
		"different resources (protocol) should not be migrated": {
			KubeResource: v1alpha1.ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cfgEntryName,
					Namespace: kubeNS,
					Annotations: map[string]string{
						common.MigrateEntryKey: "true",
					},
				},
				Spec: v1alpha1.ServiceDefaultsSpec{
					Protocol: "tcp",
				},
			},
			ConsulResource: capi.ServiceConfigEntry{
				Kind:     capi.ServiceDefaults,
				Name:     cfgEntryName,
				Protocol: protocol,
			},
			ExpErr: "migration failed: Kubernetes resource does not match existing Consul config entry",
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()

			s := runtime.NewScheme()
			s.AddKnownTypes(v1alpha1.GroupVersion, &v1alpha1.ServiceDefaults{})

			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(&c.KubeResource).Build()
			testClient := test.TestServerWithMockConnMgrWatcher(t, nil)
			testClient.TestServer.WaitForServiceIntentions(t)
			consulClient := testClient.APIClient

			// Create the service-defaults in Consul.
			success, _, err := consulClient.ConfigEntries().Set(&c.ConsulResource, nil)
			require.NoError(t, err)
			require.True(t, success, "config entry was not created")

			// Set up the reconciler.
			logger := logrtest.New(t)
			svcDefaultsReconciler := ServiceDefaultsController{
				Client: fakeClient,
				Log:    logger,
				MeshConfigController: &MeshConfigController{
					ConsulClientConfig:  testClient.Cfg,
					ConsulServerConnMgr: testClient.Watcher,
					DatacenterName:      datacenterName,
				},
			}

			defaultsNamespacedName := types.NamespacedName{
				Namespace: kubeNS,
				Name:      cfgEntryName,
			}

			// Trigger the reconciler.
			resp, err := svcDefaultsReconciler.Reconcile(ctx, ctrl.Request{NamespacedName: defaultsNamespacedName})
			if c.ExpErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), c.ExpErr)
			} else {
				require.NoError(t, err)
				require.False(t, resp.Requeue)
			}

			entryAfterReconcile := &v1alpha1.ServiceDefaults{}
			err = fakeClient.Get(ctx, defaultsNamespacedName, entryAfterReconcile)
			require.NoError(t, err)

			syncCondition := entryAfterReconcile.GetCondition(v1alpha1.ConditionSynced)
			if c.ExpErr != "" {
				// Ensure the status of the resource is migration failed.
				require.Equal(t, corev1.ConditionFalse, syncCondition.Status)
				require.Equal(t, MigrationFailedError, syncCondition.Reason)
				require.Contains(t, syncCondition.Message, c.ExpErr)

				// Check that the Consul resource hasn't changed.
				entry, _, err := consulClient.ConfigEntries().Get(capi.ServiceDefaults, cfgEntryName, nil)
				require.NoError(t, err)
				require.NotContains(t, entry.GetMeta(), common.DatacenterKey)
				require.Equal(t, protocol, entry.(*capi.ServiceConfigEntry).Protocol)
			} else {
				// Ensure the status of the resource is synced.
				expectedCondition := &v1alpha1.Condition{
					Type:   v1alpha1.ConditionSynced,
					Status: corev1.ConditionTrue,
				}
				require.True(t, cmp.Equal(syncCondition, expectedCondition, cmpopts.IgnoreFields(v1alpha1.Condition{}, "LastTransitionTime")))

				// Ensure the Consul resource has the expected metadata.
				entry, _, err := consulClient.ConfigEntries().Get(capi.ServiceDefaults, cfgEntryName, nil)
				require.NoError(t, err)
				require.Contains(t, entry.GetMeta(), common.DatacenterKey)
				require.Equal(t, "datacenter", entry.GetMeta()[common.DatacenterKey])
			}
		})
	}
}
