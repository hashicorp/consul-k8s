package connectinit

import (
	"flag"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/cenkalti/backoff"
	connectinject "github.com/hashicorp/consul-k8s/connect-inject"
	"github.com/hashicorp/consul-k8s/consul"
	"github.com/hashicorp/consul-k8s/subcommand/common"
	"github.com/hashicorp/consul-k8s/subcommand/flags"
	"github.com/hashicorp/consul/api"
	"github.com/mitchellh/cli"
)

const (
	defaultBearerTokenFile = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	defaultTokenSinkFile   = "/consul/connect-inject/acl-token"
	defaultProxyIDFile     = "/consul/connect-inject/proxyid"

	// The number of times to attempt ACL Login.
	numLoginRetries = 3
	// The number of times to attempt to read this service (60s).
	defaultServicePollingRetries = 60
)

type Command struct {
	UI cli.Ui

	flagACLAuthMethod          string // Auth Method to use for ACLs, if enabled.
	flagPodName                string // Pod name.
	flagPodNamespace           string // Pod namespace.
	flagAuthMethodNamespace    string // Consul namespace the auth-method is defined in.
	flagConsulServiceNamespace string // Consul destination namespace for the service.
	flagServiceAccountName     string // Service account name.

	bearerTokenFile                    string // Location of the bearer token. Default is /var/run/secrets/kubernetes.io/serviceaccount/token.
	tokenSinkFile                      string // Location to write the output token. Default is defaultTokenSinkFile.
	proxyIDFile                        string // Location to write the output proxyID. Default is defaultProxyIDFile.
	serviceRegistrationPollingAttempts uint64 // Number of times to poll for this service to be registered.

	flagSet *flag.FlagSet
	http    *flags.HTTPFlags

	once sync.Once
	help string
}

func (c *Command) init() {
	c.flagSet = flag.NewFlagSet("", flag.ContinueOnError)
	c.flagSet.StringVar(&c.flagACLAuthMethod, "acl-auth-method", "", "Name of the auth method to login to.")
	c.flagSet.StringVar(&c.flagPodName, "pod-name", "", "Name of the pod.")
	c.flagSet.StringVar(&c.flagPodNamespace, "pod-namespace", "", "Name of the pod namespace.")
	c.flagSet.StringVar(&c.flagAuthMethodNamespace, "auth-method-namespace", "", "Consul namespace the auth-method is defined in")
	c.flagSet.StringVar(&c.flagConsulServiceNamespace, "consul-service-namespace", "", "Consul destination namespace of the service.")
	c.flagSet.StringVar(&c.flagServiceAccountName, "service-account-name", "", "Service account name on the pod.")

	if c.bearerTokenFile == "" {
		c.bearerTokenFile = defaultBearerTokenFile
	}
	if c.tokenSinkFile == "" {
		c.tokenSinkFile = defaultTokenSinkFile
	}
	if c.proxyIDFile == "" {
		c.proxyIDFile = defaultProxyIDFile
	}
	if c.serviceRegistrationPollingAttempts == 0 {
		c.serviceRegistrationPollingAttempts = defaultServicePollingRetries
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

	cfg := api.DefaultConfig()
	cfg.Namespace = c.flagConsulServiceNamespace
	c.http.MergeOntoConfig(cfg)
	consulClient, err := consul.NewClient(cfg)
	if err != nil {
		c.UI.Error(fmt.Sprintf("Unable to get client connection: %s", err))
		return 1
	}

	// First do the ACL Login, if necessary.
	if c.flagACLAuthMethod != "" {
		// loginMeta is the default metadata that we pass to the consul login API.
		loginMeta := map[string]string{"pod": fmt.Sprintf("%s/%s", c.flagPodNamespace, c.flagPodName)}
		err = backoff.Retry(func() error {
			err := common.ConsulLogin(consulClient, c.bearerTokenFile, c.flagACLAuthMethod, c.tokenSinkFile, c.flagAuthMethodNamespace, loginMeta)
			if err != nil {
				c.UI.Error(fmt.Sprintf("Consul login failed; retrying: %s", err))
			}
			return err
		}, backoff.WithMaxRetries(backoff.NewConstantBackOff(1*time.Second), numLoginRetries))
		if err != nil {
			c.UI.Error(fmt.Sprintf("Hit maximum retries for consul login: %s", err))
			return 1
		}
		// Now update the client so that it will read the ACL token we just fetched.
		cfg.TokenFile = c.tokenSinkFile
		consulClient, err = consul.NewClient(cfg)
		if err != nil {
			c.UI.Error(fmt.Sprintf("Unable to update client connection: %s", err))
			return 1
		}
		c.UI.Info("Consul login complete")
	}

	// Now wait for the service to be registered. Do this by querying the Agent for a service
	// which maps to this pod+namespace.
	var proxyID string
	var errServiceNameMismatch error
	err = backoff.Retry(func() error {
		filter := fmt.Sprintf("Meta[%q] == %q and Meta[%q] == %q", connectinject.MetaKeyPodName, c.flagPodName, connectinject.MetaKeyKubeNS, c.flagPodNamespace)
		serviceList, err := consulClient.Agent().ServicesWithFilter(filter)
		if err != nil {
			c.UI.Error(fmt.Sprintf("unable to get Agent services: %s", err))
			return err
		}
		// Wait for the service and the connect-proxy service to be registered.
		if len(serviceList) != 2 {
			c.UI.Info("Unable to find registered services; retrying")
			return fmt.Errorf("did not find correct number of services: %d", len(serviceList))
		}
		for _, svc := range serviceList {
			c.UI.Info(fmt.Sprintf("Registered service has been detected: %s", svc.Service))
			// When ACLs are enabled: If the flagServiceAccountName is empty, it means the service name pod annotation
			// was set, so the check for service account name == consul service name has already occurred in
			// container_init.go. If the flagServiceAccountName is not empty, we need to check whether it matches the
			// Kubernetes service name.
			if c.flagACLAuthMethod != "" && c.flagServiceAccountName != "" && svc.Meta[connectinject.MetaKeyKubeServiceName] != c.flagServiceAccountName {
				// Set the error but return nil so we don't retry.
				errServiceNameMismatch = fmt.Errorf("service account name %s doesn't match Kubernetes service name %s", c.flagServiceAccountName, svc.Meta[connectinject.MetaKeyKubeServiceName])
				return nil
			}
			if svc.Kind == api.ServiceKindConnectProxy {
				// This is the proxy service ID.
				proxyID = svc.ID
				return nil
			}
		}
		// In theory we can't reach this point unless we have 2 services registered against
		// this pod and neither are the connect-proxy. We don't support this case anyway, but it
		// is necessary to return from the function.
		return fmt.Errorf("unable to find registered connect-proxy service")
	}, backoff.WithMaxRetries(backoff.NewConstantBackOff(1*time.Second), c.serviceRegistrationPollingAttempts))
	if err != nil {
		c.UI.Error(fmt.Sprintf("Timed out waiting for service registration: %v", err))
		return 1
	}
	if errServiceNameMismatch != nil {
		c.UI.Error(errServiceNameMismatch.Error())
		return 1
	}
	// Write the proxy ID to the shared volume so `consul connect envoy` can use it for bootstrapping.
	err = common.WriteFileWithPerms(c.proxyIDFile, proxyID, os.FileMode(0444))
	if err != nil {
		c.UI.Error(fmt.Sprintf("Unable to write proxy ID to file: %s", err))
		return 1
	}
	c.UI.Info("Connect initialization completed")
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
