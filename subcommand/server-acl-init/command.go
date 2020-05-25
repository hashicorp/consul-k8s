package serveraclinit

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"sync"
	"time"

	godiscover "github.com/hashicorp/consul-k8s/helper/go-discover"
	"github.com/hashicorp/consul-k8s/subcommand"
	"github.com/hashicorp/consul-k8s/subcommand/common"
	k8sflags "github.com/hashicorp/consul-k8s/subcommand/flags"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/command/flags"
	"github.com/hashicorp/go-discover"
	"github.com/hashicorp/go-hclog"
	"github.com/mitchellh/cli"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type Command struct {
	UI cli.Ui

	flags *flag.FlagSet
	k8s   *k8sflags.K8SFlags

	flagResourcePrefix string
	flagK8sNamespace   string

	flagAllowDNS bool

	flagCreateClientToken bool

	flagCreateSyncToken bool

	flagCreateInjectToken      bool
	flagCreateInjectAuthMethod bool
	flagInjectAuthMethodHost   string
	flagBindingRuleSelector    string

	flagCreateEntLicenseToken bool

	flagCreateSnapshotAgentToken bool

	flagCreateMeshGatewayToken bool

	// Flags to configure Consul connection
	flagServerAddresses     []string
	flagServerPort          uint
	flagConsulCACert        string
	flagConsulTLSServerName string
	flagUseHTTPS            bool

	// Flags for ACL replication
	flagCreateACLReplicationToken bool
	flagACLReplicationTokenFile   string

	// Flags to support namespaces
	flagEnableNamespaces                 bool   // Use namespacing on all components
	flagConsulSyncDestinationNamespace   string // Consul namespace to register all catalog sync services into if not mirroring
	flagEnableSyncK8SNSMirroring         bool   // Enables mirroring of k8s namespaces into Consul for catalog sync
	flagSyncK8SNSMirroringPrefix         string // Prefix added to Consul namespaces created when mirroring catalog sync services
	flagConsulInjectDestinationNamespace string // Consul namespace to register all injected services into if not mirroring
	flagEnableInjectK8SNSMirroring       bool   // Enables mirroring of k8s namespaces into Consul for Connect inject
	flagInjectK8SNSMirroringPrefix       string // Prefix added to Consul namespaces created when mirroring injected services

	// Flag to support a custom bootstrap token
	flagBootstrapTokenFile string

	flagLogLevel string
	flagTimeout  time.Duration

	clientset kubernetes.Interface

	// cmdTimeout is cancelled when the command timeout is reached.
	cmdTimeout    context.Context
	retryDuration time.Duration

	// log
	log hclog.Logger

	once sync.Once
	help string

	providers map[string]discover.Provider
}

