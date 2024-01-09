package consul

import (
	"fmt"
	"net/http"

	"github.com/hashicorp/consul-k8s/version"
	capi "github.com/hashicorp/consul/api"
)

// NewClient returns a Consul API client. It adds a required User-Agent
// header that describes the version of consul-k8s making the call.
func NewClient(config *capi.Config) (*capi.Client, error) {
	if config.HttpClient == nil {
		config.HttpClient = &http.Client{}
	}

	if config.Transport == nil {
		tlsClientConfig, err := capi.SetupTLSConfig(&config.TLSConfig)

		if err != nil {
			return nil, err
		}

		config.Transport = &http.Transport{TLSClientConfig: tlsClientConfig}
	} else if config.Transport.TLSClientConfig == nil {
		tlsClientConfig, err := capi.SetupTLSConfig(&config.TLSConfig)

		if err != nil {
			return nil, err
		}

		config.Transport.TLSClientConfig = tlsClientConfig
	}
	config.HttpClient.Transport = config.Transport

	client, err := capi.NewClient(config)
	if err != nil {
		return nil, err
	}
	client.AddHeader("User-Agent", fmt.Sprintf("consul-k8s/%s", version.GetHumanVersion()))
	return client, nil
}
