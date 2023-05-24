// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package apigateway

import "time"

// HelmConfig is the configuration of gateways that comes in from the user's Helm values.
type HelmConfig struct {
	// Replicas is the number of Pods in a given Deployment of API Gateway for handling requests.
	Replicas int32

	// NodeSelector places Pods in the Deployment on matching Kubernetes Nodes.
	NodeSelector map[string]string
	// Tolerations place Pods in the Deployment on Kubernetes Nodes by toleration.
	Tolerations map[string]string
	// ServiceType is the type of service that should be attached to a given Deployment.
	ServiceType *string
	// ManageSystemACLs toggles the behavior of Consul on Kubernetes creating ACLs and RBAC resources for Gateway deployments.
	ManageSystemACLs bool

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
