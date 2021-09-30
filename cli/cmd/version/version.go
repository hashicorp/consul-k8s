package version

import (
	"fmt"
	"sync"

	"github.com/hashicorp/consul-k8s/cli/cmd/common"
)

type Command struct {
	*common.BaseCommand

	Version string
	once    sync.Once
}

func (c *Command) init() {
	c.Init()
}

func (c *Command) Run(_ []string) int {
	c.once.Do(c.init)
	c.UI.Output(fmt.Sprintf("consul-k8s %s", c.Version))
	return 0
}

func (c *Command) Synopsis() string {
	return "Prints the version of the CLI."
}

func (c *Command) Help() string {
	return "Usage: consul version [options]\n"
}
