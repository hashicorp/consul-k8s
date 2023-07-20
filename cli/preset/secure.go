package preset

import "github.com/hashicorp/consul-k8s/cli/config"

// SecurePreset struct is an implementation of the Preset interface that provides
// a Helm values map that is used during installation and represents the
// the quickstart configuration for Consul on Kubernetes.
type SecurePreset struct{}

// GetValueMap returns the Helm value map representing the quickstart
// configuration for Consul on Kubernetes. It does the following:
// - server replicas equal to 1.
// - enables the service mesh.
// - enables tls.
// - enables gossip encryption.
// - enables ACLs.
func (i *SecurePreset) GetValueMap() (map[string]interface{}, error) {
	values := `
global:
  name: consul
  gossipEncryption:
    autoGenerate: true 
  tls:
    enabled: true
    enableAutoEncrypt: true
  acls:
    manageSystemACLs: true
server:
  replicas: 1
connectInject:
  enabled: true
controller:
  enabled: true
`

	return config.ConvertToMap(values), nil
}
