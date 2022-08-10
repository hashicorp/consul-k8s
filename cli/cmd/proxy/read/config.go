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

		// Don't display "non-domain name" FQDNs (e.g. local_app)
		fqdn := cluster.Cluster.FQDN
		if !strings.Contains(fqdn, ".") {
			fqdn = ""
		}

		clusters = append(clusters, Cluster{
			Name:                     strings.Split(cluster.Cluster.FQDN, ".")[0],
			FullyQualifiedDomainName: fqdn,
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
				Filters:          formatFilters(chain),
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

func formatFilters(filterChain filterChain) (filters []string) {
	// Filters can have many custom configurations, each must be handled differently.
	formatters := map[string]func(typedConfig) string{
		"type.googleapis.com/envoy.extensions.filters.network.rbac.v3.RBAC":                                     formatFilterRBAC,
		"type.googleapis.com/envoy.extensions.filters.network.tcp_proxy.v3.TcpProxy":                            formatFilterTCPProxy,
		"type.googleapis.com/envoy.extensions.filters.network.http_connection_manager.v3.HttpConnectionManager": formatFilterHTTPConnectionManager,
	}

	for _, chainFilter := range filterChain.Filters {
		if formatter, ok := formatters[chainFilter.TypedConfig.Type]; ok {
			filters = append(filters, formatter(chainFilter.TypedConfig))
		}
	}
	return
}

func formatFilterTCPProxy(config typedConfig) (filter string) {
	return "to " + config.Cluster
}

func formatFilterRBAC(cfg typedConfig) (filter string) {
	action := cfg.Rules.Action
	for _, principal := range cfg.Rules.Policies.ConsulIntentions.Principals {
		regex := principal.Authenticated.PrincipalName.SafeRegex.Regex
		filter += fmt.Sprintf("%s %s", action, regex)
	}
	return
}

func formatFilterHTTPConnectionManager(cfg typedConfig) (filter string) {
	for _, host := range cfg.RouteConfig.VirtualHosts {
		filter += strings.Join(host.Domains, ", ")
		filter += " to "

		routes := ""
		for _, route := range host.Routes {
			routes += fmt.Sprintf("%s%s", route.Route.Cluster, route.Match.Prefix)
		}
		filter += routes
	}
	return
}
