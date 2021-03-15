package connectinit

import (
	"flag"
	"fmt"
	"io/ioutil"
	"sync"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/hashicorp/consul-k8s/consul"
	"github.com/hashicorp/consul-k8s/subcommand/common"
	"github.com/hashicorp/consul-k8s/subcommand/flags"
	"github.com/hashicorp/consul/api"
	"github.com/mitchellh/cli"
)

const bearerTokenFile = "/var/run/secrets/kubernetes.io/serviceaccount/token"
const tokenSinkFile = "/consul/connect-inject/acl-token"
const proxyIDFile = "/consul/connect-inject/proxyid"
const numLoginRetries = 3
const serviceRegistrationPollingRetries = 60 // This maps to 60 seconds

type Command struct {
	UI cli.Ui

	flagACLAuthMethod                  string            // Auth Method to use for ACLs, if enabled.
	flagMeta                           map[string]string // Flag for metadata to consul login.
	flagPodName                        string            // Pod name.
	flagPodNamespace                   string            // Pod namespace.
	flagSkipServiceRegistrationPolling bool              // Whether or not to skip service registration.

	flagSet *flag.FlagSet
	http    *flags.HTTPFlags

	consulClient *api.Client

	BearerTokenFile                    string // Location of the bearer token. Default is /var/run/secrets/kubernetes.io/serviceaccount/token.
	TokenSinkFile                      string // Location to write the output token. Default is /consul/connect-inject/acl-token.
	ProxyIDFile                        string // Location to write the output proxyID. Default is /consul/connect-inject/proxyid.
	ServiceRegistrationPollingAttempts int    // Number of times to attempt service registration retry

	once sync.Once
	help string
}

func (c *Command) init() {
	c.flagSet = flag.NewFlagSet("", flag.ContinueOnError)
	c.flagSet.StringVar(&c.flagACLAuthMethod, "acl-auth-method", "", "Name of the auth method to login to.")
	c.flagSet.Var((*flags.FlagMapValue)(&c.flagMeta), "meta",
		"Metadata to set on the token, formatted as key=value. This flag may be specified multiple "+
			"times to set multiple meta fields.")
	c.flagSet.StringVar(&c.flagPodName, "pod-name", "", "Name of the pod.")
	c.flagSet.StringVar(&c.flagPodNamespace, "pod-namespace", "", "Name of the pod namespace.")

	// TODO: when the endpoints controller manages service registration this can be removed. For now it preserves back compatibility.
	c.flagSet.BoolVar(&c.flagSkipServiceRegistrationPolling, "skip-service-registration-polling", true,
		"Flag to preserve backward compatibility with service registration.")

	if c.BearerTokenFile == "" {
		c.BearerTokenFile = bearerTokenFile
	}
	if c.TokenSinkFile == "" {
		c.TokenSinkFile = tokenSinkFile
	}
	if c.ProxyIDFile == "" {
		c.ProxyIDFile = proxyIDFile
	}
	if c.ServiceRegistrationPollingAttempts == 0 {
		c.ServiceRegistrationPollingAttempts = serviceRegistrationPollingRetries
	}

	c.http = &flags.HTTPFlags{}
	flags.Merge(c.flagSet, c.http.Flags())
	c.help = flags.Usage(help, c.flagSet)
}

func (c *Command) Run(args []string) int {
	var err error
	c.once.Do(c.init)
	if err := c.flagSet.Parse(args); err != nil {
		return 1
	}
	if c.flagPodName == "" {
		c.UI.Error("-pod-name must be set")
		return 1
	}
	if c.flagPodNamespace == "" {
		c.UI.Error("-pod-namespace must be set")
		return 1
	}
	// TODO: Add namespace support
	if c.consulClient == nil {
		cfg := api.DefaultConfig()
		c.http.MergeOntoConfig(cfg)
		c.consulClient, err = consul.NewClient(cfg)
		if err != nil {
			c.UI.Error(fmt.Sprintf("Unable to get client connection: %s", err))
			return 1
		}
	}
	// First do the ACL Login, if necessary.
	if c.flagACLAuthMethod != "" {
		// Validate flags related to ACL login.
		if c.flagMeta == nil {
			c.UI.Error("-meta must be set")
			return 1
		}
		err = backoff.Retry(func() error {
			err := common.ConsulLogin(c.consulClient, c.BearerTokenFile, c.flagACLAuthMethod, c.TokenSinkFile, c.flagMeta)
			if err != nil {
				c.UI.Error(fmt.Sprintf("Consul login failed; retrying: %s", err))
			}
			return err
		}, backoff.WithMaxRetries(backoff.NewConstantBackOff(1*time.Second), uint64(numLoginRetries)))
		if err != nil {
			c.UI.Error(fmt.Sprintf("Hit maximum retries for consul login: %s", err))
			return 1
		}
		c.UI.Info("Consul login complete")
	}
	if c.flagSkipServiceRegistrationPolling {
		return 0
	}

	// Now wait for the service to be registered. Do this by querying the Agent for a service
	// which maps to this pod+namespace.
	data := ""
	err = backoff.Retry(func() error {
		filter := fmt.Sprintf("Meta[\"pod-name\"] == %s and Meta[\"k8s-namespace\"] == %s", c.flagPodName, c.flagPodNamespace)
		serviceList, err := c.consulClient.Agent().ServicesWithFilter(filter)
		if err != nil {
			c.UI.Error(fmt.Sprintf("Unable to get agent service: %s", err))
			return err
		}
		// Wait for the service and the connect-proxy service to be registered.
		if len(serviceList) != 2 {
			return fmt.Errorf("Unable to find registered service")
		}
		for _, y := range serviceList {
			c.UI.Info(fmt.Sprintf("Registered pod has been detected: %s", y.Meta["pod-name"]))
			if y.Kind == "connect-proxy" {
				// This is the proxy service ID
				data = y.ID
				return nil
			}
		}
		return fmt.Errorf("Unable to find registered service")
	}, backoff.WithMaxRetries(backoff.NewConstantBackOff(1*time.Second), uint64(c.ServiceRegistrationPollingAttempts)))
	if err != nil {
		c.UI.Error("Timed out waiting for service registration")
		return 1
	}
	// Write the proxyid to the shared volume.
	err = ioutil.WriteFile(c.ProxyIDFile, []byte(data), 0444)
	if err != nil {
		c.UI.Error(fmt.Sprintf("Unable to write proxyid out: %s", err))
		return 1
	}
	c.UI.Info("Service registration completed")
	return 0
}

func (c *Command) Synopsis() string { return synopsis }
func (c *Command) Help() string {
	c.once.Do(c.init)
	return c.help
}

const synopsis = "Inject connect init command."
const help = `
Usage: consul-k8s connect-init [options]

  Bootstraps connect-injected pod components.
  Not intended for stand-alone use.
`
