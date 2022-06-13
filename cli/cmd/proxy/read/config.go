package read

import (
	"encoding/json"
	"fmt"
	"strings"
)

type Config struct {
	Clusters  []Cluster
	Endpoints []Endpoint
	Listeners []Listener
	Routes    []Route
	Secrets   []Secret
}

type Cluster struct {
	Name                     string
	FullyQualifiedDomainName string
	Endpoints                []string
	Type                     string
	LastUpdated              string
}

type Endpoint struct{}

type Listener struct{}

type Route struct{}

type Secret struct{}

func ParseConfig(raw []byte) (Config, error) {
	var config Config

	// Parse the raw config into a map
	var cfg map[string]interface{}
	json.Unmarshal(raw, &cfg)

	// Dispatch each config element to the appropriate parser. Add to config.
	for _, element := range cfg["configs"].([]interface{}) {
		switch element.(map[string]interface{})["@type"].(string) {
		case "type.googleapis.com/envoy.admin.v3.ClustersConfigDump":
			clusters, err := parseClusters(element.(map[string]interface{}))
			if err != nil {
				return Config{}, err
			}
			config.Clusters = append(config.Clusters, clusters...)
		case "type.googleapis.com/envoy.admin.v3.EndpointsConfigDump":
		case "type.googleapis.com/envoy.admin.v3.ListenersConfigDump":
		case "type.googleapis.com/envoy.admin.v3.RoutesConfigDump":
		case "type.googleapis.com/envoy.admin.v3.SecretsConfigDump":
		}
	}

	return config, nil
}

func parseClusters(clusterCfg map[string]interface{}) ([]Cluster, error) {
	var clusters []Cluster

	static := clusterCfg["static_clusters"].([]interface{})
	dynamic := clusterCfg["dynamic_active_clusters"].([]interface{})

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
