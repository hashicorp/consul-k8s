// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

//go:build enterprise

package resources

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	logrtest "github.com/go-logr/logr/testr"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/hashicorp/consul-k8s/control-plane/api/auth/v2beta1"
	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"

	pbauth "github.com/hashicorp/consul/proto-public/pbauth/v2beta1"
	"github.com/hashicorp/consul/proto-public/pbresource"
	"github.com/hashicorp/consul/sdk/testutil"
)

// TestConsulResourceController_UpdatesConsulResourceEnt tests is a mirror of the CE test which also tests the
// enterprise traffic permissions deny action.
func TestConsulResourceController_UpdatesConsulResourceEnt(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		resource   common.ConsulResource
		expected   *pbauth.TrafficPermissions
		reconciler func(client.Client, *consul.Config, consul.ServerConnectionManager, logr.Logger) testReconciler
		updateF    func(config common.ConsulResource)
		unmarshal  func(t *testing.T, consul *pbresource.Resource) proto.Message
	}{
		{
			name: "TrafficPermissions",
			resource: &v2beta1.TrafficPermissions{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-traffic-permission",
					Namespace: metav1.NamespaceDefault,
				},
				Spec: pbauth.TrafficPermissions{
					Destination: &pbauth.Destination{
						IdentityName: "destination-identity",
					},
					Action: pbauth.Action_ACTION_ALLOW,
					Permissions: []*pbauth.Permission{
						{
							Sources: []*pbauth.Source{
								{
									Namespace: "the space namespace space",
								},
								{
									IdentityName: "source-identity",
								},
							},
							// TODO: enable this when L7 traffic permissions are supported
							//DestinationRules: []*pbauth.DestinationRule{
							//	{
							//		PathExact: "/hello",
							//		Methods:   []string{"GET", "POST"},
							//		PortNames: []string{"web", "admin"},
							//	},
							//},
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
								Partition: common.DefaultConsulPartition,
								Peer:      constants.DefaultConsulPeer,
							},
						},
						//DestinationRules: []*pbauth.DestinationRule{
						//	{
						//		PathExact: "/hello",
						//		Methods:   []string{"GET", "POST"},
						//		PortNames: []string{"web", "admin"},
						//	},
						//},
					},
				},
			},
			reconciler: func(client client.Client, cfg *consul.Config, watcher consul.ServerConnectionManager, logger logr.Logger) testReconciler {
				return &TrafficPermissionsController{
					Client: client,
					Log:    logger,
					Controller: &ConsulResourceController{
						ConsulClientConfig:  cfg,
						ConsulServerConnMgr: watcher,
					},
				}
			},
			updateF: func(resource common.ConsulResource) {
				trafficPermissions := resource.(*v2beta1.TrafficPermissions)
				trafficPermissions.Spec.Action = pbauth.Action_ACTION_DENY
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
			s.AddKnownTypes(v1alpha1.GroupVersion, c.resource)
			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(c.resource).WithStatusSubresource(c.resource).Build()

			testClient := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
				c.Experiments = []string{"resource-apis"}
			})

			// We haven't run reconcile yet, so we must create the resource
			// in Consul ourselves.
			{
				resource := c.resource.Resource(constants.DefaultConsulNS, constants.DefaultConsulPartition)
				req := &pbresource.WriteRequest{Resource: resource}
				_, err := testClient.ResourceClient.Write(ctx, req)
				require.NoError(t, err)
			}

			// Now run reconcile which should update the entry in Consul.
			{
				namespacedName := types.NamespacedName{
					Namespace: metav1.NamespaceDefault,
					Name:      c.resource.KubernetesName(),
				}
				// First get it, so we have the latest revision number.
				err := fakeClient.Get(ctx, namespacedName, c.resource)
				require.NoError(t, err)

				// Update the entry in Kube and run reconcile.
				c.updateF(c.resource)
				err = fakeClient.Update(ctx, c.resource)
				require.NoError(t, err)
				r := c.reconciler(fakeClient, testClient.Cfg, testClient.Watcher, logrtest.New(t))
				resp, err := r.Reconcile(ctx, ctrl.Request{
					NamespacedName: namespacedName,
				})
				require.NoError(t, err)
				require.False(t, resp.Requeue)

				// Now check that the object in Consul is as expected.
				req := &pbresource.ReadRequest{Id: c.resource.ResourceID(constants.DefaultConsulNS, constants.DefaultConsulPartition)}
				res, err := testClient.ResourceClient.Read(ctx, req)
				require.NoError(t, err)
				require.NotNil(t, res)
				require.Equal(t, c.resource.GetName(), res.GetResource().GetId().GetName())

				actual := c.unmarshal(t, res.GetResource())
				opts := append([]cmp.Option{protocmp.IgnoreFields(&pbresource.Resource{}, "status", "generation", "version")}, test.CmpProtoIgnoreOrder()...)
				diff := cmp.Diff(c.expected, actual, opts...)
				require.Equal(t, "", diff, "TrafficPermissions do not match")
			}
		})
	}
}
