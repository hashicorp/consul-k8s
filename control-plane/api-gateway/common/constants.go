// Copyright IBM Corp. 2018, 2026
// SPDX-License-Identifier: MPL-2.0

package common

const (
	GatewayClassControllerName = "consul.hashicorp.com/gateway-controller"

	AnnotationGatewayClassConfig = "consul.hashicorp.com/gateway-class-config"

	// The following annotation keys are used in the v1beta1.GatewayTLSConfig's Options on a v1beta1.Listener.
	TLSCipherSuitesAnnotationKey    = "api-gateway.consul.hashicorp.com/tls_cipher_suites"
	TLSMaxVersionAnnotationKey      = "api-gateway.consul.hashicorp.com/tls_max_version"
	TLSMinVersionAnnotationKey      = "api-gateway.consul.hashicorp.com/tls_min_version"
	TLSSDSClusterNameAnnotationKey  = "api-gateway.consul.hashicorp.com/tls_sds_cluster_name"
	TLSSDSCertResourceAnnotationKey = "api-gateway.consul.hashicorp.com/tls_sds_cert_resource"
)