func (c *Command) init() {
	c.flags = flag.NewFlagSet("", flag.ContinueOnError)
	c.flags.StringVar(&c.flagResourcePrefix, "resource-prefix", "",
		"Prefix to use for Kubernetes resources.")
	c.flags.StringVar(&c.flagK8sNamespace, "k8s-namespace", "",
		"Name of Kubernetes namespace where Consul and consul-k8s components are deployed.")

	c.flags.BoolVar(&c.flagAllowDNS, "allow-dns", false,
		"Toggle for updating the anonymous token to allow DNS queries to work")
	c.flags.BoolVar(&c.flagCreateClientToken, "create-client-token", true,
		"Toggle for creating a client agent token. Default is true.")
	c.flags.BoolVar(&c.flagCreateSyncToken, "create-sync-token", false,
		"Toggle for creating a catalog sync token.")

	c.flags.BoolVar(&c.flagCreateInjectToken, "create-inject-namespace-token", false,
		"Toggle for creating a connect injector token. Only required when namespaces are enabled.")
	c.flags.BoolVar(&c.flagCreateInjectAuthMethod, "create-inject-auth-method", false,
		"Toggle for creating a connect inject auth method.")
	c.flags.BoolVar(&c.flagCreateInjectAuthMethod, "create-inject-token", false,
		"Toggle for creating a connect inject auth method. Deprecated: use -create-inject-auth-method instead.")
	c.flags.StringVar(&c.flagInjectAuthMethodHost, "inject-auth-method-host", "",
		"Kubernetes Host config parameter for the auth method."+
			"If not provided, the default cluster Kubernetes service will be used.")
	c.flags.StringVar(&c.flagBindingRuleSelector, "acl-binding-rule-selector", "",
		"Selector string for connectInject ACL Binding Rule.")

	c.flags.BoolVar(&c.flagCreateEntLicenseToken, "create-enterprise-license-token", false,
		"Toggle for creating a token for the enterprise license job.")
	c.flags.BoolVar(&c.flagCreateSnapshotAgentToken, "create-snapshot-agent-token", false,
		"[Enterprise Only] Toggle for creating a token for the Consul snapshot agent deployment.")
	c.flags.BoolVar(&c.flagCreateMeshGatewayToken, "create-mesh-gateway-token", false,
		"Toggle for creating a token for a Connect mesh gateway.")

	c.flags.Var((*flags.AppendSliceValue)(&c.flagServerAddresses), "server-address",
		"The IP, DNS name or the cloud auto-join string of the Consul server(s). If providing IPs or DNS names, may be specified multiple times."+
			"At least one value is required.")
	c.flags.UintVar(&c.flagServerPort, "server-port", 8500, "The HTTP or HTTPS port of the Consul server. Defaults to 8500.")
	c.flags.StringVar(&c.flagConsulCACert, "consul-ca-cert", "",
		"Path to the PEM-encoded CA certificate of the Consul cluster.")
	c.flags.StringVar(&c.flagConsulTLSServerName, "consul-tls-server-name", "",
		"The server name to set as the SNI header when sending HTTPS requests to Consul.")
	c.flags.BoolVar(&c.flagUseHTTPS, "use-https", false,
		"Toggle for using HTTPS for all API calls to Consul.")

	c.flags.BoolVar(&c.flagEnableNamespaces, "enable-namespaces", false,
		"[Enterprise Only] Enables namespaces, in either a single Consul namespace or mirrored [Enterprise only feature]")
	c.flags.StringVar(&c.flagConsulSyncDestinationNamespace, "consul-sync-destination-namespace", "default",
		"[Enterprise Only] Indicates which Consul namespace that catalog sync will register services into. If "+
			"'-enable-sync-k8s-namespace-mirroring' is true, this is not used.")
	c.flags.BoolVar(&c.flagEnableSyncK8SNSMirroring, "enable-sync-k8s-namespace-mirroring", false, "[Enterprise Only] "+
		"Indicates that namespace mirroring will be used for catalog sync services.")
	c.flags.StringVar(&c.flagSyncK8SNSMirroringPrefix, "sync-k8s-namespace-mirroring-prefix", "",
		"[Enterprise Only] Prefix that will be added to all k8s namespaces mirrored into Consul by catalog sync "+
			"if mirroring is enabled.")
	c.flags.StringVar(&c.flagConsulInjectDestinationNamespace, "consul-inject-destination-namespace", "default",
		"[Enterprise Only] Indicates which Consul namespace that the Connect injector will register services into. If "+
			"'-enable-inject-k8s-namespace-mirroring' is true, this is not used.")
	c.flags.BoolVar(&c.flagEnableInjectK8SNSMirroring, "enable-inject-k8s-namespace-mirroring", false, "[Enterprise Only] "+
		"Indicates that namespace mirroring will be used for Connect inject services.")
	c.flags.StringVar(&c.flagInjectK8SNSMirroringPrefix, "inject-k8s-namespace-mirroring-prefix", "",
		"[Enterprise Only] Prefix that will be added to all k8s namespaces mirrored into Consul by Connect inject "+
			"if mirroring is enabled.")

	c.flags.BoolVar(&c.flagCreateACLReplicationToken, "create-acl-replication-token", false,
		"Toggle for creating a token for ACL replication between datacenters.")
	c.flags.StringVar(&c.flagACLReplicationTokenFile, "acl-replication-token-file", "",
		"Path to file containing ACL token to be used for ACL replication. If set, ACL replication is enabled.")

	c.flags.StringVar(&c.flagBootstrapTokenFile, "bootstrap-token-file", "",
		"Path to file containing ACL token for creating policies and tokens. This token must have 'acl:write' permissions."+
			"When provided, servers will not be bootstrapped and their policies and tokens will not be updated.")

	c.flags.DurationVar(&c.flagTimeout, "timeout", 10*time.Minute,
		"How long we'll try to bootstrap ACLs for before timing out, e.g. 1ms, 2s, 3m")
	c.flags.StringVar(&c.flagLogLevel, "log-level", "info",
		"Log verbosity level. Supported values (in order of detail) are \"trace\", "+
			"\"debug\", \"info\", \"warn\", and \"error\".")

	c.k8s = &k8sflags.K8SFlags{}
	flags.Merge(c.flags, c.k8s.Flags())
	c.help = flags.Usage(help, c.flags)

	// Default retry to 1s. This is exposed for setting in tests.
	if c.retryDuration == 0 {
		c.retryDuration = 1 * time.Second
	}
}

