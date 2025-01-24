// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gatewaycleanup

import (
	"github.com/hashicorp/consul-k8s/control-plane/api/mesh/v2beta1"
	corev1 "k8s.io/api/core/v1"
	"os"
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
			require.NoError(t, v2beta1.AddMeshToScheme(s))

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

func TestRunV2Resources(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct {
		gatewayClassConfig []*v2beta1.GatewayClassConfig
		gatewayClass       []*v2beta1.GatewayClass
		configMapData      string
	}{

		"v2 resources exists": {
			gatewayClassConfig: []*v2beta1.GatewayClassConfig{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-gateway",
					},
				},
			},
			gatewayClass: []*v2beta1.GatewayClass{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-gateway",
					},
				},
			},
			configMapData: `gatewayClassConfigs:
- apiVersion: mesh.consul.hashicorp.com/v2beta1
  kind: GatewayClassConfig
  metadata:
    name: test-gateway
  spec:
    deployment:
      container:
        resources:
          requests:
            cpu: 200m
            memory: 200Mi
          limits:
            cpu: 200m
            memory: 200Mi
`,
		},
		"multiple v2 resources exists": {
			gatewayClassConfig: []*v2beta1.GatewayClassConfig{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-gateway",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-gateway2",
					},
				},
			},
			gatewayClass: []*v2beta1.GatewayClass{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-gateway",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-gateway2",
					},
				},
			},
			configMapData: `gatewayClassConfigs:
- apiVersion: mesh.consul.hashicorp.com/v2beta1
  kind: GatewayClassConfig
  metadata:
    name: test-gateway
  spec:
    deployment:
      container:
        resources:
          requests:
            cpu: 200m
            memory: 200Mi
          limits:
            cpu: 200m
            memory: 200Mi
- apiVersion: mesh.consul.hashicorp.com/v2beta1
  kind: GatewayClassConfig
  metadata:
    name: test-gateway2
  spec:
    deployment:
      container:
        resources:
          requests:
            cpu: 200m
            memory: 200Mi
          limits:
            cpu: 200m
            memory: 200Mi
`,
		},
		"v2 emptyconfigmap": {
			configMapData: "",
		},
	} {
		t.Run(name, func(t *testing.T) {
			tt := tt

			t.Parallel()

			s := runtime.NewScheme()
			require.NoError(t, gwv1beta1.Install(s))
			require.NoError(t, v2beta1.AddMeshToScheme(s))
			require.NoError(t, corev1.AddToScheme(s))
			require.NoError(t, v1alpha1.AddToScheme(s))

			objs := []client.Object{}
			for _, gatewayClass := range tt.gatewayClass {
				objs = append(objs, gatewayClass)
			}
			for _, gatewayClassConfig := range tt.gatewayClassConfig {
				objs = append(objs, gatewayClassConfig)
			}

			path := createGatewayConfigFile(t, tt.configMapData, "config.yaml")

			client := fake.NewClientBuilder().WithScheme(s).WithObjects(objs...).Build()

			ui := cli.NewMockUi()
			cmd := Command{
				UI:                         ui,
				k8sClient:                  client,
				flagGatewayClassName:       "gateway-class",
				flagGatewayClassConfigName: "gateway-class-config",
				flagGatewayConfigLocation:  path,
			}

			code := cmd.Run([]string{
				"-gateway-class-config-name", "gateway-class-config",
				"-gateway-class-name", "gateway-class",
				"-gateway-config-file-location", path,
			})

			require.Equal(t, 0, code)
		})
	}
}

func createGatewayConfigFile(t *testing.T, fileContent, filename string) string {
	t.Helper()

	// create a temp file to store configuration yaml
	tmpdir := t.TempDir()
	file, err := os.CreateTemp(tmpdir, filename)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	_, err = file.WriteString(fileContent)
	if err != nil {
		t.Fatal(err)
	}
	return file.Name()
}
