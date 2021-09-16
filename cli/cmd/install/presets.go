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
connectInject:
  enabled: true
server:
  replicas: 1
  bootstrapExpect: 1
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
	yaml.Unmarshal([]byte(s), &m)
	return m
}
