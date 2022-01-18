package aclinit

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/common"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/flags"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/go-hclog"
	"github.com/mitchellh/cli"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	defaultBearerTokenFile = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	defaultTokenSinkFile   = "/consul/connect-inject/acl-token"

	// The number of times to attempt ACL Login.
	numLoginRetries = 300

	raftReplicationTimeout   = 2 * time.Second
	tokenReadPollingInterval = 100 * time.Millisecond
)

type Command struct {
	UI cli.Ui

	flags *flag.FlagSet
	k8s   *flags.K8SFlags
	http  *flags.HTTPFlags

	flagSecretName    string
	flagInitType      string
	flagNamespace     string
	flagACLDir        string
	flagTokenSinkFile string

	flagACLAuthMethod string // Auth Method to use for ACLs, if enabled.
	flagLogLevel      string
	flagLogJSON       bool

	// Flags to support partitions.
	flagEnablePartitions bool   // true if Admin Partitions are enabled
	flagPartitionName    string // name of the Admin Partition

	bearerTokenFile string // Location of the bearer token. Default is /var/run/secrets/kubernetes.io/serviceaccount/token.
	tokenSinkFile   string // Location to write the output token. Default is defaultTokenSinkFile.

	k8sClient kubernetes.Interface

	once   sync.Once
	help   string
	logger hclog.Logger

	ctx          context.Context
	consulClient *api.Client
}

func (c *Command) init() {
	c.flags = flag.NewFlagSet("", flag.ContinueOnError)

	c.flags.StringVar(&c.flagSecretName, "secret-name", "",
		"Name of secret to watch for an ACL token")
	c.flags.StringVar(&c.flagInitType, "init-type", "",
		"ACL init type. The only supported value is 'client'. If set to 'client' will write Consul client ACL config to an acl-config.json file in -acl-dir")
	c.flags.StringVar(&c.flagNamespace, "k8s-namespace", "",
		"Name of Kubernetes namespace where the servers are deployed")
	c.flags.StringVar(&c.flagACLDir, "acl-dir", "/consul/aclconfig",
		"Directory name of shared volume where client acl config file acl-config.json will be written if -init-type=client")
	c.flags.StringVar(&c.flagTokenSinkFile, "token-sink-file", "",
		"Optional filepath to write acl token")

	// Flags related to using consul login to fetch the ACL token.
	c.flags.StringVar(&c.flagACLAuthMethod, "acl-auth-method", "", "Name of the auth method to login to.")
	c.flags.StringVar(&c.flagLogLevel, "log-level", "info",
		"Log verbosity level. Supported values (in order of detail) are \"trace\", "+
			"\"debug\", \"info\", \"warn\", and \"error\".")
	c.flags.BoolVar(&c.flagLogJSON, "log-json", false,
		"Enable or disable JSON output format for logging.")

	// Flags related to Partitions.
	c.flags.BoolVar(&c.flagEnablePartitions, "enable-partitions", false,
		"[Enterprise Only] Enables Admin Partitions")
	c.flags.StringVar(&c.flagPartitionName, "partition", "",
		"[Enterprise Only] Name of the Admin Partition")

	if c.bearerTokenFile == "" {
		c.bearerTokenFile = defaultBearerTokenFile
	}
	if c.tokenSinkFile == "" {
		c.tokenSinkFile = defaultTokenSinkFile
	}

	c.k8s = &flags.K8SFlags{}
	c.http = &flags.HTTPFlags{}
	flags.Merge(c.flags, c.k8s.Flags())
	c.help = flags.Usage(help, c.flags)
}

