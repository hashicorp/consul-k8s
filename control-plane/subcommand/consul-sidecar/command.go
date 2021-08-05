package consulsidecar

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/hashicorp/consul-k8s/control-plane/subcommand/common"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/flags"
	"github.com/hashicorp/go-hclog"
	"github.com/mitchellh/cli"
)

const metricsServerShutdownTimeout = 5 * time.Second
const envoyMetricsAddr = "http://127.0.0.1:19000/stats/prometheus"

type Command struct {
	UI cli.Ui

	http                          *flags.HTTPFlags
	flagEnableServiceRegistration bool
	flagServiceConfig             string
	flagConsulBinary              string
	flagSyncPeriod                time.Duration
	flagSet                       *flag.FlagSet
	flagLogLevel                  string
	flagLogJSON                   bool

	// Flags to configure metrics merging
	flagEnableMetricsMerging bool
	flagMergedMetricsPort    string
	flagServiceMetricsPort   string
	flagServiceMetricsPath   string

	envoyMetricsGetter   metricsGetter
	serviceMetricsGetter metricsGetter

	consulCommand []string

	logger hclog.Logger
	once   sync.Once
	help   string
	sigCh  chan os.Signal
}

// metricsGetter abstracts the function of retrieving metrics. It is used to
// enable easier unit testing.
type metricsGetter interface {
	Get(url string) (resp *http.Response, err error)
}

