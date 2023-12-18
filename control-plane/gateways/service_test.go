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

func Test_meshGatewayBuilder_Service(t *testing.T) {
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
<<<<<<< HEAD
					Labels:      defaultLabels,
||||||| parent of bb907131 (use set labels for setting labels on deployments)
					Labels: map[string]string{
						"mesh.consul.hashicorp.com/managed-by": "consul-k8s",
					},
=======
					Labels: map[string]string{
						"mesh.consul.hashicorp.com/app":        "consul",
						"mesh.consul.hashicorp.com/chart":      "consul-helm",
						"mesh.consul.hashicorp.com/heritage":   "Helm",
						"mesh.consul.hashicorp.com/release":    "consul",
						"mesh.consul.hashicorp.com/managed-by": "consul-k8s",
					},
>>>>>>> bb907131 (use set labels for setting labels on deployments)
					Annotations: map[string]string{},
				},
				Spec: corev1.ServiceSpec{
<<<<<<< HEAD
					Selector: defaultLabels,
					Type:     corev1.ServiceTypeLoadBalancer,
||||||| parent of bb907131 (use set labels for setting labels on deployments)
					Selector: map[string]string{
						"mesh.consul.hashicorp.com/managed-by": "consul-k8s",
					},
					Type: corev1.ServiceTypeLoadBalancer,
=======
					Selector: map[string]string{
						"mesh.consul.hashicorp.com/managed-by": "consul-k8s",
						"mesh.consul.hashicorp.com/app":        "consul",
						"mesh.consul.hashicorp.com/chart":      "consul-helm",
						"mesh.consul.hashicorp.com/heritage":   "Helm",
						"mesh.consul.hashicorp.com/release":    "consul",
					},
					Type: corev1.ServiceTypeLoadBalancer,
>>>>>>> bb907131 (use set labels for setting labels on deployments)
					Ports: []corev1.ServicePort{
						{
							Name: "wan",
							Port: int32(443),
							TargetPort: intstr.IntOrString{
								IntVal: int32(8443),
							},
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
						},
					},
				},
				Status: corev1.ServiceStatus{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &meshGatewayBuilder{
				gateway: tt.fields.gateway,
				config:  tt.fields.config,
				gcc:     tt.fields.gcc,
			}
			result := b.Service()
			assert.Equalf(t, tt.want, result, "Service()")
		})
	}
}
