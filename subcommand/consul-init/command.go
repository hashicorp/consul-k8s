package consulinit

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/hashicorp/consul-k8s/consul"
	"github.com/hashicorp/consul-k8s/subcommand/common"
	"github.com/hashicorp/consul-k8s/subcommand/flags"
	"github.com/hashicorp/consul/api"
	"github.com/mitchellh/cli"
	"k8s.io/client-go/kubernetes"
)

const bearerTokenFile = "/var/run/secrets/kubernetes.io/serviceaccount/token"
const tokenSinkFile = "/consul/connect-inject/acl-token"

type Command struct {
	UI cli.Ui

	flagCertFile      string // TLS cert for listening (PEM)
	flagKeyFile       string // TLS cert private key (PEM)
	flagACLAuthMethod string // Auth Method to use for ACLs, if enabled
	flagConsulCACert  string // [Deprecated] Path to CA Certificate to use when communicating with Consul clients
	flagLogLevel      string

	// Flags to support namespaces
	flagEnableNamespaces           bool     // Use namespacing on all components
	flagConsulDestinationNamespace string   // Consul namespace to register everything if not mirroring
	flagAllowK8sNamespacesList     []string // K8s namespaces to explicitly inject
	flagDenyK8sNamespacesList      []string // K8s namespaces to deny injection (has precedence)
	flagEnableK8SNSMirroring       bool     // Enables mirroring of k8s namespaces into Consul
	flagK8SNSMirroringPrefix       string   // Prefix added to Consul namespaces created when mirroring
	flagCrossNamespaceACLPolicy    string   // The name of the ACL policy to add to every created namespace if ACLs are enabled

	// TODO: fix comments
	flagMeta            string // flag for metadata to consul login
	flagBearerTokenFile string // flag for metadata to consul login
	flagTokenSinkFile   string // flag for metadata to consul login
	numACLLoginRetries  int    // number of times to attempt to retry ACL().Login()

	flagSet *flag.FlagSet
	http    *flags.HTTPFlags

	consulClient *api.Client
	clientset    kubernetes.Interface

	sigCh chan os.Signal
	once  sync.Once
	help  string
	cert  atomic.Value
}

func (c *Command) init() {
	c.flagSet = flag.NewFlagSet("", flag.ContinueOnError)
	c.flagSet.StringVar(&c.flagConsulCACert, "consul-ca-cert", "",
		"[Deprecated] Please use '-ca-file' flag instead. Path to CA certificate to use if communicating with Consul clients over HTTPS.")
	c.flagSet.BoolVar(&c.flagEnableNamespaces, "enable-namespaces", false,
		"[Enterprise Only] Enables namespaces, in either a single Consul namespace or mirrored.")
	c.flagSet.StringVar(&c.flagConsulDestinationNamespace, "consul-destination-namespace", "default",
		"[Enterprise Only] Defines which Consul namespace to register all injected services into. If '-enable-k8s-namespace-mirroring' "+
			"is true, this is not used.")
	c.flagSet.BoolVar(&c.flagEnableK8SNSMirroring, "enable-k8s-namespace-mirroring", false, "[Enterprise Only] Enables "+
		"k8s namespace mirroring.")
	c.flagSet.StringVar(&c.flagK8SNSMirroringPrefix, "k8s-namespace-mirroring-prefix", "",
		"[Enterprise Only] Prefix that will be added to all k8s namespaces mirrored into Consul if mirroring is enabled.")
	c.flagSet.StringVar(&c.flagCrossNamespaceACLPolicy, "consul-cross-namespace-acl-policy", "",
		"[Enterprise Only] Name of the ACL policy to attach to all created Consul namespaces to allow service "+
			"discovery across Consul namespaces. Only necessary if ACLs are enabled.")
	c.flagSet.StringVar(&c.flagLogLevel, "log-level", "info",
		"Log verbosity level. Supported values (in order of detail) are \"trace\", "+
			"\"debug\", \"info\", \"warn\", and \"error\".")

	c.flagSet.StringVar(&c.flagACLAuthMethod, "method", "",
		"The name of the Kubernetes Auth Method to use for connectInjection if ACLs are enabled.")
	c.flagSet.StringVar(&c.flagMeta, "meta", "",
		"Metadata to be passed to consul login api.")

	c.flagSet.StringVar(&c.flagBearerTokenFile, "bearer-token-file", bearerTokenFile,
		"Token file to be used for consul login api.")
	c.flagSet.StringVar(&c.flagTokenSinkFile, "token-sink-file", tokenSinkFile,
		"Token sink file to be used for consul login api.")

	c.http = &flags.HTTPFlags{}

	flags.Merge(c.flagSet, c.http.Flags())
	c.help = flags.Usage(help, c.flagSet)
	c.numACLLoginRetries = 3

	// Wait on an interrupt or terminate for exit, be sure to init it before running
	// the controller so that we don't receive an interrupt before it's ready.
	if c.sigCh == nil {
		c.sigCh = make(chan os.Signal, 1)
		signal.Notify(c.sigCh, syscall.SIGINT, syscall.SIGTERM)
	}
}

func (c *Command) Run(args []string) int {
	c.once.Do(c.init)
	var err error
	if err := c.flagSet.Parse(args); err != nil {
		return 1
	}

	// Validate flags.
	if c.flagACLAuthMethod == "" {
		c.UI.Error("-method must be set")
		return 1
	}
	if c.flagMeta == "" {
		c.UI.Error("-meta must be set")
		return 1
	}
	if c.flagBearerTokenFile == "" {
		c.UI.Error("-bearer-token-file must be set")
		return 1
	}
	if c.flagTokenSinkFile == "" {
		c.UI.Error("-token-sink-file must be set")
		return 1
	}

	// TODO: Add namespace support
	// create Consul API config object
	if c.consulClient == nil {
		cfg := api.DefaultConfig()
		c.http.MergeOntoConfig(cfg)
		c.consulClient, err = consul.NewClient(cfg)
		if err != nil {
			c.UI.Error("unable to get client connection")
			return 1
		}
	}
	// TODO: this is hard coded :(
	meta := map[string]string{"pod": strings.Split(c.flagMeta, "=")[1]}
	retries := 0
	for {
		err = common.ConsulLogin(c.consulClient, c.flagBearerTokenFile, c.flagACLAuthMethod, c.flagTokenSinkFile, meta)
		if err != nil {
			retries++
			time.Sleep(time.Second * 1)
			if retries == c.numACLLoginRetries {
				c.UI.Error(fmt.Sprintf("unable to do consul login"))
				return 1
			}
		} else {
			c.UI.Info("consul login complete")
			return 0
		}
	}
}

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

const synopsis = "Inject Connect proxy sidecar."
const help = `
Usage: consul-k8s inject-connect [options]

  Run the admission webhook server for injecting the Consul Connect
  proxy sidecar. The sidecar uses Envoy by default.

`