func (c *Command) init() {
	c.flagSet = flag.NewFlagSet("", flag.ContinueOnError)
	c.flagSet.BoolVar(&c.flagEnableServiceRegistration, "enable-service-registration", true, "Enables consul sidecar to register the service with consul every sync period. Defaults to true.")
	c.flagSet.StringVar(&c.flagServiceConfig, "service-config", "", "Path to the service config file")
	c.flagSet.StringVar(&c.flagConsulBinary, "consul-binary", "consul", "Path to a consul binary")
	c.flagSet.DurationVar(&c.flagSyncPeriod, "sync-period", 10*time.Second, "Time between syncing the service registration. Defaults to 10s.")
	c.flagSet.StringVar(&c.flagLogLevel, "log-level", "info",
		"Log verbosity level. Supported values (in order of detail) are \"trace\", "+
			"\"debug\", \"info\", \"warn\", and \"error\". Defaults to info.")
	c.flagSet.BoolVar(&c.flagLogJSON, "log-json", false,
		"Enable or disable JSON output format for logging.")

	c.flagSet.BoolVar(&c.flagEnableMetricsMerging, "enable-metrics-merging", false, "Enables consul sidecar to run a merged metrics endpoint. Defaults to false.")
	// -merged-metrics-port, -service-metrics-port, and -service-metrics-path
	// are only used if metrics merging is enabled. -merged-metrics-port and
	// -service-metrics-path have defaults, and -service-metrics-port is
	// expected to be set by the connect-inject handler to a valid value. The
	// connect-inject handler will only enable metrics merging in the consul
	// sidecar if it finds a service metrics port greater than 0.
	c.flagSet.StringVar(&c.flagMergedMetricsPort, "merged-metrics-port", "20100", "Port to serve merged Envoy and application metrics. Defaults to 20100.")
	c.flagSet.StringVar(&c.flagServiceMetricsPort, "service-metrics-port", "0", "Port where application metrics are being served. Defaults to 0.")
	c.flagSet.StringVar(&c.flagServiceMetricsPath, "service-metrics-path", "/metrics", "Path where application metrics are being served. Defaults to /metrics.")
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

	logger, err := common.Logger(c.flagLogLevel, c.flagLogJSON)
	if err != nil {
		c.UI.Error(err.Error())
		return 1
	}
	c.logger = logger

	// Log initial configuration
	c.logger.Info("Command configuration", "enable-service-registration", c.flagEnableServiceRegistration,
		"service-config", c.flagServiceConfig,
		"consul-binary", c.flagConsulBinary,
		"sync-period", c.flagSyncPeriod,
		"log-level", c.flagLogLevel,
		"enable-metrics-merging", c.flagEnableMetricsMerging,
		"merged-metrics-port", c.flagMergedMetricsPort,
		"service-metrics-port", c.flagServiceMetricsPort,
		"service-metrics-path", c.flagServiceMetricsPath,
	)

	// signalCtx that we pass in to the main work loop, signal handling is handled in another thread
	// due to the length of time it can take for the cmd to complete causing synchronization issues
	// on shutdown. Also passing a context in so that it can interrupt the cmd and exit cleanly.
	signalCtx, cancelFunc := context.WithCancel(context.Background())
	go func() {
		sig := <-c.sigCh
		c.logger.Info(fmt.Sprintf("%s received, shutting down", sig))
		cancelFunc()
	}()

	// If metrics merging is enabled, run a merged metrics server in a goroutine
	// that serves Envoy sidecar metrics and Connect service metrics. The merged
	// metrics server will be shut down when a signal is received by the main
	// for loop using shutdownMetricsServer().
	var server *http.Server
	srvExitCh := make(chan error)
	if c.flagEnableMetricsMerging {
		c.logger.Info("Metrics is enabled, creating merged metrics server.")
		server = c.createMergedMetricsServer()

		// Run the merged metrics server.
		c.logger.Info("Running merged metrics server.")
		go func() {
			if err = server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				srvExitCh <- err
			}
		}()
	}

	// The work loop for re-registering the service. We continually re-register
	// our service every syncPeriod. Consul is smart enough to know when the
	// service hasn't changed and so won't update any indices. This means we
	// won't be causing a lot of traffic within the cluster. We tolerate Consul
	// Clients going down and will simply re-register once it's back up.
	if c.flagEnableServiceRegistration {
		c.consulCommand = []string{"services", "register"}
		c.consulCommand = append(c.consulCommand, c.parseConsulFlags()...)
		c.consulCommand = append(c.consulCommand, c.flagServiceConfig)

		go func() {
			for {
				start := time.Now()
				cmd := exec.CommandContext(signalCtx, c.flagConsulBinary, c.consulCommand...)

				// Run the command and record the stdout and stderr output.
				output, err := cmd.CombinedOutput()
				if err != nil {
					c.logger.Error("failed to sync service", "output", strings.TrimSpace(string(output)), "err", err, "duration", time.Since(start))
				} else {
					c.logger.Info("successfully synced service", "output", strings.TrimSpace(string(output)), "duration", time.Since(start))
				}
				select {
				// Re-loop after syncPeriod or exit if we receive interrupt or terminate signals.
				case <-time.After(c.flagSyncPeriod):
					continue
				case <-signalCtx.Done():
					return
				}
			}
		}()
	}

	// Block and wait for a signal or for the metrics server to exit.
	select {
	case <-signalCtx.Done():
		// After the signal is received, wait for the merged metrics server
		// to gracefully shutdown as well if it has been enabled. This can
		// take up to metricsServerShutdownTimeout seconds.
		if c.flagEnableMetricsMerging {
			c.logger.Info("Attempting to shut down metrics server.")
			c.shutdownMetricsServer(server)
		}
		return 0
	case err := <-srvExitCh:
		c.logger.Error(fmt.Sprintf("Metrics server error: %v", err))
		return 1
	}

}

// shutdownMetricsServer handles gracefully shutting down the server. This will
// call server.Shutdown(), which will indefinitely wait for connections to turn
// idle. To avoid potentially waiting forever, we pass a context to
// server.Shutdown() that will timeout in metricsServerShutdownTimeout (5) seconds.
func (c *Command) shutdownMetricsServer(server *http.Server) {
	// The shutdownCancelFunc will be unused since it is unnecessary to call it as we
	// are already about to call shutdown with a timeout. We'd only need to
	// shutdownCancelFunc if we needed to trigger something to happen when the
	// shutdownCancelFunc is called, which we do not. The reason for not
	// discarding it with _ is for the go vet check.
	shutdownCtx, shutdownCancelFunc := context.WithTimeout(context.Background(), metricsServerShutdownTimeout)
	defer shutdownCancelFunc()

	c.logger.Info("Merged metrics server exists, attempting to gracefully shut down server")
	if err := server.Shutdown(shutdownCtx); err != nil {
		c.logger.Error(fmt.Sprintf("Server shutdown failed: %s", err))
		return
	}
	c.logger.Info("Server has been shut down")
}

