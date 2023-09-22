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
	pbauth "github.com/hashicorp/consul/proto-public/pbauth/v2beta1"
	"github.com/hashicorp/consul/proto-public/pbresource"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/api/v2beta1"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
)

type testReconciler interface {
	Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error)
}

// TestMeshConfigController_createsMeshConfig validated resources are created in Consul from kube objects.
func TestMeshConfigController_createsMeshConfig(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		meshConfig common.MeshConfig
		expected   *pbauth.TrafficPermissions
		reconciler func(client.Client, *consul.Config, consul.ServerConnectionManager, logr.Logger) testReconciler
		unmarshal  func(t *testing.T, consul *pbresource.Resource) proto.Message
	}{
		{
			name: "TrafficPermissions",
			meshConfig: &v2beta1.TrafficPermissions{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-traffic-permission",
					Namespace: metav1.NamespaceDefault,
				},
				Spec: v2beta1.TrafficPermissionsSpec{
					Destination: &v2beta1.Destination{
						IdentityName: "destination-identity",
					},
					Action: v2beta1.ActionAllow,
					Permissions: v2beta1.Permissions{
						{
							Sources: v2beta1.Sources{
								{
									Namespace: "the space namespace space",
								},
								{
									IdentityName: "source-identity",
								},
							},
							DestinationRules: v2beta1.DestinationRules{
								{
									PathExact: "/hello",
									Methods:   []string{"GET", "POST"},
									PortNames: []string{"web", "admin"},
								},
							},
						},
					},
				},
			},
			expected: &pbauth.TrafficPermissions{
				Destination: &pbauth.Destination{
					IdentityName: "destination-identity",
				},
				Action: pbauth.Action_ACTION_ALLOW,
				Permissions: []*pbauth.Permission{
					{
						Sources: []*pbauth.Source{
							{
								IdentityName: "source-identity",
							},
							{
								Namespace: "the space namespace space",
							},
						},
						DestinationRules: []*pbauth.DestinationRule{
							{
								PathExact: "/hello",
								Methods:   []string{"GET", "POST"},
								PortNames: []string{"web", "admin"},
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
					},
				}
			},
			unmarshal: func(t *testing.T, resource *pbresource.Resource) proto.Message {
				data := resource.Data

				trafficPermission := &pbauth.TrafficPermissions{}
				require.NoError(t, data.UnmarshalTo(trafficPermission))
				return trafficPermission
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := context.Background()

			s := runtime.NewScheme()
			s.AddKnownTypes(v2beta1.AuthGroupVersion, c.meshConfig)
			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(c.meshConfig).Build()

			testClient := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
				c.Experiments = []string{"resource-apis"}
			})
			resourceClient, err := consul.NewResourceServiceClient(testClient.Watcher)
			require.NoError(t, err)

			r := c.reconciler(fakeClient, testClient.Cfg, testClient.Watcher, logrtest.New(t))
			namespacedName := types.NamespacedName{
				Namespace: metav1.NamespaceDefault,
				Name:      c.meshConfig.KubernetesName(),
			}
			resp, err := r.Reconcile(ctx, ctrl.Request{
				NamespacedName: namespacedName,
			})
			require.NoError(t, err)
			require.False(t, resp.Requeue)

			req := &pbresource.ReadRequest{Id: c.meshConfig.ResourceID(constants.DefaultConsulNS, constants.DefaultConsulPartition)}
			res, err := resourceClient.Read(ctx, req)
			require.NoError(t, err)
			require.NotNil(t, res)
			require.Equal(t, c.meshConfig.GetName(), res.GetResource().GetId().GetName())

			actual := c.unmarshal(t, res.GetResource())
			opts := append([]cmp.Option{protocmp.IgnoreFields(&pbresource.Resource{}, "status", "generation", "version")}, test.CmpProtoIgnoreOrder()...)
			diff := cmp.Diff(c.expected, actual, opts...)
			require.Equal(t, "", diff, "TrafficPermissions do not match")

			// Check that the status is "synced".
			err = fakeClient.Get(ctx, namespacedName, c.meshConfig)
			require.NoError(t, err)
			require.Equal(t, corev1.ConditionTrue, c.meshConfig.SyncedConditionStatus())

			// Check that the finalizer is added.
			require.Contains(t, c.meshConfig.Finalizers(), FinalizerName)
		})
	}
}

