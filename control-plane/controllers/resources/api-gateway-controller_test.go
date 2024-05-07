// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package resources

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	pbmesh "github.com/hashicorp/consul/proto-public/pbmesh/v2beta1"
	"github.com/hashicorp/consul/proto-public/pbresource"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/testing/protocmp"

	"github.com/hashicorp/consul-k8s/control-plane/api/mesh/v2beta1"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"

	logrtest "github.com/go-logr/logr/testr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestAPIGatewayController_ReconcileResourceExists(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	s := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(s))
	require.NoError(t, appsv1.AddToScheme(s))
	require.NoError(t, rbacv1.AddToScheme(s))
	require.NoError(t, v2beta1.AddMeshToScheme(s))
	s.AddKnownTypes(
		schema.GroupVersion{
			Group:   "mesh.consul.hashicorp.com",
			Version: pbmesh.Version,
		},
		&v2beta1.APIGateway{},
		&v2beta1.GatewayClass{},
		&v2beta1.GatewayClassConfig{},
	)

	apiGW := &v2beta1.APIGateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "api-gateway",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: pbmesh.APIGateway{
			GatewayClassName: "consul",
			Listeners: []*pbmesh.APIGatewayListener{
				{
					Name:     "http-listener",
					Port:     9090,
					Protocol: "http",
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(apiGW).WithStatusSubresource(apiGW).Build()

	testClient := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
		c.Experiments = []string{"resource-apis"}
	})

	gwCtrl := APIGatewayController{
		Client: fakeClient,
		Log:    logrtest.New(t),
		Scheme: s,
		Controller: &ConsulResourceController{
			ConsulClientConfig:  testClient.Cfg,
			ConsulServerConnMgr: testClient.Watcher,
		},
	}

	// ensure the resource is not in consul yet
	{
		req := &pbresource.ReadRequest{Id: apiGW.ResourceID(constants.DefaultConsulNS, constants.DefaultConsulPartition)}
		_, err := testClient.ResourceClient.Read(ctx, req)
		require.Error(t, err)
	}

	// now reconcile the resource
	{
		namespacedName := types.NamespacedName{
			Namespace: metav1.NamespaceDefault,
			Name:      apiGW.KubernetesName(),
		}

		// First get it, so we have the latest revision number.
		err := fakeClient.Get(ctx, namespacedName, apiGW)
		require.NoError(t, err)

		resp, err := gwCtrl.Reconcile(ctx, ctrl.Request{
			NamespacedName: namespacedName,
		})

		require.NoError(t, err)
		require.False(t, resp.Requeue)
	}

	// now check that the object in Consul is as expected.
	{
		expectedResource := &pbmesh.APIGateway{
			GatewayClassName: "consul",
			Listeners: []*pbmesh.APIGatewayListener{
				{
					Name:     "http-listener",
					Port:     9090,
					Protocol: "http",
				},
			},
		}
		req := &pbresource.ReadRequest{Id: apiGW.ResourceID(constants.DefaultConsulNS, constants.DefaultConsulPartition)}
		res, err := testClient.ResourceClient.Read(ctx, req)
		require.NoError(t, err)
		require.NotNil(t, res)
		require.Equal(t, apiGW.GetName(), res.GetResource().GetId().GetName())

		data := res.GetResource().Data
		actual := &pbmesh.APIGateway{}
		require.NoError(t, data.UnmarshalTo(actual))

		opts := append([]cmp.Option{protocmp.IgnoreFields(&pbresource.Resource{}, "status", "generation", "version")}, test.CmpProtoIgnoreOrder()...)
		diff := cmp.Diff(expectedResource, actual, opts...)
		require.Equal(t, "", diff, "APIGateway does not match")
	}
}

func TestAPIGatewayController_ReconcileAPIGWDoesNotExistInK8s(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	s := runtime.NewScheme()
	s.AddKnownTypes(schema.GroupVersion{
		Group:   "mesh.consul.hashicorp.com",
		Version: pbmesh.Version,
	}, &v2beta1.APIGateway{}, &v2beta1.APIGatewayList{})

	fakeClient := fake.NewClientBuilder().WithScheme(s).Build()

	testClient := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
		c.Experiments = []string{"resource-apis"}
	})

	gwCtrl := APIGatewayController{
		Client: fakeClient,
		Log:    logrtest.New(t),
		Scheme: s,
		Controller: &ConsulResourceController{
			ConsulClientConfig:  testClient.Cfg,
			ConsulServerConnMgr: testClient.Watcher,
		},
	}

	// now reconcile the resource
	{
		namespacedName := types.NamespacedName{
			Namespace: metav1.NamespaceDefault,
			Name:      "api-gateway",
		}

		resp, err := gwCtrl.Reconcile(ctx, ctrl.Request{
			NamespacedName: namespacedName,
		})

		require.NoError(t, err)
		require.False(t, resp.Requeue)
		require.Equal(t, ctrl.Result{}, resp)
	}

	// ensure the resource is not in consul
	{
		req := &pbresource.ReadRequest{Id: &pbresource.ID{
			Name: "api-gateway",
			Type: pbmesh.APIGatewayType,
			Tenancy: &pbresource.Tenancy{
				Namespace: constants.DefaultConsulNS,
				Partition: constants.DefaultConsulPartition,
			},
		}}

		_, err := testClient.ResourceClient.Read(ctx, req)
		require.Error(t, err)
	}
}
