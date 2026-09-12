// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package constants

import "os"

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

	UseTLSEnvVar          = "CONSUL_USE_TLS"
	CACertFileEnvVar      = "CONSUL_CACERT_FILE"
	CACertPEMEnvVar       = "CONSUL_CACERT_PEM"
	TLSServerNameEnvVar   = "CONSUL_TLS_SERVER_NAME"
	ConsulDualStackEnvVar = "CONSUL_DUAL_STACK"

	// AI agent role and container name constants.

	// AIAgentRole is the expected value of AnnotationAIRole for an AI agent workload.
	AIAgentRole = "ai-agent"

	// AIContainerName is the injected container name for the consul-mcp-gateway sidecar.
	// This container handles MCP body parsing: stamps x-mcp-method/tool/hitl headers.
	AIContainerName = "consul-mcp-gateway"

	// ConsulOBOInboundContainerName is the injected container name for the
	// consul-obo-inbound sidecar (previously consul-identity-processor).
	// This container handles INBOUND OBO: validates the user token on all incoming
	// requests and projects x-user-role / x-user-aud / x-claim-* before jwt_authn.
	ConsulOBOInboundContainerName = "consul-obo-inbound"

	// DefaultAIMCPOutboundPort is the loopback port the mcp-gateway dedicated outbound
	// listener binds to (maps to ai.agent.mcp.port in the service definition).
	DefaultAIMCPOutboundPort = 15101

	// DefaultAIHITLPort is the loopback port the agent HTTP server listens on for
	// human-in-the-loop approval callbacks (ai.agent.mcp.hitl.port).
	DefaultAIHITLPort = 16101

	// DefaultAIInterceptorPort is the loopback port the consul-mcp-gateway ext_proc
	// server binds to (ai.agent.interceptor.port).
	DefaultAIInterceptorPort = 21101

	// DefaultOBOInboundPort is the loopback port the consul-obo-inbound ext_proc
	// server binds to for INBOUND OBO.
	// Matches identityProcessorDefaultPort in consul-enterprise/agent/xds/egress_mcp_endpoint.go.
	DefaultOBOInboundPort = 21102

	// DefaultOBOOutboundPort is the loopback port the consul-obo-outbound ext_proc
	// server binds to for OUTBOUND OBO on all outbound calls (A2A, A2REST, A2MCP,
	// A2LLM). The corresponding Envoy outbound listener ext_proc filter points
	// here.  Must not collide with DefaultAIInterceptorPort (21101) or
	// DefaultOBOInboundPort (21102).
	DefaultOBOOutboundPort = 21103

	// ConsulOBOOutboundContainerName is the injected container name for the
	// consul-obo-outbound sidecar.
	// This container handles OUTBOUND OBO for all outbound calls from any service
	// with oauth_client=true: performs RFC 8693 token exchange and injects the
	// audience-bound JWT before the request leaves the mesh.
	ConsulOBOOutboundContainerName = "consul-obo-outbound"

	// DefaultGatewayBinary is the path to the consul-mcp-gateway binary inside the
	// consul-mcp-gateway image.  The AI Apps Dockerfile installs it at
	// /usr/local/bin/consul-mcp-gateway (COPY dist/…/consul-mcp-gateway /usr/local/bin/).
	DefaultGatewayBinary = "/usr/local/bin/consul-mcp-gateway"

	// DefaultOBOInboundBinary is the path to the consul-obo-inbound binary inside
	// the OBO inbound sidecar image.  The AI Apps Dockerfile installs it at
	// /usr/local/bin/consul-obo-inbound.
	DefaultOBOInboundBinary = "/usr/local/bin/consul-obo-inbound"

	// DefaultOBOOutboundBinary is the path to the consul-obo-outbound binary inside
	// the OBO outbound sidecar image.  The AI Apps Dockerfile installs it at
	// /usr/local/bin/consul-obo-outbound.
	DefaultOBOOutboundBinary = "/usr/local/bin/consul-obo-outbound"

	// ConsulBinarypath is the path to the consul binary inside the
	// consul-mcp-gateway image.
	ConsulBinarypath = "/app/consul"

	// DefaultDataplaneXDSPort is the fixed loopback port on which consul-dataplane
	// exposes the xDS ADS server to co-located containers.  consul-obo-outbound
	// subscribes to this endpoint as an SDS client to receive the GenericSecret
	// containing the OAuthClientConfig (oauth/<svcName>).
	// Must not collide with any Envoy admin port, OBO ports, or MCP ports.
	DefaultDataplaneXDSPort = 19500

	// DefaultEnvoyAdminPort is the loopback port on which Envoy's admin HTTP
	// server is bound by consul-dataplane (via -envoy-admin-bind-port=19000).
	// The /ready endpoint on this port returns HTTP 200 + "LIVE" only after all
	// clusters are initialised and the ADS stream to the Consul server is active.
	// consul-obo-outbound polls this endpoint before opening its SDS subscription
	// to ensure the proxy snapshot is ready to be served.
	DefaultEnvoyAdminPort = 19000
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

// IsDualStack checks ConsulDualStackEnvVar is set to true for dual stack.
func IsDualStack() bool {
	return os.Getenv(ConsulDualStackEnvVar) == "true"
}

func Getv4orv6Str(v4, v6 string) string {
	if IsDualStack() {
		return v6
	}
	return v4
}
