package read

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/hashicorp/consul-k8s/cli/common"
)

// FetchConfig opens a port forward to the Envoy admin API and fetches the
// configuration from the config dump endpoint.
func FetchConfig(ctx context.Context, portForward common.PortForwarder) ([]byte, error) {
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

	return io.ReadAll(response.Body)
}
