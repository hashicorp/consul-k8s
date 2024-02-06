// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gateways

import (
	meshv2beta1 "github.com/hashicorp/consul-k8s/control-plane/api/mesh/v2beta1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Gateway interface {
	*meshv2beta1.MeshGateway | *meshv2beta1.APIGateway
	client.Object
	GetName() string
	GetNamespace() string
	ListenersToServicePorts(int32) []corev1.ServicePort
	ListenersToContainerPorts(int32, int32) []corev1.ContainerPort
	GetAnnotations() map[string]string
	GetLabels() map[string]string
}

// gatewayBuilder is a helper struct for building the Kubernetes resources for a mesh gateway.
// This includes Deployment, Role, Service, and ServiceAccount resources.
// Configuration is combined from the MeshGateway, GatewayConfig, and GatewayClassConfig.
type gatewayBuilder[T Gateway] struct {
	gateway T
	gcc     *meshv2beta1.GatewayClassConfig
	config  GatewayConfig
}

// NewGatewayBuilder returns a new meshGatewayBuilder for the given MeshGateway,
// GatewayConfig, and GatewayClassConfig.
func NewGatewayBuilder[T Gateway](gateway T, gatewayConfig GatewayConfig, gatewayClassConfig *meshv2beta1.GatewayClassConfig) *gatewayBuilder[T] {
	return &gatewayBuilder[T]{
		gateway: gateway,
		config:  gatewayConfig,
		gcc:     gatewayClassConfig,
	}
}
