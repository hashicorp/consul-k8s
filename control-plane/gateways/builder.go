// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gateways

import (
	meshv2beta1 "github.com/hashicorp/consul-k8s/control-plane/api/mesh/v2beta1"
)

// meshGatewayBuilder is a helper struct for building the Kubernetes resources for a mesh gateway.
// This includes Deployment, Role, Service, and ServiceAccount resources.
// Configuration is combined from the MeshGateway, GatewayConfig, and GatewayClassConfig.
type meshGatewayBuilder struct {
	gateway *meshv2beta1.MeshGateway
	config  GatewayConfig
	gcc     *meshv2beta1.GatewayClassConfig
}

// NewMeshGatewayBuilder returns a new meshGatewayBuilder for the given MeshGateway,
// GatewayConfig, and GatewayClassConfig.
func NewMeshGatewayBuilder(gateway *meshv2beta1.MeshGateway, gatewayConfig GatewayConfig, gatewayClassConfig *meshv2beta1.GatewayClassConfig) *meshGatewayBuilder {
	return &meshGatewayBuilder{
		gateway: gateway,
		config:  gatewayConfig,
		gcc:     gatewayClassConfig,
	}
}
