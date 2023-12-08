// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gateways

import "github.com/hashicorp/consul-k8s/control-plane/api/common"

// GatewayConfig is a combination of settings relevant to Gateways.
type GatewayConfig struct {
	// ImageDataplane is the Consul Dataplane image to use in gateway deployments.
	ImageDataplane string
	// ImageConsulK8S is the Consul Kubernetes Control Plane image to use in gateway deployments.
	ImageConsulK8S string
	// AuthMethod method used to authenticate with Consul Server.
	AuthMethod string

	ConsulTenancyConfig common.ConsulTenancyConfig

	// LogLevel is the logging level of the deployed Consul Dataplanes.
	LogLevel string
	// LogJSON if JSONLogging has been enabled.
	LogJSON bool
	// TLSEnabled is the value of whether or not TLS has been enabled in Consul.
	TLSEnabled bool
	// PeeringEnabled toggles whether or not Peering is enabled in Consul.
	PeeringEnabled bool
	// ConsulTLSServerName the name of the server running TLS.
	ConsulTLSServerName string
	// ConsulCACert contains the Consul Certificate Authority.
	ConsulCACert string
	// ConsulConfig configuration for the consul server address.
	ConsulConfig common.ConsulConfig

	// EnableOpenShift indicates whether we're deploying into an OpenShift environment
	EnableOpenShift bool

	// MapPrivilegedServicePorts is the value which Consul will add to privileged container port values (ports < 1024)
	// defined on a Gateway.
	MapPrivilegedServicePorts int
}
