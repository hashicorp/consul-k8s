// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"strings"
	"time"
)

const componentAuthMethod = "k8s-component-auth-method"

// HelmConfig is the configuration of gateways that comes in from the user's Helm values.
// This is a combination of the apiGateway stanza and other settings that impact api-gateways.
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

	// OpenShiftEnabled indicates whether we're deploying into an OpenShift environment
	// and should create SecurityContextConstraints.
	OpenShiftEnabled bool

	// ReleaseName indicates the name of the release for the Helm installation. This value is used
	// as a prefix for some resources - SecurityContextConstraints, for example - so it must be known.
	ReleaseName string
}

type ConsulConfig struct {
	Address    string
	GRPCPort   int
	HTTPPort   int
	APITimeout time.Duration
}

func (h HelmConfig) Normalize() HelmConfig {
	if h.AuthMethod != "" {
		// strip off any DC naming off the back in case we're
		// in a secondary DC, in which case our auth method is
		// going to be a globally scoped auth method, and we want
		// to target the locally scoped one, which is the auth
		// method without the DC-specific suffix.
		tokens := strings.Split(h.AuthMethod, componentAuthMethod)
		if len(tokens) != 2 {
			// skip the normalization if we can't do it.
			return h
		}
		h.AuthMethod = tokens[0] + componentAuthMethod
	}
	return h
}
