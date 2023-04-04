// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package apigateway

import (
	"context"
	"flag"
	"io"
	"sync"
	"time"

	"github.com/hashicorp/consul-k8s/control-plane/subcommand/common"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/flags"
	"github.com/mitchellh/cli"
)

const (
	defaultGRPCPort      = 8502
	defaultSDSServerHost = "consul-api-gateway-controller.default.svc.cluster.local"
	defaultSDSServerPort = 9090
	// The amount of time to wait for the first cert write
	defaultCertWaitTime = 1 * time.Minute
)

type Command struct {
	UI    cli.Ui
	flags *flag.FlagSet

	isTest bool

	flagServerCAFile            string // CA File for CA for Consul server
	flagServerCASecret          string // CA Secret for Consul server
	flagServerCASecretNamespace string // CA Secret namespace for Consul server

	flagConsulAddress string // Consul server address

	flagPrimaryDatacenter string // Primary datacenter, may or may not be the datacenter this controller is running in

	flagSDSServerHost string // SDS server host
	flagSDSServerPort int    // SDS server port
	flagMetricsPort   int    // Port for prometheus metrics
	flagPprofPort     int    // Port for pprof profiling
	flagK8sContext    string // context to use
	flagK8sNamespace  string // namespace we're run in

	// Consul namespaces
	flagConsulDestinationNamespace string
	flagMirrorK8SNamespaces        bool
	flagMirrorK8SNamespacePrefix   string

	// Logging
	flagLogLevel string
	flagLogJSON  bool

	once   sync.Once
	output io.Writer
	help   string
	ctx    context.Context
}

// New returns a new server command
func New(ctx context.Context, ui cli.Ui, logOutput io.Writer) *Command {
	return &Command{UI: ui, output: logOutput, ctx: ctx}
}

func (c *Command) init() {
	c.flags = flag.NewFlagSet("", flag.ContinueOnError)

	c.flags.StringVar(&c.flagServerCAFile, "ca-file", "", "Path to CA for Consul server.")
	c.flags.StringVar(&c.flagServerCASecret, "ca-secret", "", "CA Secret for Consul server.")
	c.flags.StringVar(&c.flagServerCASecretNamespace, "ca-secret-namespace", "default", "CA Secret namespace for Consul server.")
	c.flags.StringVar(&c.flagConsulAddress, "consul-address", "", "Consul Address.")
	c.flags.StringVar(&c.flagPrimaryDatacenter, "primary-datacenter", "", "Name of the primary Consul datacenter")
	c.flags.StringVar(&c.flagSDSServerHost, "sds-server-host", defaultSDSServerHost, "SDS Server Host.")
	c.flags.StringVar(&c.flagK8sContext, "k8s-context", "", "Kubernetes context to use.")
	c.flags.StringVar(&c.flagK8sNamespace, "k8s-namespace", "", "Kubernetes namespace to use.")
	c.flags.IntVar(&c.flagSDSServerPort, "sds-server-port", defaultSDSServerPort, "SDS Server Port.")
	c.flags.IntVar(&c.flagMetricsPort, "metrics-port", 0, "Metrics port, if not set, metrics are not enabled.")
	c.flags.IntVar(&c.flagPprofPort, "pprof-port", 0, "Go pprof port, if not set, profiling is not enabled.")

	{
		// Consul namespaces
		c.flags.StringVar(&c.flagConsulDestinationNamespace, "consul-destination-namespace", "", "Consul namespace to register gateway services.")
		c.flags.BoolVar(&c.flagMirrorK8SNamespaces, "mirroring-k8s", false, "Register Consul gateway services based on Kubernetes namespace.")
		c.flags.StringVar(&c.flagMirrorK8SNamespacePrefix, "mirroring-k8s-prefix", "", "Namespace prefix for Consul services when mirroring Kubernetes namespaces.")
	}

	{
		// Logging
		c.flags.StringVar(&c.flagLogLevel, "log-level", "info",
			"Log verbosity level. Supported values (in order of detail) are \"trace\", "+
				"\"debug\", \"info\", \"warn\", and \"error\".")
		c.flags.BoolVar(&c.flagLogJSON, "log-json", false,
			"Enable or disable JSON output format for logging.")
	}

	c.help = flags.Usage(help, c.flags)
}

// Run creates a Kubernetes secret with data needed by secondary datacenters
// in order to federate with the primary. It's assumed this is running in the
// primary datacenter.
func (c *Command) Run(args []string) int {
	c.once.Do(c.init)

	if err := c.validateFlags(args); err != nil {
		c.UI.Error(err.Error())
		return 1
	}

	logger, err := common.Logger(c.flagLogLevel, c.flagLogJSON)
	if err != nil {
		c.UI.Error(err.Error())
		return 1
	}

	if c.ctx == nil {
		c.ctx = context.Background()
	}

	logger.Info("Welcome to API Gateway")
	return 0
}

func (c *Command) validateFlags(args []string) error {
	if err := c.flags.Parse(args); err != nil {
		return err
	}
	return nil
}

func (c *Command) Synopsis() string { return synopsis }

func (c *Command) Help() string {
	c.once.Do(c.init)
	return c.help
}

const synopsis = "Starts the api-gateway control plane server"
const help = `
Usage: consul-k8s-control-plane api-gateway [options]
`
