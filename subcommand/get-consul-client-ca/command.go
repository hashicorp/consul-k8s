package getconsulclientca

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/command/flags"
	"github.com/hashicorp/consul/lib"
	"github.com/hashicorp/go-discover"
	discoverk8s "github.com/hashicorp/go-discover/provider/k8s"
	"github.com/hashicorp/go-hclog"
	"github.com/mitchellh/cli"
)

type Command struct {
	UI cli.Ui

	flags *flag.FlagSet

	flagOutputFile    string
	flagServerAddr    string
	flagCAFile        string
	flagTLSServerName string
	flagLogLevel      string

	once sync.Once
	help string

	providers map[string]discover.Provider
}

func (c *Command) init() {
	c.flags = flag.NewFlagSet("", flag.ContinueOnError)
	c.flags.StringVar(&c.flagOutputFile, "output-file", "",
		"The path to the file where to put the Consul client's CA certificate.")
	c.flags.StringVar(&c.flagServerAddr, "server-addr", "",
		"The address of the Consul server or the Cloud auto-join string. The server must be running with TLS enabled."+
			"The default HTTPS port 8501 will be used if port is not provided.")
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
	for activeRoot == "" {
		caRoots, _, err := consulClient.Agent().ConnectCARoots(nil)
		if err != nil {
			logger.Info("Error retrieving CA roots from Consul", "err", err)
			time.Sleep(1 * time.Second)
			continue
		}

		activeRoot, err = getActiveRoot(caRoots)
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

	c.UI.Info(fmt.Sprintf("Successfully written Consul client CA to: %s", c.flagOutputFile))
	return 0
}

func (c *Command) consulClient(logger hclog.Logger) (*api.Client, error) {
	cfg := api.DefaultConfig()

	// First, check if the server address is a cloud auto-join string.
	// If it is, discover server addresses through the cloud provider.
	if strings.Contains(c.flagServerAddr, "provider=") {
		disco, err := c.newDiscover()
		if err != nil {
			return nil, err
		}
		logger.Debug("using cloud auto-join with", c.flagServerAddr)
		servers, err := disco.Addrs(c.flagServerAddr, logger.StandardLogger(&hclog.StandardLoggerOptions{
			InferLevels: true,
		}))
		if err != nil {
			return nil, err
		}

		// check if we discovered any servers
		if len(servers) == 0 {
			return nil, fmt.Errorf("could not discover any Consul servers with %q", c.flagServerAddr)
		}

		logger.Debug("discovered servers", strings.Join(servers, " "))

		// Pick the first server from the list,
		// ignoring the port since we need to use HTTP API
		firstServer := strings.SplitN(servers[0], ":", 2)[0]
		cfg.Address = fmt.Sprintf("%s:8501", firstServer)
		cfg.Scheme = "https"
	} else {
		// check if the server URL is missing a port
		host := strings.TrimPrefix(c.flagServerAddr, "https://")
		host = strings.TrimPrefix(c.flagServerAddr, "http://")
		parts := strings.SplitN(host, ":", 2)

		// Use the default HTTPS port if port is missing.
		// Otherwise, use the address the user has provided.
		if len(parts) == 1 {
			cfg.Address = fmt.Sprintf("%s:8501", c.flagServerAddr)
			cfg.Scheme = "https"
		} else {
			cfg.Address = c.flagServerAddr
		}
	}

	if c.flagCAFile != "" {
		cfg.TLSConfig.CAFile = c.flagCAFile
	}
	if c.flagTLSServerName != "" {
		cfg.TLSConfig.Address = c.flagTLSServerName
	}

	return api.NewClient(cfg)
}

// newDiscover initializes the new Discover object
// set up with all predefined providers, as well as
// the k8s provider.
func (c *Command) newDiscover() (*discover.Discover, error) {
	if c.providers == nil {
		c.providers = make(map[string]discover.Provider)
	}

	for k, v := range discover.Providers {
		c.providers[k] = v
	}
	c.providers["k8s"] = &discoverk8s.Provider{}

	return discover.New(
		discover.WithUserAgent(lib.UserAgent()),
		discover.WithProviders(c.providers),
	)
}

// getActiveRoot returns the currently active root
// from the roots list, otherwise returns error.
func getActiveRoot(roots *api.CARootList) (string, error) {
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
