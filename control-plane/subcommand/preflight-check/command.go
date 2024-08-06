// SPDX-License-Identifier: MPL-2.0

package preflight_check

import (
	"context"
	"flag"
	"fmt"
	"github.com/hashicorp/go-hclog"
	"github.com/mitchellh/cli"
	"k8s.io/client-go/kubernetes"
	"sync"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/hashicorp/consul/api"

	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/common"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/flags"
)

var retryInterval = 1 * time.Second

type Command struct {
	UI cli.Ui

	clientset    kubernetes.Interface
	consulClient *api.Client

	flags    *flag.FlagSet
	http     *flags.HTTPFlags
	k8sFlags *flags.K8SFlags

	// flags that dictate specifics for the datacenter name to check
	flagDatacenter string

	// log
	log          hclog.Logger
	flagLogLevel string
	flagLogJSON  bool

	ctx context.Context

	once sync.Once
	help string
}

func (c *Command) Run(args []string) int {
	c.once.Do(c.init)
	if err := c.flags.Parse(args); err != nil {
		c.UI.Error(fmt.Sprintf("Failed to parse args: %v", err))
		return 1
	}

	if len(c.flags.Args()) > 0 {
		c.UI.Error("Should have no non-flag arguments.")
		return 1
	}

	var err error
	c.log, err = common.Logger(c.flagLogLevel, c.flagLogJSON)
	if err != nil {
		c.UI.Error(err.Error())
		return 1
	}

	if c.clientset == nil {
		if err = c.configureKubeClient(); err != nil {
			c.UI.Error(fmt.Sprintf("error configuring kubernetes: %v", err))
			return 1
		}
	}

	// Set up Consul client because we need to make calls to Consul to retrieve
	// the datacenter name.
	if c.consulClient == nil {
		cfg := api.DefaultConfig()
		// Merge our base config containing the optional ACL token with client
		// config automatically parsed from the passed flags and environment
		// variables. For example, when running in k8s the CONSUL_HTTP_ADDR environment
		// variable will be set to the IP of the Consul client pod on the same
		// node.
		c.http.MergeOntoConfig(cfg)

		c.consulClient, err = consul.NewClient(cfg, c.http.ConsulAPITimeout())
		if err != nil {
			c.log.Error("Error creating consul client", "err", err)
			return 1
		}
	}

	var cancel context.CancelFunc
	c.ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	// Perform the datacenter preflight check
	if err = c.checkDatacenter(); err != nil {
		c.log.Error(fmt.Sprintf("Datacenter preflight check failed: %v", err))
		return 1
	}

	return 0
}

func (c *Command) init() {
	c.flags = flag.NewFlagSet("", flag.ContinueOnError)
	c.flags.StringVar(&c.flagDatacenter, "datacenter", "", "Datacenter value to be checked for immutability")
	c.flags.StringVar(&c.flagLogLevel, "log-level", "info",
		"Log verbosity level. Supported values (in order of detail) are \"trace\", "+
			"\"debug\", \"info\", \"warn\", and \"error\".")
	c.flags.BoolVar(&c.flagLogJSON, "log-json", false,
		"Enable or disable JSON output format for logging.")
	c.k8sFlags = &flags.K8SFlags{}
	flags.Merge(c.flags, c.k8sFlags.Flags())
	c.help = flags.Usage(help, c.flags)
}

// configureKubeClient initializes the K8s clientset.
func (c *Command) configureKubeClient() error {
	config, err := subcommand.K8SConfig(c.k8sFlags.KubeConfig())
	if err != nil {
		return fmt.Errorf("error retrieving Kubernetes auth: %s", err)
	}
	c.clientset, err = kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("error initializing Kubernetes client: %s", err)
	}
	return nil
}

// consulDatacenter returns the current datacenter.
func (c *Command) consulDatacenter() string {
	// withLog is a helper method we'll use in the retry loop below to ensure
	// that errors are logged.
	var withLog = func(fn func() error) func() error {
		return func() error {
			err := fn()
			if err != nil {
				c.log.Error("Error retrieving current datacenter, retrying", "err", err)
			}
			return err
		}
	}

	// Run in a retry because the Consul clients may not be running yet.
	var dc string
	_ = backoff.Retry(withLog(func() error {
		agentCfg, err := c.consulClient.Agent().Self()
		if err != nil {
			return err
		}
		if _, ok := agentCfg["Config"]; !ok {
			return fmt.Errorf("/agent/self response did not contain Config key: %s", agentCfg)
		}
		if _, ok := agentCfg["Config"]["Datacenter"]; !ok {
			return fmt.Errorf("/agent/self response did not contain Config.Datacenter key: %s", agentCfg)
		}
		var ok bool
		dc, ok = agentCfg["Config"]["Datacenter"].(string)
		if !ok {
			return fmt.Errorf("could not cast Config.Datacenter as string: %s", agentCfg)
		}
		if dc == "" {
			return fmt.Errorf("value of Config.Datacenter was empty string: %s", agentCfg)
		}
		return nil
	}), backoff.NewConstantBackOff(retryInterval))

	return dc
}

// checkDatacenter verifies if the datacenter value has been changed.
func (c *Command) checkDatacenter() error {
	// Simulate getting the current datacenter value from the existing Consul configuration
	currentDatacenter := c.consulDatacenter()

	if c.flagDatacenter != currentDatacenter {
		return fmt.Errorf("altering .Values.global.datacenter after initial cluster bootstrap is unsupported")
	}
	return nil
}

func (c *Command) Help() string {
	c.once.Do(c.init)
	return c.help
}

func (c *Command) Synopsis() string {
	return synopsis
}

const synopsis = "Perform Consul preflight operational and configuration-based checks during install/upgrade."
const help = `
Usage: consul-k8s-control-plane preflight-check [options]

  Performs Consul cluster initial preflight verification checks prior to continued cluster
  operational and configuration changes triggered by Helm and consul-k8s CLI tooling.

Options:
  -datacenter    Datacenter value to be checked for immutability.
`
