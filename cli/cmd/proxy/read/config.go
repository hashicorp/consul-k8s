package read

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	adminv3 "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	"github.com/hashicorp/consul-k8s/cli/common"
	any "google.golang.org/protobuf/types/known/anypb"
)

type Section string

// Define the different types of sections in the Envoy config.
const (
	Clusters  Section = "type.googleapis.com/envoy.admin.v3.ClustersConfigDump"
	Endpoints         = "type.googleapis.com/envoy.admin.v3.EndpointsConfigDump"
	Listeners         = "type.googleapis.com/envoy.admin.v3.ListenersConfigDump"
	Routes            = "type.googleapis.com/envoy.admin.v3.RoutesConfigDump"
	Secrets           = "type.googleapis.com/envoy.admin.v3.SecretsConfigDump"
)

// EnvoyConfig represents the configuration retrieved from a config dump at the
// admin endpoint. It wraps the Envoy ConfigDump struct to give us convenient
// access to the different sections of the config.
type EnvoyConfig struct {
	configDump *adminv3.ConfigDump
}

type Cluster struct {
	Name                     string
	FullyQualifiedDomainName string
	Endpoints                []string
	Type                     string
	LastUpdated              string
}

// NewEnvoyConfig creates a new EnvoyConfig from the raw bytes returned from the
// config dump endpoint.
func NewEnvoyConfig(cfg []byte) (*EnvoyConfig, error) {
	envoyConfig := &EnvoyConfig{}
	err := json.Unmarshal(cfg, envoyConfig)
	if err != nil {
		return nil, err
	}

	return envoyConfig, nil
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

	return NewEnvoyConfig(raw)
}

// Clusters returns a list of clusters in the Envoy config.
func (c *EnvoyConfig) Clusters() ([]Cluster, error) {
	// Get the clusters section of the config dump.
	clustersSect, err := c.section(Clusters)
	if err != nil {
		return nil, err
	}

	// Unmarshal the clusterDump section into a ClustersConfigDump.
	clusterDump := &adminv3.ClustersConfigDump{}
	err = clustersSect.UnmarshalTo(clusterDump)
	if err != nil {
		return nil, err
	}

	// Create a list of clusters and populate it with Cluster data from the dump.
	var clusters []Cluster
	for _, dynamicActiveCluster := range clusterDump.GetDynamicActiveClusters() {
		cluster := &cluster.Cluster{}
		err = dynamicActiveCluster.Cluster.UnmarshalTo(cluster)
		if err != nil {
			return nil, err
		}

		clusters = append(clusters, Cluster{
			Name:                     cluster.Name,
			FullyQualifiedDomainName: cluster.Name,
			Endpoints:                []string{},
			Type:                     cluster.GetClusterType().Name,
			LastUpdated:              dynamicActiveCluster.LastUpdated.String(),
		})
	}

	return clusters, nil
}

// UnmarshalJSON unmarshals the raw config dump bytes into EnvoyConfig.
func (c *EnvoyConfig) UnmarshalJSON(b []byte) error {
	configDump := &adminv3.ConfigDump{}
	err := json.Unmarshal(b, configDump)
	*c = EnvoyConfig{configDump: configDump}
	return err
}

// MarshalJSON marshals the EnvoyConfig into the raw config dump bytes.
func (c *EnvoyConfig) MarshalJSON() ([]byte, error) {
	return json.Marshal(c.configDump)
}

func (c *EnvoyConfig) section(section Section) (*any.Any, error) {
	var sect *any.Any
	for _, s := range c.configDump.Configs {
		if s.TypeUrl == string(section) {
			sect = s
			break
		}
	}

	if sect == nil {
		return nil, fmt.Errorf("ConfigDump section %s not found", section)
	}

	return sect, nil
}
