package serveraclinit

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/hashicorp/consul-k8s/subcommand"
	k8sflags "github.com/hashicorp/consul-k8s/subcommand/flags"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/command/flags"
	"github.com/hashicorp/go-hclog"
	"github.com/mitchellh/cli"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type Command struct {
	UI cli.Ui

	flags                         *flag.FlagSet
	k8s                           *k8sflags.K8SFlags
	flagReleaseName               string
	flagServerLabelSelector       string
	flagResourcePrefix            string
	flagReplicas                  int
	flagK8sNamespace              string
	flagAllowDNS                  bool
	flagCreateClientToken         bool
	flagCreateSyncToken           bool
	flagCreateInjectToken         bool
	flagCreateInjectAuthMethod    bool
	flagBindingRuleSelector       string
	flagCreateEntLicenseToken     bool
	flagCreateSnapshotAgentToken  bool
	flagCreateMeshGatewayToken    bool
	flagCreateACLReplicationToken bool
	flagConsulCACert              string
	flagConsulTLSServerName       string
	flagUseHTTPS                  bool

	// Flags to support namespaces
	flagEnableNamespaces                 bool   // Use namespacing on all components
	flagConsulSyncDestinationNamespace   string // Consul namespace to register all catalog sync services into if not mirroring
	flagEnableSyncK8SNSMirroring         bool   // Enables mirroring of k8s namespaces into Consul for catalog sync
	flagSyncK8SNSMirroringPrefix         string // Prefix added to Consul namespaces created when mirroring catalog sync services
	flagConsulInjectDestinationNamespace string // Consul namespace to register all injected services into if not mirroring
	flagEnableInjectK8SNSMirroring       bool   // Enables mirroring of k8s namespaces into Consul for Connect inject
	flagInjectK8SNSMirroringPrefix       string // Prefix added to Consul namespaces created when mirroring injected services

	flagLogLevel string
	flagTimeout  string

	clientset kubernetes.Interface
	// cmdTimeout is cancelled when the command timeout is reached.
	cmdTimeout    context.Context
	retryDuration time.Duration

	// Log
	Log hclog.Logger

	once sync.Once
	help string
}

