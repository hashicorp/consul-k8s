package preset

import "fmt"
import "github.com/hashicorp/consul-k8s/cli/config"

// PeeringPreset struct is an implementation of the Preset interface that provides
// a Helm values map that is used during installation and represents the
// the peering configuration for Consul on Kubernetes.
type PeeringPreset struct{}

// GetValueMap returns the Helm value map representing the quickstart
// configuration for Consul on Kubernetes. It does the following:
// - enables TLS
// - enables the Service Mesh.
// - enables Mesh Gateway
func (i *PeeringPreset) GetValueMap() (map[string]interface{}, error) {
	values := fmt.Sprintf(`
global:
  name: consul
  datacenter: %s
  peering:
    enabled: true
  tls:
    enabled: true
connectInject:
  enabled: true
meshGateway:
  enabled: true
`, GetConsulDCFromEnv())

	return config.ConvertToMap(values), nil
}
