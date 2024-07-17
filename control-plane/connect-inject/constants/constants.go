// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package constants

const (
	// LegacyConsulCAFile is the location of the Consul CA file inside the injected pod.
	// This is used with the V1 API.
	LegacyConsulCAFile = "/consul/connect-inject/consul-ca.pem"

	// ConsulCAFile is the location of the Consul CA file inside the injected pod.
	// This is used with the V2 API.
	ConsulCAFile = "/consul/mesh-inject/consul-ca.pem"

	// DefaultConsulNS is the default Consul namespace name.
	DefaultConsulNS = "default"

	// DefaultConsulPartition is the default Consul partition name.
	DefaultConsulPartition = "default"

	// DefaultConsulPeer is the name used to refer to resources that are in the same cluster.
	DefaultConsulPeer = "local"

	// ProxyDefaultInboundPort is the default inbound port for the proxy.
	ProxyDefaultInboundPort = 20000

	// ProxyDefaultHealthPort is the default HTTP health check port for the proxy.
	ProxyDefaultHealthPort = 21000

	// MetaGatewayKind is the meta key name for indicating which kind of gateway a Pod is for, if any.
	// The value should be one of "mesh", "api", or "terminating".
	MetaGatewayKind = "gateway-kind"

	// MetaKeyManagedBy is the meta key name for indicating which Kubernetes controller manages a Consul resource.
	MetaKeyManagedBy = "managed-by"

	// MetaKeyKubeNS is the meta key name for Kubernetes namespace used for the Consul services.
	MetaKeyKubeNS = "k8s-namespace"

	// MetaKeyKubeName is the meta key name for Kubernetes object name used for a Consul object.
	MetaKeyKubeName = "k8s-name"

	// MetaKeyDatacenter is the datacenter that this object was registered from.
	MetaKeyDatacenter = "datacenter"

	// MetaKeyKubeServiceName is the meta key name for Kubernetes service name used for the Consul services.
	MetaKeyKubeServiceName = "k8s-service-name"

	// MetaKeyKubeServiceAccountName is the meta key name for Kubernetes service account name used for the Consul
	// v2 workload identity.
	MetaKeyKubeServiceAccountName = "k8s-service-account-name"

	// MetaKeyPodName is the meta key name for Kubernetes pod name used for the Consul services.
	MetaKeyPodName = "pod-name"

	// MetaKeyPodUID is the meta key name for Kubernetes pod uid used for the Consul services.
	MetaKeyPodUID = "pod-uid"

	// DefaultGracefulPort is the default port that consul-dataplane uses for graceful shutdown.
	DefaultGracefulPort = 20600

	// DefaultGracefulShutdownPath is the default path that consul-dataplane uses for graceful shutdown.
	DefaultGracefulShutdownPath = "/graceful_shutdown"

	// DefaultGracefulStartupPath is the default path that consul-dataplane uses for graceful startup.
	DefaultGracefulStartupPath = "/graceful_startup"

	// DefaultWANPort is the default port that consul-dataplane uses for WAN.
	DefaultWANPort = 8443

	// ConsulKubernetesCheckType is the type of health check in Consul for Kubernetes readiness status.
	ConsulKubernetesCheckType = "kubernetes-readiness"

	// ConsulKubernetesCheckName is the name of health check in Consul for Kubernetes readiness status.
	ConsulKubernetesCheckName = "Kubernetes Readiness Check"

	KubernetesSuccessReasonMsg = "Kubernetes health checks passing"

	// MeshV2VolumePath is the name of the volume that contains the proxy ID.
	MeshV2VolumePath = "/consul/mesh-inject"

	UseTLSEnvVar        = "CONSUL_USE_TLS"
	CACertFileEnvVar    = "CONSUL_CACERT_FILE"
	CACertPEMEnvVar     = "CONSUL_CACERT_PEM"
	TLSServerNameEnvVar = "CONSUL_TLS_SERVER_NAME"
)

// GetNormalizedConsulNamespace returns the default namespace if the passed namespace
// is empty, otherwise returns back the passed in namespace.
func GetNormalizedConsulNamespace(ns string) string {
	if ns == "" {
		ns = DefaultConsulNS
	}

	return ns
}

// GetNormalizedConsulPartition returns the default partition if the passed partition
// is empty, otherwise returns back the passed in partition.
func GetNormalizedConsulPartition(ap string) string {
	if ap == "" {
		ap = DefaultConsulPartition
	}

	return ap
}

// GetNormalizedConsulPeer returns the default peer if the passed peer
// is empty, otherwise returns back the passed in peer.
func GetNormalizedConsulPeer(peer string) string {
	if peer == "" {
		peer = DefaultConsulPeer
	}

	return peer
}
