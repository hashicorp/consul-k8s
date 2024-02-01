// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gateways

import (
	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	"github.com/hashicorp/consul-k8s/control-plane/api/mesh/v2beta1"
)

// GatewayConfig is a combination of settings relevant to Gateways.
type GatewayConfig struct {
	// ImageDataplane is the Consul Dataplane image to use in gateway deployments.
	ImageDataplane string
	// ImageConsulK8S is the Consul Kubernetes Control Plane image to use in gateway deployments.
	ImageConsulK8S string
	// AuthMethod method used to authenticate with Consul Server.
	AuthMethod string

	// ConsulTenancyConfig is the configuration for the Consul Tenancy feature.
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

	// TODO(nathancoleman) Add doc
	SkipServerWatch bool
}

// GatewayResources is a collection of Kubernetes resources for a Gateway.
type GatewayResources struct {
	// GatewayClassConfigs is a list of GatewayClassConfig resources which are
	// responsible for defining configuration shared across all gateway kinds.
	GatewayClassConfigs []*v2beta1.GatewayClassConfig `json:"gatewayClassConfigs"`
	// MeshGateways is a list of MeshGateway resources which are responsible for
	// defining the configuration for a specific mesh gateway.
	// Deployments of mesh gateways have a one-to-one relationship with MeshGateway resources.
	MeshGateways []*v2beta1.MeshGateway `json:"meshGateways"`
}
