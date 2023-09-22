// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package getconsulclientca

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/go-discover"
	"github.com/hashicorp/go-hclog"
	"github.com/mitchellh/cli"

	"github.com/hashicorp/consul-k8s/control-plane/consul"
	godiscover "github.com/hashicorp/consul-k8s/control-plane/helper/go-discover"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/common"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/flags"
)

// get-consul-client-ca command talks to the Consul servers
// and retrieves the active root CA from the API to be used
// to talk to the Consul client agents.
type Command struct {
	UI cli.Ui

	flags *flag.FlagSet

	flagOutputFile       string
	flagServerAddr       string
	flagServerPort       string
	flagCAFile           string
	flagTLSServerName    string
	flagConsulAPITimeout time.Duration
	flagLogLevel         string
	flagLogJSON          bool

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
	c.flags.BoolVar(&c.flagLogJSON, "log-json", false,
		"Enable or disable JSON output format for logging.")
	c.flags.DurationVar(&c.flagConsulAPITimeout, "consul-api-timeout", 0,
		"The time in seconds that the consul API client will wait for a response from the API before cancelling the request.")
	c.help = flags.Usage(help, c.flags)
}

func (c *Command) Run(args []string) int {
	c.once.Do(c.init)
	if err := c.flags.Parse(args); err != nil {
		return 1
	}

	// Validate flags
	if err := c.validateFlags(); err != nil {
		c.UI.Error(err.Error())
		return 1
	}

	logger, err := common.Logger(c.flagLogLevel, c.flagLogJSON)
	if err != nil {
		c.UI.Error(err.Error())
		return 1
	}

	// create Consul client
	consulClient, err := c.consulClient(logger)
	if err != nil {
		c.UI.Error(fmt.Sprintf("Error initializing Consul client: %s", err))
		return 1
	}

	// Get the active CA root from Consul
	// Wait until it gets a successful response
	var activeRoot string
	_ = backoff.Retry(func() error {
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

	err = os.WriteFile(c.flagOutputFile, []byte(activeRoot), 0644)
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

	return consul.NewClient(cfg, c.flagConsulAPITimeout)
}

// consulServerAddr returns the consul server address
// as a string in the <server_ip_or_dns_name>:<server_port> format.
//
//  1. If the server address is a cloud auto-join URL,
//     it calls go-discover library to discover server addresses,
//     picks the first address from the list and uses the provided port.
//  2. Otherwise, it uses the address provided by the -server-addr
//     and the -server-port flags.
func (c *Command) consulServerAddr(logger hclog.Logger) (string, error) {

	// First, check if the server address is a cloud auto-join string.
	// If not, return serverAddr:serverPort set by the provided flags.
	if !strings.Contains(c.flagServerAddr, "provider=") {
		return fmt.Sprintf("%s:%s", c.flagServerAddr, c.flagServerPort), nil
	}

	servers, err := godiscover.ConsulServerAddresses(c.flagServerAddr, c.providers, logger)
	if err != nil {
		return "", err
	}

	// Pick the first server from the list,
	// ignoring the port since we need to use HTTP API
	// and don't care about the RPC port.
	firstServer := strings.SplitN(servers[0], ":", 2)[0]
	return fmt.Sprintf("%s:%s", firstServer, c.flagServerPort), nil
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

func (c *Command) validateFlags() error {
	if len(c.flags.Args()) > 0 {
		return errors.New("Should have no non-flag arguments.")
	}

	if c.flagOutputFile == "" {
		return errors.New("-output-file must be set")
	}

	if c.flagServerAddr == "" {
		return errors.New("-server-addr must be set")
	}

	if c.flagConsulAPITimeout <= 0 {
		return errors.New("-consul-api-timeout must be set to a value greater than 0")
	}

	return nil
}

func (c *Command) Synopsis() string { return synopsis }
func (c *Command) Help() string {
	c.once.Do(c.init)
	return c.help
}

const synopsis = "Retrieve Consul client CA if using auto-encrypt feature."
const help = `
Usage: consul-k8s-control-plane get-consul-client-ca [options]

  Retrieve Consul client CA certificate by continuously polling
  Consul servers and save it at the provided file location.

`
