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

// TODO: enable prometheus in demo installation
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
  bootstrapExpect: 1
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
  acls:
    manageSystemACLs: true
  tls:
    enabled: true
connectInject:
  enabled: true
server:
  replicas: 1
  bootstrapExpect: 1
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
