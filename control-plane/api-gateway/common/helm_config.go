// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import "time"

// HelmConfig is the configuration of gateways that comes in from the user's Helm values.
type HelmConfig struct {
	// ImageDataplane is the Consul Dataplane image to use in gateway deployments.
	ImageDataplane             string
	ImageConsulK8S             string
	ConsulDestinationNamespace string
	NamespaceMirroringPrefix   string
	EnableNamespaces           bool
	EnableOpenShift            bool
	EnableNamespaceMirroring   bool
	AuthMethod                 string
	// LogLevel is the logging level of the deployed Consul Dataplanes.
	LogLevel            string
	ConsulPartition     string
	LogJSON             bool
	TLSEnabled          bool
	PeeringEnabled      bool
	ConsulTLSServerName string
	ConsulCACert        string
	ConsulConfig        ConsulConfig
}

type ConsulConfig struct {
	Address    string
	GRPCPort   int
	HTTPPort   int
	APITimeout time.Duration
}
