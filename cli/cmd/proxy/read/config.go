package read

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	adminv3 "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
	"github.com/hashicorp/consul-k8s/cli/common"
)

type Table string

// Define the different types of tables which can be printed.
const (
	Clusters  Table = "Clusters"
	Endpoints       = "Endpoints"
	Listeners       = "Listeners"
	Routes          = "Routes"
	Secrets         = "Secrets"
)

// EnvoyConfig represents the configuration retrieved from a config dump at the
// admin endpoint. It wraps the Envoy ConfigDump struct to give us convenient
// access to the different sections of the config.
type EnvoyConfig struct {
	configDump *adminv3.ConfigDump
}

type Cluster struct{}

// NewEnvoyConfig creates a new EnvoyConfig from the raw bytes returned from the
// config dump endpoint.
func NewEnvoyConfig(cfg []byte) (*EnvoyConfig, error) {
	configDump := &adminv3.ConfigDump{}
	err := json.Unmarshal(cfg, configDump)
	if err != nil {
		return nil, err
	}

	return &EnvoyConfig{configDump: configDump}, nil
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

func (c *EnvoyConfig) Clusters() []Cluster {
	return []Cluster{}
}

// JSON returns the EnvoyConfig as JSON.
func (c *EnvoyConfig) JSON() ([]byte, error) {
	return json.Marshal(c.configDump)
}
