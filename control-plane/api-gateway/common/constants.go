// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

const (
	GatewayClassControllerName = "consul.hashicorp.com/gateway-controller"

	AnnotationGatewayClassConfig = "consul.hashicorp.com/gateway-class-config"

	// AnnotationExtAuthz toggles external authorization (ext_authz). On a Gateway
	// it sets the gateway-wide default posture; on an HTTPRoute it overrides that
	// default for every rule in the route. Supported values: "enabled", "disabled".
	AnnotationExtAuthz = "consul.hashicorp.com/ext-authz"

	// ExtAuthzEnabledValue / ExtAuthzDisabledValue are the supported values for
	// AnnotationExtAuthz.
	ExtAuthzEnabledValue  = "enabled"
	ExtAuthzDisabledValue = "disabled"

	// The following annotation keys are used in the v1beta1.GatewayTLSConfig's Options on a v1beta1.Listener.
	TLSCipherSuitesAnnotationKey    = "api-gateway.consul.hashicorp.com/tls_cipher_suites"
	TLSMaxVersionAnnotationKey      = "api-gateway.consul.hashicorp.com/tls_max_version"
	TLSMinVersionAnnotationKey      = "api-gateway.consul.hashicorp.com/tls_min_version"
	TLSSDSClusterNameAnnotationKey  = "api-gateway.consul.hashicorp.com/tls_sds_cluster_name"
	TLSSDSCertResourceAnnotationKey = "api-gateway.consul.hashicorp.com/tls_sds_cert_resource"

	// ListenerProtocolAnnotationKey selects the Consul api-gateway listener
	// protocol for a specific listener section. It is set on the Kubernetes
	// Gateway object's Annotations map and its value must be a valid Consul
	// APIGatewayListenerProtocol string: "http", "http2", "grpc", or "tcp".
	//
	// Example:
	//   annotations:
	//     api-gateway.consul.hashicorp.com/listener-<sectionName>-protocol: "grpc"
	//
	// The annotation key is constructed at runtime as:
	//   ListenerProtocolAnnotationPrefix + sectionName + ListenerProtocolAnnotationSuffix
	ListenerProtocolAnnotationPrefix = "api-gateway.consul.hashicorp.com/listener-"
	ListenerProtocolAnnotationSuffix = "-protocol"
)
