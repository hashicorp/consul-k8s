package partition_init

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"sync"
	"time"

	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/common"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/flags"
	k8sflags "github.com/hashicorp/consul-k8s/control-plane/subcommand/flags"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/go-discover"
	"github.com/hashicorp/go-hclog"
	"github.com/mitchellh/cli"
)

type Command struct {
	UI cli.Ui

	flags *flag.FlagSet
	k8s   *k8sflags.K8SFlags
	http  *flags.HTTPFlags

	flagPartitionName string

	// Flags to configure Consul connection
	flagServerAddresses     []string
	flagServerPort          uint
	flagConsulCACert        string
	flagConsulTLSServerName string
	flagUseHTTPS            bool

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

	providers map[string]discover.Provider
}

func (c *Command) init() {
	c.flags = flag.NewFlagSet("", flag.ContinueOnError)

	c.flags.StringVar(&c.flagPartitionName, "partition-name", "", "The name of the partition being created.")

	c.flags.Var((*flags.AppendSliceValue)(&c.flagServerAddresses), "server-address",
		"The IP, DNS name or the cloud auto-join string of the Consul server(s). If providing IPs or DNS names, may be specified multiple times. "+
			"At least one value is required.")
	c.flags.UintVar(&c.flagServerPort, "server-port", 8500, "The HTTP or HTTPS port of the Consul server. Defaults to 8500.")
	c.flags.StringVar(&c.flagConsulCACert, "consul-ca-cert", "",
		"Path to the PEM-encoded CA certificate of the Consul cluster.")
	c.flags.StringVar(&c.flagConsulTLSServerName, "consul-tls-server-name", "",
		"The server name to set as the SNI header when sending HTTPS requests to Consul.")
	c.flags.BoolVar(&c.flagUseHTTPS, "use-https", false,
		"Toggle for using HTTPS for all API calls to Consul.")
	c.flags.DurationVar(&c.flagTimeout, "timeout", 10*time.Minute,
		"How long we'll try to bootstrap Partitions for before timing out, e.g. 1ms, 2s, 3m")
	c.flags.StringVar(&c.flagLogLevel, "log-level", "info",
		"Log verbosity level. Supported values (in order of detail) are \"trace\", "+
			"\"debug\", \"info\", \"warn\", and \"error\".")
	c.flags.BoolVar(&c.flagLogJSON, "log-json", false,
		"Enable or disable JSON output format for logging.")

	c.k8s = &k8sflags.K8SFlags{}
	c.http = &flags.HTTPFlags{}
	flags.Merge(c.flags, c.k8s.Flags())
	flags.Merge(c.flags, c.http.Flags())
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

	serverAddresses, err := common.GetResolvedServerAddresses(c.flagServerAddresses, c.providers, c.log)
	if err != nil {
		c.UI.Error(fmt.Sprintf("Unable to discover any Consul addresses from %q: %s", c.flagServerAddresses[0], err))
		return 1
	}

	scheme := "http"
	if c.flagUseHTTPS {
		scheme = "https"
	}
	// For all of the next operations we'll need a Consul client.
	serverAddr := fmt.Sprintf("%s:%d", serverAddresses[0], c.flagServerPort)
	consulClient, err := consul.NewClient(&api.Config{
		Address: serverAddr,
		Scheme:  scheme,
		TLSConfig: api.TLSConfig{
			Address: c.flagConsulTLSServerName,
			CAFile:  c.flagConsulCACert,
		},
	})
	if err != nil {
		c.UI.Error(fmt.Sprintf("Error creating Consul client for addr %q: %s", serverAddr, err))
		return 1
	}
	for {
		partition, _, err := consulClient.Partitions().Read(c.ctx, c.flagPartitionName, nil)
		// The API does not return an error if the Partition does not exist. It returns a nil Partition.
		if err != nil {
			c.log.Error("Error reading Partition from Consul", "name", c.flagPartitionName, "error", err.Error())
		} else if partition == nil {
			// Retry Admin Partition creation until it succeeds, or we reach the command timeout.
			_, _, err = consulClient.Partitions().Create(c.ctx, &api.AdminPartition{
				Name:        c.flagPartitionName,
				Description: "Created by Helm installation",
			}, nil)
			if err == nil {
				c.log.Info("Successfully created Admin Partition", "name", c.flagPartitionName)
				return 0
			}
			c.log.Error("Error creating partition", "name", c.flagPartitionName, "error", err.Error())
		} else {
			c.log.Info("Admin Partition already exists", "name", c.flagPartitionName)
			return 0
		}
		// Wait on either the retry duration (in which case we continue) or the
		// overall command timeout.
		c.log.Info("Retrying in " + c.retryDuration.String())
		select {
		case <-time.After(c.retryDuration):
			continue
		case <-c.ctx.Done():
			c.log.Error("Timed out attempting to create partition", "name", c.flagPartitionName)
			return 1
		}
	}
}

func (c *Command) validateFlags() error {
	if len(c.flagServerAddresses) == 0 {
		return errors.New("-server-address must be set at least once")
	}

	if c.flagPartitionName == "" {
		return errors.New("-partition-name must be set")
	}
	return nil
}

const synopsis = "Initialize an Admin Partition on Consul."
const help = `
Usage: consul-k8s-control-plane partition-init [options]

  Bootstraps Consul with non-default Admin Partitions.
  It will run until the partition has been created or the operation times out. It is idempotent
  and safe to run multiple times.

`
