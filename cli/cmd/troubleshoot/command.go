// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package troubleshoot

import (
	"fmt"

	"github.com/hashicorp/consul-k8s/cli/common"
	"github.com/mitchellh/cli"
)

// TroubleshootCommand  provides a synopsis for the troubleshoot subcommands (e.g. proxy, upstreams).
type TroubleshootCommand struct {
	*common.BaseCommand
}

// Run prints out information about the subcommands.
func (c *TroubleshootCommand) Run([]string) int {
	return cli.RunResultHelp
}

func (c *TroubleshootCommand) Help() string {
	return fmt.Sprintf("%s\n\nUsage: consul-k8s troubleshoot <subcommand>", c.Synopsis())
}

func (c *TroubleshootCommand) Synopsis() string {
	return "Troubleshoot network and security configurations."
}
