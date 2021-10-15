package serveraclinit

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/common"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/flags"
	k8sflags "github.com/hashicorp/consul-k8s/control-plane/subcommand/flags"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/go-discover"
	"github.com/hashicorp/go-hclog"
	"github.com/mitchellh/cli"
	"github.com/mitchellh/mapstructure"
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

	flagCreateSyncToken    bool
	flagSyncConsulNodeName string

	flagCreateInjectToken    bool
	flagInjectAuthMethodHost string
	flagBindingRuleSelector  string

	flagCreateControllerToken bool

	flagCreateEntLicenseToken bool

	flagCreateSnapshotAgentToken bool

	flagCreateMeshGatewayToken  bool
	flagIngressGatewayNames     []string
	flagTerminatingGatewayNames []string

	// Flags to configure Consul connection.
	flagServerAddresses     []string
	flagServerPort          uint
	flagConsulCACert        string
	flagConsulTLSServerName string
	flagUseHTTPS            bool

	// Flags for ACL replication.
	flagCreateACLReplicationToken bool
	flagACLReplicationTokenFile   string

	// Flags to support partitions.
	flagEnablePartitions bool   // true if Admin Partitions are enabled
	flagPartitionName    string // name of the Admin Partition

	// Flags to support namespaces.
	flagEnableNamespaces                 bool   // Use namespacing on all components
	flagConsulSyncDestinationNamespace   string // Consul namespace to register all catalog sync services into if not mirroring
	flagEnableSyncK8SNSMirroring         bool   // Enables mirroring of k8s namespaces into Consul for catalog sync
	flagSyncK8SNSMirroringPrefix         string // Prefix added to Consul namespaces created when mirroring catalog sync services
	flagConsulInjectDestinationNamespace string // Consul namespace to register all injected services into if not mirroring
	flagEnableInjectK8SNSMirroring       bool   // Enables mirroring of k8s namespaces into Consul for Connect inject
	flagInjectK8SNSMirroringPrefix       string // Prefix added to Consul namespaces created when mirroring injected services

	// Flag to support a custom bootstrap token.
	flagBootstrapTokenFile string

	flagLogLevel string
	flagLogJSON  bool
	flagTimeout  time.Duration

	// flagFederation is used to determine which ACL policies to write and whether or not to provide suffixing
	// to the policy names when creating the policy in cases where federation is used.
	// flagFederation indicates if federation has been enabled in the cluster.
	flagFederation bool

	clientset kubernetes.Interface

	// ctx is cancelled when the command timeout is reached.
	ctx           context.Context
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
	c.flags.StringVar(&c.flagSyncConsulNodeName, "sync-consul-node-name", "k8s-sync",
		"The Consul node name to register for catalog sync. Defaults to k8s-sync. To be discoverable "+
			"via DNS, the name should only contain alpha-numerics and dashes.")

	// Previously when this flag was set, -enable-namespaces and -create-inject-auth-method
	// were always passed, so now we just look at those flags and ignore
	// this flag. We keep the flag here though so there's no error if it's
	// passed.
	var unused bool
	c.flags.BoolVar(&unused, "create-inject-namespace-token", false,
		"Toggle for creating a connect injector token. Only required when namespaces are enabled. "+
			"Deprecated: set -enable-namespaces and -create-inject-token instead.")

	c.flags.BoolVar(&c.flagCreateInjectToken, "create-inject-auth-method", false,
		"Toggle for creating a connect inject auth method. Deprecated: use -create-inject-token instead.")
	c.flags.BoolVar(&c.flagCreateInjectToken, "create-inject-token", false,
		"Toggle for creating a connect inject auth method and an ACL token.")
	c.flags.StringVar(&c.flagInjectAuthMethodHost, "inject-auth-method-host", "",
		"Kubernetes Host config parameter for the auth method."+
			"If not provided, the default cluster Kubernetes service will be used.")
	c.flags.StringVar(&c.flagBindingRuleSelector, "acl-binding-rule-selector", "",
		"Selector string for connectInject ACL Binding Rule.")

	c.flags.BoolVar(&c.flagCreateControllerToken, "create-controller-token", false,
		"Toggle for creating a token for the controller.")

	c.flags.BoolVar(&c.flagCreateEntLicenseToken, "create-enterprise-license-token", false,
		"Toggle for creating a token for the enterprise license job.")
	c.flags.BoolVar(&c.flagCreateSnapshotAgentToken, "create-snapshot-agent-token", false,
		"[Enterprise Only] Toggle for creating a token for the Consul snapshot agent deployment.")
	c.flags.BoolVar(&c.flagCreateMeshGatewayToken, "create-mesh-gateway-token", false,
		"Toggle for creating a token for a Connect mesh gateway.")
	c.flags.Var((*flags.AppendSliceValue)(&c.flagIngressGatewayNames), "ingress-gateway-name",
		"Name of an ingress gateway that needs an acl token. May be specified multiple times. "+
			"[Enterprise Only] If using Consul namespaces and registering the gateway outside of the "+
			"default namespace, specify the value in the form <GatewayName>.<ConsulNamespace>.")
	c.flags.Var((*flags.AppendSliceValue)(&c.flagTerminatingGatewayNames), "terminating-gateway-name",
		"Name of a terminating gateway that needs an acl token. May be specified multiple times. "+
			"[Enterprise Only] If using Consul namespaces and registering the gateway outside of the "+
			"default namespace, specify the value in the form <GatewayName>.<ConsulNamespace>.")

	c.flags.Var((*flags.AppendSliceValue)(&c.flagServerAddresses), "server-address",
		"The IP, DNS name or the cloud auto-join string of the Consul server(s). If providing IPs or DNS names, may be specified multiple times. "+
			"At least one value is required.")
	c.flags.UintVar(&c.flagServerPort, "server-port", 8500, "The HTTP or HTTPS port of the Consul server. Defaults to 8500.")
	c.flags.StringVar(&c.flagConsulCACert, "consul-ca-cert", "",
		"Path to the PEM-encoded CA certificate of the Consul cluster.")
	c.flags.StringVar(&c.flagConsulTLSServerName, "consul-tls-server-name", "",
		"The server name to set as the SNI header when sending HTTPS requests to Consul.")
	c.flags.BoolVar(&c.flagUseHTTPS, "use-https", false,
		"Toggle for using HTTPS for all API calls to Consul.")

	c.flags.BoolVar(&c.flagEnablePartitions, "enable-partitions", false,
		"[Enterprise Only] Enables Admin Partitions")
	c.flags.StringVar(&c.flagPartitionName, "partition", "",
		"[Enterprise Only] Name of the Admin Partition")

	c.flags.BoolVar(&c.flagEnableNamespaces, "enable-namespaces", false,
		"[Enterprise Only] Enables namespaces, in either a single Consul namespace or mirrored [Enterprise only feature]")
	c.flags.StringVar(&c.flagConsulSyncDestinationNamespace, "consul-sync-destination-namespace", consulDefaultNamespace,
		"[Enterprise Only] Indicates which Consul namespace that catalog sync will register services into. If "+
			"'-enable-sync-k8s-namespace-mirroring' is true, this is not used.")
	c.flags.BoolVar(&c.flagEnableSyncK8SNSMirroring, "enable-sync-k8s-namespace-mirroring", false, "[Enterprise Only] "+
		"Indicates that namespace mirroring will be used for catalog sync services.")
	c.flags.StringVar(&c.flagSyncK8SNSMirroringPrefix, "sync-k8s-namespace-mirroring-prefix", "",
		"[Enterprise Only] Prefix that will be added to all k8s namespaces mirrored into Consul by catalog sync "+
			"if mirroring is enabled.")
	c.flags.StringVar(&c.flagConsulInjectDestinationNamespace, "consul-inject-destination-namespace", consulDefaultNamespace,
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

	c.flags.BoolVar(&c.flagFederation, "federation", false, "Toggle for when federation has been enabled.")

	c.flags.StringVar(&c.flagBootstrapTokenFile, "bootstrap-token-file", "",
		"Path to file containing ACL token for creating policies and tokens. This token must have 'acl:write' permissions."+
			"When provided, servers will not be bootstrapped and their policies and tokens will not be updated.")

	c.flags.DurationVar(&c.flagTimeout, "timeout", 10*time.Minute,
		"How long we'll try to bootstrap ACLs for before timing out, e.g. 1ms, 2s, 3m")
	c.flags.StringVar(&c.flagLogLevel, "log-level", "info",
		"Log verbosity level. Supported values (in order of detail) are \"trace\", "+
			"\"debug\", \"info\", \"warn\", and \"error\".")
	c.flags.BoolVar(&c.flagLogJSON, "log-json", false,
		"Enable or disable JSON output format for logging.")

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

	// Validate flags
	if err := c.validateFlags(); err != nil {
		c.UI.Error(err.Error())
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
	c.ctx, cancel = context.WithTimeout(context.Background(), c.flagTimeout)
	// The context will only ever be intentionally ended by the timeout.
	defer cancel()

	var err error
	c.log, err = common.Logger(c.flagLogLevel, c.flagLogJSON)
	if err != nil {
		c.UI.Error(err.Error())
		return 1
	}

	// The ClientSet might already be set if we're in a test.
	if c.clientset == nil {
		if err := c.configureKubeClient(); err != nil {
			c.log.Error(err.Error())
			return 1
		}
	}

	serverAddresses, err := common.GetResolvedServerAddresses(c.flagServerAddresses, c.providers, c.log)
	if err != nil {
		c.UI.Error(fmt.Sprintf("Unable to discover any Consul addresses from %q: %s", c.flagServerAddresses[0], err))
		return 1
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
	clientConfig := &api.Config{
		Address: serverAddr,
		Scheme:  scheme,
		Token:   bootstrapToken,
		TLSConfig: api.TLSConfig{
			Address: c.flagConsulTLSServerName,
			CAFile:  c.flagConsulCACert,
		},
	}
	if c.flagEnablePartitions {
		clientConfig.Partition = c.flagPartitionName
	}

	consulClient, err := consul.NewClient(clientConfig)
	if err != nil {
		c.log.Error(fmt.Sprintf("Error creating Consul client for addr %q: %s", serverAddr, err))
		return 1
	}
	consulDC, primaryDC, err := c.consulDatacenterList(consulClient)
	if err != nil {
		c.log.Error("Error getting datacenter name", "err", err)
		return 1
	}
	c.log.Info("Current datacenter", "datacenter", consulDC, "primaryDC", primaryDC)
	isPrimary := consulDC == primaryDC

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

	if c.flagEnablePartitions && c.flagPartitionName == consulDefaultPartition && isPrimary {
		// Partition token is local because only the Primary datacenter can have Admin Partitions.
		err := c.createLocalACL("partitions", partitionRules, consulDC, isPrimary, consulClient)
		if err != nil {
			c.log.Error(err.Error())
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
		crossNamespaceRule, err := c.crossNamespaceRules()
		if err != nil {
			c.log.Error("Error templating cross namespace rules", "err", err)
			return 1
		}
		policyTmpl := api.ACLPolicy{
			Name:        "cross-namespace-policy",
			Description: "Policy to allow permissions to cross Consul namespaces for k8s services",
			Rules:       crossNamespaceRule,
		}
		err = c.untilSucceeds(fmt.Sprintf("creating %s policy", policyTmpl.Name),
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
			Name: consulDefaultNamespace,
			ACLs: &aclConfig,
		}
		_, _, err = consulClient.Namespaces().Update(&consulNamespace, &api.WriteOptions{})
		if err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "unexpected response code: 404") {
				// If this returns a 404 it's most likely because they're not running
				// Consul Enterprise.
				c.log.Error("Error updating the default namespace to include the cross namespace policy - ensure you're running Consul Enterprise with namespaces enabled", "err", err)
			} else {
				c.log.Error("Error updating the default namespace to include the cross namespace policy", "err", err)
			}
			return 1
		}
	}

	if c.flagCreateClientToken {
		agentRules, err := c.agentRules()
		if err != nil {
			c.log.Error("Error templating client agent rules", "err", err)
			return 1
		}

		err = c.createLocalACL("client", agentRules, consulDC, isPrimary, consulClient)
		if err != nil {
			c.log.Error(err.Error())
			return 1
		}
	}

	if c.createAnonymousPolicy(isPrimary) {
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
			err = c.createGlobalACL("catalog-sync", syncRules, consulDC, isPrimary, consulClient)
		} else {
			err = c.createLocalACL("catalog-sync", syncRules, consulDC, isPrimary, consulClient)
		}
		if err != nil {
			c.log.Error(err.Error())
			return 1
		}
	}

	if c.flagCreateInjectToken {
		err := c.configureConnectInjectAuthMethod(consulClient)
		if err != nil {
			c.log.Error(err.Error())
			return 1
		}

		// The endpoints controller needs an ACL token always.
		injectRules, err := c.injectRules()
		if err != nil {
			c.log.Error("Error templating inject rules", "err", err)
			return 1
		}

		// If namespaces are enabled, the policy and token need to be global
		// to be allowed to create namespaces.
		if c.flagEnableNamespaces {
			err = c.createGlobalACL("connect-inject", injectRules, consulDC, isPrimary, consulClient)
		} else {
			err = c.createLocalACL("connect-inject", injectRules, consulDC, isPrimary, consulClient)
		}

		if err != nil {
			c.log.Error(err.Error())
			return 1
		}
	}

	if c.flagCreateEntLicenseToken {
		var err error
		if c.flagEnablePartitions {
			err = c.createLocalACL("enterprise-license", entPartitionLicenseRules, consulDC, isPrimary, consulClient)
		} else {
			err = c.createLocalACL("enterprise-license", entLicenseRules, consulDC, isPrimary, consulClient)
		}
		if err != nil {
			c.log.Error(err.Error())
			return 1
		}
	}

	if c.flagCreateSnapshotAgentToken {
		err := c.createLocalACL("client-snapshot-agent", snapshotAgentRules, consulDC, isPrimary, consulClient)
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
		err = c.createGlobalACL("mesh-gateway", meshGatewayRules, consulDC, isPrimary, consulClient)
		if err != nil {
			c.log.Error(err.Error())
			return 1
		}
	}

	if len(c.flagIngressGatewayNames) > 0 {
		// Create a token for each ingress gateway name. Each gateway needs a
		// separate token because users may need to attach different policies
		// to each gateway token depending on what the services it represents
		for _, name := range c.flagIngressGatewayNames {
			if name == "" {
				c.log.Error("Ingress gateway names cannot be empty")
				return 1
			}

			// Parse optional namespace, erroring if a user
			// provides a namespace when not enabling namespaces.
			var namespace string
			if c.flagEnableNamespaces {
				parts := strings.SplitN(strings.TrimSpace(name), ".", 2)
				if len(parts) > 1 {
					// Name and namespace were provided
					name = parts[0]

					// Use default namespace if provided flag is of the
					// form "name."
					if parts[1] != "" {
						namespace = parts[1]
					} else {
						namespace = consulDefaultNamespace
					}
				} else {
					// Use the default Consul namespace
					namespace = consulDefaultNamespace
				}
			} else if strings.ContainsAny(name, ".") {
				c.log.Error("Gateway names shouldn't include a namespace if Consul namespaces aren't enabled",
					"gateway-name", name)
				return 1
			}

			// Define the gateway rules
			ingressGatewayRules, err := c.ingressGatewayRules(name, namespace)
			if err != nil {
				c.log.Error("Error templating ingress gateway rules", "gateway-name", name,
					"namespace", namespace, "err", err)
				return 1
			}

			// The names in the Helm chart are specified by users and so may not contain
			// the words "ingress-gateway". We need to create unique names for tokens
			// across all gateway types and so must suffix with `-ingress-gateway`.
			tokenName := fmt.Sprintf("%s-ingress-gateway", name)
			err = c.createLocalACL(tokenName, ingressGatewayRules, consulDC, isPrimary, consulClient)
			if err != nil {
				c.log.Error(err.Error())
				return 1
			}
		}
	}

	if len(c.flagTerminatingGatewayNames) > 0 {
		// Create a token for each terminating gateway name. Each gateway needs a
		// separate token because users may need to attach different policies
		// to each gateway token depending on what the services it represents
		for _, name := range c.flagTerminatingGatewayNames {
			if name == "" {
				c.log.Error("Terminating gateway names cannot be empty")
				return 1
			}

			// Parse optional namespace. This does not protect against a user
			// that provides a namespace with namespaces not enabled.
			var namespace string
			if c.flagEnableNamespaces {
				parts := strings.SplitN(strings.TrimSpace(name), ".", 2)
				if len(parts) > 1 {
					// Name and namespace were provided
					name = parts[0]

					// Use default namespace if provided flag is of the
					// form "name."
					if parts[1] != "" {
						namespace = parts[1]
					} else {
						namespace = consulDefaultNamespace
					}
				} else {
					// Use the default Consul namespace
					namespace = consulDefaultNamespace
				}
			} else if strings.ContainsAny(name, ".") {
				c.log.Error("Gateway names shouldn't include a namespace if Consul namespaces aren't enabled",
					"gateway-name", name)
				return 1
			}

			// Define the gateway rules
			terminatingGatewayRules, err := c.terminatingGatewayRules(name, namespace)
			if err != nil {
				c.log.Error("Error templating terminating gateway rules", "gateway-name", name,
					"namespace", namespace, "err", err)
				return 1
			}

			// The names in the Helm chart are specified by users and so may not contain
			// the words "ingress-gateway". We need to create unique names for tokens
			// across all gateway types and so must suffix with `-terminating-gateway`.
			tokenName := fmt.Sprintf("%s-terminating-gateway", name)
			err = c.createLocalACL(tokenName, terminatingGatewayRules, consulDC, isPrimary, consulClient)
			if err != nil {
				c.log.Error(err.Error())
				return 1
			}
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
		err = c.createGlobalACL(common.ACLReplicationTokenName, rules, consulDC, isPrimary, consulClient)
		if err != nil {
			c.log.Error(err.Error())
			return 1
		}
	}

	if c.flagCreateControllerToken {
		rules, err := c.controllerRules()
		if err != nil {
			c.log.Error("Error templating controller token rules", "err", err)
			return 1
		}
		// Controller token must be global because config entry writes all
		// go to the primary datacenter. This means secondary datacenters need
		// a token that is known by the primary datacenters.
		err = c.createGlobalACL("controller", rules, consulDC, isPrimary, consulClient)
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
	secret, err := c.clientset.CoreV1().Secrets(c.flagK8sNamespace).Get(c.ctx, secretName, metav1.GetOptions{})
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
		case <-c.ctx.Done():
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

// consulDatacenterList returns the current datacenter name and the primary datacenter using the
// /agent/self API endpoint.
func (c *Command) consulDatacenterList(client *api.Client) (string, string, error) {
	var agentCfg map[string]map[string]interface{}
	err := c.untilSucceeds("calling /agent/self to get datacenter",
		func() error {
			var opErr error
			agentCfg, opErr = client.Agent().Self()
			return opErr
		})
	if err != nil {
		return "", "", err
	}
	var agentConfig AgentConfig
	err = mapstructure.Decode(agentCfg, &agentConfig)
	if err != nil {
		return "", "", err
	}
	if agentConfig.Config.Datacenter == "" {
		return "", "", fmt.Errorf("/agent/self response did not contain Config.Datacenter key: %s", agentCfg)
	}
	if agentConfig.Config.PrimaryDatacenter == "" && agentConfig.DebugConfig.PrimaryDatacenter == "" {
		return "", "", fmt.Errorf("both Config.PrimaryDatacenter and DebugConfig.PrimaryDatacenter are empty: %s", agentCfg)
	}
	if agentConfig.Config.PrimaryDatacenter != "" {
		return agentConfig.Config.Datacenter, agentConfig.Config.PrimaryDatacenter, nil
	} else {
		return agentConfig.Config.Datacenter, agentConfig.DebugConfig.PrimaryDatacenter, nil
	}
}

type AgentConfig struct {
	Config      Config
	DebugConfig Config
}

type Config struct {
	Datacenter        string `mapstructure:"Datacenter"`
	PrimaryDatacenter string `mapstructure:"PrimaryDatacenter"`
}

// createAnonymousPolicy returns whether we should create a policy for the
// anonymous ACL token, i.e. queries without ACL tokens.
func (c *Command) createAnonymousPolicy(isPrimary bool) bool {
	// If isPrimary is not set then we're in a secondary DC.
	// In this case we assume that the primary datacenter has already created
	// the anonymous policy and attached it to the anonymous token.
	// We don't want to modify the anonymous policy in secondary datacenters
	// because it is global and we can't create separate tokens for each
	// secondary datacenter because the anonymous token is global.
	return isPrimary &&
		// Consul DNS requires the anonymous policy because DNS queries don't
		// have ACL tokens.
		(c.flagAllowDNS ||
			// If connect is enabled and federation is enabled then we know we're using multi-dc Connect.
			// In this case the anonymous policy is required because Connect
			// services in Kubernetes have local tokens which are stripped
			// on cross-dc API calls. The cross-dc API calls thus use the anonymous
			// token. Cross-dc API calls are needed by the Connect proxies to talk
			// cross-dc.
			(c.flagCreateInjectToken && c.flagFederation))
}

func (c *Command) validateFlags() error {
	if len(c.flagServerAddresses) == 0 {
		return errors.New("-server-address must be set at least once")
	}

	if c.flagResourcePrefix == "" {
		return errors.New("-resource-prefix must be set")
	}

	// For the Consul node name to be discoverable via DNS, it must contain only
	// dashes and alphanumeric characters. Length is also constrained.
	// These restrictions match those defined in Consul's agent definition.
	var invalidDnsRe = regexp.MustCompile(`[^A-Za-z0-9\\-]+`)
	const maxDNSLabelLength = 63

	if invalidDnsRe.MatchString(c.flagSyncConsulNodeName) {
		return fmt.Errorf("-sync-consul-node-name=%s is invalid: node name will not be discoverable "+
			"via DNS due to invalid characters. Valid characters include "+
			"all alpha-numerics and dashes",
			c.flagSyncConsulNodeName,
		)
	}
	if len(c.flagSyncConsulNodeName) > maxDNSLabelLength {
		return fmt.Errorf("-sync-consul-node-name=%s is invalid: node name will not be discoverable "+
			"via DNS due to it being too long. Valid lengths are between "+
			"1 and 63 bytes",
			c.flagSyncConsulNodeName,
		)
	}

	if c.flagEnablePartitions && c.flagPartitionName == "" {
		return errors.New("-partition must be set if -enable-partitions is true")
	}
	if !c.flagEnablePartitions && c.flagPartitionName != "" {
		return errors.New("-enable-partitions must be 'true' if -partition is set")
	}
	return nil
}

const consulDefaultNamespace = "default"
const consulDefaultPartition = "default"
const synopsis = "Initialize ACLs on Consul servers and other components."
const help = `
Usage: consul-k8s-control-plane server-acl-init [options]

  Bootstraps servers with ACLs and creates policies and ACL tokens for other
  components as Kubernetes Secrets.
  It will run indefinitely until all tokens have been created. It is idempotent
  and safe to run multiple times.

`
