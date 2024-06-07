// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package meshinit

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/hashicorp/consul-server-connection-manager/discovery"
	"github.com/hashicorp/consul/proto-public/pbdataplane"
	pbmesh "github.com/hashicorp/consul/proto-public/pbmesh/v2beta1"
	"github.com/hashicorp/consul/sdk/iptables"
	"github.com/hashicorp/go-hclog"
	"github.com/mitchellh/cli"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/common"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/flags"
	"github.com/hashicorp/consul-k8s/version"
)

const (
	// The number of times to attempt to read this proxy registration (120s).
	defaultMaxPollingRetries = 120
	defaultProxyIDFile       = "/consul/mesh-inject/proxyid"
)

type Command struct {
	UI cli.Ui

	flagProxyName string

	maxPollingAttempts uint64 // Number of times to poll Consul for proxy registrations.

	flagRedirectTrafficConfig string
	flagLogLevel              string
	flagLogJSON               bool

	flagSet *flag.FlagSet
	consul  *flags.ConsulFlags

	once   sync.Once
	help   string
	logger hclog.Logger

	watcher *discovery.Watcher

	// Only used in tests.
	iptablesProvider iptables.Provider
	iptablesConfig   iptables.Config
}

func (c *Command) init() {
	c.flagSet = flag.NewFlagSet("", flag.ContinueOnError)

	// V2 Flags
	c.flagSet.StringVar(&c.flagProxyName, "proxy-name", os.Getenv("PROXY_NAME"), "The Consul proxy name. This is the K8s Pod name, which is also the name of the Workload in Consul. (Required)")

	// Universal flags
	c.flagSet.StringVar(&c.flagRedirectTrafficConfig, "redirect-traffic-config", os.Getenv("CONSUL_REDIRECT_TRAFFIC_CONFIG"), "Config (in JSON format) to configure iptables for this pod.")
	c.flagSet.StringVar(&c.flagLogLevel, "log-level", "info",
		"Log verbosity level. Supported values (in order of detail) are \"trace\", "+
			"\"debug\", \"info\", \"warn\", and \"error\".")

	c.flagSet.BoolVar(&c.flagLogJSON, "log-json", false,
		"Enable or disable JSON output format for logging.")

	if c.maxPollingAttempts == 0 {
		c.maxPollingAttempts = defaultMaxPollingRetries
	}

	c.consul = &flags.ConsulFlags{}
	flags.Merge(c.flagSet, c.consul.Flags())
	c.help = flags.Usage(help, c.flagSet)
}

func (c *Command) Run(args []string) int {
	c.once.Do(c.init)

	if err := c.flagSet.Parse(args); err != nil {
		return 1
	}
	// Validate flags
	if err := c.validateFlags(); err != nil {
		c.UI.Error(err.Error())
		return 1
	}

	if c.consul.Namespace == "" {
		c.consul.Namespace = constants.DefaultConsulNS
	}
	if c.consul.Partition == "" {
		c.consul.Partition = constants.DefaultConsulPartition
	}

	// Set up logging.
	if c.logger == nil {
		var err error
		c.logger, err = common.Logger(c.flagLogLevel, c.flagLogJSON)
		if err != nil {
			c.UI.Error(err.Error())
			return 1
		}
	}

	// Create Consul API config object.
	consulConfig := c.consul.ConsulClientConfig()

	// Create a context to be used by the processes started in this command.
	ctx, cancelFunc := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancelFunc()

	// Start Consul server Connection manager.
	serverConnMgrCfg, err := c.consul.ConsulServerConnMgrConfig()
	// Disable server watch because we only need to get server IPs once.
	serverConnMgrCfg.ServerWatchDisabled = true
	if err != nil {
		c.UI.Error(fmt.Sprintf("unable to create config for consul-server-connection-manager: %s", err))
		return 1
	}
	if c.watcher == nil {
		c.watcher, err = discovery.NewWatcher(ctx, serverConnMgrCfg, c.logger.Named("consul-server-connection-manager"))
		if err != nil {
			c.UI.Error(fmt.Sprintf("unable to create Consul server watcher: %s", err))
			return 1
		}
		go c.watcher.Run() // The actual ACL login happens here
		defer c.watcher.Stop()
	}

	state, err := c.watcher.State()
	if err != nil {
		c.logger.Error("Unable to get state from consul-server-connection-manager", "error", err)
		return 1
	}

	consulClient, err := consul.NewClientFromConnMgrState(consulConfig, state)
	if err != nil {
		c.logger.Error("Unable to get client connection", "error", err)
		return 1
	}

	if version.IsFIPS() {
		// make sure we are also using FIPS Consul
		var versionInfo map[string]interface{}
		_, err := consulClient.Raw().Query("/v1/agent/version", versionInfo, nil)
		if err != nil {
			c.logger.Warn("This is a FIPS build of consul-k8s, which should be used with FIPS Consul. Unable to verify FIPS Consul while setting up Consul API client.")
		}
		if val, ok := versionInfo["FIPS"]; !ok || val == "" {
			c.logger.Warn("This is a FIPS build of consul-k8s, which should be used with FIPS Consul. A non-FIPS version of Consul was detected.")
		}
	}

	// todo (agentless): this should eventually be passed to consul-dataplane as a string so we don't need to write it to file.
	if c.consul.UseTLS && c.consul.CACertPEM != "" {
		if err = common.WriteFileWithPerms(constants.ConsulCAFile, c.consul.CACertPEM, 0444); err != nil {
			c.logger.Error("error writing CA cert file", "error", err)
			return 1
		}
	}

	dc, err := consul.NewDataplaneServiceClient(c.watcher)
	if err != nil {
		c.logger.Error("failed to create resource client", "error", err)
		return 1
	}

	var bootstrapConfig pbmesh.BootstrapConfig
	if err := backoff.Retry(c.getBootstrapParams(dc, &bootstrapConfig), backoff.WithMaxRetries(backoff.NewConstantBackOff(1*time.Second), c.maxPollingAttempts)); err != nil {
		c.logger.Error("Timed out waiting for bootstrap parameters", "error", err)
		return 1
	}

	if c.flagRedirectTrafficConfig != "" {
		c.watcher.Stop()                                        // Explicitly stop the watcher so that ACLs are cleaned up before we apply re-direction.
		err := c.applyTrafficRedirectionRules(&bootstrapConfig) // BootstrapConfig is always populated non-nil from the RPC
		if err != nil {
			c.logger.Error("error applying traffic redirection rules", "err", err)
			return 1
		}
	}

	c.logger.Info("Proxy initialization completed")
	return 0
}