func TestMeshConfigController_updatesMeshConfig(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		meshConfig common.MeshConfig
		expected   *pbauth.TrafficPermissions
		reconciler func(client.Client, *consul.Config, consul.ServerConnectionManager, logr.Logger) testReconciler
		updateF    func(config common.MeshConfig)
		unmarshal  func(t *testing.T, consul *pbresource.Resource) proto.Message
	}{
		{
			name: "TrafficPermissions",
			meshConfig: &v2beta1.TrafficPermissions{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-traffic-permission",
					Namespace: metav1.NamespaceDefault,
				},
				Spec: v2beta1.TrafficPermissionsSpec{
					Destination: &v2beta1.Destination{
						IdentityName: "destination-identity",
					},
					Action: v2beta1.ActionAllow,
					Permissions: v2beta1.Permissions{
						{
							Sources: v2beta1.Sources{
								{
									Namespace: "the space namespace space",
								},
								{
									IdentityName: "source-identity",
								},
							},
							DestinationRules: v2beta1.DestinationRules{
								{
									PathExact: "/hello",
									Methods:   []string{"GET", "POST"},
									PortNames: []string{"web", "admin"},
								},
							},
						},
					},
				},
			},
			expected: &pbauth.TrafficPermissions{
				Destination: &pbauth.Destination{
					IdentityName: "destination-identity",
				},
				Action: pbauth.Action_ACTION_DENY,
				Permissions: []*pbauth.Permission{
					{
						Sources: []*pbauth.Source{
							{
								Namespace: "the space namespace space",
							},
						},
						DestinationRules: []*pbauth.DestinationRule{
							{
								PathExact: "/hello",
								Methods:   []string{"GET", "POST"},
								PortNames: []string{"web", "admin"},
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
					},
				}
			},
			updateF: func(resource common.MeshConfig) {
				trafficPermissions := resource.(*v2beta1.TrafficPermissions)
				trafficPermissions.Spec.Action = "deny"
				trafficPermissions.Spec.Permissions[0].Sources = trafficPermissions.Spec.Permissions[0].Sources[:1]
			},
			unmarshal: func(t *testing.T, resource *pbresource.Resource) proto.Message {
				data := resource.Data

				trafficPermission := &pbauth.TrafficPermissions{}
				require.NoError(t, data.UnmarshalTo(trafficPermission))
				return trafficPermission
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := context.Background()

			s := runtime.NewScheme()
			s.AddKnownTypes(v1alpha1.GroupVersion, c.meshConfig)
			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(c.meshConfig).Build()

			testClient := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
				c.Experiments = []string{"resource-apis"}
			})
			resourceClient, err := consul.NewResourceServiceClient(testClient.Watcher)
			require.NoError(t, err)
			// We haven't run reconcile yet, so we must create the MeshConfig
			// in Consul ourselves.
			{
				resource := c.meshConfig.Resource(constants.DefaultConsulNS, constants.DefaultConsulPartition)
				req := &pbresource.WriteRequest{Resource: resource}
				_, err := resourceClient.Write(ctx, req)
				require.NoError(t, err)
			}

			// Now run reconcile which should update the entry in Consul.
			{
				namespacedName := types.NamespacedName{
					Namespace: metav1.NamespaceDefault,
					Name:      c.meshConfig.KubernetesName(),
				}
				// First get it, so we have the latest revision number.
				err := fakeClient.Get(ctx, namespacedName, c.meshConfig)
				require.NoError(t, err)

				// Update the entry in Kube and run reconcile.
				c.updateF(c.meshConfig)
				err = fakeClient.Update(ctx, c.meshConfig)
				require.NoError(t, err)
				r := c.reconciler(fakeClient, testClient.Cfg, testClient.Watcher, logrtest.New(t))
				resp, err := r.Reconcile(ctx, ctrl.Request{
					NamespacedName: namespacedName,
				})
				require.NoError(t, err)
				require.False(t, resp.Requeue)

				// Now check that the object in Consul is as expected.
				req := &pbresource.ReadRequest{Id: c.meshConfig.ResourceID(constants.DefaultConsulNS, constants.DefaultConsulPartition)}
				res, err := resourceClient.Read(ctx, req)
				require.NoError(t, err)
				require.NotNil(t, res)
				require.Equal(t, c.meshConfig.GetName(), res.GetResource().GetId().GetName())

				actual := c.unmarshal(t, res.GetResource())
				opts := append([]cmp.Option{protocmp.IgnoreFields(&pbresource.Resource{}, "status", "generation", "version")}, test.CmpProtoIgnoreOrder()...)
				diff := cmp.Diff(c.expected, actual, opts...)
				require.Equal(t, "", diff, "TrafficPermissions do not match")
			}
		})
	}
}

