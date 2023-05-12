// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package preset

import "github.com/hashicorp/consul-k8s/cli/config"

// QuickstartPreset struct is an implementation of the Preset interface that provides
// a Helm values map that is used during installation and represents the
// the quickstart configuration for Consul on Kubernetes.
type QuickstartPreset struct{}

// GetValueMap returns the Helm value map representing the quickstart
// configuration for Consul on Kubernetes. It does the following:
// - server replicas equal to 1.
// - enables the service mesh.
// - enables the ui.
// - enables metrics.
// - enables Prometheus.
func (i *QuickstartPreset) GetValueMap() (map[string]interface{}, error) {
	values := `
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
ui:
  enabled: true
  service:
    enabled: true
prometheus:
  enabled: true
`

	return config.ConvertToMap(values), nil
}
