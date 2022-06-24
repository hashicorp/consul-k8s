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

	response, err := http.Get(fmt.Sprintf("http://%s/config_dump?include_eds", endpoint))
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	if err := response.Body.Close(); err != nil {
		return nil, err
	}

	envoyConfig := &EnvoyConfig{}
	err = json.Unmarshal(raw, envoyConfig)
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

// UnmarshalJSON unmarshals the raw config dump bytes into EnvoyConfig.
func (c *EnvoyConfig) UnmarshalJSON(b []byte) error {
	// Save the original config dump bytes for marshalling. We should treat this
	// struct as immutable so this should be safe.
	c.rawCfg = b

	var root root
	err := json.Unmarshal(b, &root)

	// Dispatch each section to the appropriate parsing function by its type.
	for _, config := range root.Configs {
		switch config["@type"] {
		case "type.googleapis.com/envoy.admin.v3.ClustersConfigDump":
			clusters, err := parseClusters(config)
			if err != nil {
				return err
			}
			c.Clusters = clusters
		case "type.googleapis.com/envoy.admin.v3.EndpointsConfigDump":
			endpoints, err := parseEndpoints(config)
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

func parseClusters(rawCfg map[string]interface{}) ([]Cluster, error) {
	var clusters []Cluster

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
		var endpoints []string
		for _, endpoint := range cluster.Cluster.LoadAssignment.Endpoints {
			for _, lbEndpoint := range endpoint.LBEndpoints {
				endpoints = append(endpoints, fmt.Sprintf("%s:%d", lbEndpoint.Endpoint.Address.SocketAddress.Address,
					int(lbEndpoint.Endpoint.Address.SocketAddress.PortValue)))
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

func parseEndpoints(rawCfg map[string]interface{}) ([]Endpoint, error) {
	var endpoints []Endpoint

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
				endpoints = append(endpoints, Endpoint{
					Address: fmt.Sprintf("%s:%d", lbEndpoint.Endpoint.Address.SocketAddress.Address,
						int(lbEndpoint.Endpoint.Address.SocketAddress.PortValue)),
					Cluster: endpointConfig.EndpointConfig.Name,
					Weight:  lbEndpoint.LoadBalancingWeight,
					Status:  lbEndpoint.HealthStatus,
				})
			}
		}
	}

	return endpoints, nil
}

func parseListeners(rawCfg map[string]interface{}) ([]Listener, error) {
	var listeners []Listener

	raw, err := json.Marshal(rawCfg)
	if err != nil {
		return listeners, err
	}

	var listenersCD listenersConfigDump
	if err = json.Unmarshal(raw, &listenersCD); err != nil {
		return listeners, err
	}

	for _, listener := range listenersCD.DynamicListeners {
		address := fmt.Sprintf("%s:%d", listener.ActiveState.Listener.Address.SocketAddress.Address, int(listener.ActiveState.Listener.Address.SocketAddress.PortValue))

		// Format the filter chain configs into something more readable.
		filterChain := []FilterChain{}
		for _, chain := range listener.ActiveState.Listener.FilterChains {
			filterChainMatch, filters := formatFilterChain(chain)
			filterChain = append(filterChain, FilterChain{
				FilterChainMatch: filterChainMatch,
				Filters:          filters,
			})
		}

		listeners = append(listeners, Listener{
			Name:        strings.Split(listener.Name, ":")[0],
			Address:     address,
			FilterChain: filterChain,
			Direction:   listener.ActiveState.Listener.TrafficDirection,
			LastUpdated: listener.ActiveState.LastUpdated,
		})
	}

	return listeners, nil
}

func parseRoutes(rawCfg map[string]interface{}) ([]Route, error) {
	var routes []Route

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
	var secrets []Secret

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

func formatFilterChain(filterChain filterChain) (filterChainMatch string, filters []string) {
	fcm := []string{}
	for _, prefixRange := range filterChain.FilterChainMatch.PrefixRanges {
		fcm = append(fcm, fmt.Sprintf("%s/%d", prefixRange.AddressPrefix, int(prefixRange.PrefixLen)))
	}
	if len(fcm) == 0 {
		fcm = append(fcm, "Any")
	}
	filterChainMatch = strings.Join(fcm, ", ")

	for _, chainFilter := range filterChain.Filters {
		filter := ""

		for _, host := range chainFilter.TypedConfig.RouteConfig.VirtualHosts {
			filter += strings.Join(host.Domains, ", ")
			filter += " -> "

			routes := ""
			for _, route := range host.Routes {
				routes += fmt.Sprintf("%s%s", route.Route.Cluster, route.Match.Prefix)
			}
			filter += routes
		}

		for _, httpFilter := range chainFilter.TypedConfig.HttpFilters {
			action := httpFilter.TypedConfig.Rules.Action
			for _, principal := range httpFilter.TypedConfig.Rules.Policies.ConsulIntentions.Principals {
				regex := principal.Authenticated.PrincipalName.SafeRegex.Regex
				filter += fmt.Sprintf("\n  %s %s", action, regex)
			}
		}

		if chainFilter.TypedConfig.Cluster != "" {
			filter += fmt.Sprintf("-> %s", chainFilter.TypedConfig.Cluster)
		}

		filters = append(filters, filter)
	}
	return
}
