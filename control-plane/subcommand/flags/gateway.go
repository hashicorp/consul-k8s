// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package flags

import (
	"flag"
	"os"
	"strconv"
)

const (
	GatewayDeploymentReplicasEnvVar = "GATEWAY_DEPLOYMENT_REPLICAS"
	GatewayMaxInstancesEnvVar       = "GATEWAY_DEPLOYMENT_MAX_INSTANCES"
	GatewayMinInstancesEnvVar       = "GATEWAY_DEPLOYMENT_MIN_INSTANCES"

	GatewayNodeSelectorEnvVar    = "GATEWAY_NODE_SELECTOR"
	GatewayTolerationsEnvVar     = "GATEWAY_TOLERATIONS"
	GatewayServiceTypeEnvVar     = "GATEWAY_SERVICE_TYPE"
	GatewayCopyAnnotationsEnvVar = "GATEWAY_COPY_ANNOTATIONS"
)

// GatewayFlags contains flags related to the Gateway API.
type GatewayFlags struct {
	// DeploymentReplicas is the number of replicas for the Gateway Deployment.
	DeploymentReplicas int
	// DeploymentMaxInstances is the maximum number of instances for the Gateway Deployment.
	DeploymentMaxInstances int
	// DeploymentMinInstances is the minimum number of instances for the Gateway Deployment.
	DeploymentMinInstances int
}

func (f *GatewayFlags) Flags() *flag.FlagSet {
	fs := flag.NewFlagSet("gateway", flag.ContinueOnError)

	deploymentReplicas, _ := strconv.Atoi(os.Getenv(GatewayDeploymentReplicasEnvVar))
	deploymentMaxInstances, _ := strconv.Atoi(os.Getenv(GatewayMaxInstancesEnvVar))
	deploymentMinInstances, _ := strconv.Atoi(os.Getenv(GatewayMinInstancesEnvVar))

	fs.IntVar(&f.DeploymentReplicas, "gateway-deployment-replicas", deploymentReplicas, "The number of replicas for the API Gateway Deployment.")
	fs.IntVar(&f.DeploymentMaxInstances, "gateway-max-instances", deploymentMaxInstances, "The maximum number of replicas for the API Gateway Deployment.")
	fs.IntVar(&f.DeploymentMinInstances, "gateway-min-instances", deploymentMinInstances, "The minimum number of replicas for the API Gateway Deployment.")

	return fs
}
