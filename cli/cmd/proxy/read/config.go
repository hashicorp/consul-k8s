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
	rawCfg            []byte
	Clusters          []Cluster
	Endpoints         []Endpoint
	InboundListeners  []InboundListener
	OutboundListeners []OutboundListener
	Routes            []Route
	Secrets           []Secret
}

// Cluster represents a cluster in the Envoy config.
type Cluster struct {
	Name                     string
	FullyQualifiedDomainName string
	Endpoints                []string
	Type                     string
	LastUpdated              string
}

type Endpoint struct {
	Address string
	Cluster string
	Weight  float64
	Status  string
}

type InboundListener struct {
	Name               string
	Address            string
	Filter             string
	DestinationCluster string
	LastUpdated        string
}

type OutboundListener struct {
	Name               string
	Address            string
	FilterChainMatch   string
	DestinationCluster string
	LastUpdated        string
}

type Route struct {
	Name               string
	DestinationCluster string
	LastUpdated        string
}

type Secret struct {
	Name      string
	Type      string
	Status    string
	Valid     bool
	ValidFrom string
	ValidTo   string
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
	defer response.Body.Close()

	raw, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	envoyConfig := &EnvoyConfig{}
	err = json.Unmarshal(raw, envoyConfig)
	if err != nil {
		return nil, err
	}
	return envoyConfig, nil
}

// UnmarshalJSON unmarshals the raw config dump bytes into EnvoyConfig.
func (c *EnvoyConfig) UnmarshalJSON(b []byte) error {
	// Save the original config dump bytes for marshalling. We should treat this
	// struct as immutable so this should be safe.
	c.rawCfg = b

	var root map[string]interface{}
	err := json.Unmarshal(b, &root)

	// Dispatch each section to the appropriate parsing function by its type.
	for _, config := range root["configs"].([]interface{}) {
		switch config.(map[string]interface{})["@type"].(string) {
		case "type.googleapis.com/envoy.admin.v3.ClustersConfigDump":
			clusters, err := parseClusters(config.(map[string]interface{}))
			if err != nil {
				return err
			}
			c.Clusters = clusters

		case "type.googleapis.com/envoy.admin.v3.EndpointsConfigDump":
			endpoints, err := parseEndpoints(config.(map[string]interface{}))
			if err != nil {
				return err
			}
			c.Endpoints = endpoints
		case "type.googleapis.com/envoy.admin.v3.ListenersConfigDump":
			inbounds, outbounds, err := parseListeners(config.(map[string]interface{}))
			if err != nil {
				return err
			}
			c.InboundListeners = inbounds
			c.OutboundListeners = outbounds
		case "type.googleapis.com/envoy.admin.v3.RoutesConfigDump":
			routes, err := parseRoutes(config.(map[string]interface{}))
			if err != nil {
				return err
			}
			c.Routes = routes
		case "type.googleapis.com/envoy.admin.v3.SecretsConfigDump":
			secrets, err := parseSecrets(config.(map[string]interface{}))
			if err != nil {
				return err
			}
			c.Secrets = secrets
		}
	}

	return err
}

// MarshalJSON marshals the EnvoyConfig into the raw config dump bytes.
func (c *EnvoyConfig) MarshalJSON() ([]byte, error) {
	return c.rawCfg, nil
}

func parseClusters(rawCfg map[string]interface{}) ([]Cluster, error) {
	var clusters []Cluster

	static := rawCfg["static_clusters"].([]interface{})
	dynamic := rawCfg["dynamic_active_clusters"].([]interface{})

	for _, cluster := range append(static, dynamic...) {
		fqdn := cluster.(map[string]interface{})["cluster"].(map[string]interface{})["name"].(string)
		name := strings.Split(fqdn, ".")[0]
		ctype := cluster.(map[string]interface{})["cluster"].(map[string]interface{})["type"].(string)
		lastupdated := cluster.(map[string]interface{})["last_updated"].(string)

		var endpoints []string
		if cluster.(map[string]interface{})["cluster"].(map[string]interface{})["load_assignment"] != nil {
			for _, endpoint := range cluster.(map[string]interface{})["cluster"].(map[string]interface{})["load_assignment"].(map[string]interface{})["endpoints"].([]interface{}) {
				lbEndpoints := endpoint.(map[string]interface{})["lb_endpoints"]
				for _, lbEndpoint := range lbEndpoints.([]interface{}) {
					sockaddr := lbEndpoint.(map[string]interface{})["endpoint"].(map[string]interface{})["address"].(map[string]interface{})["socket_address"].(map[string]interface{})
					address := sockaddr["address"].(string)
					port := sockaddr["port_value"].(float64)
					endpoints = append(endpoints, fmt.Sprintf("%s:%d", address, int(port)))
				}
			}
		}

		cluster := Cluster{
			Name:                     name,
			FullyQualifiedDomainName: fqdn,
			Endpoints:                endpoints,
			Type:                     ctype,
			LastUpdated:              lastupdated,
		}

		clusters = append(clusters, cluster)
	}

	return clusters, nil
}

func parseEndpoints(rawCfg map[string]interface{}) ([]Endpoint, error) {
	var endpoints []Endpoint

	return endpoints, nil
}

func parseListeners(rawCfg map[string]interface{}) ([]InboundListener, []OutboundListener, error) {
	inbounds, outbounds := []InboundListener{}, []OutboundListener{}

	return inbounds, outbounds, nil
}

func parseRoutes(rawCfg map[string]interface{}) ([]Route, error) {
	var routes []Route

	return routes, nil
}

func parseSecrets(rawCfg map[string]interface{}) ([]Secret, error) {
	var secrets []Secret

	return secrets, nil
}
