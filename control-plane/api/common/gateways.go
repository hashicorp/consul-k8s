package common

// GatewayConfig is a combination of settings relevant to Gateways
type GatewayConfig struct {
	// ImageDataplane is the Consul Dataplane image to use in gateway deployments.
	ImageDataplane string
	// ImageConsulK8S is the Consul Kubernetes Control Plane image to use in gateway deployments.
	ImageConsulK8S             string
	ConsulDestinationNamespace string
	NamespaceMirroringPrefix   string
	EnableNamespaces           bool
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

	// EnableOpenShift indicates whether we're deploying into an OpenShift environment
	// and should create SecurityContextConstraints.
	EnableOpenShift bool

	// MapPrivilegedServicePorts is the value which Consul will add to privileged container port values (ports < 1024)
	// defined on a Gateway.
	MapPrivilegedServicePorts int
}
