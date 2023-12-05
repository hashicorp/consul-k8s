// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gateways

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (b *meshGatewayBuilder) ServiceAccount() *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      b.serviceAccountName(),
			Namespace: b.gateway.Namespace,
			Labels:    b.Labels(),
		},
	}
}

func (b *meshGatewayBuilder) serviceAccountName() string {
	return b.gateway.Name
}
