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

	// NodeSelector is the node selector for the Gateway Deployment.
	NodeSelector map[string]string
	// Tolerations is the tolerations for the Gateway Deployment.
	Tolerations map[string]string
	// ServiceType is the service type for the Gateway Service.
	ServiceType string
	// CopyAnnotations is the annotations to copy from the Gateway Service to the Gateway Deployment.
	CopyAnnotations map[string]string
}

func (f *GatewayFlags) Flags() *flag.FlagSet {
	fs := flag.NewFlagSet("gateway", flag.ContinueOnError)

	deploymentReplicas, _ := strconv.Atoi(os.Getenv(GatewayDeploymentReplicasEnvVar))
	deploymentMaxInstances, _ := strconv.Atoi(os.Getenv(GatewayMaxInstancesEnvVar))
	deploymentMinInstances, _ := strconv.Atoi(os.Getenv(GatewayMinInstancesEnvVar))
	// nodeSelector := os.Getenv(GatewayNodeSelectorEnvVar)
	// tolerations := os.Getenv(GatewayTolerationsEnvVar)
	serviceType := os.Getenv(GatewayServiceTypeEnvVar)
	// copyAnnotations := os.Getenv(GatewayCopyAnnotationsEnvVar)

	fs.IntVar(&f.DeploymentReplicas, "deployment-replicas", deploymentReplicas, "")
	fs.IntVar(&f.DeploymentMaxInstances, "max-instances", deploymentMaxInstances, "")
	fs.IntVar(&f.DeploymentMinInstances, "min-instances", deploymentMinInstances, "")
	// fs.Var((*FlagMapValue)(&f.NodeSelector), "node-selector", "")
	// fs.Var((*FlagMapValue)(&f.Tolerations), "tolerations", "")
	fs.StringVar(&f.ServiceType, "service-type", serviceType, "")
	// fs.Var((*flags.FlagMapValue)(&f.CopyAnnotations), "copy-annotations", "")

	return fs
}
