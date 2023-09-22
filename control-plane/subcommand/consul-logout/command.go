// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package consullogout

import (
	"flag"
	"sync"

	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/go-hclog"
	"github.com/mitchellh/cli"

	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/common"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/flags"
)

const (
	defaultACLTokenFile = "/consul/login/acl-token"
)

// The consul-logout command issues a Consul logout API request to delete an ACL token.
type Command struct {
	UI cli.Ui

	flagLogLevel string
	flagLogJSON  bool

	flagSet *flag.FlagSet
	http    *flags.HTTPFlags

	once   sync.Once
	help   string
	logger hclog.Logger
}

func (c *Command) init() {
	c.flagSet = flag.NewFlagSet("", flag.ContinueOnError)
	c.flagSet.StringVar(&c.flagLogLevel, "log-level", "info",
		"Log verbosity level. Supported values (in order of detail) are \"trace\", "+
			"\"debug\", \"info\", \"warn\", and \"error\".")
	c.flagSet.BoolVar(&c.flagLogJSON, "log-json", false,
		"Enable or disable JSON output format for logging.")

	c.http = &flags.HTTPFlags{}
	flags.Merge(c.flagSet, c.http.Flags())
	c.help = flags.Usage(help, c.flagSet)

}

func (c *Command) Run(args []string) int {
	var err error
	c.once.Do(c.init)

	if err := c.flagSet.Parse(args); err != nil {
		return 1
	}

	if c.http.ConsulAPITimeout() <= 0 {
		c.UI.Error("-consul-api-timeout must be set to a value greater than 0")
		return 1
	}

	if c.logger == nil {
		c.logger, err = common.Logger(c.flagLogLevel, c.flagLogJSON)
		if err != nil {
			c.UI.Error(err.Error())
			return 1
		}
	}
	// Set a default if it is not already set.
	if c.http.TokenFile() == "" {
		if err := c.http.SetTokenFile(defaultACLTokenFile); err != nil {
			c.logger.Error("Unable to update client", "error", err)
			return 1
		}
	}

	cfg := api.DefaultConfig()
	c.http.MergeOntoConfig(cfg)
	consulClient, err := consul.NewClient(cfg, c.http.ConsulAPITimeout())
	if err != nil {
		c.logger.Error("Unable to get client connection", "error", err)
		return 1
	}
	// Issue the logout.
	_, err = consulClient.ACL().Logout(&api.WriteOptions{})
	if err != nil {
		c.logger.Error("Unable to delete consul ACL token as logout failed", "error", err)
		return 1
	}
	c.logger.Error("ACL token successfully deleted")
	return 0
}

func (c *Command) Synopsis() string { return synopsis }
func (c *Command) Help() string {
	c.once.Do(c.init)
	return c.help
}

const synopsis = "Issue a consul logout to delete the ACL token."
const help = `
Usage: consul-k8s-control-plane consul-logout [options]

  Deletes the ACL token for this pod.
  Not intended for stand-alone use.
`
