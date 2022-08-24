package read

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/hashicorp/consul-k8s/cli/common"
)

// EnvoyConfig represents the configuration retrieved from a config dump at the
// admin endpoint. It wraps the Envoy ConfigDump struct to give us convenient
// access to the different sections of the config.
type EnvoyConfig struct {
	rawCfg    []byte
	Clusters  []Cluster
	Endpoints []Endpoint
	Listeners []Listener
	Routes    []Route
	Secrets   []Secret
}

// Cluster represents a cluster in the Envoy config.
type Cluster struct {
	Name                     string
	FullyQualifiedDomainName string
	Endpoints                []string
	Type                     string
	LastUpdated              string
}

// Endpoint represents an endpoint in the Envoy config.
type Endpoint struct {
	Address string
	Cluster string
	Weight  float64
	Status  string
}

// Listener represents a listener in the Envoy config.
type Listener struct {
	Name        string
	Address     string
	FilterChain []FilterChain
	Direction   string
	LastUpdated string
}

type FilterChain struct {
	Filters          []string
	FilterChainMatch string
}

// Route represents a route in the Envoy config.
type Route struct {
	Name               string
	DestinationCluster string
	LastUpdated        string
}

// Secret represents a secret in the Envoy config.
type Secret struct {
	Name        string
	Type        string
	LastUpdated string
}

// FetchConfig opens a port forward to the Envoy admin API and fetches the
// configuration from the config dump endpoint.
func FetchConfig(ctx context.Context, portForward common.PortForwarder) (*EnvoyConfig, error) {
	endpoint, err := portForward.Open(ctx)
	if err != nil {
		return nil, err
	}
	defer portForward.Close()

	// Fetch the config dump
	response, err := http.Get(fmt.Sprintf("http://%s/config_dump?include_eds", endpoint))
	if err != nil {
		return nil, err
	}
	configDump, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	if err := response.Body.Close(); err != nil {
		return nil, err
	}

	// Fetch the clusters mapping
	response, err = http.Get(fmt.Sprintf("http://%s/clusters?format=json", endpoint))
	if err != nil {
		return nil, err
	}
	clusters, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	if err := response.Body.Close(); err != nil {
		return nil, err
	}

	config := fmt.Sprintf("{\n\"config_dump\":%s,\n\"clusters\":%s}", string(configDump), string(clusters))

	envoyConfig := &EnvoyConfig{}
	err = json.Unmarshal([]byte(config), envoyConfig)
	if err != nil {
		return nil, err
	}
	return envoyConfig, nil
}

// JSON returns the original JSON Envoy config dump data which was used to create
// the Config object.
func (c *EnvoyConfig) JSON() []byte {
	return c.rawCfg
}

// UnmarshalJSON implements the json.Unmarshaler interface to unmarshal the raw
// config dump bytes into EnvoyConfig. It saves a copy of the original bytes
// which can be fetched with the JSON method.
func (c *EnvoyConfig) UnmarshalJSON(b []byte) error {
	// Save the original config dump bytes for marshalling. We should treat this
	// struct as immutable so this should be safe.
	c.rawCfg = b

	var root root
	err := json.Unmarshal(b, &root)

	clusterMapping, endpointMapping := make(map[string][]string), make(map[string]string)
	for _, clusterStatus := range root.Clusters.ClusterStatuses {
		var addresses []string
		for _, status := range clusterStatus.HostStatuses {
			address := fmt.Sprintf("%s:%d", status.Address.SocketAddress.Address, int(status.Address.SocketAddress.PortValue))
			addresses = append(addresses, address)
			endpointMapping[address] = clusterStatus.Name
		}
		clusterMapping[clusterStatus.Name] = addresses
	}

	// Dispatch each section to the appropriate parsing function by its type.
	for _, config := range root.ConfigDump.Configs {
		switch config["@type"] {
		case "type.googleapis.com/envoy.admin.v3.ClustersConfigDump":
			clusters, err := parseClusters(config, clusterMapping)
			if err != nil {
				return err
			}
			c.Clusters = clusters
		case "type.googleapis.com/envoy.admin.v3.EndpointsConfigDump":
			endpoints, err := parseEndpoints(config, endpointMapping)
			if err != nil {
				return err
			}
			c.Endpoints = endpoints
		case "type.googleapis.com/envoy.admin.v3.ListenersConfigDump":
			listeners, err := parseListeners(config)
			if err != nil {
				return err
			}
			c.Listeners = listeners
		case "type.googleapis.com/envoy.admin.v3.RoutesConfigDump":
			routes, err := parseRoutes(config)
			if err != nil {
				return err
			}
			c.Routes = routes
		case "type.googleapis.com/envoy.admin.v3.SecretsConfigDump":
			secrets, err := parseSecrets(config)
			if err != nil {
				return err
			}
			c.Secrets = secrets
		}
	}

	return err
}

