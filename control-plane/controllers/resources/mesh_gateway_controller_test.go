// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package resources

import (
	"context"
	"testing"

	logrtest "github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	pbmesh "github.com/hashicorp/consul/proto-public/pbmesh/v2beta1"
	"github.com/hashicorp/consul/sdk/testutil"

	"github.com/hashicorp/consul-k8s/control-plane/api/mesh/v2beta1"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
)

func TestMeshGatewayController_Reconcile(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		// k8sObjects is the list of Kubernetes resources that will be present in the cluster at runtime
		k8sObjects []client.Object
		// request is the request that will be provided to MeshGatewayController.Reconcile
		request ctrl.Request
		// expectedErr is the error we expect MeshGatewayController.Reconcile to return
		expectedErr error
		// expectedResult is the result we expect MeshGatewayController.Reconcile to return
		expectedResult ctrl.Result
		// postReconcile runs some set of assertions on the state of k8s after Reconcile is called
		postReconcile func(*testing.T, client.Client)
	}{
		// ServiceAccount
		{
			name: "MeshGateway created with no existing ServiceAccount",
			k8sObjects: []client.Object{
				&v2beta1.MeshGateway{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "consul",
						Name:      "mesh-gateway",
					},
					Spec: pbmesh.MeshGateway{
						GatewayClassName: "consul",
						Listeners: []*pbmesh.MeshGatewayListener{
							{
								Name:     "wan",
								Port:     8443,
								Protocol: "tcp",
							},
						},
					},
				},
			},
			request: ctrl.Request{
				NamespacedName: types.NamespacedName{
					Namespace: "consul",
					Name:      "mesh-gateway",
				},
			},
			expectedResult: ctrl.Result{},
			postReconcile: func(t *testing.T, c client.Client) {
				// Verify ServiceAccount was created
				key := client.ObjectKey{Namespace: "consul", Name: "mesh-gateway"}
				assert.NoError(t, c.Get(context.Background(), key, &corev1.ServiceAccount{}))
			},
		},
		{
			name: "MeshGateway created with existing ServiceAccount not owned by gateway",
			k8sObjects: []client.Object{
				&v2beta1.MeshGateway{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "mesh-gateway",
					},
					Spec: pbmesh.MeshGateway{
						GatewayClassName: "consul",
						Listeners: []*pbmesh.MeshGatewayListener{
							{
								Name:     "wan",
								Port:     8443,
								Protocol: "tcp",
							},
						},
					},
				},
				&corev1.ServiceAccount{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "mesh-gateway",
					},
				},
			},
			request: ctrl.Request{
				NamespacedName: types.NamespacedName{
					Namespace: "default",
					Name:      "mesh-gateway",
				},
			},
			expectedResult: ctrl.Result{},
			expectedErr:    errResourceNotOwned,
		},
		// Role
		{
			name: "MeshGateway created with no existing Role",
			k8sObjects: []client.Object{
				&v2beta1.MeshGateway{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "consul",
						Name:      "mesh-gateway",
					},
					Spec: pbmesh.MeshGateway{
						GatewayClassName: "consul",
						Listeners: []*pbmesh.MeshGatewayListener{
							{
								Name:     "wan",
								Port:     8443,
								Protocol: "tcp",
							},
						},
					},
				},
			},
			request: ctrl.Request{
				NamespacedName: types.NamespacedName{
					Namespace: "consul",
					Name:      "mesh-gateway",
				},
			},
			expectedResult: ctrl.Result{},
			postReconcile: func(t *testing.T, c client.Client) {
				// Verify Role was created
				key := client.ObjectKey{Namespace: "consul", Name: "mesh-gateway"}
				assert.NoError(t, c.Get(context.Background(), key, &rbacv1.Role{}))
			},
		},
		{
			name: "MeshGateway created with existing Role not owned by gateway",
			k8sObjects: []client.Object{
				&v2beta1.MeshGateway{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "mesh-gateway",
					},
					Spec: pbmesh.MeshGateway{
						GatewayClassName: "consul",
						Listeners: []*pbmesh.MeshGatewayListener{
							{
								Name:     "wan",
								Port:     8443,
								Protocol: "tcp",
							},
						},
					},
				},
				&rbacv1.Role{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "mesh-gateway",
					},
				},
			},
			request: ctrl.Request{
				NamespacedName: types.NamespacedName{
					Namespace: "default",
					Name:      "mesh-gateway",
				},
			},
			expectedResult: ctrl.Result{},
			expectedErr:    errResourceNotOwned,
		},
		{
			name: "MeshGateway created with existing Role owned by gateway",
			k8sObjects: []client.Object{
				&v2beta1.MeshGateway{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "mesh-gateway",
						UID:       "abc123",
					},
					Spec: pbmesh.MeshGateway{
						GatewayClassName: "consul",
						Listeners: []*pbmesh.MeshGatewayListener{
							{
								Name:     "wan",
								Port:     8443,
								Protocol: "tcp",
							},
						},
					},
				},
				&rbacv1.Role{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "mesh-gateway",
						OwnerReferences: []metav1.OwnerReference{
							{
								UID:  "abc123",
								Name: "mesh-gateway",
							},
						},
					},
				},
			},
			request: ctrl.Request{
				NamespacedName: types.NamespacedName{
					Namespace: "default",
					Name:      "mesh-gateway",
				},
			},
			expectedResult: ctrl.Result{},
			expectedErr:    nil, // The Reconcile should be a no-op
		},
		// RoleBinding
		{
			name: "MeshGateway created with no existing RoleBinding",
			k8sObjects: []client.Object{
				&v2beta1.MeshGateway{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "consul",
						Name:      "mesh-gateway",
					},
					Spec: pbmesh.MeshGateway{
						GatewayClassName: "consul",
						Listeners: []*pbmesh.MeshGatewayListener{
							{
								Name:     "wan",
								Port:     8443,
								Protocol: "tcp",
							},
						},
					},
				},
			},
			request: ctrl.Request{
				NamespacedName: types.NamespacedName{
					Namespace: "consul",
					Name:      "mesh-gateway",
				},
			},
			expectedResult: ctrl.Result{},
			postReconcile: func(t *testing.T, c client.Client) {
				// Verify RoleBinding was created
				key := client.ObjectKey{Namespace: "consul", Name: "mesh-gateway"}
				assert.NoError(t, c.Get(context.Background(), key, &rbacv1.RoleBinding{}))
			},
		},
		{
			name: "MeshGateway created with existing RoleBinding not owned by gateway",
			k8sObjects: []client.Object{
				&v2beta1.MeshGateway{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "mesh-gateway",
					},
					Spec: pbmesh.MeshGateway{
						GatewayClassName: "consul",
						Listeners: []*pbmesh.MeshGatewayListener{
							{
								Name:     "wan",
								Port:     8443,
								Protocol: "tcp",
							},
						},
					},
				},
				&rbacv1.RoleBinding{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "mesh-gateway",
					},
				},
			},
			request: ctrl.Request{
				NamespacedName: types.NamespacedName{
					Namespace: "default",
					Name:      "mesh-gateway",
				},
			},
			expectedResult: ctrl.Result{},
			expectedErr:    errResourceNotOwned,
		},
		{
			name: "MeshGateway created with existing RoleBinding owned by gateway",
			k8sObjects: []client.Object{
				&v2beta1.MeshGateway{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "mesh-gateway",
						UID:       "abc123",
					},
					Spec: pbmesh.MeshGateway{
						GatewayClassName: "consul",
						Listeners: []*pbmesh.MeshGatewayListener{
							{
								Name:     "wan",
								Port:     8443,
								Protocol: "tcp",
							},
						},
					},
				},
				&rbacv1.RoleBinding{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "mesh-gateway",
						OwnerReferences: []metav1.OwnerReference{
							{
								UID:  "abc123",
								Name: "mesh-gateway",
							},
						},
					},
				},
			},
			request: ctrl.Request{
				NamespacedName: types.NamespacedName{
					Namespace: "default",
					Name:      "mesh-gateway",
				},
			},
			expectedResult: ctrl.Result{},
			expectedErr:    nil, // The Reconcile should be a no-op
		},
		// Deployment
		{
			name: "MeshGateway created with no existing Deployment",
			k8sObjects: []client.Object{
				&v2beta1.MeshGateway{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "consul",
						Name:      "mesh-gateway",
					},
					Spec: pbmesh.MeshGateway{
						GatewayClassName: "consul",
						Listeners: []*pbmesh.MeshGatewayListener{
							{
								Name:     "wan",
								Port:     8443,
								Protocol: "tcp",
							},
						},
					},
				},
			},
			request: ctrl.Request{
				NamespacedName: types.NamespacedName{
					Namespace: "consul",
					Name:      "mesh-gateway",
				},
			},
			expectedResult: ctrl.Result{},
			postReconcile: func(t *testing.T, c client.Client) {
				// Verify Deployment was created
				key := client.ObjectKey{Namespace: "consul", Name: "mesh-gateway"}
				assert.NoError(t, c.Get(context.Background(), key, &appsv1.Deployment{}))
			},
		},
		{
			name: "MeshGateway created with existing Deployment not owned by gateway",
			k8sObjects: []client.Object{
				&v2beta1.MeshGateway{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "mesh-gateway",
					},
					Spec: pbmesh.MeshGateway{
						GatewayClassName: "consul",
						Listeners: []*pbmesh.MeshGatewayListener{
							{
								Name:     "wan",
								Port:     8443,
								Protocol: "tcp",
							},
						},
					},
				},
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "mesh-gateway",
					},
				},
			},
			request: ctrl.Request{
				NamespacedName: types.NamespacedName{
					Namespace: "default",
					Name:      "mesh-gateway",
				},
			},
			expectedResult: ctrl.Result{},
			expectedErr:    errResourceNotOwned,
		},
		{
			name: "MeshGateway created with existing Deployment owned by gateway",
			k8sObjects: []client.Object{
				&v2beta1.MeshGateway{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "mesh-gateway",
						UID:       "abc123",
					},
					Spec: pbmesh.MeshGateway{
						GatewayClassName: "consul",
						Listeners: []*pbmesh.MeshGatewayListener{
							{
								Name:     "wan",
								Port:     8443,
								Protocol: "tcp",
							},
						},
					},
				},
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "mesh-gateway",
						OwnerReferences: []metav1.OwnerReference{
							{
								UID:  "abc123",
								Name: "mesh-gateway",
							},
						},
					},
				},
			},
			request: ctrl.Request{
				NamespacedName: types.NamespacedName{
					Namespace: "default",
					Name:      "mesh-gateway",
				},
			},
			expectedResult: ctrl.Result{},
			expectedErr:    nil, // The Reconcile should be a no-op
		},
		// Service
		{
			name: "MeshGateway created with no existing Service",
			k8sObjects: []client.Object{
				&v2beta1.MeshGateway{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "consul",
						Name:      "mesh-gateway",
					},
					Spec: pbmesh.MeshGateway{
						GatewayClassName: "consul",
						Listeners: []*pbmesh.MeshGatewayListener{
							{
								Name:     "wan",
								Port:     8443,
								Protocol: "tcp",
							},
						},
					},
				},
			},
			request: ctrl.Request{
				NamespacedName: types.NamespacedName{
					Namespace: "consul",
					Name:      "mesh-gateway",
				},
			},
			expectedResult: ctrl.Result{},
			postReconcile: func(t *testing.T, c client.Client) {
				// Verify Service was created
				key := client.ObjectKey{Namespace: "consul", Name: "mesh-gateway"}
				assert.NoError(t, c.Get(context.Background(), key, &corev1.Service{}))
			},
		},
		{
			name: "MeshGateway created with existing Service not owned by gateway",
			k8sObjects: []client.Object{
				&v2beta1.MeshGateway{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "mesh-gateway",
					},
					Spec: pbmesh.MeshGateway{
						GatewayClassName: "consul",
						Listeners: []*pbmesh.MeshGatewayListener{
							{
								Name:     "wan",
								Port:     8443,
								Protocol: "tcp",
							},
						},
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "mesh-gateway",
					},
				},
			},
			request: ctrl.Request{
				NamespacedName: types.NamespacedName{
					Namespace: "default",
					Name:      "mesh-gateway",
				},
			},
			expectedResult: ctrl.Result{},
			expectedErr:    errResourceNotOwned,
		},
		{
			name: "MeshGateway created with existing Service owned by gateway",
			k8sObjects: []client.Object{
				&v2beta1.MeshGateway{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "mesh-gateway",
						UID:       "abc123",
					},
					Spec: pbmesh.MeshGateway{
						GatewayClassName: "consul",
						Listeners: []*pbmesh.MeshGatewayListener{
							{
								Name:     "wan",
								Port:     8443,
								Protocol: "tcp",
							},
						},
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "mesh-gateway",
						OwnerReferences: []metav1.OwnerReference{
							{
								UID:  "abc123",
								Name: "mesh-gateway",
							},
						},
					},
				},
			},
			request: ctrl.Request{
				NamespacedName: types.NamespacedName{
					Namespace: "default",
					Name:      "mesh-gateway",
				},
			},
			expectedResult: ctrl.Result{},
			expectedErr:    nil, // The Reconcile should be a no-op
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			consulClient := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
				c.Experiments = []string{"resource-apis"}
			})

			s := runtime.NewScheme()
			require.NoError(t, corev1.AddToScheme(s))
			require.NoError(t, appsv1.AddToScheme(s))
			require.NoError(t, rbacv1.AddToScheme(s))
			require.NoError(t, v2beta1.AddMeshToScheme(s))
			s.AddKnownTypes(v2beta1.MeshGroupVersion, &v2beta1.MeshGateway{}, &v2beta1.GatewayClass{}, &v2beta1.GatewayClassConfig{})
			fakeClient := fake.NewClientBuilder().WithScheme(s).
				WithObjects(testCase.k8sObjects...).
				WithStatusSubresource(testCase.k8sObjects...).
				Build()

			controller := MeshGatewayController{
				Client: fakeClient,
				Log:    logrtest.New(t),
				Scheme: s,
				Controller: &ConsulResourceController{
					ConsulClientConfig:  consulClient.Cfg,
					ConsulServerConnMgr: consulClient.Watcher,
				},
			}

			res, err := controller.Reconcile(context.Background(), testCase.request)
			if testCase.expectedErr != nil {
				// require.EqualError(t, err, testCase.expectedErr.Error())
				require.ErrorIs(t, err, testCase.expectedErr)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, testCase.expectedResult, res)

			if testCase.postReconcile != nil {
				testCase.postReconcile(t, fakeClient)
			}
		})
	}
}
