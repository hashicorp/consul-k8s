package connectinit

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/cenkalti/backoff"
	connectinject "github.com/hashicorp/consul-k8s/control-plane/connect-inject"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/common"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/flags"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/go-hclog"
	"github.com/mitchellh/cli"
)

const (
	defaultBearerTokenFile = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	defaultTokenSinkFile   = "/consul/connect-inject/acl-token"
	defaultProxyIDFile     = "/consul/connect-inject/proxyid"

	// The number of times to attempt to read this service (120s).
	defaultServicePollingRetries = 120
)

type Command struct {
	UI cli.Ui

	flagACLAuthMethod          string // Auth Method to use for ACLs, if enabled.
	flagConsulNodeName         string
	flagPodName                string // Pod name.
	flagPodNamespace           string // Pod namespace.
	flagPrimaryDatacenter      string // Consul primary datacenter name if running in a secondary datacenter.
	flagAuthMethodNamespace    string // Consul namespace the auth-method is defined in.
	flagConsulServiceNamespace string // Consul destination namespace for the service.
	flagServiceAccountName     string // Service account name.
	flagServiceName            string // Service name.
	flagGateway                bool
	flagGatewayKind            string
	flagLogLevel               string
	flagLogJSON                bool

	flagBearerTokenFile                string // Location of the bearer token. Default is /var/run/secrets/kubernetes.io/serviceaccount/token.
	flagACLTokenSink                   string // Location to write the output token. Default is defaultTokenSinkFile.
	flagProxyIDFile                    string // Location to write the output proxyID. Default is defaultProxyIDFile.
	flagMultiPort                      bool
	serviceRegistrationPollingAttempts uint64 // Number of times to poll for this service to be registered.
	loginAttempts                      uint64 // Number of times to retry login call; only used in tests.

	flagSet *flag.FlagSet
	http    *flags.HTTPFlags

	once   sync.Once
	help   string
	logger hclog.Logger

	nonRetryableError error
}

func (c *Command) init() {
	c.flagSet = flag.NewFlagSet("", flag.ContinueOnError)
	c.flagSet.StringVar(&c.flagACLAuthMethod, "acl-auth-method", "", "Name of the auth method to login to.")
	c.flagSet.StringVar(&c.flagPodName, "pod-name", "", "Name of the pod.")
	c.flagSet.StringVar(&c.flagConsulNodeName, "consul-node-name", "", "Name of the Consul node where services are registered.")
	c.flagSet.StringVar(&c.flagPodNamespace, "pod-namespace", "", "Name of the pod namespace.")
	c.flagSet.StringVar(&c.flagPrimaryDatacenter, "primary-datacenter", "", "Name of the primary datacenter if federation is enabled and this operation is being executed in a secondary datacenter.")
	c.flagSet.StringVar(&c.flagAuthMethodNamespace, "auth-method-namespace", "", "Consul namespace the auth-method is defined in")
	c.flagSet.StringVar(&c.flagConsulServiceNamespace, "consul-service-namespace", "", "Consul destination namespace of the service.")
	c.flagSet.StringVar(&c.flagServiceAccountName, "service-account-name", "", "Service account name on the pod.")
	c.flagSet.StringVar(&c.flagServiceName, "service-name", "", "Service name as specified via the pod annotation.")
	c.flagSet.StringVar(&c.flagBearerTokenFile, "bearer-token-file", defaultBearerTokenFile, "Path to service account token file.")
	c.flagSet.StringVar(&c.flagACLTokenSink, "acl-token-sink", defaultTokenSinkFile, "File name where where ACL token should be saved.")
	c.flagSet.StringVar(&c.flagProxyIDFile, "proxy-id-file", defaultProxyIDFile, "File name where proxy's Consul service ID should be saved.")
	c.flagSet.BoolVar(&c.flagMultiPort, "multiport", false, "If the pod is a multi port pod.")
	c.flagSet.BoolVar(&c.flagGateway, "gateway", false, "If the pod is a Consul gateway pod.")
	c.flagSet.StringVar(&c.flagGatewayKind, "gateway-kind", "", "Name of the gateway that is being registered.")
	c.flagSet.StringVar(&c.flagLogLevel, "log-level", "info",
		"Log verbosity level. Supported values (in order of detail) are \"trace\", "+
			"\"debug\", \"info\", \"warn\", and \"error\".")
	c.flagSet.BoolVar(&c.flagLogJSON, "log-json", false,
		"Enable or disable JSON output format for logging.")

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
	cfg := api.DefaultConfig()
	cfg.Namespace = c.flagConsulServiceNamespace
	c.http.MergeOntoConfig(cfg)
	consulClient, err := consul.NewClient(cfg, c.http.ConsulAPITimeout())
	if err != nil {
		c.logger.Error("Unable to get client connection", "error", err)
		return 1
	}

	// First do the ACL Login, if necessary.
	if c.flagACLAuthMethod != "" {
		// loginMeta is the default metadata that we pass to the consul login API.
		var loginMeta map[string]string
		if c.flagGateway {
			loginMeta = map[string]string{"component": c.flagGatewayKind}
		} else {
			loginMeta = map[string]string{"pod": fmt.Sprintf("%s/%s", c.flagPodNamespace, c.flagPodName)}
		}
		loginParams := common.LoginParams{
			AuthMethod:      c.flagACLAuthMethod,
			Namespace:       c.flagAuthMethodNamespace,
			Datacenter:      c.flagPrimaryDatacenter,
			BearerTokenFile: c.flagBearerTokenFile,
			TokenSinkFile:   c.flagACLTokenSink,
			Meta:            loginMeta,
			NumRetries:      c.loginAttempts,
		}
		token, err := common.ConsulLogin(consulClient, loginParams, c.logger)
		if err != nil {
			if c.flagServiceAccountName == "default" {
				c.logger.Warn("The service account name for this Pod is \"default\"." +
					" In default installations this is not a supported service account name." +
					" The service account name must match the name of the Kubernetes Service" +
					" or the consul.hashicorp.com/connect-service annotation.")
			}
			c.logger.Error("unable to complete login", "error", err)
			return 1
		}
		cfg.Token = token
	}

	// We need a new client so that we can use the ACL token that was fetched during login to do the next bit,
	// otherwise `consulClient` will still be using the bearerToken that was passed in.
	consulClient, err = consul.NewClient(cfg, c.http.ConsulAPITimeout())
	if err != nil {
		c.logger.Error("Unable to update client connection", "error", err)
		return 1
	}
	if c.flagGateway {
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
		err = backoff.Retry(c.getConnectServiceRegistrations(consulClient), backoff.WithMaxRetries(backoff.NewConstantBackOff(1*time.Second), c.serviceRegistrationPollingAttempts))
		if err != nil {
			c.logger.Error("Timed out waiting for service registration", "error", err)
			return 1
		}
		if c.nonRetryableError != nil {
			c.logger.Error("Error processing service registration", "error", c.nonRetryableError)
			return 1
		}
	}
	c.logger.Info("Connect initialization completed")
	return 0
}

