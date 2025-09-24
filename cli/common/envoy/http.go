// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package envoy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/hashicorp/consul-k8s/cli/common"
)

var ErrNoLoggersReturned = errors.New("No loggers were returned from Envoy")

// EnvoyConfig represents the configuration retrieved from a config dump at the
// admin endpoint. It wraps the Envoy ConfigDump struct to give us convenient
// access to the different sections of the config.
type EnvoyConfig struct {
	RawCfg    []byte
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

// CallLoggingEndpoint requests the logging endpoint from Envoy Admin Interface for a given port
// This is used to both read and update the logging levels (the envoy admin interface uses the same endpoint for both)
// more can be read about that endpoint https://www.envoyproxy.io/docs/envoy/latest/operations/admin#post--logging
func CallLoggingEndpoint(ctx context.Context, portForward common.PortForwarder, params *LoggerParams) (map[string]string, error) {
	endpoint, err := portForward.Open(ctx)
	if err != nil {
		return nil, err
	}

	defer portForward.Close()

	// this endpoint does not support returning json, so we've gotta parse the plain text
	response, err := http.Post(fmt.Sprintf("http://%s/logging%s", endpoint, params), "text/plain", bytes.NewBuffer([]byte{}))
	if err != nil {
		return nil, err
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to reach envoy: %v", err)
	}

	if response.StatusCode >= 400 {
		return nil, fmt.Errorf("call to envoy failed with status code: %d, and message: %s", response.StatusCode, body)
	}

	loggers := strings.Split(string(body), "\n")
	if len(loggers) == 0 {
		return nil, ErrNoLoggersReturned
	}

	logLevels := make(map[string]string)
	var name string
	var level string

	// the first line here is just a header
	for _, logger := range loggers[1:] {
		if len(logger) == 0 {
			continue
		}
		fmt.Sscanf(logger, "%s %s", &name, &level)
		name = strings.TrimRight(name, ":")
		logLevels[name] = level
	}

	return logLevels, nil
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
	return c.RawCfg
}

// UnmarshalJSON implements the json.Unmarshaler interface to unmarshal the raw
// config dump bytes into EnvoyConfig. It saves a copy of the original bytes
// which can be fetched with the JSON method.
func (c *EnvoyConfig) UnmarshalJSON(b []byte) error {
	// Save the original config dump bytes for marshalling. We should treat this
	// struct as immutable so this should be safe.
	c.RawCfg = b

	var root root
	err := json.Unmarshal(b, &root)

	clusterMapping, endpointMapping := make(map[string][]string), make(map[string]string)
	for _, clusterStatus := range root.Clusters.ClusterStatuses {
		var addresses []string
		for _, status := range clusterStatus.HostStatuses {
			address := net.JoinHostPort(status.Address.SocketAddress.Address, strconv.Itoa( int(status.Address.SocketAddress.PortValue))
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
				// Only add endpoints defined by IP addresses.
				if addr := lbEndpoint.Endpoint.Address.SocketAddress.Address; net.ParseIP(addr) != nil {
					endpoints = append(endpoints,net.JoinHostPort( addr, strconv.Itoa(int(lbEndpoint.Endpoint.Address.SocketAddress.PortValue))))
				}
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
				address := net.JoinHostPort( lbEndpoint.Endpoint.Address.SocketAddress.Address, strconv.Itoa(int(lbEndpoint.Endpoint.Address.SocketAddress.PortValue)))

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
		address := net.JoinHostPort(listener.Listener.Address.SocketAddress.Address, strconv.Itoa(int(listener.Listener.Address.SocketAddress.PortValue)))

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

func formatFilters(filters []filter) []string {
	formatted := []string{}

	// Filters can have many custom configurations, each must be handled differently.
	// [List of known extensions](https://www.envoyproxy.io/docs/envoy/latest/api-v3/config/listener/v3/listener_components.proto).
	formatters := map[string]func(filter) string{
		"type.googleapis.com/envoy.extensions.filters.network.connection_limit.v3.ConnectionLimit":              formatFilterConnectionLimit,
		"type.googleapis.com/envoy.extensions.filters.network.direct_response.v3.Config":                        formatFilterDirectResponse,
		"type.googleapis.com/envoy.extensions.filters.network.echo.v3.Echo":                                     formatFilterEcho,
		"type.googleapis.com/envoy.extensions.filters.network.ext_authz.v3.ExtAuthz":                            formatFilterExtAuthz,
		"type.googleapis.com/envoy.extensions.filters.network.http_connection_manager.v3.HttpConnectionManager": formatFilterHTTPConnectionManager,
		"type.googleapis.com/envoy.extensions.filters.network.local_ratelimit.v3.LocalRateLimit":                formatFilterLocalRatelimit,
		"type.googleapis.com/envoy.extensions.filters.network.ratelimit.v3.RateLimit":                           formatFilterRatelimit,
		"type.googleapis.com/envoy.extensions.filters.network.rbac.v3.RBAC":                                     formatFilterRBAC,
		"type.googleapis.com/envoy.extensions.filters.network.sni_cluster.v3.SniCluster":                        formatFilterSniCluster,
		"type.googleapis.com/envoy.extensions.filters.network.tcp_proxy.v3.TcpProxy":                            formatFilterTCPProxy,
	}

	for _, filter := range filters {
		if formatter, ok := formatters[filter.TypedConfig.Type]; ok {
			formatted = append(formatted, formatter(filter))
		} else {
			formatted = append(formatted, fmt.Sprintf("Unknown filter: %s", filter.TypedConfig.Type))
		}
	}
	return formatted
}

func formatFilterConnectionLimit(config filter) string {
	return fmt.Sprintf("Connection limit: %d max connections with %s delay", config.TypedConfig.MaxConnections, config.TypedConfig.Delay)
}

func formatFilterDirectResponse(config filter) string {
	out := []string{"Direct response: ->"}
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
	return "Echo: upstream will respond with the data it receives."
}

func formatFilterExtAuthz(config filter) string {
	var upstream string
	if config.TypedConfig.GrpcService.EnvoyGrpc.ClusterName != "" {
		upstream = config.TypedConfig.GrpcService.EnvoyGrpc.ClusterName
	} else if config.TypedConfig.GrpcService.GoogleGrpc.TargetUri != "" {
		upstream = config.TypedConfig.GrpcService.GoogleGrpc.TargetUri
	} else {
		upstream = "No upstream configured."
	}

	return fmt.Sprintf("External authorization: %s", upstream)
}

func formatFilterHTTPConnectionManager(config filter) string {
	out := "HTTP: "
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
	return fmt.Sprintf("Local rate limit: tokens: max %d per-fill %d, interval: %s",
		config.TypedConfig.TokenBucket.MaxTokens,
		config.TypedConfig.TokenBucket.TokensPerFill,
		config.TypedConfig.TokenBucket.FillInterval)
}

func formatFilterRatelimit(config filter) string {
	out := "Rate limit: "

	if config.TypedConfig.Domain != "" {
		out += config.TypedConfig.Domain + " "
	}

	// Rate limit using descriptors.
	if len(config.TypedConfig.Descriptors) != 0 {
		for _, descriptor := range config.TypedConfig.Descriptors {
			for _, entry := range descriptor.Entries {
				out += fmt.Sprintf("%s:%s ", entry.Key, entry.Value)
			}
			out += fmt.Sprintf("%d req per %s", descriptor.Limit.RequestsPerUnit, strings.ToLower(descriptor.Limit.Unit))
		}
	}

	// Rate limit using an external Envoy gRPC service.
	if config.TypedConfig.RateLimitService.GrpcService.EnvoyGrpc.ClusterName != "" {
		out += fmt.Sprintf("using %s ", config.TypedConfig.RateLimitService.GrpcService.EnvoyGrpc.ClusterName)
	}

	// Rate limit using an external Google gRPC service.
	if config.TypedConfig.RateLimitService.GrpcService.GoogleGrpc.TargetUri != "" {
		out += fmt.Sprintf("using %s ", config.TypedConfig.RateLimitService.GrpcService.GoogleGrpc.TargetUri)
	}

	// Notify the user that failure to reach the rate limiting service will deny the caller.
	if config.TypedConfig.FailureModeDeny {
		out += "will deny if unreachable"
	}

	return strings.Trim(out, " ")
}

func formatFilterRBAC(config filter) string {
	out := "RBAC: "
	action := config.TypedConfig.Rules.Action
	for _, principal := range config.TypedConfig.Rules.Policies.ConsulIntentions.Principals {
		regex := principal.Authenticated.PrincipalName.SafeRegex.Regex
		out += fmt.Sprintf("%s %s", action, regex)
	}
	return out
}

func formatFilterSniCluster(config filter) string {
	return "SNI: Upstream cluster name set by SNI field in TLS connection."
}

func formatFilterTCPProxy(config filter) string {
	if config.TypedConfig.Cluster == "" {
		return "TCP: No upstream cluster configured."
	}

	return "TCP: -> " + strings.Split(config.TypedConfig.Cluster, ".")[0]
}
