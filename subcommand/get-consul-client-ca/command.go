package getconsulclientca

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/hashicorp/consul-k8s/version"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/command/flags"
	"github.com/hashicorp/go-discover"
	discoverk8s "github.com/hashicorp/go-discover/provider/k8s"
	"github.com/hashicorp/go-hclog"
	"github.com/mitchellh/cli"
)

// get-consul-client-ca command talks to the Consul servers
// and retrieves the active root CA from the API to be used
// to talk to the Consul client agents.
type Command struct {
	UI cli.Ui

	flags *flag.FlagSet

	flagOutputFile      string
	flagServerAddr      string
	flagServerPort      string
	flagCAFile          string
	flagTLSServerName   string
	flagPollingInterval time.Duration
	flagLogLevel        string

	once sync.Once
	help string

	providers map[string]discover.Provider
}

func (c *Command) init() {
	c.flags = flag.NewFlagSet("", flag.ContinueOnError)
	c.flags.StringVar(&c.flagOutputFile, "output-file", "",
		"The file path for writing the Consul client's CA certificate.")
	c.flags.StringVar(&c.flagServerAddr, "server-addr", "",
		"The address of the Consul server or the cloud auto-join string. The server must be running with TLS enabled. "+
			"This value is required.")
	c.flags.StringVar(&c.flagServerPort, "server-port", "443", "The HTTPS port of the Consul server.")
	c.flags.StringVar(&c.flagCAFile, "ca-file", "",
		"The path to the CA file to use when making requests to the Consul server. This can also be provided via the CONSUL_CACERT environment variable instead if preferred. "+
			"If both values are present, the flag value will be used.")
	c.flags.StringVar(&c.flagTLSServerName, "tls-server-name", "",
		"The server name to set as the SNI header when sending HTTPS requests to Consul. This can also be provided via the CONSUL_TLS_SERVER_NAME environment variable instead if preferred. "+
			"If both values are present, the flag value will be used.")
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

	if c.flagServerAddr == "" {
		c.UI.Error(fmt.Sprintf("-server-addr must be set"))
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

	// create Consul client
	consulClient, err := c.consulClient(logger)
	if err != nil {
		c.UI.Error(fmt.Sprintf("Error initializing Consul client: %s", err))
		return 1
	}

	// Get the active CA root from Consul
	// Wait until it gets a successful response
	var activeRoot string
	backoff.Retry(func() error {
		caRoots, _, err := consulClient.Agent().ConnectCARoots(nil)
		if err != nil {
			logger.Error("Error retrieving CA roots from Consul", "err", err)
			return err
		}

		activeRoot, err = getActiveRoot(caRoots)
		if err != nil {
			logger.Error("Could not get an active root", "err", err)
			return err
		}

		return nil
	}, backoff.NewConstantBackOff(1*time.Second))

	err = ioutil.WriteFile(c.flagOutputFile, []byte(activeRoot), 0644)
	if err != nil {
		c.UI.Error(fmt.Sprintf("Error writing CA file: %s", err))
		return 1
	}

	c.UI.Info(fmt.Sprintf("Successfully wrote Consul client CA to: %s", c.flagOutputFile))
	return 0
}

// consulClient returns a Consul API client.
func (c *Command) consulClient(logger hclog.Logger) (*api.Client, error) {
	// Create default Consul config.
	// This will also read any environment variables.
	cfg := api.DefaultConfig()

	// change the scheme to HTTPS
	// since we don't want to send unencrypted requests
	cfg.Scheme = "https"

	addr, err := c.consulServerAddr(logger)
	if err != nil {
		return nil, err
	}
	if addr != "" {
		cfg.Address = addr
	}

	// Set the CA file and TLS server name if the flag is provided.
	// This will overwrite any env variables values for these flags.
	if c.flagCAFile != "" {
		cfg.TLSConfig.CAFile = c.flagCAFile
	}
	if c.flagTLSServerName != "" {
		cfg.TLSConfig.Address = c.flagTLSServerName
	}

	return api.NewClient(cfg)
}

// consulServerAddr returns the consul server address
// as a string in the <server_ip_or_dns_name>:<server_port> format.
//
// 1. If the server address is a cloud auto-join URL,
//    it calls go-discover library to discover server addresses,
//    picks the first address from the list and uses the provided port.
// 2. Otherwise, it uses the address provided by the -server-addr
//    and the -server-port flags.
func (c *Command) consulServerAddr(logger hclog.Logger) (string, error) {
	// First, check if the server address is a cloud auto-join string.
	// If it is, discover server addresses through the cloud provider.
	// This code was adapted from
	// https://github.com/hashicorp/consul/blob/c5fe112e59f6e8b03159ec8f2dbe7f4a026ce823/agent/retry_join.go#L55-L89.
	if strings.Contains(c.flagServerAddr, "provider=") {
		disco, err := c.newDiscover()
		if err != nil {
			return "", err
		}
		logger.Debug("using cloud auto-join", "server-addr", c.flagServerAddr)
		servers, err := disco.Addrs(c.flagServerAddr, logger.StandardLogger(&hclog.StandardLoggerOptions{
			InferLevels: true,
		}))
		if err != nil {
			return "", err
		}

		// check if we discovered any servers
		if len(servers) == 0 {
			return "", fmt.Errorf("could not discover any Consul servers with %q", c.flagServerAddr)
		}

		logger.Debug("discovered servers", strings.Join(servers, " "))

		// Pick the first server from the list,
		// ignoring the port since we need to use HTTP API
		// and don't care about the RPC port.
		firstServer := strings.SplitN(servers[0], ":", 2)[0]
		return fmt.Sprintf("%s:%s", firstServer, c.flagServerPort), nil
	}

	// Otherwise, return serverAddr:serverPort set by the provided flags
	return fmt.Sprintf("%s:%s", c.flagServerAddr, c.flagServerPort), nil
}

// newDiscover initializes the new Discover object
// set up with all predefined providers, as well as
// the k8s provider.
// This code was adapted from
// https://github.com/hashicorp/consul/blob/c5fe112e59f6e8b03159ec8f2dbe7f4a026ce823/agent/retry_join.go#L42-L53
func (c *Command) newDiscover() (*discover.Discover, error) {
	if c.providers == nil {
		c.providers = make(map[string]discover.Provider)
	}

	for k, v := range discover.Providers {
		c.providers[k] = v
	}
	c.providers["k8s"] = &discoverk8s.Provider{}

	userAgent := fmt.Sprintf("consul-k8s/%s (https://www.consul.io/)", version.GetHumanVersion())
	return discover.New(
		discover.WithUserAgent(userAgent),
		discover.WithProviders(c.providers),
	)
}

// getActiveRoot returns the currently active root
// from the roots list, otherwise returns error.
func getActiveRoot(roots *api.CARootList) (string, error) {
	if roots == nil {
		return "", fmt.Errorf("ca root list is nil")
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
