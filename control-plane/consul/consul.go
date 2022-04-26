package consul

import (
	"fmt"
	"net/http"
	"time"

	"github.com/hashicorp/consul-k8s/control-plane/version"
	capi "github.com/hashicorp/consul/api"
)

// NewClient returns a Consul API client. It adds a required User-Agent
// header that describes the version of consul-k8s making the call.
func NewClient(config *capi.Config) (*capi.Client, error) {
	if config.HttpClient == nil {
		config.HttpClient = &http.Client{
			Timeout: 2 * time.Second,
		}
	}
	err := capi.SetClientConfig(config)
	if err != nil {
		return nil, err
	}
	err = capi.SetClientTransportWithTLSConfig(config.HttpClient, config.Transport, config.TLSConfig)
	if err != nil {
		return nil, err
	}
	client, err := capi.NewClient(config)
	if err != nil {
		return nil, err
	}
	client.AddHeader("User-Agent", fmt.Sprintf("consul-k8s/%s", version.GetHumanVersion()))
	return client, nil
}