// createMergedMetricsServer sets up the merged metrics server.
func (c *Command) createMergedMetricsServer() *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/stats/prometheus", c.mergedMetricsHandler)

	mergedMetricsServerAddr := fmt.Sprintf("127.0.0.1:%s", c.flagMergedMetricsPort)
	server := &http.Server{Addr: mergedMetricsServerAddr, Handler: mux}

	// The default http.Client timeout is indefinite, so adding a timeout makes
	// sure that requests don't hang.
	client := &http.Client{
		Timeout: time.Second * 10,
	}
	// http.Client satisfies the metricsGetter interface.
	c.envoyMetricsGetter = client
	c.serviceMetricsGetter = client

	return server
}

// mergedMetricsHandler has the logic to append both Envoy and service metrics
// together, logging if it's unsuccessful at either.
func (c *Command) mergedMetricsHandler(rw http.ResponseWriter, _ *http.Request) {

	envoyMetrics, err := c.envoyMetricsGetter.Get(envoyMetricsAddr)
	if err != nil {
		// If there is an error scraping Envoy, we want the handler to return
		// without writing anything to the response, and log the error.
		c.logger.Error(fmt.Sprintf("Error scraping Envoy proxy metrics: %s", err.Error()))
		return
	}

	// Write Envoy metrics to the response.
	defer func() {
		err = envoyMetrics.Body.Close()
		if err != nil {
			c.logger.Error(fmt.Sprintf("Error closing envoy metrics body: %s", err.Error()))
		}
	}()
	envoyMetricsBody, err := ioutil.ReadAll(envoyMetrics.Body)
	if err != nil {
		c.logger.Error(fmt.Sprintf("Couldn't read Envoy proxy metrics: %s", err.Error()))
		return
	}
	_, err = rw.Write(envoyMetricsBody)
	if err != nil {
		c.logger.Error(fmt.Sprintf("Error writing envoy metrics body: %s", err.Error()))
	}

	serviceMetricsAddr := fmt.Sprintf("http://127.0.0.1:%s%s", c.flagServiceMetricsPort, c.flagServiceMetricsPath)
	serviceMetrics, err := c.serviceMetricsGetter.Get(serviceMetricsAddr)
	if err != nil {
		c.logger.Warn(fmt.Sprintf("Error scraping service metrics: %s", err.Error()))
		// Since we've already written the Envoy metrics to the response, we can
		// return at this point if we were unable to get service metrics.
		return
	}

	// Since serviceMetrics will be non-nil if there are no errors, write the
	// service metrics to the response as well.
	defer func() {
		err = serviceMetrics.Body.Close()
		if err != nil {
			c.logger.Error(fmt.Sprintf("Error closing service metrics body: %s", err.Error()))
		}
	}()
	serviceMetricsBody, err := ioutil.ReadAll(serviceMetrics.Body)
	if err != nil {
		c.logger.Error(fmt.Sprintf("Couldn't read service metrics: %s", err.Error()))
		return
	}
	_, err = rw.Write(serviceMetricsBody)
	if err != nil {
		c.logger.Error(fmt.Sprintf("Error writing service metrics body: %s", err.Error()))
	}
}

// validateFlags validates the flags.
func (c *Command) validateFlags() error {
	if !c.flagEnableServiceRegistration && !c.flagEnableMetricsMerging {
		return errors.New("at least one of -enable-service-registration or -enable-metrics-merging must be true")
	}
	if c.flagEnableServiceRegistration {
		if c.flagSyncPeriod == 0 {
			// if sync period is 0, then the select loop will
			// always pick the first case, and it'll be impossible
			// to terminate the command gracefully with SIGINT.
			return errors.New("-sync-period must be greater than 0")
		}
		if c.flagServiceConfig == "" {
			return errors.New("-service-config must be set")
		}
		if c.flagConsulBinary == "" {
			return errors.New("-consul-binary must be set")
		}
		_, err := os.Stat(c.flagServiceConfig)
		if os.IsNotExist(err) {
			return fmt.Errorf("-service-config file %q not found", c.flagServiceConfig)
		}
		_, err = exec.LookPath(c.flagConsulBinary)
		if err != nil {
			return fmt.Errorf("-consul-binary %q not found: %s", c.flagConsulBinary, err)
		}
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
Usage: consul-k8s-control-plane consul-sidecar [options]

  Run as a sidecar to your Connect service. Ensures that your service
  is registered with the local Consul client.

`
