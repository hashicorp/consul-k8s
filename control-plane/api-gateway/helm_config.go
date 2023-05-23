// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package apigateway

// HelmConfig is the configuration of gateways that comes in from the user's Helm values.
type HelmConfig struct {
	// Image is the Consul Dataplane image to use in gateway deployments.
	Image string
	// Replicas is the number of Pods in a given Deployment of API Gateway for handling requests.
	Replicas int32
	// LogLevel is the logging level of the deployed Consul Dataplanes.
	LogLevel string
	// NodeSelector places Pods in the Deployment on matching Kubernetes Nodes.
	NodeSelector map[string]string
	// Tolerations place Pods in the Deployment on Kubernetes Nodes by toleration.
	Tolerations map[string]string
	// ServiceType is the type of service that should be attached to a given Deployment.
	ServiceType *string
	// CopyAnnotations defines a mapping of annotations to be copied from the Gateway to the Service created.
	CopyAnnotations map[string]string
	// MaxInstances is the maximum number of replicas in the Deployment of API Gateway for handling requests.
	MaxInstances int32
	// MinInstances is the minimum number of replicas in the Deployment of API Gateway for handling requests.
	MinInstances int32
	// ManageSystemACLs toggles the behavior of Consul on Kubernetes creating ACLs and RBAC resources for Gateway deployments.
	ManageSystemACLs bool
}