func (c *Command) Synopsis() string { return synopsis }
func (c *Command) Help() string {
	c.once.Do(c.init)
	return c.help
}

// Run bootstraps ACLs on Consul servers and writes the bootstrap token to a
// Kubernetes secret.
// Given various flags, it will also create policies and associated ACL tokens
// and store the tokens as Kubernetes Secrets.
// The function will retry its tasks indefinitely until they are complete.
func (c *Command) Run(args []string) int {
	c.once.Do(c.init)
	if err := c.flags.Parse(args); err != nil {
		return 1
	}
	if len(c.flags.Args()) > 0 {
		c.UI.Error("Should have no non-flag arguments.")
		return 1
	}
	if len(c.flagServerAddresses) == 0 {
		c.UI.Error("-server-address must be set at least once")
		return 1
	}
	if c.flagResourcePrefix == "" {
		c.UI.Error("-resource-prefix must be set")
		return 1
	}

	var aclReplicationToken string
	if c.flagACLReplicationTokenFile != "" {
		// Load the ACL replication token from file.
		tokenBytes, err := ioutil.ReadFile(c.flagACLReplicationTokenFile)
		if err != nil {
			c.UI.Error(fmt.Sprintf("Unable to read ACL replication token from file %q: %s", c.flagACLReplicationTokenFile, err))
			return 1
		}
		if len(tokenBytes) == 0 {
			c.UI.Error(fmt.Sprintf("ACL replication token file %q is empty", c.flagACLReplicationTokenFile))
			return 1
		}
		aclReplicationToken = strings.TrimSpace(string(tokenBytes))
	}

	var providedBootstrapToken string
	if c.flagBootstrapTokenFile != "" {
		// Load the bootstrap token from file.
		tokenBytes, err := ioutil.ReadFile(c.flagBootstrapTokenFile)
		if err != nil {
			c.UI.Error(fmt.Sprintf("Unable to read bootstrap token from file %q: %s", c.flagBootstrapTokenFile, err))
			return 1
		}
		if len(tokenBytes) == 0 {
			c.UI.Error(fmt.Sprintf("Bootstrap token file %q is empty", c.flagBootstrapTokenFile))
			return 1
		}
		providedBootstrapToken = strings.TrimSpace(string(tokenBytes))
	}

	var cancel context.CancelFunc
	c.cmdTimeout, cancel = context.WithTimeout(context.Background(), c.flagTimeout)
	// The context will only ever be intentionally ended by the timeout.
	defer cancel()

	// Configure our logger
	level := hclog.LevelFromString(c.flagLogLevel)
	if level == hclog.NoLevel {
		c.UI.Error(fmt.Sprintf("Unknown log level: %s", c.flagLogLevel))
		return 1
	}
	c.log = hclog.New(&hclog.LoggerOptions{
		Level:  level,
		Output: os.Stderr,
	})

	serverAddresses := c.flagServerAddresses
	// Check if the provided addresses contain a cloud-auto join string.
	// If yes, call godiscover to discover addresses of the Consul servers.
	if len(c.flagServerAddresses) == 1 && strings.Contains(c.flagServerAddresses[0], "provider=") {
		var err error
		serverAddresses, err = godiscover.ConsulServerAddresses(c.flagServerAddresses[0], c.providers, c.log)
		if err != nil {
			c.UI.Error(fmt.Sprintf("Unable to discover any Consul addresses from %q: %s", c.flagServerAddresses[0], err))
			return 1
		}
	}

	// The ClientSet might already be set if we're in a test.
	if c.clientset == nil {
		if err := c.configureKubeClient(); err != nil {
			c.log.Error(err.Error())
			return 1
		}
	}

	scheme := "http"
	if c.flagUseHTTPS {
		scheme = "https"
	}

	var updateServerPolicy bool
	var bootstrapToken string

	if c.flagBootstrapTokenFile != "" {
		// If bootstrap token is provided, we skip server bootstrapping and use
		// the provided token to create policies and tokens for the rest of the components.
		c.log.Info("Bootstrap token is provided so skipping Consul server ACL bootstrapping")
		bootstrapToken = providedBootstrapToken
	} else if c.flagACLReplicationTokenFile != "" {
		// If ACL replication is enabled, we don't need to ACL bootstrap the servers
		// since they will be performing replication.
		// We can use the replication token as our bootstrap token because it
		// has permissions to create policies and tokens.
		c.log.Info("ACL replication is enabled so skipping Consul server ACL bootstrapping")
		bootstrapToken = aclReplicationToken
	} else {
		// Check if we've already been bootstrapped.
		var err error
		bootTokenSecretName := c.withPrefix("bootstrap-acl-token")
		bootstrapToken, err = c.getBootstrapToken(bootTokenSecretName)
		if err != nil {
			c.log.Error(fmt.Sprintf("Unexpected error looking for preexisting bootstrap Secret: %s", err))
			return 1
		}

		if bootstrapToken != "" {
			c.log.Info(fmt.Sprintf("ACLs already bootstrapped - retrieved bootstrap token from Secret %q", bootTokenSecretName))

			// Mark that we should update the server ACL policy in case
			// there are namespace related config changes. Because of the
			// organization of the server token creation code, the policy
			// otherwise won't be updated.
			updateServerPolicy = true
		} else {
			c.log.Info("No bootstrap token from previous installation found, continuing on to bootstrapping")
			bootstrapToken, err = c.bootstrapServers(serverAddresses, bootTokenSecretName, scheme)
			if err != nil {
				c.log.Error(err.Error())
				return 1
			}
		}
	}

	// For all of the next operations we'll need a Consul client.
	serverAddr := fmt.Sprintf("%s:%d", serverAddresses[0], c.flagServerPort)
	consulClient, err := api.NewClient(&api.Config{
		Address: serverAddr,
		Scheme:  scheme,
		Token:   bootstrapToken,
		TLSConfig: api.TLSConfig{
			Address: c.flagConsulTLSServerName,
			CAFile:  c.flagConsulCACert,
		},
	})
	if err != nil {
		c.log.Error(fmt.Sprintf("Error creating Consul client for addr %q: %s", serverAddr, err))
		return 1
	}

	consulDC, err := c.consulDatacenter(consulClient)
	if err != nil {
		c.log.Error("Error getting datacenter name", "err", err)
		return 1
	}
	c.log.Info("Current datacenter", "datacenter", consulDC)

	// With the addition of namespaces, the ACL policies associated
	// with the server tokens may need to be updated if Enterprise Consul
	// users upgrade to 1.7+. This updates the policy if the bootstrap
	// token had previously existed, which signals a potential config change.
	if updateServerPolicy {
		_, err = c.setServerPolicy(consulClient)
		if err != nil {
			c.log.Error("Error updating the server ACL policy", "err", err)
			return 1
		}
	}

	// If namespaces are enabled, to allow cross-Consul-namespace permissions
	// for services from k8s, the Consul `default` namespace needs a policy
	// allowing service discovery in all namespaces. Each namespace that is
	// created by consul-k8s components (this bootstrapper, catalog sync or
	// connect inject) needs to reference this policy on namespace creation
	// to finish the cross namespace permission setup.
	if c.flagEnableNamespaces {
		policyTmpl := api.ACLPolicy{
			Name:        "cross-namespace-policy",
			Description: "Policy to allow permissions to cross Consul namespaces for k8s services",
			Rules:       crossNamespaceRules,
		}
		err := c.untilSucceeds(fmt.Sprintf("creating %s policy", policyTmpl.Name),
			func() error {
				return c.createOrUpdateACLPolicy(policyTmpl, consulClient)
			})
		if err != nil {
			c.log.Error("Error creating or updating the cross namespace policy", "err", err)
			return 1
		}

		// Apply this to the PolicyDefaults for the Consul `default` namespace
		aclConfig := api.NamespaceACLConfig{
			PolicyDefaults: []api.ACLLink{
				{Name: policyTmpl.Name},
			},
		}
		consulNamespace := api.Namespace{
			Name: "default",
			ACLs: &aclConfig,
		}
		_, _, err = consulClient.Namespaces().Update(&consulNamespace, &api.WriteOptions{})
		if err != nil {
			c.log.Error("Error updating the default namespace to include the cross namespace policy", "err", err)
			return 1
		}
	}

	if c.flagCreateClientToken {
		agentRules, err := c.agentRules()
		if err != nil {
			c.log.Error("Error templating client agent rules", "err", err)
			return 1
		}

		err = c.createLocalACL("client", agentRules, consulDC, consulClient)
		if err != nil {
			c.log.Error(err.Error())
			return 1
		}
	}

	if c.createAnonymousPolicy() {
		err := c.configureAnonymousPolicy(consulClient)
		if err != nil {
			c.log.Error(err.Error())
			return 1
		}
	}

	if c.flagCreateSyncToken {
		syncRules, err := c.syncRules()
		if err != nil {
			c.log.Error("Error templating sync rules", "err", err)
			return 1
		}

		// If namespaces are enabled, the policy and token needs to be global
		// to be allowed to create namespaces.
		if c.flagEnableNamespaces {
			err = c.createGlobalACL("catalog-sync", syncRules, consulDC, consulClient)
		} else {
			err = c.createLocalACL("catalog-sync", syncRules, consulDC, consulClient)
		}
		if err != nil {
			c.log.Error(err.Error())
			return 1
		}
	}

	if c.flagCreateInjectToken {
		injectRules, err := c.injectRules()
		if err != nil {
			c.log.Error("Error templating inject rules", "err", err)
			return 1
		}

		// If namespaces are enabled, the policy and token needs to be global
		// to be allowed to create namespaces.
		if c.flagEnableNamespaces {
			err = c.createGlobalACL("connect-inject", injectRules, consulDC, consulClient)
		} else {
			err = c.createLocalACL("connect-inject", injectRules, consulDC, consulClient)
		}

		if err != nil {
			c.log.Error(err.Error())
			return 1
		}
	}

	if c.flagCreateEntLicenseToken {
		err := c.createLocalACL("enterprise-license", entLicenseRules, consulDC, consulClient)
		if err != nil {
			c.log.Error(err.Error())
			return 1
		}
	}

	if c.flagCreateSnapshotAgentToken {
		err := c.createLocalACL("client-snapshot-agent", snapshotAgentRules, consulDC, consulClient)
		if err != nil {
			c.log.Error(err.Error())
			return 1
		}
	}

	if c.flagCreateMeshGatewayToken {
		meshGatewayRules, err := c.meshGatewayRules()
		if err != nil {
			c.log.Error("Error templating dns rules", "err", err)
			return 1
		}

		// Mesh gateways require a global policy/token because they must
		// discover services in other datacenters.
		err = c.createGlobalACL("mesh-gateway", meshGatewayRules, consulDC, consulClient)
		if err != nil {
			c.log.Error(err.Error())
			return 1
		}
	}

	if c.flagCreateInjectAuthMethod {
		err := c.configureConnectInject(consulClient)
		if err != nil {
			c.log.Error(err.Error())
			return 1
		}
	}

	if c.flagCreateACLReplicationToken {
		rules, err := c.aclReplicationRules()
		if err != nil {
			c.log.Error("Error templating acl replication token rules", "err", err)
			return 1
		}
		// Policy must be global because it replicates from the primary DC
		// and so the primary DC needs to be able to accept the token.
		err = c.createGlobalACL(common.ACLReplicationTokenName, rules, consulDC, consulClient)
		if err != nil {
			c.log.Error(err.Error())
			return 1
		}
	}

	c.log.Info("server-acl-init completed successfully")
	return 0
}

