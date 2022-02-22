package connectinit

import (
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

	// The number of times to attempt ACL Login.
	numLoginRetries = 3
	// The number of times to attempt to read this service (120s).
	defaultServicePollingRetries = 120

	raftReplicationTimeout   = 2 * time.Second
	tokenReadPollingInterval = 100 * time.Millisecond
)

type Command struct {
	UI cli.Ui

	flagACLAuthMethod          string // Auth Method to use for ACLs, if enabled.
	flagPodName                string // Pod name.
	flagPodNamespace           string // Pod namespace.
	flagAuthMethodNamespace    string // Consul namespace the auth-method is defined in.
	flagConsulServiceNamespace string // Consul destination namespace for the service.
	flagServiceAccountName     string // Service account name.
	flagServiceName            string // Service name.
	flagLogLevel               string
	flagLogJSON                bool

	flagBearerTokenFile                string // Location of the bearer token. Default is /var/run/secrets/kubernetes.io/serviceaccount/token.
	flagACLTokenSink                   string // Location to write the output token. Default is defaultTokenSinkFile.
	flagProxyIDFile                    string // Location to write the output proxyID. Default is defaultProxyIDFile.
	flagMultiPort                      bool
	serviceRegistrationPollingAttempts uint64 // Number of times to poll for this service to be registered.

	flagSet *flag.FlagSet
	http    *flags.HTTPFlags

	once   sync.Once
	help   string
	logger hclog.Logger
}

