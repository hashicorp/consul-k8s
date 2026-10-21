// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package featuregateset

import (
	"context"
	"flag"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/consul-k8s/control-plane/consul"
	godiscover "github.com/hashicorp/consul-k8s/control-plane/helper/go-discover"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/common"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/flags"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-netaddrs"
	"github.com/mitchellh/cli"
)

type Command struct {
	UI cli.Ui

	flags         *flag.FlagSet
	consul        *flags.ConsulFlags
	gates         flags.FlagMapValue
	flagLogLevel  string
	flagLogJSON   bool
	flagTimeout   time.Duration
	retryDuration time.Duration
	log           hclog.Logger
	once          sync.Once
	help          string
}

func (c *Command) init() {
	c.flags = flag.NewFlagSet("", flag.ContinueOnError)
	c.flags.StringVar(&c.flagLogLevel, "log-level", "info", "Log verbosity level.")
	c.flags.BoolVar(&c.flagLogJSON, "log-json", false, "Enable JSON logging.")
	c.flags.DurationVar(&c.flagTimeout, "timeout", 10*time.Minute, "How long to wait for a Consul leader and set feature gates.")
	c.flags.Var(&c.gates, "feature-gate", "Feature gate intent as name=true|false. May be specified multiple times.")
	c.consul = &flags.ConsulFlags{}
	flags.Merge(c.flags, c.consul.Flags())
	c.help = flags.Usage(help, c.flags)
	if c.retryDuration == 0 {
		c.retryDuration = time.Second
	}
}

func (c *Command) Run(args []string) int {
	c.once.Do(c.init)
	if err := c.flags.Parse(args); err != nil {
		c.UI.Error(err.Error())
		return 1
	}
	if err := c.validateFlags(); err != nil {
		c.UI.Error(err.Error())
		return 1
	}
	var err error
	c.log, err = common.Logger(c.flagLogLevel, c.flagLogJSON)
	if err != nil {
		c.UI.Error(err.Error())
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), c.flagTimeout)
	defer cancel()

	client, err := c.waitForLeader(ctx)
	if err != nil {
		c.log.Error("Unable to connect to Consul with an elected leader", "error", err)
		return 1
	}
	names := make([]string, 0, len(c.gates))
	for name := range c.gates {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		enabled, err := strconv.ParseBool(c.gates[name])
		if err != nil {
			c.log.Error("Invalid feature gate value", "name", name, "error", err)
			return 1
		}
		c.log.Info("Setting feature gate", "name", name, "enabled", enabled)
		result, err := client.Operator().FeatureGateSet(name, enabled, 0, (&api.WriteOptions{}).WithContext(ctx))
		if err != nil {
			c.log.Error("Error setting feature gate", "name", name, "error", err)
			return 1
		}
		if !result.Applied {
			c.log.Error("Feature gate intent was not applied", "name", name)
			return 1
		}
	}
	return 0
}

func (c *Command) serverAddresses(ctx context.Context) ([]string, error) {
	if !strings.HasPrefix(c.consul.Addresses, "exec=") && strings.Contains(c.consul.Addresses, "provider=") {
		return godiscover.ConsulServerAddresses(c.consul.Addresses, nil, c.log)
	}
	ipAddrs, err := netaddrs.IPAddrs(ctx, c.consul.Addresses, c.log)
	if err != nil {
		return nil, err
	}
	if len(ipAddrs) == 0 {
		return nil, fmt.Errorf("no Consul server addresses were resolved")
	}
	addresses := make([]string, 0, len(ipAddrs))
	for _, addr := range ipAddrs {
		addresses = append(addresses, addr.String())
	}
	c.log.Info("Refreshing server IP addresses", "addresses", addresses)
	return addresses, nil
}

func (c *Command) waitForLeader(ctx context.Context) (*api.Client, error) {
	for {
		addresses, err := c.serverAddresses(ctx)
		if err != nil {
			c.log.Error("Error resolving Consul server addresses", "error", err)
		} else {
			for _, address := range addresses {
				config := c.consul.ConsulClientConfig()
				config.APIClientConfig.Address = net.JoinHostPort(address, strconv.Itoa(config.HTTPPort))
				client, err := consul.NewClient(config.APIClientConfig, config.APITimeout)
				if err != nil {
					return nil, fmt.Errorf("error creating Consul client: %w", err)
				}
				leader, err := client.Status().LeaderWithQueryOptions((&api.QueryOptions{}).WithContext(ctx))
				if err == nil && leader != "" {
					c.log.Info("Consul leader is ready", "leader", leader)
					return client, nil
				}
				if err != nil {
					c.log.Error("Error checking Consul leader", "address", address, "error", err)
				} else {
					c.log.Info("Waiting for Consul leader", "address", address)
				}
			}
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(c.retryDuration):
		}
	}
}

func (c *Command) validateFlags() error {
	if c.flags.NArg() != 0 {
		return fmt.Errorf("should have no non-flag arguments")
	}
	if c.consul.Addresses == "" {
		return fmt.Errorf("-addresses must be set")
	}
	// go-netaddrs requires an executable after exec=.
	if strings.HasPrefix(c.consul.Addresses, "exec=") && strings.TrimSpace(strings.TrimPrefix(c.consul.Addresses, "exec=")) == "" {
		return fmt.Errorf("exec= requires an executable")
	}
	if c.consul.HTTPPort <= 0 || c.consul.HTTPPort > 65535 {
		return fmt.Errorf("-http-port must be between 1 and 65535")
	}
	if c.consul.APITimeout <= 0 || c.flagTimeout <= 0 {
		return fmt.Errorf("-api-timeout and -timeout must be greater than zero")
	}
	for name, value := range c.gates {
		if name == "" {
			return fmt.Errorf("feature gate name must not be empty")
		}
		if _, err := strconv.ParseBool(value); err != nil {
			return fmt.Errorf("invalid value for feature gate %q: %w", name, err)
		}
	}
	return nil
}

func (c *Command) Synopsis() string {
	return "Set Consul feature gate intent after a leader is elected."
}

func (c *Command) Help() string {
	c.once.Do(c.init)
	return c.help
}

const help = `
Usage: consul-k8s-control-plane feature-gate-set [options]

  Resolves Consul server addresses, waits for a leader, and sets feature gate intent.
  Supports DNS names, IP addresses, and exec= server discovery.
`