func (c *Command) validateFlags() error {
	if c.flagProxyName == "" {
		return errors.New("-proxy-name must be set")
	}
	return nil
}

func (c *Command) Synopsis() string { return synopsis }
func (c *Command) Help() string {
	c.once.Do(c.init)
	return c.help
}

func (c *Command) getBootstrapParams(
	client pbdataplane.DataplaneServiceClient,
	bootstrapConfig *pbmesh.BootstrapConfig,
) backoff.Operation {
	return func() error {
		req := &pbdataplane.GetEnvoyBootstrapParamsRequest{
			ProxyId:   c.flagProxyName,
			Namespace: c.consul.Namespace,
			Partition: c.consul.Partition,
		}
		res, err := client.GetEnvoyBootstrapParams(context.Background(), req)
		if err != nil {
			c.logger.Error("Unable to get bootstrap parameters", "error", err)
			return err
		}
		if res.GetBootstrapConfig() != nil {
			*bootstrapConfig = *res.GetBootstrapConfig()
		}
		return nil
	}
}

// This below implementation is loosely based on
// https://github.com/hashicorp/consul/blob/fe2d41ddad9ba2b8ff86cbdebbd8f05855b1523c/command/connect/redirecttraffic/redirect_traffic.go#L136.

func (c *Command) applyTrafficRedirectionRules(config *pbmesh.BootstrapConfig) error {
	err := json.Unmarshal([]byte(c.flagRedirectTrafficConfig), &c.iptablesConfig)
	if err != nil {
		return err
	}
	if c.iptablesProvider != nil {
		c.iptablesConfig.IptablesProvider = c.iptablesProvider
	}

	// TODO: provide dynamic updates to the c.iptablesConfig.ProxyOutboundPort
	// We currently don't have a V2 endpoint that can gather the fully synthesized ProxyConfiguration.
	// We need this to dynamically set c.iptablesConfig.ProxyOutboundPort with the outbound port configuration from
	// pbmesh.DynamicConfiguration.TransparentProxy.OutboundListenerPort.
	// We would either need to grab another resource that has this information rendered in it, or add
	// pbmesh.DynamicConfiguration to the GetBootstrapParameters rpc.
	// Right now this is an edge case because the mesh webhook configured the flagRedirectTrafficConfig with the default
	// 15001 port.

	// TODO: provide dyanmic updates to the c.iptablesConfig.ProxyInboundPort
	// This is the `mesh` port in the workload resource.
	// Right now this will always be the default port (20000)

	if config.StatsBindAddr != "" {
		_, port, err := net.SplitHostPort(config.StatsBindAddr)
		if err != nil {
			return fmt.Errorf("failed parsing host and port from StatsBindAddr: %s", err)
		}

		c.iptablesConfig.ExcludeInboundPorts = append(c.iptablesConfig.ExcludeInboundPorts, port)
	}

	// Configure any relevant information from the proxy service
	err = iptables.Setup(c.iptablesConfig)
	if err != nil {
		return err
	}
	c.logger.Info("Successfully applied traffic redirection rules")
	return nil
}

const (
	synopsis = "Inject mesh init command."
	help     = `
Usage: consul-k8s-control-plane mesh-init [options]

  Bootstraps mesh-injected pod components.
  Uses V2 Consul Catalog APIs.
  Not intended for stand-alone use.
`
)
