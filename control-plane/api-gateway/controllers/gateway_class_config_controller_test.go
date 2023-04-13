// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package controllers

import (
	"context"
	"errors"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
	"testing"

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

var (
	errExpected     = errors.New("expected")
	classConfigName = types.NamespacedName{
		Name:      "config",
		Namespace: "default",
	}
)

func TestGatewayClassConfigSetup(t *testing.T) {
	require.Error(t, (&Controller{}).SetupWithManager(nil))
}

func TestGatewayClassConfigReconcile(t *testing.T) {
	t.Parallel()
	deletionTimestamp := meta.Now()
	cases := []struct {
		name       string
		k8sObjects func() []runtime.Object
		nodeMeta   map[string]string
		expErr     string
		reque      bool
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
			expErr: "",
			reque:  false,
		},
		{
			name: "GatewayClassConfig Does Not Exist",
			k8sObjects: func() []runtime.Object {
				gatewayClassConfig := v1alpha1.GatewayClassConfig{}
				return []runtime.Object{&gatewayClassConfig}
			},
			expErr: "",
			reque:  false,
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
			expErr: "",
			reque:  false,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			s := runtime.NewScheme()
			gatewayClass := gwv1beta1.GatewayClass{}
			gatewayClassList := gwv1beta1.GatewayClassList{}
			k8sObjects := append(tt.k8sObjects(), &gatewayClass, &gatewayClassList)
			s.AddKnownTypes(v1alpha1.GroupVersion, k8sObjects...)
			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(tt.k8sObjects()...).Build()

			// Create the gateway class config controller.
			gcc := &Controller{
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
