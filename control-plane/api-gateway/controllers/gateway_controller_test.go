package controllers

import (
	"context"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"testing"

	logrtest "github.com/go-logr/logr/testing"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

func TestGatewayReconciler(t *testing.T) {
	t.Parallel()

	namespace := "test-namespace"
	name := "test-gateway"
	gatewayClassName := gwv1beta1.ObjectName("test-gateway-class")

	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: namespace,
			Name:      name,
		},
	}

	cases := map[string]struct {
		gateway            *gwv1beta1.Gateway
		k8sObjects         []runtime.Object
		expectedResult     ctrl.Result
		expectedError      error
		expectedFinalizers []string
		expectedIsDeleted  bool
		expectedConditions []metav1.Condition
	}{
		"successful reconcile with no change": {
			gateway: &gwv1beta1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  namespace,
					Name:       name,
					Finalizers: []string{gatewayFinalizer},
				},
				Spec: gwv1beta1.GatewaySpec{
					GatewayClassName: gatewayClassName,
				},
			},
			k8sObjects: []runtime.Object{
				&gwv1beta1.GatewayClass{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "",
						Name:      string(gatewayClassName),
						Finalizers: []string{
							gatewayClassFinalizer,
						},
					},
					Spec: gwv1beta1.GatewayClassSpec{
						ControllerName: GatewayClassControllerName,
					},
				},
			},
			expectedResult:     ctrl.Result{},
			expectedError:      nil,
			expectedFinalizers: []string{gatewayFinalizer},
			expectedIsDeleted:  false,
			expectedConditions: []metav1.Condition{
				{
					Type:    accepted,
					Status:  metav1.ConditionTrue,
					Reason:  configurationAccepted,
					Message: "Configuration accepted",
				},
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s := runtime.NewScheme()
			require.NoError(t, gwv1beta1.Install(s))
			require.NoError(t, v1alpha1.AddToScheme(s))

			objs := tc.k8sObjects
			if tc.gateway != nil {
				objs = append(objs, tc.gateway)
			}

			fakeClient := registerFieldIndexersForTest(fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(objs...)).Build()

			r := &GatewayController{
				Client: fakeClient,
				Log:    logrtest.NewTestLogger(t),
			}
			result, err := r.Reconcile(context.Background(), req)

			require.Equal(t, tc.expectedResult, result)
			require.Equal(t, tc.expectedError, err)

			// TODO: Add back with real implementation of reconcile
			//// Check the GatewayClass after reconciliation.
			//g := &gwv1beta1.Gateway{}
			//err = r.Client.Get(context.Background(), req.NamespacedName, g)
			//
			//require.NoError(t, client.IgnoreNotFound(err))
			//require.Equal(t, tc.expectedFinalizers, g.ObjectMeta.Finalizers)
			//require.Equal(t, len(tc.expectedConditions), len(g.Status.Conditions), "expected %+v, got %+v", tc.expectedConditions, g.Status.Conditions)
			//for i, expectedCondition := range tc.expectedConditions {
			//	require.True(t, equalConditions(expectedCondition, g.Status.Conditions[i]), "expected %+v, got %+v", expectedCondition, g.Status.Conditions[i])
			//}
		})
	}
}
func TestObjectsToRequests(t *testing.T) {
	t.Parallel()

	namespace := "test-namespace"
	name := "test-gatewayclass"

	namespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}

	cases := map[string]struct {
		objects        []metav1.Object
		expectedResult []reconcile.Request
	}{
		"successful conversion of gateway to request": {
			objects: []metav1.Object{
				&gwv1beta1.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      name,
					},
					Spec: gwv1beta1.GatewaySpec{
						GatewayClassName: GatewayClassControllerName,
					},
				},
			},
			expectedResult: []reconcile.Request{
				{
					NamespacedName: namespacedName,
				},
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			requests := objectsToRequests(tc.objects)

			require.Equal(t, tc.expectedResult, requests)
		})
	}
}