// getBootstrapToken returns the existing bootstrap token if there is one by
// reading the Kubernetes Secret with name secretName.
// If there is no bootstrap token yet, then it returns an empty string (not an error).
func (c *Command) getBootstrapToken(secretName string) (string, error) {
	secret, err := c.clientset.CoreV1().Secrets(c.flagK8sNamespace).Get(secretName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return "", nil
		}
		return "", err
	}
	token, ok := secret.Data[common.ACLTokenSecretKey]
	if !ok {
		return "", fmt.Errorf("secret %q does not have data key 'token'", secretName)
	}
	return string(token), nil
}

func (c *Command) configureKubeClient() error {
	config, err := subcommand.K8SConfig(c.k8s.KubeConfig())
	if err != nil {
		return fmt.Errorf("error retrieving Kubernetes auth: %s", err)
	}
	c.clientset, err = kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("error initializing Kubernetes client: %s", err)
	}
	return nil
}

// untilSucceeds runs op until it returns a nil error.
// If c.cmdTimeout is cancelled it will exit.
func (c *Command) untilSucceeds(opName string, op func() error) error {
	for {
		err := op()
		if err == nil {
			c.log.Info(fmt.Sprintf("Success: %s", opName))
			break
		}
		c.log.Error(fmt.Sprintf("Failure: %s", opName), "err", err)
		c.log.Info("Retrying in " + c.retryDuration.String())
		// Wait on either the retry duration (in which case we continue) or the
		// overall command timeout.
		select {
		case <-time.After(c.retryDuration):
			continue
		case <-c.cmdTimeout.Done():
			return errors.New("reached command timeout")
		}
	}
	return nil
}

