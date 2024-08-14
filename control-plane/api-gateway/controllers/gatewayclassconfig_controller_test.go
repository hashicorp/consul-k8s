// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package controllers

import (
	"context"
	"testing"
	"time"

	logrtest "github.com/go-logr/logr/testr"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/stretchr/testify/require"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gwv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

func TestGatewayClassConfigReconcile(t *testing.T) {
	t.Parallel()
	deletionTimestamp := meta.Now()
	cases := []struct {
		name         string
		k8sObjects   func() []runtime.Object
		expErr       string
		requeue      bool
		requeueAfter time.Duration
	}{
		{
			name: "Successfully reconcile without any changes",
			k8sObjects: func() []runtime.Object {
				gatewayClassConfig := v1alpha1.GatewayClassConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name: "consul-api-gateway",
					},
				}
				return []runtime.Object{&gatewayClassConfig}
			},
		},
		{
			name: "GatewayClassConfig Does Not Exist",
			k8sObjects: func() []runtime.Object {
				return []runtime.Object{}
			},
		},
		{
			name: "Remove not-in-use GatewayClassConfig",
			k8sObjects: func() []runtime.Object {
				gatewayClassConfig := v1alpha1.GatewayClassConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "consul-api-gateway",
						DeletionTimestamp: &deletionTimestamp,
						Finalizers:        []string{gatewayClassConfigFinalizer},
					},
				}
				return []runtime.Object{&gatewayClassConfig}
			},
		},
		{
			name: "Try to remove in-use GatewayClassConfig",
			k8sObjects: func() []runtime.Object {
				gatewayClassConfig := v1alpha1.GatewayClassConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "consul-api-gateway",
						DeletionTimestamp: &deletionTimestamp,
						Finalizers:        []string{gatewayClassConfigFinalizer},
					},
				}
				gatewayClass := gwv1beta1.GatewayClass{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name: "consul-api-gateway-class",
					},
					Spec: gwv1beta1.GatewayClassSpec{
						ParametersRef: &gwv1beta1.ParametersReference{
							Group:     gwv1beta1.Group(v1alpha1.ConsulHashicorpGroup),
							Kind:      v1alpha1.GatewayClassConfigKind,
							Name:      gatewayClassConfig.ObjectMeta.Name,
							Namespace: nil,
						},
					},
					Status: gwv1beta1.GatewayClassStatus{},
				}
				return []runtime.Object{&gatewayClassConfig, &gatewayClass}
			},
			requeueAfter: time.Second * 10,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			s := runtime.NewScheme()
			require.NoError(t, clientgoscheme.AddToScheme(s))
			require.NoError(t, gwv1alpha2.Install(s))
			require.NoError(t, gwv1beta1.Install(s))
			require.NoError(t, v1alpha1.AddToScheme(s))

			fakeClient := fake.NewClientBuilder().WithScheme(s).
				WithRuntimeObjects(tt.k8sObjects()...).
				WithStatusSubresource(&v1alpha1.GatewayClassConfig{}).
				Build()

			// Create the gateway class config controller.
			gcc := &GatewayClassConfigController{
				Client: fakeClient,
				Log:    logrtest.New(t),
			}

			resp, err := gcc.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: types.NamespacedName{
					Namespace: "",
					Name:      "consul-api-gateway",
				},
			})
			if tt.expErr != "" {
				require.EqualError(t, err, tt.expErr)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tt.requeue, resp.Requeue)
		})
	}
}
