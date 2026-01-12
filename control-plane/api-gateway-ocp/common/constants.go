// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

const (
	GatewayClassControllerName = "consul.hashicorp.com/gateway-controller-ocp"

	AnnotationGatewayClassConfig = "consul.hashicorp.com/gateway-class-config-ocp"

	// The following annotation keys are used in the v1beta1.GatewayTLSConfig's Options on a v1beta1.Listener.
	TLSCipherSuitesAnnotationKey = "api-gateway-ocp.consul.hashicorp.com/tls_cipher_suites"
	TLSMaxVersionAnnotationKey   = "api-gateway-ocp.consul.hashicorp.com/tls_max_version"
	TLSMinVersionAnnotationKey   = "api-gateway-ocp.consul.hashicorp.com/tls_min_version"
)
