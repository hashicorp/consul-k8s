// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package controllers

import (
	"context"
	"fmt"
	"testing"

	logrtest "github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gwv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	"sigs.k8s.io/gateway-api/apis/v1beta1"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
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
		expectedIsDeleted  bool
		expectedConditions []metav1.Condition
	}{
		"successful reconcile with no change": {
			gatewayClass: &gwv1beta1.GatewayClass{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  namespace,
					Name:       name,
					Finalizers: []string{gatewayClassFinalizer},
				},
				Spec: gwv1beta1.GatewayClassSpec{
					ControllerName: common.GatewayClassControllerName,
				},
			},
			expectedResult:     ctrl.Result{},
			expectedError:      nil,
			expectedFinalizers: []string{gatewayClassFinalizer},
			expectedIsDeleted:  false,
			expectedConditions: []metav1.Condition{
				{
					Type:    accepted,
					Status:  metav1.ConditionTrue,
					Reason:  accepted,
					Message: "GatewayClass Accepted",
				},
			},
		},
		"successful reconcile that adds finalizer": {
			gatewayClass: &gwv1beta1.GatewayClass{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  namespace,
					Name:       name,
					Finalizers: []string{},
				},
				Spec: gwv1beta1.GatewayClassSpec{
					ControllerName: common.GatewayClassControllerName,
				},
			},
			expectedResult:     ctrl.Result{},
			expectedError:      nil,
			expectedFinalizers: []string{gatewayClassFinalizer},
			expectedConditions: []metav1.Condition{},
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
			expectedResult:     ctrl.Result{},
			expectedError:      nil,
			expectedConditions: []metav1.Condition{},
		},
		"attempt to reconcile a GatewayClass with a different controller name removing our finalizer": {
			gatewayClass: &gwv1beta1.GatewayClass{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  namespace,
					Name:       name,
					Finalizers: []string{gatewayClassFinalizer},
				},
				Spec: gwv1beta1.GatewayClassSpec{
					ControllerName: "foo",
				},
			},
			expectedResult:     ctrl.Result{},
			expectedError:      nil,
			expectedConditions: []metav1.Condition{},
		},
		"attempt to reconcile a GatewayClass with an incorrect parametersRef type": {
			gatewayClass: &gwv1beta1.GatewayClass{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  namespace,
					Name:       name,
					Finalizers: []string{gatewayClassFinalizer},
				},
				Spec: gwv1beta1.GatewayClassSpec{
					ControllerName: common.GatewayClassControllerName,
					ParametersRef: &gwv1beta1.ParametersReference{
						Kind: "some-nonsense",
					},
				},
			},
			expectedResult:     ctrl.Result{},
			expectedError:      nil,
			expectedFinalizers: []string{gatewayClassFinalizer},
			expectedConditions: []metav1.Condition{
				{
					Type:    accepted,
					Status:  metav1.ConditionFalse,
					Reason:  invalidParameters,
					Message: fmt.Sprintf("Incorrect type for parametersRef. Expected GatewayClassConfig, got %q.", "some-nonsense"),
				},
			},
		},
		"attempt to reconcile a GatewayClass with a GatewayClassConfig that does not exist": {
			gatewayClass: &gwv1beta1.GatewayClass{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  namespace,
					Name:       name,
					Finalizers: []string{gatewayClassFinalizer},
				},
				Spec: gwv1beta1.GatewayClassSpec{
					ControllerName: common.GatewayClassControllerName,
					ParametersRef: &gwv1beta1.ParametersReference{
						Kind: v1alpha1.GatewayClassConfigKind,
						Name: "does-not-exist",
					},
				},
			},
			expectedResult:     ctrl.Result{},
			expectedError:      nil,
			expectedFinalizers: []string{gatewayClassFinalizer},
			expectedConditions: []metav1.Condition{
				{
					Type:    accepted,
					Status:  metav1.ConditionFalse,
					Reason:  invalidParameters,
					Message: fmt.Sprintf("GatewayClassConfig not found %q.", "does-not-exist"),
				},
			},
		},
		"attempt to reconcile a non-existent object": {
			k8sObjects:         []runtime.Object{},
			expectedResult:     ctrl.Result{},
			expectedError:      nil,
			expectedConditions: []metav1.Condition{},
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
					ControllerName: common.GatewayClassControllerName,
				},
			},
			expectedResult:     ctrl.Result{},
			expectedError:      nil,
			expectedFinalizers: []string{},
			expectedIsDeleted:  true,
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
					ControllerName: common.GatewayClassControllerName,
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
			expectedResult:     ctrl.Result{},
			expectedError:      nil,
			expectedFinalizers: []string{gatewayClassFinalizer},
		},
		// */
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s := runtime.NewScheme()
			require.NoError(t, clientgoscheme.AddToScheme(s))
			require.NoError(t, gwv1alpha2.Install(s))
			require.NoError(t, gwv1beta1.Install(s))
			require.NoError(t, v1alpha1.AddToScheme(s))

			objs := tc.k8sObjects
			if tc.gatewayClass != nil {
				objs = append(objs, tc.gatewayClass)
			}

			fakeClient := registerFieldIndexersForTest(
				fake.NewClientBuilder().WithScheme(s).
					WithRuntimeObjects(objs...).
					WithStatusSubresource(&gwv1beta1.GatewayClass{})).Build()

			r := &GatewayClassController{
				Client:         fakeClient,
				ControllerName: common.GatewayClassControllerName,
				Log:            logrtest.New(t),
			}
			result, err := r.Reconcile(context.Background(), req)

			require.Equal(t, tc.expectedResult, result)
			require.Equal(t, tc.expectedError, err)

			// Check the GatewayClass after reconciliation.
			gc := &gwv1beta1.GatewayClass{}
			err = r.Client.Get(context.Background(), req.NamespacedName, gc)

			if tc.gatewayClass == nil || tc.expectedIsDeleted {
				// There shouldn't be a GatewayClass to check.
				require.True(t, apierrors.IsNotFound(err))
				return
			}

			require.NoError(t, client.IgnoreNotFound(err))
			require.Equal(t, tc.expectedFinalizers, gc.ObjectMeta.Finalizers)
			require.Equal(t, len(tc.expectedConditions), len(gc.Status.Conditions), "expected %+v, got %+v", tc.expectedConditions, gc.Status.Conditions)
			for i, expectedCondition := range tc.expectedConditions {
				require.True(t, equalConditions(expectedCondition, gc.Status.Conditions[i]), "expected %+v, got %+v", expectedCondition, gc.Status.Conditions[i])
			}
		})
	}
}
