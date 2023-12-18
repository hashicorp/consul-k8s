// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gateways

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	meshv2beta1 "github.com/hashicorp/consul-k8s/control-plane/api/mesh/v2beta1"
)

const port = int32(443)

func (b *meshGatewayBuilder) Service() *corev1.Service {
	var (
		containerConfig *meshv2beta1.GatewayClassContainerConfig
		portModifier    = int32(0)
		serviceType     = corev1.ServiceType("")
	)

	if b.gcc != nil {
		containerConfig = b.gcc.Spec.Deployment.Container
		portModifier = containerConfig.PortModifier
		serviceType = *b.gcc.Spec.Service.Type
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        b.gateway.Name,
			Namespace:   b.gateway.Namespace,
			Labels:      b.Labels(),
			Annotations: b.Annotations(),
		},
		Spec: corev1.ServiceSpec{
			Selector: b.Labels(),
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
