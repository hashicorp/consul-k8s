package config

import "sigs.k8s.io/yaml"

// GlobalNameConsul is used to set the global name of an install to consul.
const GlobalNameConsul = `
global:
  name: consul
`

// ConvertToMap is a helper function that converts a YAML string to a map.
func ConvertToMap(s string) map[string]interface{} {
	var m map[string]interface{}
	_ = yaml.Unmarshal([]byte(s), &m)
	return m
}
