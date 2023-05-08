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
	ServiceType              *string
	UseHostPorts             bool
	CopyAnnotations          map[string]string
	MaxInstances             int32
	MinInstances             int32
	ConsulNamespaceMirroring bool
	ManageSystemACLs         bool
}
