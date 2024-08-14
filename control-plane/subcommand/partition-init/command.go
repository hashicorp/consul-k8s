// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package partition_init

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"sync"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/mitchellh/cli"

	"github.com/hashicorp/consul-server-connection-manager/discovery"
	"github.com/hashicorp/consul/api"

	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/common"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/flags"
)

type Command struct {
	UI cli.Ui

	flags  *flag.FlagSet
	consul *flags.ConsulFlags

	flagLogLevel string
	flagLogJSON  bool
	flagTimeout  time.Duration

	// ctx is cancelled when the command timeout is reached.
	ctx           context.Context
	retryDuration time.Duration

	// log
	log hclog.Logger

	once sync.Once
	help string
}

func (c *Command) init() {
	c.flags = flag.NewFlagSet("", flag.ContinueOnError)

	c.flags.DurationVar(&c.flagTimeout, "timeout", 10*time.Minute,
		"How long we'll try to bootstrap Partitions for before timing out, e.g. 1ms, 2s, 3m")
	c.flags.StringVar(&c.flagLogLevel, "log-level", "info",
		"Log verbosity level. Supported values (in order of detail) are \"trace\", "+
			"\"debug\", \"info\", \"warn\", and \"error\".")
	c.flags.BoolVar(&c.flagLogJSON, "log-json", false,
		"Enable or disable JSON output format for logging.")

	c.consul = &flags.ConsulFlags{}
	flags.Merge(c.flags, c.consul.Flags())
	c.help = flags.Usage(help, c.flags)

	// Default retry to 1s. This is exposed for setting in tests.
	if c.retryDuration == 0 {
		c.retryDuration = 1 * time.Second
	}
}

func (c *Command) Synopsis() string { return synopsis }

func (c *Command) Help() string {
	c.once.Do(c.init)
	return c.help
}

func (c *Command) ensurePartition(scm consul.ServerConnectionManager) error {
	state, err := scm.State()
	if err != nil {
		c.UI.Error(fmt.Sprintf("unable to get Consul server addresses from watcher: %s", err))
		return err
	}

	consulClient, err := consul.NewClientFromConnMgrState(c.consul.ConsulClientConfig(), state)
	if err != nil {
		c.UI.Error(fmt.Sprintf("unable to create Consul client: %s", err))
		return err
	}

	for {
		partition, _, err := consulClient.Partitions().Read(c.ctx, c.consul.Partition, nil)
		// The API does not return an error if the Partition does not exist. It returns a nil Partition.
		if err != nil {
			c.log.Error("Error reading Partition from Consul", "name", c.consul.Partition, "error", err.Error())
		} else if partition == nil {
			// Retry Admin Partition creation until it succeeds, or we reach the command timeout.
			_, _, err = consulClient.Partitions().Create(c.ctx, &api.Partition{
				Name:        c.consul.Partition,
				Description: "Created by Helm installation",
			}, nil)
			if err == nil {
				c.log.Info("Successfully created Admin Partition", "name", c.consul.Partition)
				return nil
			}
			c.log.Error("Error creating partition", "name", c.consul.Partition, "error", err.Error())
		} else {
			c.log.Info("Admin Partition already exists", "name", c.consul.Partition)
			return nil
		}
		// Wait on either the retry duration (in which case we continue) or the
		// overall command timeout.
		c.log.Info("Retrying in " + c.retryDuration.String())
		select {
		case <-time.After(c.retryDuration):
			continue
		case <-c.ctx.Done():
			c.log.Error("Timed out attempting to create partition", "name", c.consul.Partition)
			return fmt.Errorf("")
		}
	}
}

// Run bootstraps Admin Partitions on Consul servers.
// The function will retry its tasks until success, or it exceeds its timeout.
func (c *Command) Run(args []string) int {
	c.once.Do(c.init)
	if err := c.flags.Parse(args); err != nil {
		return 1
	}
	if len(c.flags.Args()) > 0 {
		c.UI.Error("Should have no non-flag arguments.")
		return 1
	}

	// Validate flags
	if err := c.validateFlags(); err != nil {
		c.UI.Error(err.Error())
		return 1
	}
	var cancel context.CancelFunc
	c.ctx, cancel = context.WithTimeout(context.Background(), c.flagTimeout)
	// The context will only ever be intentionally ended by the timeout.
	defer cancel()

	var err error
	c.log, err = common.Logger(c.flagLogLevel, c.flagLogJSON)
	if err != nil {
		c.UI.Error(err.Error())
		return 1
	}

	// Start Consul server Connection manager
	serverConnMgrCfg, err := c.consul.ConsulServerConnMgrConfig()
	serverConnMgrCfg.ServerWatchDisabled = true
	if err != nil {
		c.UI.Error(fmt.Sprintf("unable to create config for consul-server-connection-manager: %s", err))
		return 1
	}
	watcher, err := discovery.NewWatcher(c.ctx, serverConnMgrCfg, c.log.Named("consul-server-connection-manager"))
	if err != nil {
		c.UI.Error(fmt.Sprintf("unable to create Consul server watcher: %s", err))
		return 1
	}

	go watcher.Run()
	defer watcher.Stop()

	err = c.ensurePartition(watcher)
	if err != nil {
		return 1
	}
	return 0
}

func (c *Command) validateFlags() error {
	if len(c.consul.Addresses) == 0 {
		return errors.New("-addresses must be set")
	}

	if c.consul.Partition == "" {
		return errors.New("-partition must be set")
	}

	if c.consul.APITimeout <= 0 {
		return errors.New("-api-timeout must be set to a value greater than 0")
	}

	return nil
}

const synopsis = "Initialize an Admin Partition in Consul."
const help = `
Usage: consul-k8s-control-plane partition-init [options]

  Bootstraps Consul with non-default Admin Partitions.
  It will run until the partition has been created or the operation times out. It is idempotent
  and safe to run multiple times.

`
