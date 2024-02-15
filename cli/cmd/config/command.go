// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package config

import (
	"fmt"

	"github.com/hashicorp/consul-k8s/cli/common"
	"github.com/mitchellh/cli"
)

// ConfigCommand  provides a synopsis for the config subcommands (e.g. read).
type ConfigCommand struct {
	*common.BaseCommand
}

// Run prints out information about the subcommands.
func (c *ConfigCommand) Run([]string) int {
	return cli.RunResultHelp
}

func (c *ConfigCommand) Help() string {
	return fmt.Sprintf("%s\n\nUsage: consul-k8s config <subcommand>", c.Synopsis())
}

func (c *ConfigCommand) Synopsis() string {
	return "Operate on configuration"
}
