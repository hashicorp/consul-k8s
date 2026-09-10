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

	// AnnotationTLSEnabled opts a Gateway into Consul-managed, zero-touch TLS
	// termination. When set to "true" the translated api-gateway config entry
	// carries a gateway-level TLS { Enabled = true } block; combined with an
	// HTTPS listener that has no certificateRefs, the gateway terminates HTTPS
	// using its auto-issued Connect leaf certificate (whose SANs include
	// *.<gateway>.consul). Supported value: "true".
	AnnotationTLSEnabled = "consul.hashicorp.com/tls-enabled"

	// TLSEnabledValue is the supported value for AnnotationTLSEnabled.
	TLSEnabledValue = "true"

	// The following annotation keys are used in the v1beta1.GatewayTLSConfig's Options on a v1beta1.Listener.
	TLSCipherSuitesAnnotationKey    = "api-gateway.consul.hashicorp.com/tls_cipher_suites"
	TLSMaxVersionAnnotationKey      = "api-gateway.consul.hashicorp.com/tls_max_version"
	TLSMinVersionAnnotationKey      = "api-gateway.consul.hashicorp.com/tls_min_version"
	TLSSDSClusterNameAnnotationKey  = "api-gateway.consul.hashicorp.com/tls_sds_cluster_name"
	TLSSDSCertResourceAnnotationKey = "api-gateway.consul.hashicorp.com/tls_sds_cert_resource"
)
