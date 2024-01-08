package subcommand

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/mitchellh/cli"

	"github.com/hashicorp/consul-k8s/subcommand/common"
	"github.com/hashicorp/consul-k8s/subcommand/flags"
)

type Command struct {
	UI cli.Ui

	http              *flags.HTTPFlags
	flagServiceConfig string
	flagConsulBinary  string
	flagSyncPeriod    time.Duration
	flagSet           *flag.FlagSet
	flagLogLevel      string

	consulCommand []string

	once  sync.Once
	help  string
	sigCh chan os.Signal
}

func (c *Command) init() {
	c.flagSet = flag.NewFlagSet("", flag.ContinueOnError)
	c.flagSet.StringVar(&c.flagServiceConfig, "service-config", "", "Path to the service config file")
	c.flagSet.StringVar(&c.flagConsulBinary, "consul-binary", "consul", "Path to a consul binary")
	c.flagSet.DurationVar(&c.flagSyncPeriod, "sync-period", 10*time.Second, "Time between syncing the service registration. Defaults to 10s.")
	c.flagSet.StringVar(&c.flagLogLevel, "log-level", "info",
		"Log verbosity level. Supported values (in order of detail) are \"trace\", "+
			"\"debug\", \"info\", \"warn\", and \"error\". Defaults to info.")

	c.help = flags.Usage(help, c.flagSet)
	c.http = &flags.HTTPFlags{}
	flags.Merge(c.flagSet, c.http.Flags())
	c.help = flags.Usage(help, c.flagSet)

	// Wait on an interrupt or terminate to exit. This channel must be initialized before
	// Run() is called so that there are no race conditions where the channel
	// is not defined.
	if c.sigCh == nil {
		c.sigCh = make(chan os.Signal, 1)
		signal.Notify(c.sigCh, syscall.SIGINT, syscall.SIGTERM)
	}
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

	err := c.validateFlags()
	if err != nil {
		c.UI.Error("Error: " + err.Error())
		return 1
	}

	logger, err := common.Logger(c.flagLogLevel)
	if err != nil {
		c.UI.Error(err.Error())
		return 1
	}

	// Log initial configuration
	logger.Info("Command configuration", "service-config", c.flagServiceConfig,
		"consul-binary", c.flagConsulBinary,
		"sync-period", c.flagSyncPeriod,
		"log-level", c.flagLogLevel)

	c.consulCommand = []string{"services", "register"}
	c.consulCommand = append(c.consulCommand, c.parseConsulFlags()...)
	c.consulCommand = append(c.consulCommand, c.flagServiceConfig)

	// ctx that we pass in to the main work loop, signal handling is handled in another thread
	// due to the length of time it can take for the cmd to complete causing synchronization issues
	// on shutdown. Also passing a context in so that it can interrupt the cmd and exit cleanly.
	ctx, cancelFunc := context.WithCancel(context.Background())
	go func() {
		select {
		case sig := <-c.sigCh:
			logger.Info(fmt.Sprintf("%s received, shutting down", sig))
			cancelFunc()
			return
		}
	}()
	// The main work loop. We continually re-register our service every
	// syncPeriod. Consul is smart enough to know when the service hasn't changed
	// and so won't update any indices. This means we won't be causing a lot
	// of traffic within the cluster. We tolerate Consul Clients going down
	// and will simply re-register once it's back up.
	//
	// The loop will only exit when the Pod is shut down and we receive a SIGINT.
	for {
		start := time.Now()
		cmd := exec.CommandContext(ctx, c.flagConsulBinary, c.consulCommand...)

		// Run the command and record the stdout and stderr output
		output, err := cmd.CombinedOutput()
		if err != nil {
			logger.Error("failed to sync service", "output", strings.TrimSpace(string(output)), "err", err, "duration", time.Since(start))
		} else {
			logger.Info("successfully synced service", "output", strings.TrimSpace(string(output)), "duration", time.Since(start))
		}
		select {
		// Re-loop after syncPeriod or exit if we receive interrupt or terminate signals.
		case <-time.After(c.flagSyncPeriod):
			continue
		case <-ctx.Done():
			return 0
		}
	}
}

// validateFlags validates the flags.
func (c *Command) validateFlags() error {
	if c.flagServiceConfig == "" {
		return errors.New("-service-config must be set")
	}
	if c.flagConsulBinary == "" {
		return errors.New("-consul-binary must be set")
	}
	if c.flagSyncPeriod == 0 {
		// if sync period is 0, then the select loop will
		// always pick the first case, and it'll be impossible
		// to terminate the command gracefully with SIGINT.
		return errors.New("-sync-period must be greater than 0")
	}

	_, err := os.Stat(c.flagServiceConfig)
	if os.IsNotExist(err) {
		err = fmt.Errorf("-service-config file %q not found", c.flagServiceConfig)
		return fmt.Errorf("-service-config file %q not found", c.flagServiceConfig)
	}
	_, err = exec.LookPath(c.flagConsulBinary)
	if err != nil {
		return fmt.Errorf("-consul-binary %q not found: %s", c.flagConsulBinary, err)
	}

	return nil
}

// parseConsulFlags creates Consul client command flags
// from command's HTTP flags and returns them as an array of strings.
func (c *Command) parseConsulFlags() []string {
	var consulCommandFlags []string
	c.http.Flags().VisitAll(func(f *flag.Flag) {
		if f.Value.String() != "" {
			consulCommandFlags = append(consulCommandFlags, fmt.Sprintf("-%s=%s", f.Name, f.Value.String()))
		}
	})
	return consulCommandFlags
}

// interrupt sends os.Interrupt signal to the command
// so it can exit gracefully. This function is needed for tests
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

const synopsis = "Consul sidecar for Connect."
const help = `
Usage: consul-k8s consul-sidecar [options]

  Run as a sidecar to your Connect service. Ensures that your service
  is registered with the local Consul client.

`
