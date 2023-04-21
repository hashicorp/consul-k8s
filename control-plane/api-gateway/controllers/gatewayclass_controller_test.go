package controllers

import (
	"context"
	"testing"
	"time"

	logrtest "github.com/go-logr/logr/testing"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/gateway-api/apis/v1beta1"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

func TestGatewayClassReconciler(t *testing.T) {
	t.Parallel()

	namespace := "" // GatewayClass is cluster-scoped.
	name := "test-gatewayclass"

	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: namespace,
			Name:      name,
		},
	}

	deletionTimestamp := metav1.Now()

	cases := map[string]struct {
		gatewayClass       *gwv1beta1.GatewayClass
		k8sObjects         []runtime.Object
		expectedResult     ctrl.Result
		expectedError      error
		expectedFinalizers []string
	}{
		"successful reconcile with no change": {
			gatewayClass: &gwv1beta1.GatewayClass{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      name,
					Finalizers: []string{
						gatewayClassFinalizer,
					},
				},
				Spec: gwv1beta1.GatewayClassSpec{
					ControllerName: GatewayClassControllerName,
				},
			},

			expectedResult:     ctrl.Result{},
			expectedError:      nil,
			expectedFinalizers: []string{gatewayClassFinalizer},
		},
		"successful reconcile that adds finalizer": {
			gatewayClass: &gwv1beta1.GatewayClass{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  namespace,
					Name:       name,
					Finalizers: []string{},
				},
				Spec: gwv1beta1.GatewayClassSpec{
					ControllerName: GatewayClassControllerName,
				},
			},
			expectedResult:     ctrl.Result{Requeue: true},
			expectedError:      nil,
			expectedFinalizers: []string{gatewayClassFinalizer},
		},
		"attempt to reconcile a GatewayClass with a different controller name": {
			gatewayClass: &gwv1beta1.GatewayClass{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  namespace,
					Name:       name,
					Finalizers: []string{},
				},
				Spec: gwv1beta1.GatewayClassSpec{
					ControllerName: "foo",
				},
			},
			expectedResult: ctrl.Result{},
			expectedError:  nil,
		},
		"attempt to reconcile a non-existent object": {
			k8sObjects:     []runtime.Object{},
			expectedResult: ctrl.Result{},
			expectedError:  nil,
		},
		"attempt to remove a GatewayClass that is not in use": {
			gatewayClass: &gwv1beta1.GatewayClass{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      name,
					Finalizers: []string{
						gatewayClassFinalizer,
					},
					DeletionTimestamp: &deletionTimestamp,
				},
				Spec: gwv1beta1.GatewayClassSpec{
					ControllerName: GatewayClassControllerName,
				},
			},
			expectedResult:     ctrl.Result{},
			expectedError:      nil,
			expectedFinalizers: []string{},
		},
		"attempt to remove a GatewayClass that is in use": {
			gatewayClass: &gwv1beta1.GatewayClass{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      name,
					Finalizers: []string{
						gatewayClassFinalizer,
					},
					DeletionTimestamp: &deletionTimestamp,
				},
				Spec: gwv1beta1.GatewayClassSpec{
					ControllerName: GatewayClassControllerName,
				},
			},
			k8sObjects: []runtime.Object{
				&gwv1beta1.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "test-gateway",
					},
					Spec: gwv1beta1.GatewaySpec{
						GatewayClassName: v1beta1.ObjectName(name),
					},
				},
			},
			expectedResult:     ctrl.Result{RequeueAfter: 10 * time.Second},
			expectedError:      nil,
			expectedFinalizers: []string{gatewayClassFinalizer},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s := runtime.NewScheme()
			require.NoError(t, gwv1beta1.Install(s))
			require.NoError(t, v1alpha1.AddToScheme(s))

			objs := tc.k8sObjects
			if tc.gatewayClass != nil {
				objs = append(objs, tc.gatewayClass)
			}

			fakeClient := registerFieldIndexersForTest(fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(objs...)).Build()

			r := &GatewayClassController{
				Client:         fakeClient,
				ControllerName: GatewayClassControllerName,
				Log:            logrtest.NewTestLogger(t),
			}
			result, err := r.Reconcile(context.Background(), req)

			require.Equal(t, tc.expectedResult, result)
			require.Equal(t, tc.expectedError, err)

			if tc.gatewayClass != nil {
				gc := &gwv1beta1.GatewayClass{}
				err := r.Client.Get(context.Background(), req.NamespacedName, gc)
				require.NoError(t, client.IgnoreNotFound(err))

				if err == nil { // This skips the "not found case".
					require.Equal(t, tc.expectedFinalizers, gc.ObjectMeta.Finalizers)
				}
			}
		})
	}
}
