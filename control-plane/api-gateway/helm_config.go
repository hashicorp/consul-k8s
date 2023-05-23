package apigateway

// HelmConfig is the configuration of gateways that comes in from the user's Helm values.
type HelmConfig struct {
	// Image is the Consul Dataplane image to use in gateway deployments.
	Image string
	// LogLevel is the logging level of the deployed Consul Dataplanes.
	LogLevel string

	// Replicas is the number of Pods in a given Deployment of API Gateway for handling requests.
	Replicas int32
	// MaxInstances is the maximum number of replicas in the Deployment of API Gateway for handling requests.
	MaxInstances int32
	// MinInstances is the minimum number of replicas in the Deployment of API Gateway for handling requests.
	MinInstances int32

	// ManageSystemACLs toggles the behavior of Consul on Kubernetes creating ACLs and RBAC resources for Gateway deployments.
	ManageSystemACLs bool
}
