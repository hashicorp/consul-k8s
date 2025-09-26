// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package connectinit

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
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/iptables"
	"github.com/hashicorp/go-hclog"
	"github.com/mitchellh/cli"
	"github.com/mitchellh/mapstructure"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/namespaces"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/common"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/flags"
	"github.com/hashicorp/consul-k8s/version"
)

const (
	defaultProxyIDFile = "/consul/connect-inject/proxyid"

	// The number of times to attempt to read this service (120s).
	defaultServicePollingRetries = 120
)

type Command struct {
	UI cli.Ui

	flagConsulNodeName        string
	flagPodName               string // Pod name.
	flagPodNamespace          string // Pod namespace.
	flagServiceAccountName    string // Service account name.
	flagServiceName           string // Service name.
	flagGatewayKind           string
	flagRedirectTrafficConfig string
	flagLogLevel              string
	flagLogJSON               bool

	flagProxyIDFile string // Location to write the output proxyID. Default is defaultProxyIDFile.
	flagMultiPort   bool

	serviceRegistrationPollingAttempts uint64 // Number of times to poll for this service to be registered.

	flagSet *flag.FlagSet
	consul  *flags.ConsulFlags

	once   sync.Once
	help   string
	logger hclog.Logger

	watcher *discovery.Watcher

	nonRetryableError error

	// Only used in tests.
	iptablesProvider iptables.Provider
	iptablesConfig   iptables.Config
}

