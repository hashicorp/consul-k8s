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

	var static []interface{}
	if staticClusters, ok := rawCfg["static_clusters"]; ok {
		static = staticClusters.([]interface{})
	}

	var dynamic []interface{}
	if dynamicClusters, ok := rawCfg["dynamic_active_clusters"]; ok {
		dynamic = dynamicClusters.([]interface{})
	}

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

	var static []interface{}
	if staticEndpoints, ok := rawCfg["static_endpoint_configs"]; ok {
		static = staticEndpoints.([]interface{})
	}

	var dynamic []interface{}
	if dynamicEndpoints, ok := rawCfg["dynamic_endpoint_configs"]; ok {
		dynamic = dynamicEndpoints.([]interface{})
	}

	for _, endpoint := range append(static, dynamic...) {
		endpointConfig := endpoint.(map[string]interface{})["endpoint_config"].(map[string]interface{})

		var cluster string
		if clusterName, ok := endpointConfig["cluster_name"]; ok {
			cluster = clusterName.(string)
		}

		if endpointCollection, ok := endpointConfig["endpoints"]; ok {
			for _, ep := range endpointCollection.([]interface{}) {
				if lbEndpoints, ok := ep.(map[string]interface{})["lb_endpoints"]; ok {
					for _, lbEndpoint := range lbEndpoints.([]interface{}) {
						socketAddr := lbEndpoint.(map[string]interface{})["endpoint"].(map[string]interface{})["address"].(map[string]interface{})["socket_address"].(map[string]interface{})

						address := fmt.Sprintf("%s:%d", socketAddr["address"].(string), int(socketAddr["port_value"].(float64)))
						weight := lbEndpoint.(map[string]interface{})["load_balancing_weight"].(float64)
						status := lbEndpoint.(map[string]interface{})["health_status"].(string)

						endpoints = append(endpoints, Endpoint{
							Address: address,
							Cluster: cluster,
							Weight:  weight,
							Status:  status,
						})
					}
				}

			}
		}
	}

	return endpoints, nil
}

func parseListeners(rawCfg map[string]interface{}) ([]InboundListener, []OutboundListener, error) {
	inbounds, outbounds := []InboundListener{}, []OutboundListener{}

	if rawCfg["dynamic_listeners"] != nil {
		for _, listener := range rawCfg["dynamic_listeners"].([]interface{}) {
			listener_ := listener.(map[string]interface{})

			name := strings.Split(listener_["name"].(string), ":")[0]
			addr := strings.SplitN(listener_["name"].(string), ":", 2)[1]

			activeState := listener_["active_state"].(map[string]interface{})
			lastUpdated := activeState["last_updated"].(string)

			activeStateListener := activeState["listener"].(map[string]interface{})
			direction := activeStateListener["traffic_direction"].(string)

			if direction == "INBOUND" {
				rule, cluster := "", ""

				if activeStateListener["filter_chains"] != nil {
					filterChains := activeStateListener["filter_chains"].([]interface{})
					for _, filterChain := range filterChains {
						fc := filterChain.(map[string]interface{})
						if fc["filters"] != nil {
							for _, filter := range fc["filters"].([]interface{}) {
								f := filter.(map[string]interface{})
								typedConfig := f["typed_config"].(map[string]interface{})
								if typedConfig["rules"] != nil {
									rules := typedConfig["rules"].(map[string]interface{})
									action := rules["action"].(string)
									policies := rules["policies"].(map[string]interface{})
									cil4 := policies["consul-intentions-layer4"].(map[string]interface{})
									principals := cil4["principals"].([]interface{})

									regex := []string{}
									for _, principal := range principals {
										p := principal.(map[string]interface{})
										r := p["authenticated"].(map[string]interface{})["principal_name"].(map[string]interface{})["safe_regex"].(map[string]interface{})["regex"].(string)
										regex = append(regex, r)
									}

									rule = fmt.Sprintf("%s %s", action, strings.Join(regex, ","))
								}
								if typedConfig["cluster"] != nil {
									cluster = typedConfig["cluster"].(string)
								}
							}
						}
					}
				}

				inbounds = append(inbounds, InboundListener{
					Name:               name,
					Address:            addr,
					Filter:             rule,
					DestinationCluster: cluster,
					LastUpdated:        lastUpdated,
				})
			}

			if direction == "OUTBOUND" {
				fcm, dest := []string{}, []string{}
				if activeStateListener["filter_chains"] != nil {

					fcs := activeStateListener["filter_chains"].([]interface{})
					for _, fc := range fcs {
						fcm := []string{}
						dest := []string{}
						fc_ := fc.(map[string]interface{})
						if fc_["filter_chain_match"] != nil {
							fcmtch := fc_["filter_chain_match"].(map[string]interface{})
							prs := fcmtch["prefix_ranges"].([]interface{})
							for _, pr := range prs {
								pr_ := pr.(map[string]interface{})
								fcm = append(fcm, pr_["address_prefix"].(string))
							}
						}
						if fc_["filters"] != nil {
							fltrs := fc_["filters"].([]interface{})
							for _, fltr := range fltrs {
								fltr_ := fltr.(map[string]interface{})
								if fltr_["typed_config"] != nil {
									tc := fltr_["typed_config"].(map[string]interface{})
									if tc["cluster"] != nil {
										dest = append(dest, strings.Split(tc["cluster"].(string), ".")[0])
									}
									if tc["route_config"] != nil {
										rc := tc["route_config"].(map[string]interface{})
										if rc["virtual_hosts"] != nil {
											vhs := rc["virtual_hosts"].([]interface{})
											for _, vh := range vhs {
												vh_ := vh.(map[string]interface{})
												if vh_["routes"] != nil {
													rts := vh_["routes"].([]interface{})
													for _, rt := range rts {
														rt_ := rt.(map[string]interface{})
														r := rt_["route"].(map[string]interface{})
														dest = append(dest, strings.Split(r["cluster"].(string), ".")[0])
													}
												}
											}
										}
									}
								}
							}
						}
					}
				}

				outbounds = append(outbounds, OutboundListener{
					Name:               name,
					Address:            addr,
					FilterChainMatch:   strings.Join(fcm, ","),
					DestinationCluster: strings.Join(dest, ", "),
					LastUpdated:        lastUpdated,
				})
			}
		}
	}

	return inbounds, outbounds, nil
}

func parseRoutes(rawCfg map[string]interface{}) ([]Route, error) {
	var routes []Route

	if rawCfg["static_route_configs"] != nil {
		for _, static_route_config := range rawCfg["static_route_configs"].([]interface{}) {
			src_ := static_route_config.(map[string]interface{})

			destinationCluster := ""
			lastUpdated := src_["last_updated"].(string)

			routecfg := src_["route_config"].(map[string]interface{})
			name := routecfg["name"].(string)

			for _, vh := range routecfg["virtual_hosts"].([]interface{}) {
				vh_ := vh.(map[string]interface{})
				for _, rt := range vh_["routes"].([]interface{}) {
					rt_ := rt.(map[string]interface{})
					r := rt_["route"].(map[string]interface{})
					match := rt_["match"].(map[string]interface{})["prefix"].(string)
					destinationCluster = fmt.Sprintf("%s%s", r["cluster"].(string), match)
				}
			}

			routes = append(routes, Route{
				Name:               name,
				DestinationCluster: destinationCluster,
				LastUpdated:        lastUpdated,
			})
		}
	}

	return routes, nil
}

func parseSecrets(rawCfg map[string]interface{}) ([]Secret, error) {
	var secrets []Secret

	return secrets, nil
}
