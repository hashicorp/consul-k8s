package install

import "sigs.k8s.io/yaml"

const (
	PresetDemo   = "demo"
	PresetSecure = "secure"
)

// presets is a map of pre-configured helm values.
var presets = map[string]interface{}{
	PresetDemo:   convert(demo),
	PresetSecure: convert(secure),
}

var demo = `
global:
  name: consul
  metrics:
    enabled: true
    enableAgentMetrics: true
connectInject:
  enabled: true
  metrics: 
    defaultEnabled: true
    defaultEnableMerging: true
    enableGatewayMetrics: true
server:
  replicas: 1
controller:
  enabled: true
ui: 
  enabled: true
  service:
    enabled: true
prometheus:
  enabled: true
`

var secure = `
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

var globalNameConsul = `
global:
  name: consul
`

// convert is a helper function that converts a YAML string to a map.
func convert(s string) map[string]interface{} {
	var m map[string]interface{}
	_ = yaml.Unmarshal([]byte(s), &m)
	return m
}