func (c *Command) init() {
	c.flagSet = flag.NewFlagSet("", flag.ContinueOnError)
	c.flagSet.StringVar(&c.flagPodName, "pod-name", "", "Name of the pod.")
	c.flagSet.StringVar(&c.flagConsulNodeName, "consul-node-name", os.Getenv("CONSUL_NODE_NAME"), "Name of the Consul node where services are registered.")
	c.flagSet.StringVar(&c.flagPodNamespace, "pod-namespace", "", "Name of the pod namespace.")
	c.flagSet.StringVar(&c.flagServiceAccountName, "service-account-name", "", "Service account name on the pod.")
	c.flagSet.StringVar(&c.flagServiceName, "service-name", "", "Service name as specified via the pod annotation.")
	c.flagSet.StringVar(&c.flagProxyIDFile, "proxy-id-file", defaultProxyIDFile, "File name where proxy's Consul service ID should be saved.")
	c.flagSet.BoolVar(&c.flagMultiPort, "multiport", false, "If the pod is a multi port pod.")
	c.flagSet.StringVar(&c.flagGatewayKind, "gateway-kind", "", "Kind of gateway that is being registered: ingress-gateway, terminating-gateway, or mesh-gateway.")
	c.flagSet.StringVar(&c.flagRedirectTrafficConfig, "redirect-traffic-config", os.Getenv("CONSUL_REDIRECT_TRAFFIC_CONFIG"), "Config (in JSON format) to configure iptables for this pod.")
	c.flagSet.StringVar(&c.flagLogLevel, "log-level", "info",
		"Log verbosity level. Supported values (in order of detail) are \"trace\", "+
			"\"debug\", \"info\", \"warn\", and \"error\".")
	c.flagSet.BoolVar(&c.flagLogJSON, "log-json", false,
		"Enable or disable JSON output format for logging.")

	if c.serviceRegistrationPollingAttempts == 0 {
		c.serviceRegistrationPollingAttempts = defaultServicePollingRetries
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
		go c.watcher.Run()
		defer c.watcher.Stop()
	}

	state, err := c.watcher.State()
	if err != nil {
		c.logger.Error("Unable to get state from consul-server-connection-manager", "error", err)
		return 1
	}

	consulClient, err := consul.NewClientFromConnMgrState(consulConfig, state)
	if err != nil {
		if c.flagServiceAccountName == "default" {
			c.logger.Warn("The service account name for this Pod is \"default\"." +
				" In default installations this is not a supported service account name." +
				" The service account name must match the name of the Kubernetes Service" +
				" or the consul.hashicorp.com/connect-service annotation.")
		}
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
	proxyService := &api.AgentService{}
	if c.flagGatewayKind != "" {
		err = backoff.Retry(c.getGatewayRegistration(consulClient), backoff.WithMaxRetries(backoff.NewConstantBackOff(1*time.Second), c.serviceRegistrationPollingAttempts))
		if err != nil {
			c.logger.Error("Timed out waiting for gateway registration", "error", err)
			return 1
		}
		if c.nonRetryableError != nil {
			c.logger.Error("Error processing gateway registration", "error", c.nonRetryableError)
			return 1
		}
	} else {
		var err = backoff.Retry(c.getConnectServiceRegistrations(consulClient, proxyService), backoff.WithMaxRetries(backoff.NewConstantBackOff(1*time.Second), c.serviceRegistrationPollingAttempts))
		if err != nil {
			c.logger.Error("Timed out waiting for service registration", "error", err)
			return 1
		}
		if c.nonRetryableError != nil {
			c.logger.Error("Error processing service registration", "error", c.nonRetryableError)
			return 1
		}
	}

	// todo (agentless): this should eventually be passed to consul-dataplane as a string so we don't need to write it to file.
	if c.consul.UseTLS && c.consul.CACertPEM != "" {
		if err = common.WriteFileWithPerms(constants.LegacyConsulCAFile, c.consul.CACertPEM, 0444); err != nil {
			c.logger.Error("error writing CA cert file", "error", err)
			return 1
		}
	}

	if c.flagRedirectTrafficConfig != "" {
		dualStack := false
		if os.Getenv(constants.ConsulDualStackEnvVar) == "true" {
			dualStack = true
		}
		c.watcher.Stop() // Explicitly stop the watcher so that ACLs are cleaned up before we apply re-direction.
		err = c.applyTrafficRedirectionRules(proxyService, dualStack)
		if err != nil {
			c.logger.Error("error applying traffic redirection rules", "err", err)
			return 1
		}
	}

	c.logger.Info("Connect initialization completed")
	return 0
}

func (c *Command) getConnectServiceRegistrations(consulClient *api.Client, proxyService *api.AgentService) backoff.Operation {
	var proxyID string
	registrationRetryCount := 0
	return func() error {
		registrationRetryCount++
		filter := fmt.Sprintf("Meta[%q] == %q and Meta[%q] == %q ",
			constants.MetaKeyPodName, c.flagPodName, constants.MetaKeyKubeNS, c.flagPodNamespace)
		if c.flagMultiPort && c.flagServiceName != "" {
			// If the service name is set and this is a multi-port pod there may be multiple services registered for
			// this one Pod. If so, we want to ensure the service and proxy matching our expected name is registered.
			filter += fmt.Sprintf(` and (Service == %q or Service == "%s-sidecar-proxy")`, c.flagServiceName, c.flagServiceName)
		}
		serviceList, _, err := consulClient.Catalog().NodeServiceList(c.flagConsulNodeName,
			&api.QueryOptions{Filter: filter, MergeCentralConfig: true})
		if err != nil {
			c.logger.Error("Unable to get services", "error", err)
			return err
		}
		// Wait for the service and the connect-proxy service to be registered.
		if len(serviceList.Services) != 2 {
			c.logger.Info("Unable to find registered services; retrying")
			// Once every 10 times we're going to print this informational message to the pod logs so that
			// it is not "lost" to the user at the end of the retries when the pod enters a CrashLoop.
			if registrationRetryCount%10 == 0 {
				c.logger.Info("Check to ensure a Kubernetes service has been created for this application." +
					" If your pod is not starting also check the connect-inject deployment logs.")
			}
			if len(serviceList.Services) > 2 {
				c.logger.Error("There are multiple Consul services registered for this pod when there must only be one." +
					" Check if there are multiple Kubernetes services selecting this pod and add the label" +
					" `consul.hashicorp.com/service-ignore: \"true\"` to all services except the one used by Consul for handling requests.")
			}

			return fmt.Errorf("did not find correct number of services, found: %d, services: %+v", len(serviceList.Services), serviceList)
		}
		for _, svc := range serviceList.Services {
			c.logger.Info("Registered service has been detected", "service", svc.Service)
			if c.consul.ConsulLogin.AuthMethod != "" {
				if c.flagServiceName != "" && c.flagServiceAccountName != c.flagServiceName {
					// Save an error but return nil so that we don't retry this step.
					c.nonRetryableError = fmt.Errorf("service account name %s doesn't match annotation service name %s", c.flagServiceAccountName, c.flagServiceName)
					return nil
				}

				if c.flagServiceName == "" && svc.Kind != api.ServiceKindConnectProxy && c.flagServiceAccountName != svc.Service {
					// Save an error but return nil so that we don't retry this step.
					c.nonRetryableError = fmt.Errorf("service account name %s doesn't match Consul service name %s", c.flagServiceAccountName, svc.Service)
					return nil
				}
			}
			if svc.Kind == api.ServiceKindConnectProxy {
				// This is the proxy service ID.
				proxyID = svc.ID
				*proxyService = *svc
			}
		}

		if proxyID == "" {
			// In theory we can't reach this point unless we have 2 services registered against
			// this pod and neither are the connect-proxy. We don't support this case anyway, but it
			// is necessary to return from the function.
			c.logger.Error("Unable to write proxy ID to file", "error", err)
			return fmt.Errorf("unable to find registered connect-proxy service")
		}

		// Write the proxy ID to the shared volume so `consul connect envoy` can use it for bootstrapping.
		if err = common.WriteFileWithPerms(c.flagProxyIDFile, proxyID, os.FileMode(0444)); err != nil {
			// Save an error but return nil so that we don't retry this step.
			c.nonRetryableError = err
			return nil
		}

		return nil
	}
}

func (c *Command) getGatewayRegistration(client *api.Client) backoff.Operation {
	var proxyID string
	registrationRetryCount := 0
	return func() error {
		registrationRetryCount++
		var gatewayList *api.CatalogNodeServiceList
		var err error
		filter := fmt.Sprintf("Meta[%q] == %q and Meta[%q] == %q ",
			constants.MetaKeyPodName, c.flagPodName, constants.MetaKeyKubeNS, c.flagPodNamespace)
		if c.consul.Namespace != "" {
			gatewayList, _, err = client.Catalog().NodeServiceList(c.flagConsulNodeName, &api.QueryOptions{Filter: filter, Namespace: namespaces.WildcardNamespace})
		} else {
			gatewayList, _, err = client.Catalog().NodeServiceList(c.flagConsulNodeName, &api.QueryOptions{Filter: filter})
		}
		if err != nil {
			c.logger.Error("Unable to get gateway", "error", err)
			return err
		}
		// Wait for the service and the connect-proxy service to be registered.
		if len(gatewayList.Services) != 1 {
			c.logger.Info("Unable to find registered gateway; retrying")
			// Once every 10 times we're going to print this informational message to the pod logs so that
			// it is not "lost" to the user at the end of the retries when the pod enters a CrashLoop.
			if registrationRetryCount%10 == 0 {
				c.logger.Info("Check to ensure a Kubernetes service has been created for this application." +
					" If your pod is not starting also check the connect-inject deployment logs.")
			}
			if len(gatewayList.Services) > 1 {
				c.logger.Error("There are multiple Consul gateway services registered for this pod when there must only be one." +
					" Check if there are multiple Kubernetes services selecting this gateway pod and add the label" +
					" `consul.hashicorp.com/service-ignore: \"true\"` to all services except the one used by Consul for handling requests.")
			}
			return fmt.Errorf("did not find correct number of gateways, found: %d, services: %+v", len(gatewayList.Services), gatewayList)
		}
		for _, gateway := range gatewayList.Services {
			switch gateway.Kind {
			case api.ServiceKindAPIGateway, api.ServiceKindMeshGateway, api.ServiceKindIngressGateway, api.ServiceKindTerminatingGateway:
				proxyID = gateway.ID
			}
		}
		if proxyID == "" {
			// In theory we can't reach this point unless we have a service registered against
			// this pod but it isnt a Connect Gateway. We don't support this case, but it
			// is necessary to return from the function.
			c.nonRetryableError = fmt.Errorf("unable to find registered connect-proxy service")
			return nil
		}

		// Write the proxy ID to the shared volume so the consul-dataplane can use it for bootstrapping.
		if err := common.WriteFileWithPerms(c.flagProxyIDFile, proxyID, os.FileMode(0444)); err != nil {
			// Save an error but return nil so that we don't retry this step.
			c.nonRetryableError = err
			return nil
		}

		return nil
	}
}

func (c *Command) validateFlags() error {
	if c.flagPodName == "" {
		return errors.New("-pod-name must be set")
	}
	if c.flagPodNamespace == "" {
		return errors.New("-pod-namespace must be set")
	}
	if c.consul.ConsulLogin.AuthMethod != "" && c.flagServiceAccountName == "" && c.flagGatewayKind == "" {
		return errors.New("-service-account-name must be set when ACLs are enabled")
	}
	if c.flagConsulNodeName == "" {
		return errors.New("-consul-node-name must be set")
	}

	return nil
}

func (c *Command) Synopsis() string { return synopsis }
func (c *Command) Help() string {
	c.once.Do(c.init)
	return c.help
}

// This below implementation is loosely based on
// https://github.com/hashicorp/consul/blob/fe2d41ddad9ba2b8ff86cbdebbd8f05855b1523c/command/connect/redirecttraffic/redirect_traffic.go#L136.

// trafficRedirectProxyConfig is a snippet of xds/config.go
// with only the configuration values that we need to parse from Proxy.Config
// to apply traffic redirection rules.
type trafficRedirectProxyConfig struct {
	BindPort      int    `mapstructure:"bind_port"`
	StatsBindAddr string `mapstructure:"envoy_stats_bind_addr"`
}

func (c *Command) applyTrafficRedirectionRules(svc *api.AgentService, dualStack bool) error {
	err := json.Unmarshal([]byte(c.flagRedirectTrafficConfig), &c.iptablesConfig)
	if err != nil {
		return err
	}
	if c.iptablesProvider != nil {
		c.iptablesConfig.IptablesProvider = c.iptablesProvider
	}

	if svc.Proxy.TransparentProxy != nil && svc.Proxy.TransparentProxy.OutboundListenerPort != 0 {
		c.iptablesConfig.ProxyOutboundPort = svc.Proxy.TransparentProxy.OutboundListenerPort
	}

	// Decode proxy's opaque config so that we can use it later to configure
	// traffic redirection with iptables.
	var trCfg trafficRedirectProxyConfig
	if err = mapstructure.WeakDecode(svc.Proxy.Config, &trCfg); err != nil {
		return fmt.Errorf("failed parsing Proxy.Config: %s", err)
	}
	if trCfg.BindPort != 0 {
		c.iptablesConfig.ProxyInboundPort = trCfg.BindPort
	}

	if trCfg.StatsBindAddr != "" {
		_, port, err := net.SplitHostPort(trCfg.StatsBindAddr)
		if err != nil {
			return fmt.Errorf("failed parsing host and port from envoy_stats_bind_addr: %s", err)
		}

		c.iptablesConfig.ExcludeInboundPorts = append(c.iptablesConfig.ExcludeInboundPorts, port)
	}

	// Configure any relevant information from the proxy service
	err = iptables.Setup(c.iptablesConfig, dualStack)
	if err != nil {
		return err
	}
	c.logger.Info("Successfully applied traffic redirection rules")
	return nil
}

const synopsis = "Inject connect init command."
const help = `
Usage: consul-k8s-control-plane connect-init [options]

  Bootstraps connect-injected pod components.
  Not intended for stand-alone use.
`
