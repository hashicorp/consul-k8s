// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gateways

import (
	"testing"

	pbmesh "github.com/hashicorp/consul/proto-public/pbmesh/v2beta1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	meshv2beta1 "github.com/hashicorp/consul-k8s/control-plane/api/mesh/v2beta1"
)

func Test_gatewayBuilder_meshGateway_Service(t *testing.T) {
	lbType := corev1.ServiceTypeLoadBalancer

	type fields struct {
		gateway *meshv2beta1.MeshGateway
		config  GatewayConfig
		gcc     *meshv2beta1.GatewayClassConfig
	}
	tests := []struct {
		name   string
		fields fields
		want   *corev1.Service
	}{
		{
			name: "service resource crd created - happy path",
			fields: fields{
				gateway: &meshv2beta1.MeshGateway{
					Spec: pbmesh.MeshGateway{
						GatewayClassName: "test-gateway-class",
						Listeners: []*pbmesh.MeshGatewayListener{
							{
								Name:     "wan",
								Port:     443,
								Protocol: "TCP",
							},
						},
					},
				},
				config: GatewayConfig{},
				gcc: &meshv2beta1.GatewayClassConfig{
					Spec: meshv2beta1.GatewayClassConfigSpec{
						GatewayClassAnnotationsAndLabels: meshv2beta1.GatewayClassAnnotationsAndLabels{
							Labels: meshv2beta1.GatewayClassAnnotationsLabelsConfig{
								Set: map[string]string{
									"app":      "consul",
									"chart":    "consul-helm",
									"heritage": "Helm",
									"release":  "consul",
								},
							},
						},
						Deployment: meshv2beta1.GatewayClassDeploymentConfig{
							Container: &meshv2beta1.GatewayClassContainerConfig{
								PortModifier: 8000,
							},
						},
						Service: meshv2beta1.GatewayClassServiceConfig{
							Type: &lbType,
						},
					},
				},
			},
			want: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						labelManagedBy: "consul-k8s",
						"app":          "consul",
						"chart":        "consul-helm",
						"heritage":     "Helm",
						"release":      "consul",
					},
					Annotations: map[string]string{},
				},
				Spec: corev1.ServiceSpec{
					Selector: map[string]string{
						labelManagedBy: "consul-k8s",
						"app":          "consul",
						"chart":        "consul-helm",
						"heritage":     "Helm",
						"release":      "consul",
					},
					Type: corev1.ServiceTypeLoadBalancer,
					Ports: []corev1.ServicePort{
						{
							Name: "wan",
							Port: int32(443),
							TargetPort: intstr.IntOrString{
								IntVal: int32(8443),
							},
							Protocol: "TCP",
						},
					},
				},
				Status: corev1.ServiceStatus{},
			},
		},
		{
			name: "create service resource crd - gcc is nil",
			fields: fields{
				gateway: &meshv2beta1.MeshGateway{
					Spec: pbmesh.MeshGateway{
						GatewayClassName: "test-gateway-class",
						Listeners: []*pbmesh.MeshGatewayListener{
							{
								Name:     "wan",
								Port:     443,
								Protocol: "TCP",
							},
						},
					},
				},
				config: GatewayConfig{},
				gcc:    nil,
			},
			want: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      defaultLabels,
					Annotations: map[string]string{},
				},
				Spec: corev1.ServiceSpec{
					Selector: defaultLabels,
					Type:     "",
					Ports: []corev1.ServicePort{
						{
							Name: "wan",
							Port: int32(443),
							TargetPort: intstr.IntOrString{
								IntVal: int32(443),
							},
							Protocol: "TCP",
						},
					},
				},
				Status: corev1.ServiceStatus{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &gatewayBuilder[*meshv2beta1.MeshGateway]{
				gateway: tt.fields.gateway,
				config:  tt.fields.config,
				gcc:     tt.fields.gcc,
			}
			result := b.Service()
			assert.Equalf(t, tt.want, result, "Service()")
		})
	}
}