func parseClusters(rawCfg map[string]interface{}, clusterMapping map[string][]string) ([]Cluster, error) {
	clusters := make([]Cluster, 0)

	raw, err := json.Marshal(rawCfg)
	if err != nil {
		return clusters, err
	}

	var clustersCD clustersConfigDump
	if err = json.Unmarshal(raw, &clustersCD); err != nil {
		return clusters, err
	}

	for _, cluster := range append(clustersCD.StaticClusters, clustersCD.DynamicActiveClusters...) {
		// Join nested endpoint data into a slice of strings.
		endpoints := make([]string, 0)
		for _, endpoint := range cluster.Cluster.LoadAssignment.Endpoints {
			for _, lbEndpoint := range endpoint.LBEndpoints {
				endpoints = append(endpoints, fmt.Sprintf("%s:%d", lbEndpoint.Endpoint.Address.SocketAddress.Address,
					int(lbEndpoint.Endpoint.Address.SocketAddress.PortValue)))
			}
		}

		// Add addresses discovered by EDS if not already added
		if addresses, ok := clusterMapping[cluster.Cluster.FQDN]; ok {
			for _, endpoint := range addresses {
				alreadyAdded := false
				for _, existingEndpoint := range endpoints {
					if existingEndpoint == endpoint {
						alreadyAdded = true
						break
					}
				}
				if !alreadyAdded {
					endpoints = append(endpoints, endpoint)
				}
			}
		}

		clusters = append(clusters, Cluster{
			Name:                     strings.Split(cluster.Cluster.FQDN, ".")[0],
			FullyQualifiedDomainName: cluster.Cluster.FQDN,
			Endpoints:                endpoints,
			Type:                     cluster.Cluster.ClusterType,
			LastUpdated:              cluster.LastUpdated,
		})
	}

	return clusters, nil
}

func parseEndpoints(rawCfg map[string]interface{}, endpointMapping map[string]string) ([]Endpoint, error) {
	endpoints := make([]Endpoint, 0)

	raw, err := json.Marshal(rawCfg)
	if err != nil {
		return endpoints, err
	}

	var endpointsCD endpointsConfigDump
	if err = json.Unmarshal(raw, &endpointsCD); err != nil {
		return endpoints, err
	}

	for _, endpointConfig := range append(endpointsCD.StaticEndpointConfigs, endpointsCD.DynamicEndpointConfigs...) {
		for _, endpoint := range endpointConfig.EndpointConfig.Endpoints {
			for _, lbEndpoint := range endpoint.LBEndpoints {
				address := fmt.Sprintf("%s:%d", lbEndpoint.Endpoint.Address.SocketAddress.Address, int(lbEndpoint.Endpoint.Address.SocketAddress.PortValue))

				cluster := endpointConfig.EndpointConfig.Name
				// Fill in cluster from EDS endpoint mapping.
				if edsCluster, ok := endpointMapping[address]; ok && cluster == "" {
					cluster = edsCluster
				}

				endpoints = append(endpoints, Endpoint{
					Address: address,
					Cluster: strings.Split(cluster, ".")[0],
					Weight:  lbEndpoint.LoadBalancingWeight,
					Status:  lbEndpoint.HealthStatus,
				})
			}
		}
	}

	return endpoints, nil
}

