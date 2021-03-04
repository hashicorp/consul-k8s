package consulinit

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/hashicorp/consul-k8s/consul"
	"github.com/hashicorp/consul-k8s/subcommand/common"
	"github.com/hashicorp/consul-k8s/subcommand/flags"
	"github.com/hashicorp/consul/api"
	"github.com/mitchellh/cli"
)

const bearerTokenFile = "/var/run/secrets/kubernetes.io/serviceaccount/token"
const tokenSinkFile = "/consul/connect-inject/acl-token"

type Command struct {
	UI cli.Ui

	flagACLAuthMethod   string // Auth Method to use for ACLs, if enabled
	flagMeta            string // Flag for metadata to consul login.
	flagBearerTokenFile string // Location of the bearer token.
	flagTokenSinkFile   string // Location to write the output token.
	numACLLoginRetries  int    // Number of times to attempt to retry ACL().Login(), default: 3.

	flagSet *flag.FlagSet
	http    *flags.HTTPFlags

	consulClient *api.Client

	sigCh chan os.Signal
	once  sync.Once
	help  string
	cert  atomic.Value
}

func (c *Command) init() {
	c.flagSet = flag.NewFlagSet("", flag.ContinueOnError)
	c.flagSet.StringVar(&c.flagACLAuthMethod, "method", "",
		"The name of the Kubernetes Auth Method to use for connectInjection if ACLs are enabled.")
	c.flagSet.StringVar(&c.flagMeta, "meta", "",
		"Metadata to be passed to consul login api.")
	c.flagSet.StringVar(&c.flagBearerTokenFile, "bearer-token-file", bearerTokenFile,
		"Token file to be used for consul login api.")
	c.flagSet.StringVar(&c.flagTokenSinkFile, "token-sink-file", tokenSinkFile,
		"Token sink file to be used for consul login api.")

	c.http = &flags.HTTPFlags{}

	flags.Merge(c.flagSet, c.http.Flags())
	c.help = flags.Usage(help, c.flagSet)

	// Default number of times to attempt ACL().Login()
	c.numACLLoginRetries = 3

	// Wait on an interrupt or terminate for exit, be sure to init it before running
	// the controller so that we don't receive an interrupt before it's ready.
	if c.sigCh == nil {
		c.sigCh = make(chan os.Signal, 1)
		signal.Notify(c.sigCh, syscall.SIGINT, syscall.SIGTERM)
	}
}

func (c *Command) Run(args []string) int {
	c.once.Do(c.init)
	var err error
	if err := c.flagSet.Parse(args); err != nil {
		return 1
	}

	// Validate flags.
	if c.flagACLAuthMethod == "" {
		c.UI.Error("-method must be set")
		return 1
	}
	if c.flagMeta == "" {
		c.UI.Error("-meta must be set")
		return 1
	}
	if c.flagBearerTokenFile == "" {
		c.UI.Error("-bearer-token-file must be set")
		return 1
	}
	if c.flagTokenSinkFile == "" {
		c.UI.Error("-token-sink-file must be set")
		return 1
	}

	// TODO: Add namespace support
	if c.consulClient == nil {
		cfg := api.DefaultConfig()
		c.http.MergeOntoConfig(cfg)
		c.consulClient, err = consul.NewClient(cfg)
		if err != nil {
			c.UI.Error("unable to get client connection")
			return 1
		}
	}

	// This is hardcoded because we hardcode it in the init container.
	meta := map[string]string{"pod": strings.Split(c.flagMeta, "=")[1]}
	retries := 0
	for {
		err = common.ConsulLogin(c.consulClient, c.flagBearerTokenFile, c.flagACLAuthMethod, c.flagTokenSinkFile, meta)
		if err != nil {
			retries++
			time.Sleep(time.Second * 1)
			if retries == c.numACLLoginRetries {
				c.UI.Error(fmt.Sprintf("unable to do consul login"))
				return 1
			}
		} else {
			c.UI.Info("consul login complete")
			return 0
		}
	}
}

func (c *Command) interrupt() {
	c.sendSignal(syscall.SIGINT)
}

func (c *Command) sendSignal(sig os.Signal) {
	c.sigCh <- sig
}

func (c *Command) Synopsis() string { return synopsis }
func (c *Command) Help() string {
	c.once.Do(c.init)
	return c.help
}

const synopsis = "Inject connect init command."
const help = `
Usage: consul-k8s consul-init [options]

  Bootstraps connect-injected pod components.
  Not intended for stand-alone use.
`
