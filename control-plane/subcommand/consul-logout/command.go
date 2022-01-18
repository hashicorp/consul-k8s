package consullogout

import (
	"flag"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/common"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/flags"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/go-hclog"
	"github.com/mitchellh/cli"
	"io/ioutil"
	"sync"
)

type Command struct {
	UI cli.Ui

	flagLogLevel string
	flagLogJSON  bool

	flagSet *flag.FlagSet
	http    *flags.HTTPFlags

	tokenSinkFile string
	consulClient  *api.Client

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

const (
	defaultAclTokenLocation = "/consul/connect-inject/acl-token"
)

func (c *Command) Run(args []string) int {
	var err error
	c.once.Do(c.init)

	if err := c.flagSet.Parse(args); err != nil {
		return 1
	}
	if c.logger == nil {
		c.logger, err = common.Logger(c.flagLogLevel, c.flagLogJSON)
		if err != nil {
			c.UI.Error(err.Error())
			return 1
		}
	}
	if c.tokenSinkFile == "" {
		c.tokenSinkFile = defaultAclTokenLocation
	}

	if c.consulClient == nil {
		cfg := api.DefaultConfig()
		c.http.MergeOntoConfig(cfg)
		c.consulClient, err = consul.NewClient(cfg)
		if err != nil {
			c.logger.Error("Unable to get client connection", "error", err)
			return 1
		}
	}

	token, err := ioutil.ReadFile(c.tokenSinkFile)
	if err != nil {
		c.logger.Error("Unable to read ACL token", "error", err)
		return 1
	}

	_, err = c.consulClient.ACL().Logout(&api.WriteOptions{
		Token: string(token),
	})
	if err != nil {
		c.logger.Error("Unable to destroy consul ACL token", "error", err)
		return 1
	}
	c.logger.Error("ACL token succesfully destroyed")
	return 0
}

func (c *Command) Synopsis() string { return synopsis }
func (c *Command) Help() string {
	c.once.Do(c.init)
	return c.help
}

const synopsis = "Issue a consul logout to destroy the ACL token."
const help = `
Usage: consul-k8s-control-plane consul-logout [options]

  Destroys the ACL token for this pod.
  Not intended for stand-alone use.
`
