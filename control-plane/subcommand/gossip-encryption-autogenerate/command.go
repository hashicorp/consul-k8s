package gossipencryptionautogenerate

import (
	"flag"
	"fmt"
	"sync"

	"github.com/hashicorp/consul-k8s/control-plane/subcommand/common"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/flags"
	"github.com/hashicorp/go-hclog"
	"github.com/mitchellh/cli"
)

type Command struct {
	UI cli.Ui

	flags *flag.FlagSet

	// flags that dictate where the Kubernetes secret will be stored
	flagSecretName string
	flagSecretKey  string

	// log
	log          hclog.Logger
	flagLogLevel string
	flagLogJSON  bool

	once sync.Once
	help string
}

// Run parses flags and runs the command.
func (c *Command) Run(args []string) int {
	var err error

	c.once.Do(c.init)

	if err := c.flags.Parse(args); err != nil {
		c.UI.Error(fmt.Errorf("failed to parse args: %v", err).Error())
		return 1
	}

	if err = c.validateFlags(); err != nil {
		c.UI.Error(fmt.Errorf("failed to validate flags: %v", err).Error())
		return 1
	}

	c.log, err = common.Logger(c.flagLogLevel, c.flagLogJSON)
	if err != nil {
		c.UI.Error(err.Error())
		return 1
	}

	secret := Secret{
		Name: c.flagSecretName,
		Key:  c.flagSecretKey,
	}

	if err = secret.Generate(); err != nil {
		c.UI.Error(fmt.Errorf("failed to generate gossip secret: %v", err).Error())
		return 1
	}

	if err = secret.PostToKubernetes(); err != nil {
		c.UI.Error(fmt.Errorf("failed to add secret to Kubernetes: %v", err).Error())
		return 1
	}

	return 0
}

// Help returns the command's help text.
func (c *Command) Help() string {
	c.once.Do(c.init)
	return c.help
}

// Synopsis returns a one-line synopsis of the command.
func (c *Command) Synopsis() string {
	return synopsis
}

// init is run once to set up usage documentation for flags.
func (c *Command) init() {
	c.flags = flag.NewFlagSet("", flag.ContinueOnError)

	c.flags.StringVar(&c.flagLogLevel, "log-level", "info",
		"Log verbosity level. Supported values (in order of detail) are \"trace\", "+
			"\"debug\", \"info\", \"warn\", and \"error\".")
	c.flags.BoolVar(&c.flagLogJSON, "log-json", false, "Enable or disable JSON output format for logging.")
	c.flags.StringVar(&c.flagSecretName, "secret-name", "", "Name of the secret to create.")
	c.flags.StringVar(&c.flagSecretKey, "secret-key", "key", "Name of the secret key to create.")

	c.help = flags.Usage(help, c.flags)
}

// validateFlags ensures that all required flags are set.
func (c *Command) validateFlags() error {
	if c.flagSecretName == "" {
		return fmt.Errorf("-secret-name must be set")
	}

	return nil
}

const synopsis = "Generate a secret for gossip encryption."
const help = `
Usage: consul-k8s-control-plane gossip-encryption-autogenerate [options]

  Bootstraps the installation with a secret for gossip encryption.
`
