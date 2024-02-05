package proxy

import (
	"fmt"

	"github.com/hashicorp/consul-k8s/cli/common"
	"github.com/mitchellh/cli"
)

// ProxyCommand  provides a synopsis for the proxy subcommands (e.g. read).
type ProxyCommand struct {
	*common.BaseCommand
}

// Run prints out information about the subcommands.
func (c *ProxyCommand) Run([]string) int {
	return cli.RunResultHelp
}

func (c *ProxyCommand) Help() string {
	return fmt.Sprintf("%s\n\nUsage: consul-k8s proxy <subcommand>", c.Synopsis())
}

func (c *ProxyCommand) Synopsis() string {
	return "Inspect Envoy proxies managed by Consul."
}
