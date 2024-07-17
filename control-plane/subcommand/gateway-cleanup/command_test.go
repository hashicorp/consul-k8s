// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gatewaycleanup

import (
	"testing"

	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

func TestRun(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct {
		gatewayClassConfig *v1alpha1.GatewayClassConfig
		gatewayClass       *gwv1beta1.GatewayClass
	}{
		"both exist": {
			gatewayClassConfig: &v1alpha1.GatewayClassConfig{},
			gatewayClass:       &gwv1beta1.GatewayClass{},
		},
		"gateway class config doesn't exist": {
			gatewayClass: &gwv1beta1.GatewayClass{},
		},
		"gateway class doesn't exist": {
			gatewayClassConfig: &v1alpha1.GatewayClassConfig{},
		},
		"neither exist": {},
		"finalizers on gatewayclass blocking deletion": {
			gatewayClassConfig: &v1alpha1.GatewayClassConfig{},
			gatewayClass:       &gwv1beta1.GatewayClass{ObjectMeta: metav1.ObjectMeta{Finalizers: []string{"finalizer"}}},
		},
		"finalizers on gatewayclassconfig blocking deletion": {
			gatewayClassConfig: &v1alpha1.GatewayClassConfig{ObjectMeta: metav1.ObjectMeta{Finalizers: []string{"finalizer"}}},
			gatewayClass:       &gwv1beta1.GatewayClass{},
		},
	} {
		t.Run(name, func(t *testing.T) {
			tt := tt

			t.Parallel()

			s := runtime.NewScheme()
			require.NoError(t, gwv1beta1.Install(s))
			require.NoError(t, v1alpha1.AddToScheme(s))

			objs := []client.Object{}
			if tt.gatewayClass != nil {
				tt.gatewayClass.Name = "gateway-class"
				objs = append(objs, tt.gatewayClass)
			}
			if tt.gatewayClassConfig != nil {
				tt.gatewayClassConfig.Name = "gateway-class-config"
				objs = append(objs, tt.gatewayClassConfig)
			}

			client := fake.NewClientBuilder().WithScheme(s).WithObjects(objs...).Build()

			ui := cli.NewMockUi()
			cmd := Command{
				UI:                         ui,
				k8sClient:                  client,
				flagGatewayClassName:       "gateway-class",
				flagGatewayClassConfigName: "gateway-class-config",
			}

			code := cmd.Run([]string{
				"-gateway-class-config-name", "gateway-class-config",
				"-gateway-class-name", "gateway-class",
			})

			require.Equal(t, 0, code)
		})
	}
}