func parseListeners(rawCfg map[string]interface{}) ([]Listener, error) {
	listeners := make([]Listener, 0)

	raw, err := json.Marshal(rawCfg)
	if err != nil {
		return listeners, err
	}

	var listenersCD listenersConfigDump
	if err = json.Unmarshal(raw, &listenersCD); err != nil {
		return listeners, err
	}

	listenersConfig := []listenerConfig{}
	for _, listener := range listenersCD.DynamicListeners {
		listenersConfig = append(listenersConfig, listener.ActiveState)
	}
	listenersConfig = append(listenersConfig, listenersCD.StaticListeners...)

	for _, listener := range listenersConfig {
		address := fmt.Sprintf("%s:%d", listener.Listener.Address.SocketAddress.Address, int(listener.Listener.Address.SocketAddress.PortValue))

		// Format the filter chain configs into something more readable.
		filterChain := []FilterChain{}
		for _, chain := range listener.Listener.FilterChains {
			filterChainMatch := []string{}
			for _, prefixRange := range chain.FilterChainMatch.PrefixRanges {
				filterChainMatch = append(filterChainMatch, fmt.Sprintf("%s/%d", prefixRange.AddressPrefix, int(prefixRange.PrefixLen)))
			}
			if len(filterChainMatch) == 0 {
				filterChainMatch = append(filterChainMatch, "Any")
			}

			filterChain = append(filterChain, FilterChain{
				FilterChainMatch: strings.Join(filterChainMatch, ", "),
				Filters:          formatFilters(chain.Filters),
			})
		}

		direction := "UNSPECIFIED"
		if listener.Listener.TrafficDirection != "" {
			direction = listener.Listener.TrafficDirection
		}

		listeners = append(listeners, Listener{
			Name:        strings.Split(listener.Listener.Name, ":")[0],
			Address:     address,
			FilterChain: filterChain,
			Direction:   direction,
			LastUpdated: listener.LastUpdated,
		})
	}

	return listeners, nil
}

func parseRoutes(rawCfg map[string]interface{}) ([]Route, error) {
	routes := make([]Route, 0)

	raw, err := json.Marshal(rawCfg)
	if err != nil {
		return routes, err
	}

	var routesCD routesConfigDump
	if err = json.Unmarshal(raw, &routesCD); err != nil {
		return routes, err
	}

	for _, route := range routesCD.StaticRouteConfigs {
		destinationClusters := []string{}
		for _, host := range route.RouteConfig.VirtualHosts {
			for _, routeCfg := range host.Routes {
				destinationClusters = append(destinationClusters,
					fmt.Sprintf("%s%s", routeCfg.Route.Cluster, routeCfg.Match.Prefix))
			}
		}

		routes = append(routes, Route{
			Name:               route.RouteConfig.Name,
			DestinationCluster: strings.Join(destinationClusters, ", "),
			LastUpdated:        route.LastUpdated,
		})
	}

	return routes, nil
}

func parseSecrets(rawCfg map[string]interface{}) ([]Secret, error) {
	secrets := make([]Secret, 0)

	raw, err := json.Marshal(rawCfg)
	if err != nil {
		return secrets, err
	}

	var secretsCD secretsConfigDump
	if err = json.Unmarshal(raw, &secretsCD); err != nil {
		return secrets, err
	}

	for _, secret := range secretsCD.StaticSecrets {
		secrets = append(secrets, Secret{
			Name:        secret.Name,
			Type:        "Static",
			LastUpdated: secret.LastUpdated,
		})
	}

	for _, secret := range secretsCD.DynamicActiveSecrets {
		secrets = append(secrets, Secret{
			Name:        secret.Name,
			Type:        "Dynamic Active",
			LastUpdated: secret.LastUpdated,
		})
	}

	for _, secret := range secretsCD.DynamicWarmingSecrets {
		secrets = append(secrets, Secret{
			Name:        secret.Name,
			Type:        "Dynamic Warming",
			LastUpdated: secret.LastUpdated,
		})
	}

	return secrets, nil
}

