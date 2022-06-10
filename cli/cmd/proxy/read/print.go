package read

import "github.com/hashicorp/consul-k8s/cli/common/terminal"

type Table string

const (
	Clusters  Table = "type.googleapis.com/envoy.admin.v3.ClustersConfigDump"
	Endpoints       = "type.googleapis.com/envoy.admin.v3.EndpointsConfigDump"
	Listeners       = "type.googleapis.com/envoy.admin.v3.ListenersConfigDump"
	Routes          = "type.googleapis.com/envoy.admin.v3.RoutesConfigDump"
	Secrets         = "type.googleapis.com/envoy.admin.v3.SecretsConfigDump"
)

func Print(ui terminal.UI, config interface{}, tables ...Table) error {
	return nil
}
