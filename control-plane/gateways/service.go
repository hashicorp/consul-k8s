// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gateways

import (
	meshv2beta1 "github.com/hashicorp/consul-k8s/control-plane/api/mesh/v2beta1"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

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
			Labels:      b.labelsForService(),
			Annotations: b.annotationsForService(),
		},
		Spec: corev1.ServiceSpec{
			Selector: b.labelsForDeployment(),
			Type:     serviceType,
			Ports:    b.Ports(portModifier),
		},
	}
}

// Ports build a list of ports from the listener objects. In theory there should only ever be a WAN port on
// mesh gateway but building the ports from a list of listeners will allow for easier compatability with other
// gateway patterns in the future.
func (b *meshGatewayBuilder) Ports(portModifier int32) []corev1.ServicePort {

	ports := []corev1.ServicePort{}

	if len(b.gateway.Spec.Listeners) == 0 {
		//If empty use the default value. This should always be set, but in case it's not, this check
		//will prevent a panic.
		return []corev1.ServicePort{
			{
				Name: "wan",
				Port: constants.DefaultWANPort,
				TargetPort: intstr.IntOrString{
					IntVal: constants.DefaultWANPort + portModifier,
				},
			},
		}
	}
	for _, listener := range b.gateway.Spec.Listeners {
		port := int32(listener.Port)
		ports = append(ports, corev1.ServicePort{
			Name: listener.Name,
			Port: port,
			TargetPort: intstr.IntOrString{
				IntVal: port + portModifier,
			},
		})
	}
	return ports
}
