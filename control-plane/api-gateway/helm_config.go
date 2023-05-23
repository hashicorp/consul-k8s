// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package apigateway

// HelmConfig is the configuration of gateways that comes in from the user's Helm values.
type HelmConfig struct {
	// Image is the Consul Dataplane image to use in gateway deployments.
	Image string
	// LogLevel is the logging level of the deployed Consul Dataplanes.
	LogLevel string

	// ManageSystemACLs toggles the behavior of Consul on Kubernetes creating ACLs and RBAC resources for Gateway deployments.
	ManageSystemACLs bool
}
