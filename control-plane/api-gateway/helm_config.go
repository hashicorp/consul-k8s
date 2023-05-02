package apigateway

// HelmConfig is the configuration of gateways that comes in from the user's Helm values.
type HelmConfig struct {
	// Image is the Consul Dataplane image to use in gateway deployments.
	Image                    string
	Replicas                 int32
	LogLevel                 string
	NodeSelector             map[string]string
	Tolerations              map[string]string
	ServiceType              string
	UseHostPorts             bool
	CopyAnnotations          map[string]string
	MaxInstances             int32
	MinInstances             int32
	ConsulNamespaceMirroring bool
}
