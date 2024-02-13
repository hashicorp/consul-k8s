// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gateways

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	meshv2beta1 "github.com/hashicorp/consul-k8s/control-plane/api/mesh/v2beta1"
)

func TestNewMeshGatewayBuilder_ServiceAccount(t *testing.T) {
	b := NewGatewayBuilder(&meshv2beta1.MeshGateway{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "mesh-gateway",
		},
	}, GatewayConfig{}, nil, MeshGatewayAnnotationKind)

	expected := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   "default",
			Name:        "mesh-gateway",
			Labels:      defaultLabels,
			Annotations: map[string]string{},
		},
	}

	assert.Equal(t, expected, b.ServiceAccount())
}
