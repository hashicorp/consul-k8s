package constants

const (
	// ConsulCAFile is the location of the Consul CA file inside the injected pod.
	ConsulCAFile = "/consul/connect-inject/consul-ca.pem"

	// ProxyDefaultInboundPort is the default inbound port for the proxy.
	ProxyDefaultInboundPort = 20000

	// MetaKeyKubeNS is the meta key name for Kubernetes namespace used for the Consul services.
	MetaKeyKubeNS = "k8s-namespace"

	// MetaKeyPodName is the meta key name for Kubernetes pod name used for the Consul services.
	MetaKeyPodName = "pod-name"
)
