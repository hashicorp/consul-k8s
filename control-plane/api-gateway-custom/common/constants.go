// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

const (
	GatewayClassControllerName = "consul.hashicorp.com/gateway-controller-custom"

	AnnotationGatewayClassConfig = "consul.hashicorp.com/gateway-class-config-custom"

	// The following annotation keys are used in the v1beta1.GatewayTLSConfig's Options on a v1beta1.Listener.
	TLSCipherSuitesAnnotationKey = "api-gateway-custom.consul.hashicorp.com/tls_cipher_suites"
	TLSMaxVersionAnnotationKey   = "api-gateway-custom.consul.hashicorp.com/tls_max_version"
	TLSMinVersionAnnotationKey   = "api-gateway-custom.consul.hashicorp.com/tls_min_version"
)