func TestMeshConfigController_deletesMeshConfig(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name                   string
		MeshConfigWithDeletion common.MeshConfig
		reconciler             func(client.Client, *consul.Config, consul.ServerConnectionManager, logr.Logger) testReconciler
	}{
		{
			name: "TrafficPermissions",
			MeshConfigWithDeletion: &v2beta1.TrafficPermissions{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-name",
					Namespace:         metav1.NamespaceDefault,
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
					Finalizers:        []string{FinalizerName},
				},
				Spec: v2beta1.TrafficPermissionsSpec{
					Destination: &v2beta1.Destination{
						IdentityName: "destination-identity",
					},
					Action: v2beta1.ActionAllow,
					Permissions: v2beta1.Permissions{
						{
							Sources: v2beta1.Sources{
								{
									Namespace: "the space namespace space",
								},
								{
									IdentityName: "source-identity",
								},
							},
							DestinationRules: v2beta1.DestinationRules{
								{
									PathExact: "/hello",
									Methods:   []string{"GET", "POST"},
									PortNames: []string{"web", "admin"},
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
					},
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := context.Background()

			s := runtime.NewScheme()
			s.AddKnownTypes(v2beta1.AuthGroupVersion, c.MeshConfigWithDeletion)
			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(c.MeshConfigWithDeletion).Build()

			testClient := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
				c.Experiments = []string{"resource-apis"}
			})
			resourceClient, err := consul.NewResourceServiceClient(testClient.Watcher)
			require.NoError(t, err)

			// We haven't run reconcile yet, so we must create the config entry
			// in Consul ourselves.
			{
				resource := c.MeshConfigWithDeletion.Resource(constants.DefaultConsulNS, constants.DefaultConsulPartition)
				req := &pbresource.WriteRequest{Resource: resource}
				_, err := resourceClient.Write(ctx, req)
				require.NoError(t, err)
			}

			// Now run reconcile. It's marked for deletion so this should delete it.
			{
				namespacedName := types.NamespacedName{
					Namespace: metav1.NamespaceDefault,
					Name:      c.MeshConfigWithDeletion.KubernetesName(),
				}
				r := c.reconciler(fakeClient, testClient.Cfg, testClient.Watcher, logrtest.New(t))
				resp, err := r.Reconcile(context.Background(), ctrl.Request{
					NamespacedName: namespacedName,
				})
				require.NoError(t, err)
				require.False(t, resp.Requeue)

				// Now check that the object in Consul is as expected.
				req := &pbresource.ReadRequest{Id: c.MeshConfigWithDeletion.ResourceID(constants.DefaultConsulNS, constants.DefaultConsulPartition)}
				_, err = resourceClient.Read(ctx, req)
				require.Error(t, err)
				require.True(t, isNotFoundErr(err))
			}
		})
	}
}