func Test_MergeService(t *testing.T) {
	testCases := []struct {
		name     string
		a, b     *corev1.Service
		assertFn func(*testing.T, *corev1.Service)
	}{
		{
			name: "new service gets desired annotations + labels",
			a:    &corev1.Service{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "service"}},
			b: &corev1.Service{ObjectMeta: metav1.ObjectMeta{
				Namespace:   "default",
				Name:        "service",
				Annotations: map[string]string{"b": "b"},
				Labels:      map[string]string{"b": "b"},
			}},
			assertFn: func(t *testing.T, result *corev1.Service) {
				assert.Equal(t, map[string]string{"b": "b"}, result.Annotations)
				assert.Equal(t, map[string]string{"b": "b"}, result.Labels)
			},
		},
		{
			name: "existing service keeps existing annotations + labels and gains desired annotations + labels + type",
			a: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:         "default",
					Name:              "service",
					CreationTimestamp: metav1.Now(),
					Annotations:       map[string]string{"a": "a"},
					Labels:            map[string]string{"a": "a"},
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeClusterIP,
				},
			},
			b: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   "default",
					Name:        "service",
					Annotations: map[string]string{"b": "b"},
					Labels:      map[string]string{"b": "b"},
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeLoadBalancer,
				},
			},
			assertFn: func(t *testing.T, result *corev1.Service) {
				assert.Equal(t, map[string]string{"a": "a", "b": "b"}, result.Annotations)
				assert.Equal(t, map[string]string{"a": "a", "b": "b"}, result.Labels)

				assert.Equal(t, corev1.ServiceTypeLoadBalancer, result.Spec.Type)
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			MergeService(testCase.a, testCase.b)
			testCase.assertFn(t, testCase.a)
		})
	}
}

func Test_gatewayBuilder_apiGateway_Service(t *testing.T) {
	lbType := corev1.ServiceTypeLoadBalancer

	type fields struct {
		gateway *meshv2beta1.APIGateway
		config  GatewayConfig
		gcc     *meshv2beta1.GatewayClassConfig
	}
	tests := []struct {
		name   string
		fields fields
		want   *corev1.Service
	}{
		{
			name: "service resource crd created - happy path",
			fields: fields{
				gateway: &meshv2beta1.APIGateway{
					Spec: pbmesh.APIGateway{
						GatewayClassName: "test-gateway-class",
						Listeners: []*pbmesh.APIGatewayListener{
							{
								Name:     "http-listener",
								Port:     80,
								Protocol: "http",
							},
						},
					},
				},
				config: GatewayConfig{},
				gcc: &meshv2beta1.GatewayClassConfig{
					Spec: meshv2beta1.GatewayClassConfigSpec{
						GatewayClassAnnotationsAndLabels: meshv2beta1.GatewayClassAnnotationsAndLabels{
							Labels: meshv2beta1.GatewayClassAnnotationsLabelsConfig{
								Set: map[string]string{
									"app":      "consul",
									"chart":    "consul-helm",
									"heritage": "Helm",
									"release":  "consul",
								},
							},
						},
						Deployment: meshv2beta1.GatewayClassDeploymentConfig{
							Container: &meshv2beta1.GatewayClassContainerConfig{
								PortModifier: 8000,
							},
						},
						Service: meshv2beta1.GatewayClassServiceConfig{
							Type: &lbType,
						},
					},
				},
			},
			want: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						labelManagedBy: "consul-k8s",
						"app":          "consul",
						"chart":        "consul-helm",
						"heritage":     "Helm",
						"release":      "consul",
					},
					Annotations: map[string]string{},
				},
				Spec: corev1.ServiceSpec{
					Selector: map[string]string{
						labelManagedBy: "consul-k8s",
						"app":          "consul",
						"chart":        "consul-helm",
						"heritage":     "Helm",
						"release":      "consul",
					},
					Type: corev1.ServiceTypeLoadBalancer,
					Ports: []corev1.ServicePort{
						{
							Name: "http-listener",
							Port: int32(80),
							TargetPort: intstr.IntOrString{
								IntVal: int32(8080),
							},
							Protocol: "http",
						},
					},
				},
				Status: corev1.ServiceStatus{},
			},
		},
		{
			name: "create service resource crd - gcc is nil",
			fields: fields{
				gateway: &meshv2beta1.APIGateway{
					Spec: pbmesh.APIGateway{
						GatewayClassName: "test-gateway-class",
						Listeners: []*pbmesh.APIGatewayListener{
							{
								Name:     "http-listener",
								Port:     80,
								Protocol: "http",
							},
						},
					},
				},
				config: GatewayConfig{},
				gcc:    nil,
			},
			want: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      defaultLabels,
					Annotations: map[string]string{},
				},
				Spec: corev1.ServiceSpec{
					Selector: defaultLabels,
					Type:     "",
					Ports: []corev1.ServicePort{
						{
							Name: "http-listener",
							Port: int32(80),
							TargetPort: intstr.IntOrString{
								IntVal: int32(80),
							},
							Protocol: "http",
						},
					},
				},
				Status: corev1.ServiceStatus{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &gatewayBuilder[*meshv2beta1.APIGateway]{
				gateway: tt.fields.gateway,
				config:  tt.fields.config,
				gcc:     tt.fields.gcc,
			}
			result := b.Service()
			assert.Equalf(t, tt.want, result, "Service()")
		})
	}
}
