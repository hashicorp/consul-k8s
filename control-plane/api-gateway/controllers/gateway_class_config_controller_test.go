// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package controllers

import (
	"context"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
	"testing"
	"time"

	logrtest "github.com/go-logr/logr/testing"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/stretchr/testify/require"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestGatewayClassConfigSetup(t *testing.T) {
	require.Error(t, (&GatewayClassConfigController{}).SetupWithManager(nil))
}

func TestGatewayClassConfigReconcile(t *testing.T) {
	t.Parallel()
	deletionTimestamp := meta.Now()
	cases := []struct {
		name       string
		k8sObjects func() []runtime.Object
		expErr     string
		reque      bool
		requeAfter time.Duration
	}{
		{
			name: "Happy Path",
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
			requeAfter: time.Second * 10,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			s := runtime.NewScheme()
			k8sSchemaObjects := append(tt.k8sObjects(), &gwv1beta1.GatewayClass{}, &gwv1beta1.GatewayClassList{}, &v1alpha1.GatewayClassConfig{})
			s.AddKnownTypes(v1alpha1.GroupVersion, k8sSchemaObjects...)
			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(tt.k8sObjects()...).Build()

			// Create the gateway class config controller.
			gcc := &GatewayClassConfigController{
				Client: fakeClient,
				Log:    logrtest.NewTestLogger(t),
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
			require.Equal(t, tt.reque, resp.Requeue)
		})
	}
}
