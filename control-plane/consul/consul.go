package consul

import (
	"fmt"

	"github.com/hashicorp/consul-k8s/control-plane/version"
	capi "github.com/hashicorp/consul/api"
)

// NewClient returns a Consul API client. It adds a required User-Agent
// header that describes the version of consul-k8s making the call.
func NewClient(config *capi.Config) (*capi.Client, error) {
	client, err := capi.NewClient(config)
	if err != nil {
		return nil, err
	}
	client.AddHeader("User-Agent", fmt.Sprintf("consul-k8s/%s", version.GetHumanVersion()))
	return client, nil
}