func (c *Command) getConnectServiceRegistrations(consulClient *api.Client) backoff.Operation {
	var proxyID string
	registrationRetryCount := 0
	return func() error {
		registrationRetryCount++
		filter := fmt.Sprintf("Meta[%q] == %q and Meta[%q] == %q ",
			connectinject.MetaKeyPodName, c.flagPodName, connectinject.MetaKeyKubeNS, c.flagPodNamespace)
		if c.flagMultiPort && c.flagServiceName != "" {
			// If the service name is set and this is a multi-port pod there may be multiple services registered for
			// this one Pod. If so, we want to ensure the service and proxy matching our expected name is registered.
			filter += fmt.Sprintf(` and (Service == %q or Service == "%s-sidecar-proxy")`, c.flagServiceName, c.flagServiceName)
		}
		serviceList, _, err := consulClient.Catalog().NodeServiceList(c.flagConsulNodeName, &api.QueryOptions{Filter: filter})
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
			if c.flagACLAuthMethod != "" {
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

		if err := common.WriteFileWithPerms(c.flagProxyIDFile, proxyID, os.FileMode(0444)); err != nil {
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
		filter := fmt.Sprintf("Meta[%q] == %q and Meta[%q] == %q ",
			connectinject.MetaKeyPodName, c.flagPodName, connectinject.MetaKeyKubeNS, c.flagPodNamespace)

		gatewayList, _, err := client.Catalog().NodeServiceList(c.flagConsulNodeName, &api.QueryOptions{Filter: filter})
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
			case api.ServiceKindMeshGateway, api.ServiceKindIngressGateway, api.ServiceKindTerminatingGateway:
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
	if c.flagACLAuthMethod != "" && c.flagServiceAccountName == "" && !c.flagGateway {
		return errors.New("-service-account-name must be set when ACLs are enabled")
	}
	if c.flagConsulNodeName == "" {
		return errors.New("-consul-node-name must be set")
	}
	if c.flagGateway && c.flagGatewayKind == "" {
		return errors.New("-gateway-kind must be set if -gateway is set")
	}

	if c.http.ConsulAPITimeout() <= 0 {
		return errors.New("-consul-api-timeout must be set to a value greater than 0")
	}
	return nil
}

func (c *Command) Synopsis() string { return synopsis }
func (c *Command) Help() string {
	c.once.Do(c.init)
	return c.help
}

const synopsis = "Inject connect init command."
const help = `
Usage: consul-k8s-control-plane connect-init [options]

  Bootstraps connect-injected pod components.
  Not intended for stand-alone use.
`
