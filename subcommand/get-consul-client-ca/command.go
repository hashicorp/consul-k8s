package getconsulclientca

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"sync"
	"time"

	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/command/flags"
	"github.com/hashicorp/go-hclog"
	"github.com/mitchellh/cli"
)

type Command struct {
	UI cli.Ui

	flags *flag.FlagSet

	flagOutputFile    string
	flagHttpAddr      string
	flagCAFile        string
	flagTLSServerName string
	flagLogLevel      string

	once sync.Once
	help string
}

func (c *Command) init() {
	c.flags = flag.NewFlagSet("", flag.ContinueOnError)
	c.flags.StringVar(&c.flagOutputFile, "output-file", "",
		"The path to the file where to put the Consul client's CA certificate.")
	c.flags.StringVar(&c.flagHttpAddr, "http-addr", "",
		"The HTTP address of the Consul server. This can also be provided via the CONSUL_HTTP_ADDR environment variable.")
	c.flags.StringVar(&c.flagCAFile, "ca-file", "",
		"The path to the CA file to use when making requests to the Consul server. This can also be provided via the CONSUL_CACERT environment variable")
	c.flags.StringVar(&c.flagTLSServerName, "tls-server-name", "",
		"The server name to set as the SNI header when sending HTTPS requests to Consul. This can also be provided via the CONSUL_TLS_SERVER_NAME environment variable.")
	c.flags.StringVar(&c.flagLogLevel, "log-level", "info",
		"Log verbosity level. Supported values (in order of detail) are \"trace\", "+
			"\"debug\", \"info\", \"warn\", and \"error\".")

	c.help = flags.Usage(help, c.flags)
}

func (c *Command) Run(args []string) int {
	c.once.Do(c.init)
	if err := c.flags.Parse(args); err != nil {
		return 1
	}
	if len(c.flags.Args()) > 0 {
		c.UI.Error(fmt.Sprintf("Should have no non-flag arguments."))
		return 1
	}

	if c.flagOutputFile == "" {
		c.UI.Error(fmt.Sprintf("-output-file must be set"))
		return 1
	}

	// create Consul client
	consulClient, err := c.consulClient()
	if err != nil {
		c.UI.Error(fmt.Sprintf("Error initializing Consul client: %s", err))
		return 1
	}

	// create a logger
	level := hclog.LevelFromString(c.flagLogLevel)
	if level == hclog.NoLevel {
		c.UI.Error(fmt.Sprintf("Unknown log level: %s", c.flagLogLevel))
		return 1
	}
	logger := hclog.New(&hclog.LoggerOptions{
		Level:  level,
		Output: os.Stderr,
	})

	// Get the active CA root from Consul
	// Wait until it gets a successful response
	var activeRoot string
	for activeRoot == "" {
		caRoots, _, err := consulClient.Agent().ConnectCARoots(nil)
		if err != nil {
			logger.Info("Error retrieving CA roots from Consul", "err", err)
			time.Sleep(1 * time.Second)
			continue
		}

		activeRoot, err = c.getActiveRoot(caRoots)
		if err != nil {
			logger.Info("Could not get an active root", "err", err)
			time.Sleep(1 * time.Second)
			continue
		}
	}

	err = ioutil.WriteFile(c.flagOutputFile, []byte(activeRoot), 0644)
	if err != nil {
		c.UI.Error(fmt.Sprintf("Error writing CA file: %s", err))
		return 1
	}

	return 0
}

func (c *Command) consulClient() (*api.Client, error) {
	cfg := api.DefaultConfig()
	if c.flagHttpAddr != "" {
		cfg.Address = c.flagHttpAddr
	}
	if c.flagCAFile != "" {
		cfg.TLSConfig.CAFile = c.flagCAFile
	}
	if c.flagTLSServerName != "" {
		cfg.TLSConfig.Address = c.flagTLSServerName
	}

	return api.NewClient(cfg)
}

func (c *Command) getActiveRoot(roots *api.CARootList) (string, error) {
	if roots == nil {
		return "", fmt.Errorf("ca roots is nil")
	}
	if roots.Roots == nil {
		return "", fmt.Errorf("ca roots is nil")
	}
	if len(roots.Roots) == 0 {
		return "", fmt.Errorf("the list of root CAs is empty")
	}

	for _, root := range roots.Roots {
		if root.Active {
			return root.RootCertPEM, nil
		}
	}
	return "", fmt.Errorf("none of the roots were active")
}

func (c *Command) Synopsis() string { return synopsis }
func (c *Command) Help() string {
	c.once.Do(c.init)
	return c.help
}

const synopsis = "Retrieve Consul client CA if using auto-encrypt feature."
const help = `
Usage: consul-k8s get-consul-client-ca [options]

  Retrieve Consul client CA certificate by continuously polling
  Consul servers and save it at the provided file location.

`
