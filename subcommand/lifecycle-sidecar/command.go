package subcommand

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"time"

	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/command/flags"
	"github.com/hashicorp/go-hclog"
	"github.com/mitchellh/cli"
	"github.com/prometheus/common/log"
)

type Command struct {
	UI cli.Ui

	http              *flags.HTTPFlags
	flagServiceConfig string
	flagConsulBinary  string
	flagSyncPeriod    string
	flagSet           *flag.FlagSet
	flagLogLevel      string

	once         sync.Once
	help         string
	consulClient *api.Client
	sigCh        chan os.Signal
}

func (c *Command) init() {
	c.flagSet = flag.NewFlagSet("", flag.ContinueOnError)
	c.flagSet.StringVar(&c.flagServiceConfig, "service-config", "", "Path to the service config file")
	c.flagSet.StringVar(&c.flagConsulBinary, "consul-binary", "", "Path to a consul binary")
	c.flagSet.StringVar(&c.flagSyncPeriod, "sync-period", "10s", "Time between syncing the service registration. Defaults to 10s.")
	c.flagSet.StringVar(&c.flagLogLevel, "log-level", "info",
		"Log verbosity level. Supported values (in order of detail) are \"trace\", "+
			"\"debug\", \"info\", \"warn\", and \"error\". Defaults to info.")

	c.help = flags.Usage(help, c.flagSet)
	c.http = &flags.HTTPFlags{}
	flags.Merge(c.flagSet, c.http.ClientFlags())
	c.help = flags.Usage(help, c.flagSet)
	c.sigCh = make(chan os.Signal, 1)
}

// Run continually re-registers the service with Consul.
// This is needed because if the Consul Client pod is restarted, it loses all
// its service registrations.
// This command expects to be run as a sidecar and to be injected by the
// mutating webhook.
func (c *Command) Run(args []string) int {
	c.once.Do(c.init)
	if err := c.flagSet.Parse(args); err != nil {
		return 1
	}

	syncPeriod, logLevel, err := c.validateFlags()
	if err != nil {
		c.UI.Error("Error: " + err.Error())
		return 1
	}

	logger := hclog.New(&hclog.LoggerOptions{
		Level:  logLevel,
		Output: os.Stderr,
	})

	// Log initial configuration
	logger.Info("Command configuration", "service-config", c.flagServiceConfig,
		"consul-binary", c.flagConsulBinary,
		"sync-period", syncPeriod,
		"log-level", logLevel)

	// Set up channel for graceful SIGINT shutdown.
	signal.Notify(c.sigCh, os.Interrupt)

	// The main work loop. We continually re-register our service every
	// syncPeriod. Consul is smart enough to know when the service hasn't changed
	// and so won't update any indices. This means we won't be causing a lot
	// of traffic within the cluster. We tolerate Consul Clients going down
	// and will simply re-register once it's back up.
	//
	// The loop will only exit when the Pod is shut down and we receive a SIGINT.
	for {
		var cmd *exec.Cmd
		if c.http.TokenFile() == "" {
			cmd = exec.Command(c.flagConsulBinary, "services", "register", c.flagServiceConfig)
		} else {
			cmd = exec.Command(c.flagConsulBinary, "services", "register", c.flagServiceConfig,
				fmt.Sprintf("-token-file=%s", c.http.TokenFile()))
		}

		// Run the command and record the stdout and stderr output
		output, err := cmd.CombinedOutput()
		if err != nil {
			logger.Error("failed to sync service", "output", output, "err", err)
		} else {
			logger.Info("successfully synced service", "output", output)
		}

		// Re-loop after syncPeriod or exit if we receive an interrupt.
		select {
		case <-time.After(syncPeriod):
			continue
		case <-c.sigCh:
			log.Info("SIGINT received, shutting down")
			return 0
		}
	}
}

// validateFlags validates the flags and returns the parsed syncPeriod and
// logLevel.
func (c *Command) validateFlags() (syncPeriod time.Duration, logLevel hclog.Level, err error) {
	if c.flagServiceConfig == "" {
		err = errors.New("-service-config must be set")
		return
	}
	if c.flagConsulBinary == "" {
		err = errors.New("-consul-binary must be set")
		return
	}
	syncPeriod, err = time.ParseDuration(c.flagSyncPeriod)
	if err != nil {
		err = fmt.Errorf("-sync-period is invalid: %s", err)
		return
	}
	_, err = os.Stat(c.flagServiceConfig)
	if os.IsNotExist(err) {
		err = fmt.Errorf("-service-config file %q not found", c.flagServiceConfig)
		return
	}
	_, err = os.Stat(c.flagConsulBinary)
	if os.IsNotExist(err) {
		err = fmt.Errorf("-consul-binary %q not found", c.flagConsulBinary)
		return
	}
	logLevel = hclog.LevelFromString(c.flagLogLevel)
	if logLevel == hclog.NoLevel {
		err = fmt.Errorf("unknown log level: %s", c.flagLogLevel)
		return
	}
	return
}

// interrupt sends os.Interrupt signal to the command
// so it can exit gracefully. This function is needed for tests
func (c *Command) interrupt() {
	c.sigCh <- os.Interrupt
}

func (c *Command) Synopsis() string { return synopsis }
func (c *Command) Help() string {
	c.once.Do(c.init)
	return c.help
}

const synopsis = "Connect lifecycle sidecar."
const help = `
Usage: consul-k8s lifecycle-sidecar [options]

  Run as a sidecar to your Connect service. Ensures that your service
  is registered with the local Consul client.

`
