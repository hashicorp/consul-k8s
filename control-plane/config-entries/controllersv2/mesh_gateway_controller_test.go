package controllersv2

import (
	"context"
	"errors"
	"testing"
	"time"

	logrtest "github.com/go-logr/logr/testr"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
	"github.com/hashicorp/consul-k8s/control-plane/api/mesh/v2beta1"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
)

func TestMeshGatewayController_Reconcile(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		// k8sObjects is the list of Kubernetes resources that will be present in the cluster at runtime
		k8sObjects []runtime.Object
		// request is the request that will be provided to MeshGatewayController.Reconcile
		request ctrl.Request
		// expectedErr is the error we expect MeshGatewayController.Reconcile to return
		expectedErr error
		// expectedResult is the result we expect MeshGatewayController.Reconcile to return
		expectedResult ctrl.Result
		// postReconcile runs some set of assertions on the state of k8s after Reconcile is called
		postReconcile func(*testing.T, client.Client)
	}{
		{
			name: "MeshGateway created with no existing ServiceAccount",
			k8sObjects: []runtime.Object{
				&v2beta1.MeshGateway{
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
			postReconcile: func(t *testing.T, c client.Client) {
				// Verify ServiceAccount was created
				key := client.ObjectKey{Namespace: "default", Name: "mesh-gateway"}
				assert.NoError(t, c.Get(context.Background(), key, &corev1.ServiceAccount{}))
			},
		},
		{
			name: "MeshGateway created with existing ServiceAccount not owned by gateway",
			k8sObjects: []runtime.Object{
				&v2beta1.MeshGateway{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "mesh-gateway",
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
			expectedErr:    errors.New("existing resource not owned by controller"),
		},
		{
			name: "MeshGateway created with existing ServiceAccount owned by gateway",
			k8sObjects: []runtime.Object{
				&v2beta1.MeshGateway{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "mesh-gateway",
						UID:       "abc123",
					},
				},
				&corev1.ServiceAccount{
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
		{
			name: "MeshGateway deleted with existing ServiceAccount not owned by gateway",
			k8sObjects: []runtime.Object{
				&v2beta1.MeshGateway{
					ObjectMeta: metav1.ObjectMeta{
						Namespace:         "default",
						Name:              "mesh-gateway",
						DeletionTimestamp: common.PointerTo(metav1.NewTime(time.Now())),
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
			expectedErr:    errors.New("existing resource not owned by controller"),
			postReconcile: func(t *testing.T, c client.Client) {
				// Verify ServiceAccount was not deleted
				key := client.ObjectKey{Namespace: "default", Name: "mesh-gateway"}
				assert.NoError(t, c.Get(context.Background(), key, &corev1.ServiceAccount{}))
			},
		},
		{
			name: "MeshGateway deleted with existing ServiceAccount owned by gateway",
			k8sObjects: []runtime.Object{
				&v2beta1.MeshGateway{
					ObjectMeta: metav1.ObjectMeta{
						Namespace:         "default",
						Name:              "mesh-gateway",
						UID:               "abc123",
						DeletionTimestamp: common.PointerTo(metav1.NewTime(time.Now())),
					},
				},
				&corev1.ServiceAccount{
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
			postReconcile: func(t *testing.T, c client.Client) {
				// Verify ServiceAccount was deleted
				key := client.ObjectKey{Namespace: "default", Name: "mesh-gateway"}
				err := c.Get(context.Background(), key, &corev1.ServiceAccount{})
				assert.True(t, k8serr.IsNotFound(err))
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			consulClient := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
				c.Experiments = []string{"resource-apis"}
			})

			s := runtime.NewScheme()
			require.NoError(t, corev1.AddToScheme(s))
			s.AddKnownTypes(v2beta1.MeshGroupVersion, &v2beta1.MeshGateway{})
			fakeClient := fake.NewClientBuilder().WithScheme(s).
				WithRuntimeObjects(testCase.k8sObjects...).
				Build()

			controller := MeshGatewayController{
				Client: fakeClient,
				Log:    logrtest.New(t),
				Scheme: s,
				MeshConfigController: &MeshConfigController{
					ConsulClientConfig:  consulClient.Cfg,
					ConsulServerConnMgr: consulClient.Watcher,
				},
			}

			res, err := controller.Reconcile(context.Background(), testCase.request)
			if testCase.expectedErr != nil {
				require.EqualError(t, err, testCase.expectedErr.Error())
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