func (c *Command) init() {
	c.flags = flag.NewFlagSet("", flag.ContinueOnError)
	c.flags.StringVar(&c.flagReleaseName, "release-name", "",
		"Name of Consul Helm release. Deprecated: Use -server-label-selector=component=server,app=consul,release=<release-name> instead")
	c.flags.StringVar(&c.flagServerLabelSelector, "server-label-selector", "",
		"Selector (label query) to select Consul server statefulset pods, supports '=', '==', and '!='. (e.g. -l key1=value1,key2=value2)")
	c.flags.StringVar(&c.flagResourcePrefix, "resource-prefix", "",
		"Prefix to use for Kubernetes resources. If not set, the \"<release-name>-consul\" prefix is used, where <release-name> is the value set by the -release-name flag.")
	c.flags.IntVar(&c.flagReplicas, "expected-replicas", 1,
		"Number of expected Consul server replicas")
	c.flags.StringVar(&c.flagK8sNamespace, "k8s-namespace", "",
		"Name of Kubernetes namespace where the servers are deployed")
	c.flags.BoolVar(&c.flagAllowDNS, "allow-dns", false,
		"Toggle for updating the anonymous token to allow DNS queries to work")
	c.flags.BoolVar(&c.flagCreateClientToken, "create-client-token", true,
		"Toggle for creating a client agent token")
	c.flags.BoolVar(&c.flagCreateSyncToken, "create-sync-token", false,
		"Toggle for creating a catalog sync token")
	c.flags.BoolVar(&c.flagCreateInjectToken, "create-inject-namespace-token", false,
		"Toggle for creating a connect injector token. Only required when namespaces are enabled.")
	c.flags.BoolVar(&c.flagCreateInjectAuthMethod, "create-inject-auth-method", false,
		"Toggle for creating a connect inject auth method.")
	c.flags.BoolVar(&c.flagCreateInjectAuthMethod, "create-inject-token", false,
		"Toggle for creating a connect inject auth method. Deprecated: use -create-inject-auth-method instead.")
	c.flags.StringVar(&c.flagBindingRuleSelector, "acl-binding-rule-selector", "",
		"Selector string for connectInject ACL Binding Rule")
	c.flags.BoolVar(&c.flagCreateEntLicenseToken, "create-enterprise-license-token", false,
		"Toggle for creating a token for the enterprise license job")
	c.flags.BoolVar(&c.flagCreateSnapshotAgentToken, "create-snapshot-agent-token", false,
		"Toggle for creating a token for the Consul snapshot agent deployment (enterprise only)")
	c.flags.BoolVar(&c.flagCreateMeshGatewayToken, "create-mesh-gateway-token", false,
		"Toggle for creating a token for a Connect mesh gateway")
	c.flags.BoolVar(&c.flagCreateACLReplicationToken, "create-acl-replication-token", false,
		"Toggle for creating a token for ACL replication between datacenters")
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
	c.flags.StringVar(&c.flagTimeout, "timeout", "10m",
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
		c.UI.Error(fmt.Sprintf("Should have no non-flag arguments."))
		return 1
	}
	timeout, err := time.ParseDuration(c.flagTimeout)
	if err != nil {
		c.UI.Error(fmt.Sprintf("%q is not a valid timeout: %s", c.flagTimeout, err))
		return 1
	}
	if c.flagReleaseName != "" && c.flagServerLabelSelector != "" {
		c.UI.Error("-release-name and -server-label-selector cannot both be set")
		return 1
	}
	if c.flagServerLabelSelector != "" && c.flagResourcePrefix == "" {
		c.UI.Error("if -server-label-selector is set -resource-prefix must also be set")
		return 1
	}
	if c.flagReleaseName == "" && c.flagServerLabelSelector == "" {
		c.UI.Error("-release-name or -server-label-selector must be set")
		return 1
	}
	// If only the -release-name is set, we use it as the label selector.
	if c.flagReleaseName != "" {
		c.flagServerLabelSelector = fmt.Sprintf("app=consul,component=server,release=%s", c.flagReleaseName)
	}

	var cancel context.CancelFunc
	c.cmdTimeout, cancel = context.WithTimeout(context.Background(), timeout)
	// The context will only ever be intentionally ended by the timeout.
	defer cancel()

	// Configure our logger
	level := hclog.LevelFromString(c.flagLogLevel)
	if level == hclog.NoLevel {
		c.UI.Error(fmt.Sprintf("Unknown log level: %s", c.flagLogLevel))
		return 1
	}
	c.Log = hclog.New(&hclog.LoggerOptions{
		Level:  level,
		Output: os.Stderr,
	})

	// The ClientSet might already be set if we're in a test.
	if c.clientset == nil {
		if err := c.configureKubeClient(); err != nil {
			c.Log.Error(err.Error())
			return 1
		}
	}

	scheme := "http"
	if c.flagUseHTTPS {
		scheme = "https"
	}
	// Wait if there's a rollout of servers.
	ssName := c.withPrefix("server")
	err = c.untilSucceeds(fmt.Sprintf("waiting for rollout of statefulset %s", ssName), func() error {
		// Note: We can't use the -server-label-selector flag to find the statefulset
		// because in older versions of consul-helm it wasn't labeled with
		// component: server. We also can't drop that label because it's required
		// for targeting the right server Pods.
		statefulset, err := c.clientset.AppsV1().StatefulSets(c.flagK8sNamespace).Get(ssName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if statefulset.Status.CurrentRevision == statefulset.Status.UpdateRevision {
			return nil
		}
		return fmt.Errorf("rollout is in progress (CurrentRevision=%s UpdateRevision=%s)",
			statefulset.Status.CurrentRevision, statefulset.Status.UpdateRevision)
	})
	if err != nil {
		c.Log.Error(err.Error())
		return 1
	}

	// Check if we've already been bootstrapped.
	bootTokenSecretName := c.withPrefix("bootstrap-acl-token")
	bootstrapToken, err := c.getBootstrapToken(bootTokenSecretName)
	if err != nil {
		c.Log.Error(fmt.Sprintf("Unexpected error looking for preexisting bootstrap Secret: %s", err))
		return 1
	}

	var updateServerPolicy bool
	if bootstrapToken != "" {
		c.Log.Info(fmt.Sprintf("ACLs already bootstrapped - retrieved bootstrap token from Secret %q", bootTokenSecretName))

		// Mark that we should update the server ACL policy in case
		// there are namespace related config changes. Because of the
		// organization of the server token creation code, the policy
		// otherwise won't be updated.
		updateServerPolicy = true
	} else {
		c.Log.Info("No bootstrap token from previous installation found, continuing on to bootstrapping")
		bootstrapToken, err = c.bootstrapServers(bootTokenSecretName, scheme)
		if err != nil {
			c.Log.Error(err.Error())
			return 1
		}
	}

	// For all of the next operations we'll need a Consul client.
	serverPods, err := c.getConsulServers(1, scheme)
	if err != nil {
		c.Log.Error(err.Error())
		return 1
	}
	serverAddr := serverPods[0].Addr
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
		c.Log.Error(fmt.Sprintf("Error creating Consul client for addr %q: %s", serverAddr, err))
		return 1
	}

	// With the addition of namespaces, the ACL policies associated
	// with the server tokens may need to be updated if Enterprise Consul
	// users upgrade to 1.7+. This updates the policy if the bootstrap
	// token had previously existed, which signals a potential config change.
	if updateServerPolicy {
		_, err = c.setServerPolicy(consulClient)
		if err != nil {
			c.Log.Error("Error updating the server ACL policy", "err", err)
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
			c.Log.Error("Error creating or updating the cross namespace policy", "err", err)
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
			c.Log.Error("Error updating the default namespace to include the cross namespace policy", "err", err)
			return 1
		}
	}

	if c.flagCreateClientToken {
		agentRules, err := c.agentRules()
		if err != nil {
			c.Log.Error("Error templating client agent rules", "err", err)
			return 1
		}

		err = c.createACL("client", agentRules, consulClient)
		if err != nil {
			c.Log.Error(err.Error())
			return 1
		}
	}

	if c.flagAllowDNS {
		err := c.configureDNSPolicies(consulClient)
		if err != nil {
			c.Log.Error(err.Error())
			return 1
		}
	}

	if c.flagCreateSyncToken {
		syncRules, err := c.syncRules()
		if err != nil {
			c.Log.Error("Error templating sync rules", "err", err)
			return 1
		}

		err = c.createACL("catalog-sync", syncRules, consulClient)
		if err != nil {
			c.Log.Error(err.Error())
			return 1
		}
	}

	if c.flagCreateInjectToken {
		injectRules, err := c.injectRules()
		if err != nil {
			c.Log.Error("Error templating inject rules", "err", err)
			return 1
		}

		err = c.createACL("connect-inject", injectRules, consulClient)
		if err != nil {
			c.Log.Error(err.Error())
			return 1
		}
	}

	if c.flagCreateEntLicenseToken {
		err := c.createACL("enterprise-license", entLicenseRules, consulClient)
		if err != nil {
			c.Log.Error(err.Error())
			return 1
		}
	}

	if c.flagCreateSnapshotAgentToken {
		err := c.createACL("client-snapshot-agent", snapshotAgentRules, consulClient)
		if err != nil {
			c.Log.Error(err.Error())
			return 1
		}
	}

	if c.flagCreateMeshGatewayToken {
		meshGatewayRules, err := c.meshGatewayRules()
		if err != nil {
			c.Log.Error("Error templating dns rules", "err", err)
			return 1
		}

		err = c.createACL("mesh-gateway", meshGatewayRules, consulClient)
		if err != nil {
			c.Log.Error(err.Error())
			return 1
		}
	}

	if c.flagCreateInjectAuthMethod {
		err := c.configureConnectInject(consulClient)
		if err != nil {
			c.Log.Error(err.Error())
			return 1
		}
	}

	if c.flagCreateACLReplicationToken {
		rules, err := c.aclReplicationRules()
		if err != nil {
			c.Log.Error("Error templating acl replication token rules", "err", err)
			return 1
		}
		err = c.createACL("acl-replication", rules, consulClient)
		if err != nil {
			c.Log.Error(err.Error())
			return 1
		}
	}

	c.Log.Info("server-acl-init completed successfully")
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
	token, ok := secret.Data["token"]
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
			c.Log.Info(fmt.Sprintf("Success: %s", opName))
			break
		}
		c.Log.Error(fmt.Sprintf("Failure: %s", opName), "err", err)
		c.Log.Info("Retrying in " + c.retryDuration.String())
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
// on the -release-name or -resource-prefix flags.
func (c *Command) withPrefix(resource string) string {
	if c.flagResourcePrefix != "" {
		return fmt.Sprintf("%s-%s", c.flagResourcePrefix, resource)
	}
	// This is to support an older version of the Helm chart that only specified
	// the -release-name flag. We ensure that this is set if -resource-prefix
	// is not set when parsing the flags.
	return fmt.Sprintf("%s-consul-%s", c.flagReleaseName, resource)
}

const synopsis = "Initialize ACLs on Consul servers and other components."
const help = `
Usage: consul-k8s server-acl-init [options]

  Bootstraps servers with ACLs and creates policies and ACL tokens for other
  components as Kubernetes Secrets.
  It will run indefinitely until all tokens have been created. It is idempotent
  and safe to run multiple times.

`
