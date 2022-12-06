package constants

const (
	// KeyInjectStatus is the key of the annotation that is added to
	// a pod after an injection is done.
	KeyInjectStatus = "consul.hashicorp.com/connect-inject-status"

	// KeyTransparentProxyStatus is the key of the annotation that is added to
	// a pod when transparent proxy is done.
	KeyTransparentProxyStatus = "consul.hashicorp.com/transparent-proxy-status"

	// KeyManagedBy is the key of the label that is added to pods managed
	// by the Endpoints controller. This is to support upgrading from consul-k8s
	// without Endpoints controller to consul-k8s with Endpoints controller
	// without disrupting services managed the old way.
	KeyManagedBy = "consul.hashicorp.com/connect-inject-managed-by"

	// AnnotationInject is the key of the annotation that controls whether
	// injection is explicitly enabled or disabled for a pod. This should
	// be set to a truthy or falsy value, as parseable by strconv.ParseBool.
	AnnotationInject = "consul.hashicorp.com/connect-inject"

	// AnnotationGatewayKind is the key of the annotation that indicates pods
	// that represent Consul Connect Gateways. This should be set to a
	// value that is either "mesh", "ingress" or "terminating".
	AnnotationGatewayKind = "consul.hashicorp.com/gateway-kind"

	// AnnotationGatewayConsulServiceName is the key of the annotation whose value
	// is the service name with which the mesh gateway is registered.
	AnnotationGatewayConsulServiceName = "consul.hashicorp.com/gateway-consul-service-name"

	// AnnotationMeshGatewayContainerPort is the key of the annotation whose value is
	// used as the port and also registered as the LAN port when the mesh-gateway
	// service is registered.
	AnnotationMeshGatewayContainerPort = "consul.hashicorp.com/mesh-gateway-container-port"

	// AnnotationGatewayWANSource is the key of the annotation that determines which
	// source to use to determine the wan address and wan port for the mesh-gateway
	// service registration.
	AnnotationGatewayWANSource = "consul.hashicorp.com/gateway-wan-address-source"

	// AnnotationGatewayWANAddress is the key of the annotation that when the source
	// of the mesh-gateway is 'Static', is the value of the WAN address for the gateway.
	AnnotationGatewayWANAddress = "consul.hashicorp.com/gateway-wan-address-static"

	// AnnotationGatewayWANPort is the key of the annotation whose value is the
	// WAN port for the mesh-gateway service registration.
	AnnotationGatewayWANPort = "consul.hashicorp.com/gateway-wan-port"

	// AnnotationGatewayNamespace is the key of the annotation that indicates the
	// Consul namespace where a Terminating or Ingress Gateway pod is deployed.
	AnnotationGatewayNamespace = "consul.hashicorp.com/gateway-namespace"

	// AnnotationInjectMountVolumes is the key of the annotation that controls whether
	// the data volume that connect inject uses to store data including the Consul ACL token
	// is mounted to other containers in the pod. It is a comma-separated list of container names
	// to mount the volume on. It will be mounted at the path `/consul/connect-inject`.
	AnnotationInjectMountVolumes = "consul.hashicorp.com/connect-inject-mount-volume"

	// AnnotationService is the name of the service to proxy.
	// This defaults to the name of the Kubernetes service associated with the pod.
	AnnotationService = "consul.hashicorp.com/connect-service"

	// AnnotationKubernetesService is the name of the Kubernetes service to register.
	// This allows a pod to specify what Kubernetes service should trigger a Consul
	// service registration in the case of multiple services referencing a deployment.
	AnnotationKubernetesService = "consul.hashicorp.com/kubernetes-service"

	// AnnotationPort is the name or value of the port to proxy incoming
	// connections to.
	AnnotationPort = "consul.hashicorp.com/connect-service-port"

	// AnnotationUpstreams is a list of upstreams to register with the
	// proxy in the format of `<service-name>:<local-port>,...`. The
	// service name should map to a Consul service namd and the local port
	// is the local port in the pod that the listener will bind to. It can
	// be a named port.
	AnnotationUpstreams = "consul.hashicorp.com/connect-service-upstreams"

	// AnnotationTags is a list of tags to register with the service
	// this is specified as a comma separated list e.g. abc,123.
	AnnotationTags = "consul.hashicorp.com/service-tags"

	// AnnotationMeta is a list of metadata key/value pairs to add to the service
	// registration. This is specified in the format `<key>:<value>`
	// e.g. consul.hashicorp.com/service-meta-foo:bar.
	AnnotationMeta = "consul.hashicorp.com/service-meta-"

	// annotations for sidecar proxy resource limits.
	AnnotationSidecarProxyCPULimit      = "consul.hashicorp.com/sidecar-proxy-cpu-limit"
	AnnotationSidecarProxyCPURequest    = "consul.hashicorp.com/sidecar-proxy-cpu-request"
	AnnotationSidecarProxyMemoryLimit   = "consul.hashicorp.com/sidecar-proxy-memory-limit"
	AnnotationSidecarProxyMemoryRequest = "consul.hashicorp.com/sidecar-proxy-memory-request"

	// annotation makes sidecar shutdown gracefully.
	AnnotationSidecarProxyGracefulShutdown = "consul.hashicorp.com/sidecar-proxy-graceful-shutdown"

	// annotation to hold starting of app containers till sidecar proxy is started.
	AnnotationSidecarProxyHoldApplicationUntilProxyStarts = "consul.hashicorp.com/sidecar-hold-app-until-proxy-starts"

	// annotations for sidecar volumes.
	AnnotationConsulSidecarUserVolume      = "consul.hashicorp.com/consul-sidecar-user-volume"
	AnnotationConsulSidecarUserVolumeMount = "consul.hashicorp.com/consul-sidecar-user-volume-mount"

	// annotations for sidecar concurrency.
	AnnotationEnvoyProxyConcurrency = "consul.hashicorp.com/consul-envoy-proxy-concurrency"

	// annotations for metrics to configure where Prometheus scrapes
	// metrics from, whether to run a merged metrics endpoint on the consul
	// sidecar, and configure the connect service metrics.
	AnnotationEnableMetrics        = "consul.hashicorp.com/enable-metrics"
	AnnotationEnableMetricsMerging = "consul.hashicorp.com/enable-metrics-merging"
	AnnotationMergedMetricsPort    = "consul.hashicorp.com/merged-metrics-port"
	AnnotationPrometheusScrapePort = "consul.hashicorp.com/prometheus-scrape-port"
	AnnotationPrometheusScrapePath = "consul.hashicorp.com/prometheus-scrape-path"
	AnnotationServiceMetricsPort   = "consul.hashicorp.com/service-metrics-port"
	AnnotationServiceMetricsPath   = "consul.hashicorp.com/service-metrics-path"

	// annotations for configuring TLS for Prometheus.
	AnnotationPrometheusCAFile   = "consul.hashicorp.com/prometheus-ca-file"
	AnnotationPrometheusCAPath   = "consul.hashicorp.com/prometheus-ca-path"
	AnnotationPrometheusCertFile = "consul.hashicorp.com/prometheus-cert-file"
	AnnotationPrometheusKeyFile  = "consul.hashicorp.com/prometheus-key-file"

	// AnnotationEnvoyExtraArgs is a space-separated list of arguments to be passed to the
	// envoy binary. See list of args here: https://www.envoyproxy.io/docs/envoy/latest/operations/cli
	// e.g. consul.hashicorp.com/envoy-extra-args: "--log-level debug --disable-hot-restart"
	// The arguments passed in via this annotation will take precendence over arguments
	// passed via the -envoy-extra-args flag.
	AnnotationEnvoyExtraArgs = "consul.hashicorp.com/envoy-extra-args"

	// AnnotationConsulNamespace is the Consul namespace the service is registered into.
	AnnotationConsulNamespace = "consul.hashicorp.com/consul-namespace"

	// KeyConsulDNS enables or disables Consul DNS for a given pod. It can also be set as a label
	// on a namespace to define the default behaviour for connect-injected pods which do not otherwise override this setting
	// with their own annotation.
	// This annotation/label takes a boolean value (true/false).
	KeyConsulDNS = "consul.hashicorp.com/consul-dns"

	// KeyTransparentProxy enables or disables transparent proxy for a given pod. It can also be set as a label
	// on a namespace to define the default behaviour for connect-injected pods which do not otherwise override this setting
	// with their own annotation.
	// This annotation/label takes a boolean value (true/false).
	KeyTransparentProxy = "consul.hashicorp.com/transparent-proxy"

	// AnnotationTProxyExcludeInboundPorts is a comma-separated list of inbound ports to exclude from traffic redirection.
	AnnotationTProxyExcludeInboundPorts = "consul.hashicorp.com/transparent-proxy-exclude-inbound-ports"

	// AnnotationTProxyExcludeOutboundPorts is a comma-separated list of outbound ports to exclude from traffic redirection.
	AnnotationTProxyExcludeOutboundPorts = "consul.hashicorp.com/transparent-proxy-exclude-outbound-ports"

	// AnnotationTProxyExcludeOutboundCIDRs is a comma-separated list of outbound CIDRs to exclude from traffic redirection.
	AnnotationTProxyExcludeOutboundCIDRs = "consul.hashicorp.com/transparent-proxy-exclude-outbound-cidrs"

	// AnnotationTProxyExcludeUIDs is a comma-separated list of additional user IDs to exclude from traffic redirection.
	AnnotationTProxyExcludeUIDs = "consul.hashicorp.com/transparent-proxy-exclude-uids"

	// AnnotationTransparentProxyOverwriteProbes controls whether the Kubernetes probes should be overwritten
	// to point to the Envoy proxy when running in Transparent Proxy mode.
	AnnotationTransparentProxyOverwriteProbes = "consul.hashicorp.com/transparent-proxy-overwrite-probes"

	// AnnotationRedirectTraffic stores iptables.Config information so that the CNI plugin can use it to apply
	// iptables rules.
	AnnotationRedirectTraffic = "consul.hashicorp.com/redirect-traffic-config"

	// AnnotationOriginalPod is the value of the pod before being overwritten by the consul
	// webhook/meshWebhook.
	AnnotationOriginalPod = "consul.hashicorp.com/original-pod"

	// AnnotationPeeringVersion is the version of the peering resource and can be utilized
	// to explicitly perform the peering operation again.
	AnnotationPeeringVersion = "consul.hashicorp.com/peering-version"

	// AnnotationConsulK8sVersion is the current version of this binary.
	AnnotationConsulK8sVersion = "consul.hashicorp.com/connect-k8s-version"

	// LabelServiceIgnore is a label that can be added to a service to prevent it from being
	// registered with Consul.
	LabelServiceIgnore = "consul.hashicorp.com/service-ignore"

	// LabelPeeringToken is a label that can be added to a secret to allow it to be watched
	// by the peering controllers.
	LabelPeeringToken = "consul.hashicorp.com/peering-token"

	// Injected is used as the annotation value for keyInjectStatus and annotationInjected.
	Injected = "injected"

	// Enabled is used as the annotation value for keyTransparentProxyStatus.
	Enabled = "enabled"

	// ManagedByValue is the value for keyManagedBy.
	ManagedByValue = "consul-k8s-endpoints-controller"
)

// Annotations used by Prometheus.
const (
	AnnotationPrometheusScrape = "prometheus.io/scrape"
	AnnotationPrometheusPath   = "prometheus.io/path"
	AnnotationPrometheusPort   = "prometheus.io/port"
)