func formatFilters(filters []filter) (formatted []string) {
	// Filters can have many custom configurations, each must be handled differently.
	// [List of known extensions](https://www.envoyproxy.io/docs/envoy/latest/api-v3/config/listener/v3/listener_components.proto).
	formatters := map[string]func(filter) string{
		"type.googleapis.com/envoy.extensions.filters.network.connection_limit.v3.ConnectionLimit":              formatFilterConnectionLimit,
		"type.googleapis.com/envoy.extensions.filters.network.direct_response.v3.Config":                        formatFilterDirectResponse,
		"type.googleapis.com/envoy.extensions.filters.network.echo.v3.Echo":                                     formatFilterEcho,
		"type.googleapis.com/envoy.extensions.filters.network.ext_authz.v3.ExtAuthz":                            formatFilterExtAuthz,
		"type.googleapis.com/envoy.extensions.filters.network.http_connection_manager.v3.HttpConnectionManager": formatFilterHTTPConnectionManager,
		"type.googleapis.com/envoy.extensions.filters.network.local_ratelimit.v3.LocalRateLimit":                formatFilterLocalRatelimit,
		"type.googleapis.com/envoy.extensions.filters.network.mongo_proxy.v3.MongoProxy":                        formatFilterMongoProxy,
		"type.googleapis.com/envoy.extensions.filters.network.ratelimit.v3.RateLimit":                           formatFilterRatelimit,
		"type.googleapis.com/envoy.extensions.filters.network.rbac.v3.RBAC":                                     formatFilterRBAC,
		"type.googleapis.com/envoy.extensions.filters.network.redis_proxy.v3.RedisProxy":                        formatFilterRedisProxy,
		"type.googleapis.com/envoy.extensions.filters.network.sni_cluster.v3.SniCluster":                        formatFilterSniCluster,
		"type.googleapis.com/envoy.extensions.filters.network.sni_dynamic_forward_proxy.v3.FilterConfig":        formatFilterSniDynamicForwardProxy,
		"type.googleapis.com/envoy.extensions.filters.network.tcp_proxy.v3.TcpProxy":                            formatFilterTCPProxy,
		"type.googleapis.com/envoy.extensions.filters.network.thrift_proxy.v3.ThriftProxy":                      formatFilterThriftProxy,
		"type.googleapis.com/envoy.extensions.filters.network.wasm.v3.Wasm":                                     formatFilterWasm,
		"type.googleapis.com/envoy.extensions.filters.network.zookeeper_proxy.v3.ZooKeeperProxy":                formatFilterZookeeperProxy,
	}

	for _, chainFilter := range filters {
		if formatter, ok := formatters[chainFilter.TypedConfig.Type]; ok {
			formatted = append(formatted, formatter(chainFilter))
		} else {
			formatted = append(formatted, "unknown filter format")
		}
	}
	return formatted
}

func formatFilterConnectionLimit(config filter) string {
	return fmt.Sprintf("%d max connections with %s delay", config.TypedConfig.MaxConnections, config.TypedConfig.Delay)
}

func formatFilterDirectResponse(config filter) string {
	out := []string{"->"}
	if file := config.TypedConfig.Response.Filename; file != "" {
		out = append(out, fmt.Sprintf("file:%s", file))
	}
	if inlineBytes := config.TypedConfig.Response.InlineBytes; len(inlineBytes) != 0 {
		if len(inlineBytes) > 24 {
			out = append(out, fmt.Sprintf("bytes:%s...", string(inlineBytes)[:24]))
		} else {
			out = append(out, fmt.Sprintf("bytes:%s", string(inlineBytes)))
		}
	}
	if inlineString := config.TypedConfig.Response.InlineString; inlineString != "" {
		if len(inlineString) > 24 {
			out = append(out, fmt.Sprintf("string:%s...", inlineString[:24]))
		} else {
			out = append(out, fmt.Sprintf("string:%s", inlineString))
		}
	}
	if envVar := config.TypedConfig.Response.EnvironmentVariable; envVar != "" {
		out = append(out, fmt.Sprintf("env:%s", envVar))
	}

	return strings.Join(out, " ")
}

func formatFilterEcho(config filter) string {
	return ""
}

func formatFilterExtAuthz(config filter) string {
	return ""
}

func formatFilterHTTPConnectionManager(config filter) string {
	var out string
	for _, host := range config.TypedConfig.RouteConfig.VirtualHosts {
		out += strings.Join(host.Domains, ", ")
		out += " -> "

		routes := ""
		for _, route := range host.Routes {
			routes += fmt.Sprintf("%s%s", route.Route.Cluster, route.Match.Prefix)
		}
		out += routes
	}
	return out
}

func formatFilterLocalRatelimit(config filter) string {
	return ""
}

func formatFilterMongoProxy(config filter) string {
	return ""
}

func formatFilterRatelimit(config filter) string {
	return ""
}

func formatFilterRBAC(config filter) string {
	var out string
	action := config.TypedConfig.Rules.Action
	for _, principal := range config.TypedConfig.Rules.Policies.ConsulIntentions.Principals {
		regex := principal.Authenticated.PrincipalName.SafeRegex.Regex
		out += fmt.Sprintf("%s %s", action, regex)
	}
	return out
}

func formatFilterRedisProxy(config filter) string {
	return ""
}

func formatFilterSniCluster(config filter) string {
	return ""
}

func formatFilterSniDynamicForwardProxy(config filter) string {
	return ""
}

func formatFilterTCPProxy(config filter) string {
	return "-> " + config.TypedConfig.Cluster
}

func formatFilterThriftProxy(config filter) (filter string) {
	return filter
}

func formatFilterWasm(config filter) (filter string) {
	return filter
}

func formatFilterZookeeperProxy(config filter) (filter string) {
	return filter
}
