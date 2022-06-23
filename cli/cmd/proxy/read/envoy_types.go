package read

/* Envoy Types
These types are based on the JSON returned from the Envoy Config Dump API on the
admin interface. They are a subset of what is returned from that API to support
unmarshaling in the ConfigDump struct.
Please refer to the Envoy config dump documentation when modifying or extending:
https://www.envoyproxy.io/docs/envoy/latest/api-v3/admin/v3/config_dump.proto
*/

type root struct {
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
	DynamicListeners []listenerConfig `json:"dynamic_listeners"`
}

type listenerConfig struct {
	Name        string      `json:"name"`
	ActiveState activeState `json:"active_state"`
}

type activeState struct {
	Listener         listener `json:"listener"`
	TrafficDirection string   `json:"traffic_direction"`
}

type listener struct {
	Address      address       `json:"address"`
	FilterChains []filterChain `json:"filter_chains"`
}

type filterChain struct {
	Filters []filter `json:"filter"`
}

type filter struct {
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
