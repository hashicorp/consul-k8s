package connectinject

const (
	// keyInjectStatus is the key of the annotation that is added to
	// a pod after an injection is done.
	keyInjectStatus = "consul.hashicorp.com/connect-inject-status"

	// keyTransparentProxyStatus is the key of the annotation that is added to
	// a pod when transparent proxy is done.
	keyTransparentProxyStatus = "consul.hashicorp.com/transparent-proxy-status"

	// keyManagedBy is the key of the label that is added to pods managed
	// by the Endpoints controller. This is to support upgrading from consul-k8s
	// without Endpoints controller to consul-k8s with Endpoints controller
	// without disrupting services managed the old way.
	keyManagedBy = "consul.hashicorp.com/connect-inject-managed-by"

	// annotationInject is the key of the annotation that controls whether
	// injection is explicitly enabled or disabled for a pod. This should
	// be set to a truthy or falsy value, as parseable by strconv.ParseBool.
	annotationInject = "consul.hashicorp.com/connect-inject"

	// annotationGatewayKind is the key of the annotation that indicates pods
	// that represent Consul Connect Gateways. This should be set to a
	// value that is either "mesh", "ingress" or "terminating".
	annotationGatewayKind = "consul.hashicorp.com/gateway-kind"

	// annotationMeshGatewayConsulServiceName is the key of the annotation whose value
	// is the service name with which the mesh gateway is registered.
	annotationMeshGatewayConsulServiceName = "consul.hashicorp.com/mesh-gateway-consul-service-name"

	// annotationMeshGatewayContainerPort is the key of the annotation whose value is
	// used as the port and also registered as the LAN port when the mesh-gateway
	// service is registered.
	annotationMeshGatewayContainerPort = "consul.hashicorp.com/mesh-gateway-container-port"

	// annotationMeshGatewaySource is the key of the annotation that determines which
	// source to use to determine the wan address and wan port for the mesh-gateway
	// service registration.
	annotationMeshGatewaySource = "consul.hashicorp.com/mesh-gateway-wan-address-source"

	// annotationMeshGatewayWANAddress is the key of the annotation that when the source
	// of the mesh-gateway is 'Static', is the value of the WAN address for the gateway.
	annotationMeshGatewayWANAddress = "consul.hashicorp.com/mesh-gateway-wan-address-static"

	// annotationMeshGatewayWANPort is the key of the annotation whose value is the
	// WAN port for the mesh-gateway service registration.
	annotationMeshGatewayWANPort = "consul.hashicorp.com/mesh-gateway-wan-port"

	// annotationInjectMountVolumes is the key of the annotation that controls whether
	// the data volume that connect inject uses to store data including the Consul ACL token
	// is mounted to other containers in the pod. It is a comma-separated list of container names
	// to mount the volume on. It will be mounted at the path `/consul/connect-inject`.
	annotationInjectMountVolumes = "consul.hashicorp.com/connect-inject-mount-volume"

	// annotationService is the name of the service to proxy.
	// This defaults to the name of the Kubernetes service associated with the pod.
	annotationService = "consul.hashicorp.com/connect-service"

	// annotationKubernetesService is the name of the Kubernetes service to register.
	// This allows a pod to specify what Kubernetes service should trigger a Consul
	// service registration in the case of multiple services referencing a deployment.
	annotationKubernetesService = "consul.hashicorp.com/kubernetes-service"

	// annotationPort is the name or value of the port to proxy incoming
	// connections to.
	annotationPort = "consul.hashicorp.com/connect-service-port"

	// annotationProtocol contains the protocol that should be used for
	// the service that is being injected. Valid values are "http", "http2",
	// "grpc" and "tcp".
	//
	// Deprecated: This annotation is no longer supported.
	annotationProtocol = "consul.hashicorp.com/connect-service-protocol"

	// annotationUpstreams is a list of upstreams to register with the
	// proxy in the format of `<service-name>:<local-port>,...`. The
	// service name should map to a Consul service namd and the local port
	// is the local port in the pod that the listener will bind to. It can
	// be a named port.
	annotationUpstreams = "consul.hashicorp.com/connect-service-upstreams"

	// annotationTags is a list of tags to register with the service
	// this is specified as a comma separated list e.g. abc,123.
	annotationTags = "consul.hashicorp.com/service-tags"

	// annotationConnectTags is a list of tags to register with the service
	// this is specified as a comma separated list e.g. abc,123
	//
	// Deprecated: 'consul.hashicorp.com/service-tags' is the new annotation
	// and should be used instead. We made this change because the tagging is
	// not specific to connect as both the connect proxy *and* the Consul
	// service that gets registered is tagged.
	annotationConnectTags = "consul.hashicorp.com/connect-service-tags"

	// annotationMeta is a list of metadata key/value pairs to add to the service
	// registration. This is specified in the format `<key>:<value>`
	// e.g. consul.hashicorp.com/service-meta-foo:bar.
	annotationMeta = "consul.hashicorp.com/service-meta-"

	// annotationSyncPeriod controls the -sync-period flag passed to the
	// consul-k8s consul-sidecar command. This flag controls how often the
	// service is synced (i.e. re-registered) with the local agent.
	//
	// Deprecated: This annotation is no longer supported.
	annotationSyncPeriod = "consul.hashicorp.com/connect-sync-period"

	// annotations for sidecar proxy resource limits.
	annotationSidecarProxyCPULimit      = "consul.hashicorp.com/sidecar-proxy-cpu-limit"
	annotationSidecarProxyCPURequest    = "consul.hashicorp.com/sidecar-proxy-cpu-request"
	annotationSidecarProxyMemoryLimit   = "consul.hashicorp.com/sidecar-proxy-memory-limit"
	annotationSidecarProxyMemoryRequest = "consul.hashicorp.com/sidecar-proxy-memory-request"

	// annotations for consul sidecar resource limits.
	annotationConsulSidecarCPULimit      = "consul.hashicorp.com/consul-sidecar-cpu-limit"
	annotationConsulSidecarCPURequest    = "consul.hashicorp.com/consul-sidecar-cpu-request"
	annotationConsulSidecarMemoryLimit   = "consul.hashicorp.com/consul-sidecar-memory-limit"
	annotationConsulSidecarMemoryRequest = "consul.hashicorp.com/consul-sidecar-memory-request"

	// annotations for sidecar volumes.
	annotationConsulSidecarUserVolume      = "consul.hashicorp.com/consul-sidecar-user-volume"
	annotationConsulSidecarUserVolumeMount = "consul.hashicorp.com/consul-sidecar-user-volume-mount"

	// annotations for sidecar concurrency.
	annotationEnvoyProxyConcurrency = "consul.hashicorp.com/consul-envoy-proxy-concurrency"

	// annotations for metrics to configure where Prometheus scrapes
	// metrics from, whether to run a merged metrics endpoint on the consul
	// sidecar, and configure the connect service metrics.
	annotationEnableMetrics        = "consul.hashicorp.com/enable-metrics"
	annotationEnableMetricsMerging = "consul.hashicorp.com/enable-metrics-merging"
	annotationMergedMetricsPort    = "consul.hashicorp.com/merged-metrics-port"
	annotationPrometheusScrapePort = "consul.hashicorp.com/prometheus-scrape-port"
	annotationPrometheusScrapePath = "consul.hashicorp.com/prometheus-scrape-path"
	annotationServiceMetricsPort   = "consul.hashicorp.com/service-metrics-port"
	annotationServiceMetricsPath   = "consul.hashicorp.com/service-metrics-path"

	// todo (agentless): uncomment once consul-dataplane supports metrics
	/*
		annotations for configuring TLS for Prometheus.
		annotationPrometheusCAFile   = "consul.hashicorp.com/prometheus-ca-file"
		annotationPrometheusCAPath   = "consul.hashicorp.com/prometheus-ca-path"
		annotationPrometheusCertFile = "consul.hashicorp.com/prometheus-cert-file"
		annotationPrometheusKeyFile  = "consul.hashicorp.com/prometheus-key-file"
	*/

	// annotationEnvoyExtraArgs is a space-separated list of arguments to be passed to the
	// envoy binary. See list of args here: https://www.envoyproxy.io/docs/envoy/latest/operations/cli
	// e.g. consul.hashicorp.com/envoy-extra-args: "--log-level debug --disable-hot-restart"
	// The arguments passed in via this annotation will take precendence over arguments
	// passed via the -envoy-extra-args flag.
	annotationEnvoyExtraArgs = "consul.hashicorp.com/envoy-extra-args"

	// annotationConsulNamespace is the Consul namespace the service is registered into.
	annotationConsulNamespace = "consul.hashicorp.com/consul-namespace"

	// keyConsulDNS enables or disables Consul DNS for a given pod. It can also be set as a label
	// on a namespace to define the default behaviour for connect-injected pods which do not otherwise override this setting
	// with their own annotation.
	// This annotation/label takes a boolean value (true/false).
	keyConsulDNS = "consul.hashicorp.com/consul-dns"

	// keyTransparentProxy enables or disables transparent proxy for a given pod. It can also be set as a label
	// on a namespace to define the default behaviour for connect-injected pods which do not otherwise override this setting
	// with their own annotation.
	// This annotation/label takes a boolean value (true/false).
	keyTransparentProxy = "consul.hashicorp.com/transparent-proxy"

	// annotationTProxyExcludeInboundPorts is a comma-separated list of inbound ports to exclude from traffic redirection.
	annotationTProxyExcludeInboundPorts = "consul.hashicorp.com/transparent-proxy-exclude-inbound-ports"

	// annotationTProxyExcludeOutboundPorts is a comma-separated list of outbound ports to exclude from traffic redirection.
	annotationTProxyExcludeOutboundPorts = "consul.hashicorp.com/transparent-proxy-exclude-outbound-ports"

	// annotationTProxyExcludeOutboundCIDRs is a comma-separated list of outbound CIDRs to exclude from traffic redirection.
	annotationTProxyExcludeOutboundCIDRs = "consul.hashicorp.com/transparent-proxy-exclude-outbound-cidrs"

	// annotationTProxyExcludeUIDs is a comma-separated list of additional user IDs to exclude from traffic redirection.
	annotationTProxyExcludeUIDs = "consul.hashicorp.com/transparent-proxy-exclude-uids"

	// annotationTransparentProxyOverwriteProbes controls whether the Kubernetes probes should be overwritten
	// to point to the Envoy proxy when running in Transparent Proxy mode.
	annotationTransparentProxyOverwriteProbes = "consul.hashicorp.com/transparent-proxy-overwrite-probes"

	// annotationRedirectTraffic stores iptables.Config information so that the CNI plugin can use it to apply
	// iptables rules.
	annotationRedirectTraffic = "consul.hashicorp.com/redirect-traffic-config"

	// annotationOriginalPod is the value of the pod before being overwritten by the consul
	// webhook/meshWebhook.
	annotationOriginalPod = "consul.hashicorp.com/original-pod"

	// annotationPeeringVersion is the version of the peering resource and can be utilized
	// to explicitly perform the peering operation again.
	annotationPeeringVersion = "consul.hashicorp.com/peering-version"

	// labelServiceIgnore is a label that can be added to a service to prevent it from being
	// registered with Consul.
	labelServiceIgnore = "consul.hashicorp.com/service-ignore"

	// labelPeeringToken is a label that can be added to a secret to allow it to be watched
	// by the peering controllers.
	labelPeeringToken = "consul.hashicorp.com/peering-token"

	// injected is used as the annotation value for keyInjectStatus and annotationInjected.
	injected = "injected"

	// enabled is used as the annotation value for keyTransparentProxyStatus.
	enabled = "enabled"

	// endpointsController is the value for keyManagedBy.
	managedByValue = "consul-k8s-endpoints-controller"
)

// Annotations used by Prometheus.
const (
	annotationPrometheusScrape = "prometheus.io/scrape"
	annotationPrometheusPath   = "prometheus.io/path"
	annotationPrometheusPort   = "prometheus.io/port"
)