// withPrefix returns the name of resource with the correct prefix based
// on the -resource-prefix flag.
func (c *Command) withPrefix(resource string) string {
	return fmt.Sprintf("%s-%s", c.flagResourcePrefix, resource)
}

const synopsis = "Initialize ACLs on Consul servers and other components."
const help = `
Usage: consul-k8s server-acl-init [options]

  Bootstraps servers with ACLs and creates policies and ACL tokens for other
  components as Kubernetes Secrets.
  It will run indefinitely until all tokens have been created. It is idempotent
  and safe to run multiple times.

`

// consulDatacenter returns the current datacenter name using the
// /agent/self API endpoint.
func (c *Command) consulDatacenter(client *api.Client) (string, error) {
	var agentCfg map[string]map[string]interface{}
	err := c.untilSucceeds("calling /agent/self to get datacenter",
		func() error {
			var opErr error
			agentCfg, opErr = client.Agent().Self()
			return opErr
		})
	if err != nil {
		return "", err
	}
	if _, ok := agentCfg["Config"]; !ok {
		return "", fmt.Errorf("/agent/self response did not contain Config key: %s", agentCfg)
	}
	if _, ok := agentCfg["Config"]["Datacenter"]; !ok {
		return "", fmt.Errorf("/agent/self response did not contain Config.Datacenter key: %s", agentCfg)
	}
	dc, ok := agentCfg["Config"]["Datacenter"].(string)
	if !ok {
		return "", fmt.Errorf("could not cast Config.Datacenter as string: %s", agentCfg)
	}
	if dc == "" {
		return "", fmt.Errorf("value of Config.Datacenter was empty string: %s", agentCfg)
	}
	return dc, nil
}

// createAnonymousPolicy returns whether we should create a policy for the
// anonymous ACL token, i.e. queries without ACL tokens.
func (c *Command) createAnonymousPolicy() bool {
	// If c.flagACLReplicationTokenFile is set then we're in a secondary DC.
	// In this case we assume that the primary datacenter has already created
	// the anonymous policy and attached it to the anonymous token.
	// We don't want to modify the anonymous policy in secondary datacenters
	// because it is global and we can't create separate tokens for each
	// secondary datacenter because the anonymous token is global.
	return c.flagACLReplicationTokenFile == "" &&
		// Consul DNS requires the anonymous policy because DNS queries don't
		// have ACL tokens.
		(c.flagAllowDNS ||
			// If the connect auth method and ACL replication token are being
			// created then we know we're using multi-dc Connect.
			// In this case the anonymous policy is required because Connect
			// services in Kubernetes have local tokens which are stripped
			// on cross-dc API calls. The cross-dc API calls thus use the anonymous
			// token. Cross-dc API calls are needed by the Connect proxies to talk
			// cross-dc.
			(c.flagCreateInjectAuthMethod && c.flagCreateACLReplicationToken))
}
