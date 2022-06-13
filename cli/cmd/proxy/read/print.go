package read

import "github.com/hashicorp/consul-k8s/cli/common/terminal"

type Table string

const (
	Clusters  Table = "Clusters"
	Endpoints       = "Endpoints"
	Listeners       = "Listeners"
	Routes          = "Routes"
	Secrets         = "Secrets"
)

func Print(ui terminal.UI, config Config, tables ...Table) error {
	return nil
}
