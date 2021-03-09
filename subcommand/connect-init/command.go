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
const serviceRegistrationPollingRetries = 6 // This maps to 60 seconds

type Command struct {
	UI cli.Ui

	flagACLAuthMethod                  string            // Auth Method to use for ACLs, if enabled.
	flagMeta                           map[string]string // Flag for metadata to consul login.
	flagBearerTokenFile                string            // Location of the bearer token.
	flagTokenSinkFile                  string            // Location to write the output token.
	flagProxyIDFile                    string            // Location to write the output proxyID.
	flagPodName                        string            // Pod name.
	flagPodNamespace                   string            // Pod namespace.
	flagServiceAccountName             string            // ServiceAccountName for this service.
	flagSkipServiceRegistrationPolling bool              // Whether or not to skip service registration.

	flagSet *flag.FlagSet
	http    *flags.HTTPFlags

	consulClient *api.Client

	once sync.Once
	help string
}

func (c *Command) init() {
	c.flagSet = flag.NewFlagSet("", flag.ContinueOnError)
	c.flagSet.StringVar(&c.flagACLAuthMethod, "acl-auth-method", "", "Name of the auth method to login to.")
	c.flagSet.Var((*flags.FlagMapValue)(&c.flagMeta), "meta",
		"Metadata to set on the token, formatted as key=value. This flag may be specified multiple "+
			"times to set multiple meta fields.")
	c.flagSet.StringVar(&c.flagBearerTokenFile, "bearer-token-file", bearerTokenFile,
		"Path to a file containing a secret bearer token to use with this auth method. "+
			"Default is /var/run/secrets/kubernetes.io/serviceaccount/token.")
	c.flagSet.StringVar(&c.flagTokenSinkFile, "token-sink-file", tokenSinkFile,
		"The most recent token's SecretID is kept up to date in this file. Default is /consul/connect-inject/acl-token.")
	c.flagSet.StringVar(&c.flagProxyIDFile, "proxyid-file", proxyIDFile, "Location to write the output proxyid file.")
	c.flagSet.StringVar(&c.flagPodName, "pod-name", "", "Name of the pod.")
	c.flagSet.StringVar(&c.flagPodNamespace, "pod-namespace", "", "Name of the pod namespace.")
	c.flagSet.StringVar(&c.flagServiceAccountName, "service-account-name", "", "The service account name for this service.")

	// TODO: we dont need this if we can mock the login bits
	c.flagSet.BoolVar(&c.flagSkipServiceRegistrationPolling, "skip-service-registration-polling", true,
		"The service account name for this service.")

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
		if c.flagServiceAccountName == "" {
			c.UI.Error("-service-account-name must be set")
			return 1
		}
		err = backoff.Retry(func() error {
			err := common.ConsulLogin(c.consulClient, c.flagBearerTokenFile, c.flagACLAuthMethod, c.flagTokenSinkFile, c.flagMeta)
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
	// which maps to this one.
	// In the case of ACLs this will match the serviceAccountName, we query on this.
	// If ACLs are disabled we query all services and search through
	// the list for a service with `meta["pod-name"]` that matches this pod.
	data := ""
	err = backoff.Retry(func() error {
		if c.flagACLAuthMethod == "" {
			// TODO: can we filter this request somehow?
			filter := fmt.Sprintf("Kind != `%s` and Meta.pod-name == %s and Meta.k8s-namespace == %s", "connect-proxy", c.flagPodName, c.flagPodNamespace)
			serviceList, err := c.consulClient.Agent().ServicesWithFilter(filter)
			if err != nil {
				c.UI.Error(fmt.Sprintf("Unable to get agent services: %s", err))
				return err
			}
			for _, y := range serviceList {
				// TODO: in theory we've already filtered enough.. can we just return?
				if y.Kind != "connect-proxy" && y.Meta["pod-name"] == c.flagPodName && y.Meta["k8s-namespace"] == c.flagPodNamespace {
					c.UI.Info(fmt.Sprintf("Registered pod has been detected: %s", y.Meta["pod-name"]))
					data = fmt.Sprintf("%s-%s-%s", c.flagPodName, y.ID, "sidecar-proxy")
					return nil
				}
			}
			return fmt.Errorf("Unable to find registered service")
		} else {
			// If ACLs are enabled we don't have permission to go through the list of all services
			svc, _, err := c.consulClient.Agent().Service(c.flagServiceAccountName, &api.QueryOptions{})
			if err != nil {
				c.UI.Error(fmt.Sprintf("Unable to write proxyid out: %s", err))
				return err
			} else {
				if svc == nil {
					c.UI.Info(fmt.Sprintf("unable to fetch registered service for %v", c.flagServiceAccountName))
					return fmt.Errorf("Unable to find registered service")
				}
				data = fmt.Sprintf("%s-%s-%s", c.flagPodName, svc.ID, "sidecar-proxy")
			}
			return nil
		}
	}, backoff.WithMaxRetries(backoff.NewConstantBackOff(1*time.Second), serviceRegistrationPollingRetries))
	if err != nil {
		c.UI.Error("Timed out waiting for service registration")
		return 1
	}
	// Write the proxyid to the shared volume.
	err = ioutil.WriteFile(c.flagProxyIDFile, []byte(data), 0444)
	if err != nil {
		c.UI.Error(fmt.Sprintf("Unable to write proxyid out: %s", err))
		return 1
	}
	c.UI.Info("Bootstrapping completed")
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