func (c *Command) Run(args []string) int {
	var err error
	c.once.Do(c.init)
	if err = c.flags.Parse(args); err != nil {
		return 1
	}
	if len(c.flags.Args()) > 0 {
		c.UI.Error("Should have no non-flag arguments.")
		return 1
	}

	if c.ctx == nil {
		c.ctx = context.Background()
	}

	// Create the Kubernetes clientset
	if c.k8sClient == nil {
		config, err := subcommand.K8SConfig(c.k8s.KubeConfig())
		if err != nil {
			c.UI.Error(fmt.Sprintf("Error retrieving Kubernetes auth: %s", err))
			return 1
		}
		c.k8sClient, err = kubernetes.NewForConfig(config)
		if err != nil {
			c.UI.Error(fmt.Sprintf("Error initializing Kubernetes client: %s", err))
			return 1
		}
	}

	// Set up logging.
	if c.logger == nil {
		c.logger, err = common.Logger(c.flagLogLevel, c.flagLogJSON)
		if err != nil {
			c.UI.Error(err.Error())
			return 1
		}
	}

	if c.flagACLAuthMethod != "" {
		cfg := api.DefaultConfig()
		if c.flagEnablePartitions {
			cfg.Partition = c.flagPartitionName
		}
		c.http.MergeOntoConfig(cfg)
		if c.consulClient == nil {
			c.consulClient, err = consul.NewClient(cfg)
			if err != nil {
				c.logger.Error("Unable to get client connection", "error", err)
				return 1
			}

		}
		err = backoff.Retry(func() error {
			//err := common.ConsulLogin(c.consulClient, c.bearerTokenFile, c.flagACLAuthMethod, c.tokenSinkFile, c.flagAuthMethodNamespace, map[string]string{})
			err := common.ConsulLogin(c.consulClient, c.bearerTokenFile, c.flagACLAuthMethod, c.tokenSinkFile, "", map[string]string{})
			if err != nil {
				c.logger.Error("Consul login failed; retrying", "error", err)
			}
			return err
		}, backoff.WithMaxRetries(backoff.NewConstantBackOff(1*time.Second), numLoginRetries))
		if err != nil {
			c.logger.Error("Hit maximum retries for consul login", "error", err)
			return 1
		}
		c.logger.Info("Consul login complete")
		// Now fetch the token that was just created so we can use it in subsequent client api calls.
		token, err := os.ReadFile(c.tokenSinkFile)
		if err != nil {
			c.logger.Error("Unable to read token sink file after login", "error", err)
			return 1
		}

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
			_, _, err := c.consulClient.ACL().TokenReadSelf(&api.QueryOptions{
				AllowStale: true,
				Token:      string(token),
			})
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
		return 0
	}
	// Check if the client secret exists yet
	// If not, wait until it does
	var secret string
	for {
		var err error
		secret, err = c.getSecret(c.flagSecretName)
		if err != nil {
			c.UI.Error(fmt.Sprintf("Error getting Kubernetes secret: %s", err))
		}
		if err == nil {
			break
		}
		time.Sleep(1 * time.Second)
	}

	if c.flagInitType == "client" {
		// Construct extra client config json with acl details
		// This will be mounted as a volume for the client to use
		var buf bytes.Buffer
		tpl := template.Must(template.New("root").Parse(strings.TrimSpace(clientACLConfigTpl)))
		err := tpl.Execute(&buf, secret)
		if err != nil {
			c.UI.Error(fmt.Sprintf("Error creating template: %s", err))
			return 1
		}

		// Write the data out as a file.
		// Must be 0644 because this is written by the consul-k8s user but needs
		// to be readable by the consul user.
		err = ioutil.WriteFile(filepath.Join(c.flagACLDir, "acl-config.json"), buf.Bytes(), 0644)
		if err != nil {
			c.UI.Error(fmt.Sprintf("Error writing config file: %s", err))
			return 1
		}
	}

	if c.flagTokenSinkFile != "" {
		// Must be 0600 in case this command is re-run. In that case we need
		// to have permissions to overwrite our file.
		err := ioutil.WriteFile(c.flagTokenSinkFile, []byte(secret), 0600)
		if err != nil {
			c.UI.Error(fmt.Sprintf("Error writing token to file %q: %s", c.flagTokenSinkFile, err))
			return 1
		}
	}

	return 0
}

func (c *Command) getSecret(secretName string) (string, error) {
	secret, err := c.k8sClient.CoreV1().Secrets(c.flagNamespace).Get(c.ctx, secretName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	// Extract token
	return string(secret.Data["token"]), nil
}

func (c *Command) Synopsis() string { return synopsis }
func (c *Command) Help() string {
	c.once.Do(c.init)
	return c.help
}

const synopsis = "Initialize ACLs on non-server components."
const help = `
Usage: consul-k8s-control-plane acl-init [options]

  Bootstraps non-server components with ACLs by waiting for a
  secret to be populated with an ACL token to be used.

`

const clientACLConfigTpl = `
{
  "acl": {
    "enabled": true,
    "default_policy": "deny",
    "down_policy": "extend-cache",
    "tokens": {
      "agent": "{{ . }}"
    }
  }
}
`