func TestMeshConfigController_errorUpdatesSyncStatus(t *testing.T) {
	t.Parallel()

	req := require.New(t)
	ctx := context.Background()
	trafficpermissions := &v2beta1.TrafficPermissions{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: v2beta1.TrafficPermissionsSpec{
			Destination: &v2beta1.Destination{
				IdentityName: "destination-identity",
			},
			Action: v2beta1.ActionAllow,
			Permissions: v2beta1.Permissions{
				{
					Sources: v2beta1.Sources{
						{
							IdentityName: "source-identity",
						},
					},
				},
			},
		},
	}

	s := runtime.NewScheme()
	s.AddKnownTypes(v2beta1.AuthGroupVersion, trafficpermissions)
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(trafficpermissions).Build()

	testClient := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
		c.Experiments = []string{"resource-apis"}
	})

	// Get watcher state to make sure we can get a healthy address.
	state, err := testClient.Watcher.State()
	require.NoError(t, err)
	// Stop the server before calling reconcile imitating a server that's not running.
	_ = testClient.TestServer.Stop()

	reconciler := &TrafficPermissionsController{
		Client: fakeClient,
		Log:    logrtest.New(t),
		MeshConfigController: &MeshConfigController{
			ConsulClientConfig:  testClient.Cfg,
			ConsulServerConnMgr: testClient.Watcher,
		},
	}

	// ReconcileEntry should result in an error.
	namespacedName := types.NamespacedName{
		Namespace: metav1.NamespaceDefault,
		Name:      trafficpermissions.KubernetesName(),
	}
	resp, err := reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: namespacedName,
	})
	req.Error(err)

	expErr := fmt.Sprintf("connection error: desc = \"transport: Error while dialing: dial tcp 127.0.0.1:%d: connect: connection refused\"", state.Address.Port)
	req.Contains(err.Error(), expErr)
	req.False(resp.Requeue)

	// Check that the status is "synced=false".
	err = fakeClient.Get(ctx, namespacedName, trafficpermissions)
	req.NoError(err)
	status, reason, errMsg := trafficpermissions.SyncedCondition()
	req.Equal(corev1.ConditionFalse, status)
	req.Equal("ConsulAgentError", reason)
	req.Contains(errMsg, expErr)
}

// TestMeshConfigController_setsSyncedToTrue tests that if the resource hasn't changed in
// Consul but our resource's synced status isn't set to true, then we update its status.
func TestMeshConfigController_setsSyncedToTrue(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := runtime.NewScheme()

	trafficpermissions := &v2beta1.TrafficPermissions{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: v2beta1.TrafficPermissionsSpec{
			Destination: &v2beta1.Destination{
				IdentityName: "destination-identity",
			},
			Action: v2beta1.ActionAllow,
			Permissions: v2beta1.Permissions{
				{
					Sources: v2beta1.Sources{
						{
							IdentityName: "source-identity",
						},
					},
				},
			},
		},
		Status: v2beta1.Status{
			Conditions: v2beta1.Conditions{
				{
					Type:   v2beta1.ConditionSynced,
					Status: corev1.ConditionUnknown,
				},
			},
		},
	}
	s.AddKnownTypes(v2beta1.AuthGroupVersion, trafficpermissions)

	// The config entry exists in kube but its status will be nil.
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(trafficpermissions).Build()

	testClient := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
		c.Experiments = []string{"resource-apis"}
	})
	resourceClient, err := consul.NewResourceServiceClient(testClient.Watcher)
	require.NoError(t, err)

	reconciler := &TrafficPermissionsController{
		Client: fakeClient,
		Log:    logrtest.New(t),
		MeshConfigController: &MeshConfigController{
			ConsulClientConfig:  testClient.Cfg,
			ConsulServerConnMgr: testClient.Watcher,
		},
	}

	// Create the resource in Consul to mimic that it was created
	// successfully (but its status hasn't been updated).
	{
		resource := trafficpermissions.Resource(constants.DefaultConsulNS, constants.DefaultConsulPartition)
		req := &pbresource.WriteRequest{Resource: resource}
		_, err := resourceClient.Write(ctx, req)
		require.NoError(t, err)
	}

	namespacedName := types.NamespacedName{
		Namespace: metav1.NamespaceDefault,
		Name:      trafficpermissions.KubernetesName(),
	}
	resp, err := reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: namespacedName,
	})
	require.NoError(t, err)
	require.False(t, resp.Requeue)

	// Check that the status is now "synced".
	err = fakeClient.Get(ctx, namespacedName, trafficpermissions)
	require.NoError(t, err)
	require.Equal(t, corev1.ConditionTrue, trafficpermissions.SyncedConditionStatus())
}