func (c *Command) init() {
	c.flagSet = flag.NewFlagSet("", flag.ContinueOnError)
	c.flagSet.StringVar(&c.flagACLAuthMethod, "acl-auth-method", "", "Name of the auth method to login to.")
	c.flagSet.StringVar(&c.flagPodName, "pod-name", "", "Name of the pod.")
	c.flagSet.StringVar(&c.flagPodNamespace, "pod-namespace", "", "Name of the pod namespace.")
	c.flagSet.StringVar(&c.flagAuthMethodNamespace, "auth-method-namespace", "", "Consul namespace the auth-method is defined in")
	c.flagSet.StringVar(&c.flagConsulServiceNamespace, "consul-service-namespace", "", "Consul destination namespace of the service.")
	c.flagSet.StringVar(&c.flagServiceAccountName, "service-account-name", "", "Service account name on the pod.")
	c.flagSet.StringVar(&c.flagServiceName, "service-name", "", "Service name as specified via the pod annotation.")
	c.flagSet.StringVar(&c.flagBearerTokenFile, "bearer-token-file", defaultBearerTokenFile, "Path to service account token file.")
	c.flagSet.StringVar(&c.flagACLTokenSink, "acl-token-sink", defaultTokenSinkFile, "File name where where ACL token should be saved.")
	c.flagSet.StringVar(&c.flagProxyIDFile, "proxy-id-file", defaultProxyIDFile, "File name where proxy's Consul service ID should be saved.")
	c.flagSet.BoolVar(&c.flagMultiPort, "multiport", false, "If the pod is a multi port pod.")
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
	if c.flagPodName == "" {
		c.UI.Error("-pod-name must be set")
		return 1
	}
	if c.flagPodNamespace == "" {
		c.UI.Error("-pod-namespace must be set")
		return 1
	}
	if c.flagACLAuthMethod != "" && c.flagServiceAccountName == "" {
		c.UI.Error("-service-account-name must be set when ACLs are enabled")
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
	consulClient, err := consul.NewClient(cfg)
	if err != nil {
		c.logger.Error("Unable to get client connection", "error", err)
		return 1
	}

	// First do the ACL Login, if necessary.
	if c.flagACLAuthMethod != "" {
		// loginMeta is the default metadata that we pass to the consul login API.
		loginMeta := map[string]string{"pod": fmt.Sprintf("%s/%s", c.flagPodNamespace, c.flagPodName)}
		err = backoff.Retry(func() error {
			err := common.ConsulLogin(consulClient, c.flagBearerTokenFile, c.flagACLAuthMethod, c.flagACLTokenSink, c.flagAuthMethodNamespace, loginMeta)
			if err != nil {
				c.logger.Error("Consul login failed; retrying", "error", err)
			}
			return err
		}, backoff.WithMaxRetries(backoff.NewConstantBackOff(1*time.Second), numLoginRetries))
		if err != nil {
			if c.flagServiceAccountName == "default" {
				c.logger.Warn("The service account name for this Pod is \"default\"." +
					" In default installations this is not a supported service account name." +
					" The service account name must match the name of the Kubernetes Service" +
					" or the consul.hashicorp.com/connect-service annotation.")
			}
			c.logger.Error("Hit maximum retries for consul login", "error", err)
			return 1
		}
		// Now update the client so that it will read the ACL token we just fetched.
		cfg.TokenFile = c.flagACLTokenSink
		consulClient, err = consul.NewClient(cfg)
		if err != nil {
			c.logger.Error("Unable to update client connection", "error", err)
			return 1
		}
		c.logger.Info("Consul login complete")

		// A workaround to check that the ACL token is replicated to other Consul servers.
		//
		// A consul client may reach out to a follower instead of a leader to resolve the token during the
		// call to get services below. This is because clients talk to servers in the stale consistency mode
		// to decrease the load on the servers (see https://www.consul.io/docs/architecture/consensus#stale).
		// In that case, it's possible that the token isn't replicated
		// to that server instance yet. The client will then get an "ACL not found" error
		// and subsequently cache this not found response. Then our call below
		// to get services from the agent will keep hitting the same "ACL not found" error
		// until the cache entry expires (determined by the `acl_token_ttl` which defaults to 30 seconds).
		// This is not great because it will delay app start up time by 30 seconds in most cases
		// (if you are running 3 servers, then the probability of ending up on a follower is close to 2/3).
		//
		// To help with that, we try to first read the token in the stale consistency mode until we
		// get a successful response. This should not take more than 100ms because raft replication
		// should in most cases take less than that (see https://www.consul.io/docs/install/performance#read-write-tuning)
		// but we set the timeout to 2s to be sure.
		//
		// Note though that this workaround does not eliminate this problem completely. It's still possible
		// for this call and the next call to reach different servers and those servers to have different
		// states from each other.
		// For example, this call can reach a leader and succeed, while the call below can go to a follower
		// that is still behind the leader and get an "ACL not found" error.
		// However, this is a pretty unlikely case because
		// clients have sticky connections to a server, and those connections get rebalanced only every 2-3min.
		// And so, this workaround should work in a vast majority of cases.
		c.logger.Info("Checking that the ACL token exists when reading it in the stale consistency mode")
		// Use raft timeout and polling interval to determine the number of retries.
		numTokenReadRetries := uint64(raftReplicationTimeout.Milliseconds() / tokenReadPollingInterval.Milliseconds())
		err = backoff.Retry(func() error {
			_, _, err := consulClient.ACL().TokenReadSelf(&api.QueryOptions{AllowStale: true})
			if err != nil {
				c.logger.Error("Unable to read ACL token; retrying", "err", err)
			}
			return err
		}, backoff.WithMaxRetries(backoff.NewConstantBackOff(tokenReadPollingInterval), numTokenReadRetries))
		if err != nil {
			c.logger.Error("Unable to read ACL token from a Consul server; "+
				"please check that your server cluster is healthy", "err", err)
			return 1
		}
		c.logger.Info("Successfully read ACL token from the server")
	}

	// Now wait for the service to be registered. Do this by querying the Agent for a service
	// which maps to this pod+namespace.
	var proxyID string
	registrationRetryCount := 0
	var errServiceNameMismatch error
	err = backoff.Retry(func() error {
		registrationRetryCount++
		filter := fmt.Sprintf("Meta[%q] == %q and Meta[%q] == %q ",
			connectinject.MetaKeyPodName, c.flagPodName, connectinject.MetaKeyKubeNS, c.flagPodNamespace)
		if c.flagMultiPort && c.flagServiceName != "" {
			// If the service name is set and this is a multi-port pod there may be multiple services registered for
			// this one Pod. If so, we want to ensure the service and proxy matching our expected name is registered.
			filter += fmt.Sprintf(` and (Service == %q or Service == "%s-sidecar-proxy")`, c.flagServiceName, c.flagServiceName)
		}
		serviceList, err := consulClient.Agent().ServicesWithFilter(filter)
		if err != nil {
			c.logger.Error("Unable to get Agent services", "error", err)
			return err
		}
		// Wait for the service and the connect-proxy service to be registered.
		if len(serviceList) != 2 {
			c.logger.Info("Unable to find registered services; retrying")
			// Once every 10 times we're going to print this informational message to the pod logs so that
			// it is not "lost" to the user at the end of the retries when the pod enters a CrashLoop.
			if registrationRetryCount%10 == 0 {
				c.logger.Info("Check to ensure a Kubernetes service has been created for this application." +
					" If your pod is not starting also check the connect-inject deployment logs.")
			}
			if len(serviceList) > 2 {
				c.logger.Error("There are multiple Consul services registered for this pod when there must only be one." +
					" Check if there are multiple Kubernetes services selecting this pod and add the label" +
					" `consul.hashicorp.com/service-ignore: \"true\"` to all services except the one used by Consul for handling requests.")
			}

			return fmt.Errorf("did not find correct number of services, found: %d, services: %+v", len(serviceList), serviceList)
		}
		for _, svc := range serviceList {
			c.logger.Info("Registered service has been detected", "service", svc.Service)
			if c.flagACLAuthMethod != "" {
				if c.flagServiceName != "" && c.flagServiceAccountName != c.flagServiceName {
					// Set the error but return nil so we don't retry.
					errServiceNameMismatch = fmt.Errorf("service account name %s doesn't match annotation service name %s", c.flagServiceAccountName, c.flagServiceName)
					return nil
				}

				if c.flagServiceName == "" && svc.Kind != api.ServiceKindConnectProxy && c.flagServiceAccountName != svc.Service {
					// Set the error but return nil so we don't retry.
					errServiceNameMismatch = fmt.Errorf("service account name %s doesn't match Consul service name %s", c.flagServiceAccountName, svc.Service)
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
			return fmt.Errorf("unable to find registered connect-proxy service")
		}
		return nil
	}, backoff.WithMaxRetries(backoff.NewConstantBackOff(1*time.Second), c.serviceRegistrationPollingAttempts))
	if err != nil {
		c.logger.Error("Timed out waiting for service registration", "error", err)
		return 1
	}
	if errServiceNameMismatch != nil {
		c.logger.Error(errServiceNameMismatch.Error())
		return 1
	}
	// Write the proxy ID to the shared volume so `consul connect envoy` can use it for bootstrapping.
	err = common.WriteFileWithPerms(c.flagProxyIDFile, proxyID, os.FileMode(0444))
	if err != nil {
		c.logger.Error("Unable to write proxy ID to file", "error", err)
		return 1
	}
	c.logger.Info("Connect initialization completed")
	return 0
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
