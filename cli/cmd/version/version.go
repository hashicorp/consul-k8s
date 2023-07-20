package version

import (
	"github.com/hashicorp/consul-k8s/cli/common"
	"github.com/hashicorp/consul-k8s/cli/common/terminal"
)

type Command struct {
	*common.BaseCommand

	// Version is the Consul on Kubernetes CLI version.
	Version string
}

// Run prints the version of the Consul on Kubernetes CLI.
func (c *Command) Run(_ []string) int {
	c.UI.Output("consul-k8s %s", c.Version, terminal.WithInfoStyle())
	return 0
}

// Help returns a description of the command and how it is used.
func (c *Command) Help() string {
	return "Usage: consul-k8s version\n\n" + c.Synopsis()
}

// Synopsis returns a one-line command summary.
func (c *Command) Synopsis() string {
	return "Print the version of the Consul on Kubernetes CLI."
}
