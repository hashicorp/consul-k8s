// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gateways

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	meshv2beta1 "github.com/hashicorp/consul-k8s/control-plane/api/mesh/v2beta1"
)

func (b *gatewayBuilder[T]) Service() *corev1.Service {
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
			Name:        b.gateway.GetName(),
			Namespace:   b.gateway.GetNamespace(),
			Labels:      b.labelsForService(),
			Annotations: b.annotationsForService(),
		},
		Spec: corev1.ServiceSpec{
			Selector: b.labelsForDeployment(),
			Type:     serviceType,
			Ports:    b.gateway.ListenersToServicePorts(portModifier),
		},
	}
}

// MergeService is used to update a corev1.Service without overwriting any
// existing annotations or labels that were placed there by other vendors.
//
// based on https://github.com/kubernetes-sigs/controller-runtime/blob/4000e996a202917ad7d40f02ed8a2079a9ce25e9/pkg/controller/controllerutil/example_test.go
func MergeService(existing, desired *corev1.Service) {
	existing.Spec = desired.Spec

	// Only overwrite fields if the Service doesn't exist yet
	if existing.ObjectMeta.CreationTimestamp.IsZero() {
		existing.ObjectMeta.OwnerReferences = desired.ObjectMeta.OwnerReferences
		existing.Annotations = desired.Annotations
		existing.Labels = desired.Labels
		return
	}

	// If the Service already exists, add any desired annotations + labels to existing set
	for k, v := range desired.ObjectMeta.Annotations {
		existing.ObjectMeta.Annotations[k] = v
	}
	for k, v := range desired.ObjectMeta.Labels {
		existing.ObjectMeta.Labels[k] = v
	}
}
