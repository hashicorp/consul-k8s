// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gateways

import (
	meshv2beta1 "github.com/hashicorp/consul-k8s/control-plane/api/mesh/v2beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func (b *meshGatewayBuilder) Service() *corev1.Service {
	var (
		portModifier    = int32(0)
		port            = int32(443)
		serviceType     = corev1.ServiceType("")
		containerConfig *meshv2beta1.GatewayClassContainerConfig
	)

	if b.gcc != nil {
		containerConfig = b.gcc.Spec.Deployment.Container
		portModifier = containerConfig.PortModifier
		serviceType = *b.gcc.Spec.Service.Type
		port = b.gcc.Spec.Service.Port
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        b.gateway.Name,
			Namespace:   b.gateway.Namespace,
			Labels:      b.labelsForService(),
			Annotations: b.annotationsForService(),
		},
		Spec: corev1.ServiceSpec{
			Selector: b.labelsForDeployment(),
			Type:     serviceType,
			Ports: []corev1.ServicePort{
				{
					Name: "wan",
					Port: port,
					TargetPort: intstr.IntOrString{
						IntVal: port + portModifier,
					},
				},
			},
		},
	}
}
