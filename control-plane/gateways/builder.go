// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gateways

import (
	meshv2beta1 "github.com/hashicorp/consul-k8s/control-plane/api/mesh/v2beta1"
)

type meshGatewayBuilder struct {
	gateway *meshv2beta1.MeshGateway
	config  GatewayConfig
	gcc     *meshv2beta1.GatewayClassConfig
}

func NewMeshGatewayBuilder(gateway *meshv2beta1.MeshGateway, gatewayConfig GatewayConfig, gatewayClassConfig *meshv2beta1.GatewayClassConfig) *meshGatewayBuilder {
	return &meshGatewayBuilder{
		gateway: gateway,
		config:  gatewayConfig,
		gcc:     gatewayClassConfig,
	}
}