// TestMeshConfigController_doesNotCreateUnownedMeshConfig test that if the resource
// exists in Consul but is not managed by the controller, creating/updating the resource fails.
func TestMeshConfigController_doesNotCreateUnownedMeshConfig(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	s := runtime.NewScheme()
	trafficpermissions := &v2beta1.TrafficPermissions{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: v2beta1.TrafficPermissionsSpec{
			Destination: &v2beta1.Destination{
				IdentityName: "destination-identity",
			},
			Action: v2beta1.ActionAllow,
			Permissions: v2beta1.Permissions{
				{
					Sources: v2beta1.Sources{
						{
							IdentityName: "source-identity",
						},
					},
				},
			},
		},
	}
	s.AddKnownTypes(v2beta1.AuthGroupVersion, trafficpermissions)
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(trafficpermissions).Build()

	testClient := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
		c.Experiments = []string{"resource-apis"}
	})
	resourceClient, err := consul.NewResourceServiceClient(testClient.Watcher)
	require.NoError(t, err)

	unmanagedResource := trafficpermissions.Resource(constants.DefaultConsulNS, constants.DefaultConsulPartition)
	unmanagedResource.Metadata = make(map[string]string) // Zero out the metadata

	// We haven't run reconcile yet. We must create the resource
	// in Consul ourselves, without the metadata indicating it is owned by the controller.
	{
		req := &pbresource.WriteRequest{Resource: unmanagedResource}
		_, err := resourceClient.Write(ctx, req)
		require.NoError(t, err)
	}

	// Now run reconcile which should **not** update the entry in Consul.
	{
		namespacedName := types.NamespacedName{
			Namespace: metav1.NamespaceDefault,
			Name:      trafficpermissions.KubernetesName(),
		}
		// First get it, so we have the latest revision number.
		err := fakeClient.Get(ctx, namespacedName, trafficpermissions)
		require.NoError(t, err)

		// Attempt to create the entry in Kube and run reconcile.
		reconciler := TrafficPermissionsController{
			Client: fakeClient,
			Log:    logrtest.New(t),
			MeshConfigController: &MeshConfigController{
				ConsulClientConfig:  testClient.Cfg,
				ConsulServerConnMgr: testClient.Watcher,
			},
		}
		resp, err := reconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: namespacedName,
		})
		require.EqualError(t, err, "resource already exists in Consul")
		require.False(t, resp.Requeue)

		// Now check that the object in Consul is as expected.
		req := &pbresource.ReadRequest{Id: trafficpermissions.ResourceID(constants.DefaultConsulNS, constants.DefaultConsulPartition)}
		readResp, err := resourceClient.Read(ctx, req)
		require.NoError(t, err)
		require.NotNil(t, readResp.GetResource())
		opts := append([]cmp.Option{
			protocmp.IgnoreFields(&pbresource.Resource{}, "status", "generation", "version"),
			protocmp.IgnoreFields(&pbresource.ID{}, "uid")},
			test.CmpProtoIgnoreOrder()...)
		diff := cmp.Diff(unmanagedResource, readResp.GetResource(), opts...)
		require.Equal(t, "", diff, "TrafficPermissions do not match")

		// Check that the status is "synced=false".
		err = fakeClient.Get(ctx, namespacedName, trafficpermissions)
		require.NoError(t, err)
		status, reason, errMsg := trafficpermissions.SyncedCondition()
		require.Equal(t, corev1.ConditionFalse, status)
		require.Equal(t, "ExternallyManagedConfigError", reason)
		require.Equal(t, errMsg, "resource already exists in Consul")
	}

}

