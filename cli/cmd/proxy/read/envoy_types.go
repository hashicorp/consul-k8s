package read

/* Envoy Types
These types are based on the JSON returned from the Envoy Config Dump API on the
admin interface. They are a subset of what is returned from that API to support
unmarshaling in the ConfigDump struct.
Please refer to the Envoy config dump documentation when modifying or extending:
https://www.envoyproxy.io/docs/envoy/latest/api-v3/admin/v3/config_dump.proto
*/

type root struct {
	ConfigDump configDump `json:"config_dump"`
	Clusters   clusters   `json:"clusters"`
}

type configDump struct {
	Configs []map[string]interface{} `json:"configs"`
}

type clustersConfigDump struct {
	ConfigType            string          `json:"@type"`
	StaticClusters        []clusterConfig `json:"static_clusters"`
	DynamicActiveClusters []clusterConfig `json:"dynamic_active_clusters"`
}

type clusterConfig struct {
	Cluster     clusterMeta `json:"cluster"`
	LastUpdated string      `json:"last_updated"`
}

type clusterMeta struct {
	FQDN           string         `json:"name"`
	ClusterType    string         `json:"type"`
	LoadAssignment loadAssignment `json:"load_assignment"`
}

type loadAssignment struct {
	Endpoints []endpoint `json:"endpoints"`
}

type endpoint struct {
	LBEndpoints []lbEndpoint `json:"lb_endpoints"`
}

type lbEndpoint struct {
	Endpoint            ep      `json:"endpoint"`
	HealthStatus        string  `json:"health_status"`
	LoadBalancingWeight float64 `json:"load_balancing_weight"`
}

type ep struct {
	Address address `json:"address"`
}

type address struct {
	SocketAddress socketAddress `json:"socket_address"`
}

type socketAddress struct {
	Address   string  `json:"address"`
	PortValue float64 `json:"port_value"`
}

type endpointsConfigDump struct {
	ConfigType             string              `json:"@type"`
	StaticEndpointConfigs  []endpointConfigMap `json:"static_endpoint_configs"`
	DynamicEndpointConfigs []endpointConfigMap `json:"dynamic_endpoint_configs"`
}

type endpointConfigMap struct {
	EndpointConfig endpointConfig `json:"endpoint_config"`
}

type endpointConfig struct {
	ConfigType string     `json:"@type"`
	Name       string     `json:"cluster_name"`
	Endpoints  []endpoint `json:"endpoints"`
}

type listenersConfigDump struct {
	ConfigType       string           `json:"@type"`
	DynamicListeners []dynamicConfig  `json:"dynamic_listeners"`
	StaticListeners  []listenerConfig `json:"static_listeners"`
}

type dynamicConfig struct {
	Name        string         `json:"name"`
	ActiveState listenerConfig `json:"active_state"`
}

type listenerConfig struct {
	Listener    listener `json:"listener"`
	LastUpdated string   `json:"last_updated"`
}

type listener struct {
	Name             string        `json:"name"`
	Address          address       `json:"address"`
	FilterChains     []filterChain `json:"filter_chains"`
	TrafficDirection string        `json:"traffic_direction"`
}

type filterChain struct {
	Filters          []filter         `json:"filters"`
	FilterChainMatch filterChainMatch `json:"filter_chain_match"`
}

type filter struct {
	Name        string      `json:"name"`
	TypedConfig typedConfig `json:"typed_config"`
}

type typedConfig struct {
	Type        string            `json:"@type"`
	Cluster     string            `json:"cluster"`
	RouteConfig filterRouteConfig `json:"route_config"`
	HttpFilters []httpFilter      `json:"http_filters"`
	Rules       rules             `json:"rules"`
}

type filterRouteConfig struct {
	Name         string              `json:"name"`
	VirtualHosts []filterVirtualHost `json:"virtual_hosts"`
}

type filterVirtualHost struct {
	Name    string        `json:"name"`
	Domains []string      `json:"domains"`
	Routes  []filterRoute `json:"routes"`
}

type filterRoute struct {
	Match filterMatch        `json:"match"`
	Route filterRouteCluster `json:"route"`
}

type filterMatch struct {
	Prefix string `json:"prefix"`
}

type filterRouteCluster struct {
	Cluster string `json:"cluster"`
}

type filterChainMatch struct {
	PrefixRanges []prefixRange `json:"prefix_ranges"`
}

type prefixRange struct {
	AddressPrefix string  `json:"address_prefix"`
	PrefixLen     float64 `json:"prefix_len"`
}

type httpFilter struct {
	TypedConfig httpTypedConfig `json:"typed_config"`
}

type httpTypedConfig struct {
	Rules rules `json:"rules"`
}

type rules struct {
	Action   string                  `json:"action"`
	Policies httpTypedConfigPolicies `json:"policies"`
}

type httpTypedConfigPolicies struct {
	ConsulIntentions httpTypedConfigConsulIntentions `json:"consul-intentions-layer4"`
}

type httpTypedConfigConsulIntentions struct {
	Principals []principal `json:"principals"`
}

type principal struct {
	Authenticated authenticated `json:"authenticated"`
}

type authenticated struct {
	PrincipalName principalName `json:"principal_name"`
}

type principalName struct {
	SafeRegex safeRegex `json:"safe_regex"`
}

type safeRegex struct {
	Regex string `json:"regex"`
}

type routesConfigDump struct {
	ConfigType         string           `json:"@type"`
	StaticRouteConfigs []routeConfigMap `json:"static_route_configs"`
}

type routeConfigMap struct {
	RouteConfig routeConfig `json:"route_config"`
	LastUpdated string      `json:"last_updated"`
}

type routeConfig struct {
	Name         string        `json:"name"`
	VirtualHosts []virtualHost `json:"virtual_hosts"`
}

type virtualHost struct {
	Routes []route `json:"routes"`
}

type route struct {
	Match routeMatch `json:"match"`
	Route routeRoute `json:"route"`
}

type routeMatch struct {
	Prefix string `json:"prefix"`
}

type routeRoute struct {
	Cluster string `json:"cluster"`
}

type secretsConfigDump struct {
	ConfigType            string            `json:"@type"`
	StaticSecrets         []secretConfigMap `json:"static_secrets"`
	DynamicActiveSecrets  []secretConfigMap `json:"dynamic_active_secrets"`
	DynamicWarmingSecrets []secretConfigMap `json:"dynamic_warming_secrets"`
}

type secretConfigMap struct {
	Name        string `json:"name"`
	Secret      secret `json:"secret"`
	LastUpdated string `json:"last_updated"`
}

type secret struct {
	Type              string            `json:"@type"`
	TLSCertificate    tlsCertificate    `json:"tls_certificate"`
	ValidationContext validationContext `json:"validation_context"`
}

type tlsCertificate struct {
	CertificateChain certificateChain `json:"certificate_chain"`
}

type validationContext struct {
	TrustedCA trustedCA `json:"trusted_ca"`
}

type certificateChain struct {
	InlineBytes string `json:"inline_bytes"`
}

type trustedCA struct {
	InlineBytes string `json:"inline_bytes"`
}

type clusters struct {
	ClusterStatuses []clusterStatus `json:"cluster_statuses"`
}

type clusterStatus struct {
	Name         string       `json:"name"`
	HostStatuses []hostStatus `json:"host_statuses"`
}

type hostStatus struct {
	Address address `json:"address"`
}
