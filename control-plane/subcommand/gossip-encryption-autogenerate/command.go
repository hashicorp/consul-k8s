package gossipencryptionautogenerate

import (
	"flag"
	"fmt"
	"sync"

	"github.com/hashicorp/consul-k8s/control-plane/subcommand/common"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/flags"
	"github.com/hashicorp/go-hclog"
	"github.com/mitchellh/cli"
	"k8s.io/client-go/kubernetes"
)

const synopsis = "Generate a secret for gossip encryption"
const help = `
Usage: consul-k8s-control-plane gossip-encryption-autogenerate [options]

  Bootstraps the installation with a secret for gossip encryption.
`

type Command struct {
	UI        cli.Ui
	clientset kubernetes.Interface

	flags *flag.FlagSet

	// flags that dictate where the Kubernetes secret will be stored
	flagSecretName string
	flagSecretKey  string

	// secret for encrypting gossip
	gossipEncryptionSecret string

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
		c.UI.Error(fmt.Sprintf("Failed to parse args: %v", err))
		return 1
	}

	if err = c.validateFlags(); err != nil {
		c.UI.Error(err.Error())
		return 1
	}

	c.log, err = common.Logger(c.flagLogLevel, c.flagLogJSON)
	if err != nil {
		c.UI.Error(err.Error())
		return 1
	}

	c.gossipEncryptionSecret = generateSecret()

	if err := c.postSecretToKubernetes(); err != nil {
		c.UI.Error(err.Error())
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

	c.flags.StringVar(&c.flagSecretName, "secret-name", "", "Name of the secret to create")
	c.flags.StringVar(&c.flagSecretKey, "secret-key", "", "Name of the secret key to create")

	c.help = flags.Usage(help, c.flags)
}

// validateFlags ensures that all required flags are set.
func (c *Command) validateFlags() error {
	if (c.flagSecretName == "") || (c.flagSecretKey == "") {
		return fmt.Errorf("-secret-name and -secret-key must be set")
	}

	return nil
}

// postSecretToKubernetes stores the generated secret in the Kubernetes secret store.
func (c *Command) postSecretToKubernetes() error {
	// TODO: post the secret to the Kubernetes secret store. How do I connect into Kubernetes from here?
	return nil
}

// generateSecret uses Consul Keygen to generate a secret for gossip encryption.
func generateSecret() string {
	// TODO: generate the secret using Consul Keygen. How do I connect into Consul from here?
	return ""
}