// TestMeshConfigController_doesNotDeleteUnownedConfig tests that if the resource
// exists in Consul but is not managed by the controller, deleting the resource does
// not delete the Consul resource.
func TestMeshConfigController_doesNotDeleteUnownedConfig(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := runtime.NewScheme()

	trafficpermissionsWithDeletion := &v2beta1.TrafficPermissions{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "foo",
			Namespace:         metav1.NamespaceDefault,
			DeletionTimestamp: &metav1.Time{Time: time.Now()},
			Finalizers:        []string{FinalizerName},
		},
		Spec: v2beta1.TrafficPermissionsSpec{
			Destination: &v2beta1.Destination{
				IdentityName: "destination-identity",
			},
			Action: v2beta1.ActionAllow,
			Permissions: v2beta1.Permissions{
				{
					Sources: v2beta1.Sources{
						{
							IdentityName: "source-identity",
						},
					},
				},
			},
		},
	}
	s.AddKnownTypes(v2beta1.AuthGroupVersion, trafficpermissionsWithDeletion)
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(trafficpermissionsWithDeletion).Build()

	testClient := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
		c.Experiments = []string{"resource-apis"}
	})
	resourceClient, err := consul.NewResourceServiceClient(testClient.Watcher)
	require.NoError(t, err)

	reconciler := &TrafficPermissionsController{
		Client: fakeClient,
		Log:    logrtest.New(t),
		MeshConfigController: &MeshConfigController{
			ConsulClientConfig:  testClient.Cfg,
			ConsulServerConnMgr: testClient.Watcher,
		},
	}

	unmanagedResource := trafficpermissionsWithDeletion.Resource(constants.DefaultConsulNS, constants.DefaultConsulPartition)
	unmanagedResource.Metadata = make(map[string]string) // Zero out the metadata

	// We haven't run reconcile yet. We must create the resource
	// in Consul ourselves, without the metadata indicating it is owned by the controller.
	{
		req := &pbresource.WriteRequest{Resource: unmanagedResource}
		_, err := resourceClient.Write(ctx, req)
		require.NoError(t, err)
	}

	// Now run reconcile. It's marked for deletion so this should delete the kubernetes resource
	// but not the consul config entry.
	{
		namespacedName := types.NamespacedName{
			Namespace: metav1.NamespaceDefault,
			Name:      trafficpermissionsWithDeletion.KubernetesName(),
		}
		resp, err := reconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: namespacedName,
		})
		require.NoError(t, err)
		require.False(t, resp.Requeue)

		// Now check that the object in Consul is as expected.
		req := &pbresource.ReadRequest{Id: trafficpermissionsWithDeletion.ResourceID(constants.DefaultConsulNS, constants.DefaultConsulPartition)}
		readResp, err := resourceClient.Read(ctx, req)
		require.NoError(t, err)
		require.NotNil(t, readResp.GetResource())
		opts := append([]cmp.Option{
			protocmp.IgnoreFields(&pbresource.Resource{}, "status", "generation", "version"),
			protocmp.IgnoreFields(&pbresource.ID{}, "uid")},
			test.CmpProtoIgnoreOrder()...)
		diff := cmp.Diff(unmanagedResource, readResp.GetResource(), opts...)
		require.Equal(t, "", diff, "TrafficPermissions do not match")

		// Check that the resource is deleted from cluster.
		tp := &v2beta1.TrafficPermissions{}
		_ = fakeClient.Get(ctx, namespacedName, tp)
		require.Empty(t, tp.Finalizers())
	}
}
