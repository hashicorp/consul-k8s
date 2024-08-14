// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package serveraclinit

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/hashicorp/consul-server-connection-manager/discovery"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-netaddrs"
	vaultApi "github.com/hashicorp/vault/api"
	"github.com/mitchellh/cli"
	"github.com/mitchellh/mapstructure"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"k8s.io/client-go/kubernetes"

	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/common"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/flags"
	k8sflags "github.com/hashicorp/consul-k8s/control-plane/subcommand/flags"
)

const dnsProxyName = "dns-proxy"

type Command struct {
	UI cli.Ui

	flags       *flag.FlagSet
	k8s         *k8sflags.K8SFlags
	consulFlags *flags.ConsulFlags

	flagResourcePrefix string
	flagK8sNamespace   string

	flagAllowDNS bool

	flagSetServerTokens bool

	flagClient bool

	flagSyncCatalog        bool
	flagSyncConsulNodeName string

	flagConnectInject       bool
	flagAuthMethodHost      string
	flagBindingRuleSelector string

	flagCreateEntLicenseToken bool
	flagCreateDDAgentToken    bool

	flagSnapshotAgent bool

	flagMeshGateway             bool
	flagIngressGatewayNames     []string
	flagTerminatingGatewayNames []string

	// Flags to configure Consul connection.
	flagServerPort uint

	// Flags for ACL replication.
	flagCreateACLReplicationToken bool
	flagACLReplicationTokenFile   string

	// Flags to support partitions.
	flagPartitionTokenFile string

	// Flags to support peering.
	flagEnablePeering bool // true if Cluster Peering is enabled

	// Flags to support namespaces.
	flagEnableNamespaces                 bool   // Use namespacing on all components
	flagConsulSyncDestinationNamespace   string // Consul namespace to register all catalog sync services into if not mirroring
	flagEnableSyncK8SNSMirroring         bool   // Enables mirroring of k8s namespaces into Consul for catalog sync
	flagSyncK8SNSMirroringPrefix         string // Prefix added to Consul namespaces created when mirroring catalog sync services
	flagConsulInjectDestinationNamespace string // Consul namespace to register all injected services into if not mirroring
	flagEnableInjectK8SNSMirroring       bool   // Enables mirroring of k8s namespaces into Consul for Connect inject
	flagInjectK8SNSMirroringPrefix       string // Prefix added to Consul namespaces created when mirroring injected services

	// Flags for the secrets backend.
	flagSecretsBackend           SecretsBackendType
	flagBootstrapTokenSecretName string
	flagBootstrapTokenSecretKey  string

	flagLogLevel string
	flagLogJSON  bool
	flagTimeout  time.Duration

	// flagFederation is used to determine which ACL policies to write and whether or not to provide suffixing
	// to the policy names when creating the policy in cases where federation is used.
	// flagFederation indicates if federation has been enabled in the cluster.
	flagFederation bool

	backend     SecretsBackend // for unit testing.
	clientset   kubernetes.Interface
	vaultClient *vaultApi.Client

	watcher consul.ServerConnectionManager

	// ctx is cancelled when the command timeout is reached.
	ctx           context.Context
	retryDuration time.Duration

	// the amount of time to contact the Consul API before timing out
	apiTimeoutDuration time.Duration

	// log
	log hclog.Logger

	state discovery.State

	once         sync.Once
	help         string
	flagDNSProxy bool
}

func (c *Command) init() {

	c.flags = flag.NewFlagSet("", flag.ContinueOnError)
	c.flags.StringVar(&c.flagResourcePrefix, "resource-prefix", "",
		"Prefix to use for Kubernetes resources.")
	c.flags.StringVar(&c.flagK8sNamespace, "k8s-namespace", "",
		"Name of Kubernetes namespace where Consul and consul-k8s components are deployed.")

	c.flags.BoolVar(&c.flagSetServerTokens, "set-server-tokens", true, "Toggle for setting agent tokens for the servers.")

	c.flags.BoolVar(&c.flagAllowDNS, "allow-dns", false,
		"Toggle for updating the anonymous token to allow DNS queries to work")
	c.flags.BoolVar(&c.flagClient, "client", true,
		"Toggle for creating a client agent token. Default is true.")

	c.flags.BoolVar(&c.flagSyncCatalog, "sync-catalog", false,
		"Toggle for configuring ACL login for sync catalog.")
	c.flags.StringVar(&c.flagSyncConsulNodeName, "sync-consul-node-name", "k8s-sync",
		"The Consul node name to register for catalog sync. Defaults to k8s-sync. To be discoverable "+
			"via DNS, the name should only contain alpha-numerics and dashes.")

	c.flags.BoolVar(&c.flagConnectInject, "connect-inject", false,
		"Toggle for configuring ACL login for Connect inject.")
	c.flags.StringVar(&c.flagAuthMethodHost, "auth-method-host", "",
		"Kubernetes Host config parameter for the auth method."+
			"If not provided, the default cluster Kubernetes service will be used.")
	c.flags.StringVar(&c.flagBindingRuleSelector, "acl-binding-rule-selector", "",
		"Selector string for connectInject ACL Binding Rule.")

	c.flags.BoolVar(&c.flagCreateEntLicenseToken, "create-enterprise-license-token", false,
		"Toggle for creating a token for the enterprise license job.")
	c.flags.BoolVar(&c.flagSnapshotAgent, "snapshot-agent", false,
		"[Enterprise Only] Toggle for configuring ACL login for the snapshot agent.")
	c.flags.BoolVar(&c.flagMeshGateway, "mesh-gateway", false,
		"Toggle for configuring ACL login for the mesh gateway.")
	c.flags.Var((*flags.AppendSliceValue)(&c.flagIngressGatewayNames), "ingress-gateway-name",
		"Name of an ingress gateway that needs an acl token. May be specified multiple times. "+
			"[Enterprise Only] If using Consul namespaces and registering the gateway outside of the "+
			"default namespace, specify the value in the form <GatewayName>.<ConsulNamespace>.")
	c.flags.Var((*flags.AppendSliceValue)(&c.flagTerminatingGatewayNames), "terminating-gateway-name",
		"Name of a terminating gateway that needs an acl token. May be specified multiple times. "+
			"[Enterprise Only] If using Consul namespaces and registering the gateway outside of the "+
			"default namespace, specify the value in the form <GatewayName>.<ConsulNamespace>.")

	c.flags.UintVar(&c.flagServerPort, "server-port", 8500, "The HTTP or HTTPS port of the Consul server. Defaults to 8500.")

	c.flags.StringVar(&c.flagPartitionTokenFile, "partition-token-file", "",
		"[Enterprise Only] Path to file containing ACL token to be used in non-default partitions.")

	c.flags.BoolVar(&c.flagEnablePeering, "enable-peering", false,
		"Enables Cluster Peering.")

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

	c.flags.StringVar((*string)(&c.flagSecretsBackend), "secrets-backend", "kubernetes",
		`The secrets backend to use. Either "vault" or "kubernetes". Defaults to "kubernetes"`)
	c.flags.StringVar(&c.flagBootstrapTokenSecretName, "bootstrap-token-secret-name", "",
		"The name of the Vault or Kubernetes secret for the bootstrap token. This token must have `ac::write` permission "+
			"in order to create policies and tokens. If not provided or if the secret is empty, then this command will "+
			"bootstrap ACLs and write the bootstrap token to this secret.")
	c.flags.StringVar(&c.flagBootstrapTokenSecretKey, "bootstrap-token-secret-key", "",
		"The key within the Vault or Kubernetes secret containing the bootstrap token.")
	c.flags.BoolVar(&c.flagCreateDDAgentToken, "create-dd-agent-token", false,
		"Enable ACL token creation for datadog agent integration"+
			"Configures the following permissions to grant datadog agent metrics scraping permissions with Consul ACLs enabled"+
			"agent_prefix \"\" {\n  policy = \"read\"\n}\nservice_prefix \"\" {\n  policy = \"read\"\n}\nnode_prefix \"\" {\n  policy = \"read\"\n}")

	c.flags.DurationVar(&c.flagTimeout, "timeout", 10*time.Minute,
		"How long we'll try to bootstrap ACLs for before timing out, e.g. 1ms, 2s, 3m")
	c.flags.StringVar(&c.flagLogLevel, "log-level", "info",
		"Log verbosity level. Supported values (in order of detail) are \"trace\", "+
			"\"debug\", \"info\", \"warn\", and \"error\".")
	c.flags.BoolVar(&c.flagLogJSON, "log-json", false,
		"Enable or disable JSON output format for logging.")

	c.flags.BoolVar(&c.flagDNSProxy, dnsProxyName, false,
		"Toggle for configuring ACL login for the DNS proxy.")

	c.k8s = &k8sflags.K8SFlags{}
	c.consulFlags = &flags.ConsulFlags{}
	flags.Merge(c.flags, c.k8s.Flags())
	flags.Merge(c.flags, c.consulFlags.Flags())
	c.help = flags.Usage(help, c.flags)

	// Default retry to 1s. This is exposed for setting in tests.
	if c.retryDuration == 0 {
		c.retryDuration = 1 * time.Second
	}

	// Most of the API calls are in an infinite loop until the command cancels. This timeout
	// allows us to refresh the server IPs so that calls will succeed.
	if c.apiTimeoutDuration == 0 {
		c.apiTimeoutDuration = 2 * time.Minute
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
	defer c.quitVaultAgent()
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
		var err error
		aclReplicationToken, err = loadTokenFromFile(c.flagACLReplicationTokenFile)
		if err != nil {
			c.UI.Error(err.Error())
			return 1
		}
	}

	var partitionToken string
	if c.flagPartitionTokenFile != "" {
		var err error
		partitionToken, err = loadTokenFromFile(c.flagPartitionTokenFile)
		if err != nil {
			c.UI.Error(err.Error())
			return 1
		}
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

	if err := c.configureSecretsBackend(); err != nil {
		c.log.Error(err.Error())
		return 1
	}

	var bootstrapToken string
	if c.flagACLReplicationTokenFile != "" && !c.flagCreateACLReplicationToken {
		// If ACL replication is enabled, we don't need to ACL bootstrap the servers
		// since they will be performing replication.
		// We can use the replication token as our bootstrap token because it
		// has permissions to create policies and tokens.
		c.log.Info("ACL replication is enabled so skipping Consul server ACL bootstrapping")
		bootstrapToken = aclReplicationToken
	} else {
		// During upgrades, there is a rare case where a consul-server statefulset may be rotated out while the
		// bootstrap tokens are being updated. Catch this case, refresh the server ip addresses and try again.
		if err := backoff.Retry(func() error {
			ipAddrs, err := c.serverIPAddresses()
			if err != nil {
				c.log.Error(err.Error())
				return err
			}

			bootstrapToken, err = c.bootstrapServers(ipAddrs, c.backend)
			if err != nil {
				c.log.Error(err.Error())
				return err
			}
			return nil
		}, exponentialBackoffWithMaxInterval()); err != nil {
			c.log.Error(err.Error())
			return 1
		}
	}

	// Start Consul server Connection manager
	var watcher consul.ServerConnectionManager
	serverConnMgrCfg, err := c.consulFlags.ConsulServerConnMgrConfig()
	if err != nil {
		c.UI.Error(fmt.Sprintf("unable to create config for consul-server-connection-manager: %s", err))
		return 1
	}
	serverConnMgrCfg.Credentials.Type = discovery.CredentialsTypeStatic
	serverConnMgrCfg.Credentials.Static = discovery.StaticTokenCredential{Token: bootstrapToken}
	if c.watcher == nil {
		watcher, err = discovery.NewWatcher(c.ctx, serverConnMgrCfg, c.log.Named("consul-server-connection-manager"))
		if err != nil {
			c.UI.Error(fmt.Sprintf("unable to create Consul server watcher: %s", err))
			return 1
		}
	} else {
		watcher = c.watcher
	}

	go watcher.Run()
	defer watcher.Stop()

	c.state, err = watcher.State()
	if err != nil {
		c.UI.Error(fmt.Sprintf("unable to get Consul server addresses from watcher: %s", err))
		return 1
	}

	dynamicClient, err := consul.NewDynamicClientFromConnMgr(c.consulFlags.ConsulClientConfig(), watcher)
	if err != nil {
		c.log.Error(fmt.Sprintf("Error creating Consul client for addr %q: %s", c.state.Address, err))
		return 1
	}
	consulDC, primaryDC, err := c.consulDatacenterList(dynamicClient)
	if err != nil {
		c.log.Error("Error getting datacenter name", "err", err)
		return 1
	}
	c.log.Info("Current datacenter", "datacenter", consulDC, "primaryDC", primaryDC)
	primary := consulDC == primaryDC

	if c.consulFlags.Partition == consulDefaultPartition && primary {
		// Partition token is local because only the Primary datacenter can have Admin Partitions.
		if c.flagPartitionTokenFile != "" {
			err = c.createACLWithSecretID("partitions", partitionRules, consulDC, primary, dynamicClient, partitionToken, true)
		} else {
			err = c.createLocalACL("partitions", partitionRules, consulDC, primary, dynamicClient)
		}
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
				return c.createOrUpdateACLPolicy(policyTmpl, dynamicClient)
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
		_, _, err = dynamicClient.ConsulClient.Namespaces().Update(&consulNamespace, &api.WriteOptions{})
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

	// Create the component auth method, this is the auth method that Consul components will use
	// to issue an `ACL().Login()` against at startup, for local tokens.
	localComponentAuthMethodName := c.withPrefix("k8s-component-auth-method")
	err = c.configureLocalComponentAuthMethod(dynamicClient, localComponentAuthMethodName)
	if err != nil {
		c.log.Error(err.Error())
		return 1
	}

	globalComponentAuthMethodName := fmt.Sprintf("%s-%s", localComponentAuthMethodName, consulDC)
	if !primary && c.flagAuthMethodHost != "" {
		err = c.configureGlobalComponentAuthMethod(dynamicClient, globalComponentAuthMethodName, primaryDC)
		if err != nil {
			c.log.Error(err.Error())
			return 1
		}
	}

	if c.flagClient {
		agentRules, err := c.agentRules()
		if err != nil {
			c.log.Error("Error templating client agent rules", "err", err)
			return 1
		}

		serviceAccountName := c.withPrefix("client")
		err = c.createACLPolicyRoleAndBindingRule("client", agentRules, consulDC, primaryDC, false, primary, localComponentAuthMethodName, serviceAccountName, dynamicClient)
		if err != nil {
			c.log.Error(err.Error())
			return 1
		}
	}

	if c.createAnonymousPolicy(primary) {
		// When the default partition is in a VM, the anonymous policy does not allow cross-partition
		// DNS lookups. The anonymous policy in the default partition needs to be updated in order to
		// support this use-case. Creating a separate anonymous token client that updates the anonymous
		// policy and token in the default partition ensures this works.
		anonTokenConfig := c.consulFlags.ConsulClientConfig()
		if c.consulFlags.Partition != "" {
			anonTokenConfig.APIClientConfig.Partition = consulDefaultPartition
		}
		anonTokenClient, err := consul.NewDynamicClientFromConnMgr(anonTokenConfig, watcher)
		if err != nil {
			c.log.Error(err.Error())
			return 1
		}

		err = c.configureAnonymousPolicy(anonTokenClient)
		if err != nil {
			c.log.Error(err.Error())
			return 1
		}
	}

	if c.flagSyncCatalog {
		syncRules, err := c.syncRules()
		if err != nil {
			c.log.Error("Error templating sync rules", "err", err)
			return 1
		}

		serviceAccountName := c.withPrefix("sync-catalog")
		componentAuthMethodName := localComponentAuthMethodName

		// If namespaces are enabled, the policy and token need to be global to be allowed to create namespaces.
		if c.flagEnableNamespaces {
			// Create the catalog sync ACL Policy, Role and BindingRule.
			// SyncCatalog token must be global when namespaces are enabled. This means secondary datacenters need
			// a token that is known by the primary datacenters.
			if !primary {
				componentAuthMethodName = globalComponentAuthMethodName
			}
			err = c.createACLPolicyRoleAndBindingRule("sync-catalog", syncRules, consulDC, primaryDC, globalPolicy, primary, componentAuthMethodName, serviceAccountName, dynamicClient)
		} else {
			err = c.createACLPolicyRoleAndBindingRule("sync-catalog", syncRules, consulDC, primaryDC, localPolicy, primary, componentAuthMethodName, serviceAccountName, dynamicClient)
		}
		if err != nil {
			c.log.Error(err.Error())
			return 1
		}
	}

	if c.flagConnectInject {
		connectAuthMethodName := c.withPrefix("k8s-auth-method")
		err := c.configureConnectInjectAuthMethod(dynamicClient, connectAuthMethodName)
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

		serviceAccountName := c.withPrefix("connect-injector")
		componentAuthMethodName := localComponentAuthMethodName

		// Create the connect-inject ACL Policy, Role and BindingRule but do not issue any ACLTokens or create Kube Secrets.
		// ConnectInjector token must be global. This means secondary datacenters need
		// a token that is known by the primary datacenters.
		if !primary {
			componentAuthMethodName = globalComponentAuthMethodName
		}
		err = c.createACLPolicyRoleAndBindingRule("connect-inject", injectRules, consulDC, primaryDC, globalPolicy, primary, componentAuthMethodName, serviceAccountName, dynamicClient)
		if err != nil {
			c.log.Error(err.Error())
			return 1
		}
	}

	if c.flagCreateEntLicenseToken {
		var err error
		if c.consulFlags.Partition != "" {
			err = c.createLocalACL("enterprise-license", entPartitionLicenseRules, consulDC, primary, dynamicClient)
		} else {
			err = c.createLocalACL("enterprise-license", entLicenseRules, consulDC, primary, dynamicClient)
		}
		if err != nil {
			c.log.Error(err.Error())
			return 1
		}
	}

	if c.flagSnapshotAgent {
		serviceAccountName := c.withPrefix("server")
		if err := c.createACLPolicyRoleAndBindingRule("snapshot-agent", snapshotAgentRules, consulDC, primaryDC, localPolicy, primary, localComponentAuthMethodName, serviceAccountName, dynamicClient); err != nil {
			c.log.Error(err.Error())
			return 1
		}
	}

	if c.flagMeshGateway {
		rules, err := c.meshGatewayRules()
		if err != nil {
			c.log.Error("Error templating mesh gateway rules", "err", err)
			return 1
		}
		serviceAccountName := c.withPrefix("mesh-gateway")

		// Mesh gateways require a global policy/token because they must
		// discover services in other datacenters.
		authMethodName := localComponentAuthMethodName
		if !primary {
			authMethodName = globalComponentAuthMethodName
		}
		err = c.createACLPolicyRoleAndBindingRule("mesh-gateway", rules, consulDC, primaryDC, globalPolicy, primary, authMethodName, serviceAccountName, dynamicClient)
		if err != nil {
			c.log.Error(err.Error())
			return 1
		}
	}

	if len(c.flagIngressGatewayNames) > 0 {
		params := ConfigureGatewayParams{
			GatewayType:    "ingress",
			GatewayNames:   c.flagIngressGatewayNames,
			AuthMethodName: localComponentAuthMethodName,
			RulesGenerator: c.ingressGatewayRules,
			ConsulDC:       consulDC,
			PrimaryDC:      primaryDC,
			Primary:        primary,
		}
		err := c.configureGateway(params, dynamicClient)
		if err != nil {
			c.log.Error(err.Error())
			return 1
		}
	}

	if len(c.flagTerminatingGatewayNames) > 0 {
		params := ConfigureGatewayParams{
			GatewayType:    "terminating",
			GatewayNames:   c.flagTerminatingGatewayNames,
			AuthMethodName: localComponentAuthMethodName,
			RulesGenerator: c.terminatingGatewayRules,
			ConsulDC:       consulDC,
			PrimaryDC:      primaryDC,
			Primary:        primary,
		}
		err := c.configureGateway(params, dynamicClient)
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
		if aclReplicationToken != "" {
			err = c.createACLWithSecretID(common.ACLReplicationTokenName, rules, consulDC, primary, dynamicClient, aclReplicationToken, false)
		} else {
			err = c.createGlobalACL(common.ACLReplicationTokenName, rules, consulDC, primary, dynamicClient)
		}
		if err != nil {
			c.log.Error(err.Error())
			return 1
		}
	}

	if c.flagCreateDDAgentToken {
		var err error
		rules, err := c.datadogAgentRules()
		if err != nil {
			c.log.Error("Error templating datadog agent metrics token rules", "err", err)
			return 1
		}
		err = c.createLocalACL(common.DatadogAgentTokenName, rules, consulDC, primary, dynamicClient)
		if err != nil {
			c.log.Error(err.Error())
			return 1
		}
	}

	if c.flagDNSProxy {
		serviceAccountName := c.withPrefix(dnsProxyName)

		dnsProxyRules, err := c.dnsProxyRules()
		if err != nil {
			c.log.Error("Error templating dns-proxy rules", "err", err)
			return 1
		}

		if err := c.createACLPolicyRoleAndBindingRule(dnsProxyName, dnsProxyRules, consulDC, primaryDC, localPolicy, primary, localComponentAuthMethodName, serviceAccountName, dynamicClient); err != nil {
			c.log.Error(err.Error())
			return 1
		}
	}

	c.log.Info("server-acl-init completed successfully")
	return 0
}

// exponentialBackoffWithMaxInterval creates an exponential backoff but limits the
// maximum backoff to 10 seconds so that we don't find ourselves in a situation
// where we are waiting for minutes before retries.
func exponentialBackoffWithMaxInterval() *backoff.ExponentialBackOff {
	backoff := backoff.NewExponentialBackOff()
	backoff.MaxInterval = 10 * time.Second
	backoff.Reset()
	return backoff
}

// configureGlobalComponentAuthMethod sets up an AuthMethod in the primary datacenter,
// that the Consul components will use to issue global ACL tokens with.
func (c *Command) configureGlobalComponentAuthMethod(client *consul.DynamicClient, authMethodName, primaryDC string) error {
	// Create the auth method template. This requires calls to the kubernetes environment.
	authMethod, err := c.createAuthMethodTmpl(authMethodName, false)
	if err != nil {
		return err
	}
	authMethod.TokenLocality = "global"
	writeOptions := &api.WriteOptions{Datacenter: primaryDC}
	return c.createAuthMethod(client, &authMethod, writeOptions)
}

// configureLocalComponentAuthMethod sets up an AuthMethod in the same datacenter,
// that the Consul components will use to issue local ACL tokens with.
func (c *Command) configureLocalComponentAuthMethod(client *consul.DynamicClient, authMethodName string) error {
	// Create the auth method template. This requires calls to the kubernetes environment.
	authMethod, err := c.createAuthMethodTmpl(authMethodName, false)
	if err != nil {
		return err
	}
	return c.createAuthMethod(client, &authMethod, &api.WriteOptions{})
}

// createAuthMethod creates the desired Authmethod.
func (c *Command) createAuthMethod(client *consul.DynamicClient, authMethod *api.ACLAuthMethod, writeOptions *api.WriteOptions) error {
	return c.untilSucceeds(fmt.Sprintf("creating auth method %s", authMethod.Name),
		func() error {
			var err error
			err = client.RefreshClient()
			if err != nil {
				c.log.Error("could not refresh client", err)
			}
			// `AuthMethodCreate` will also be able to update an existing
			// AuthMethod based on the name provided. This means that any
			// configuration changes will correctly update the AuthMethod.
			_, _, err = client.ConsulClient.ACL().AuthMethodCreate(authMethod, writeOptions)
			return err
		})
}

type gatewayRulesGenerator func(name, namespace string) (string, error)

// ConfigureGatewayParams are parameters used to configure Ingress and Terminating Gateways.
type ConfigureGatewayParams struct {
	// GatewayType specifies whether it is an ingress or terminating gateway.
	GatewayType string
	// GatewayNames is the collection of gateways that have been specified.
	GatewayNames []string
	// AuthMethodName is the authmethod for which to register the binding rules and policies for the gateways
	AuthMethodName string
	// RuleGenerator is the function that supplies the rules that will be added to the policy.
	RulesGenerator gatewayRulesGenerator
	// ConsulDC is the name of the DC where the gateways will be registered
	ConsulDC string
	// PrimaryDC is the name of the Primary Data Center
	PrimaryDC string
	// Primary specifies whether the ConsulDC is the Primary Data Center
	Primary bool
}

func (c *Command) configureGateway(gatewayParams ConfigureGatewayParams, client *consul.DynamicClient) error {
	// Each gateway needs to be configured
	// separately because users may need to attach different policies
	// to each gateway role depending on what services it represents.
	for _, name := range gatewayParams.GatewayNames {
		if name == "" {
			errMessage := fmt.Sprintf("%s gateway name cannot be empty",
				cases.Title(language.English).String(gatewayParams.GatewayType))
			c.log.Error(errMessage)
			return errors.New(errMessage)
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
			errMessage := "gateway names shouldn't include a namespace if Consul namespaces aren't enabled"
			c.log.Error(errMessage, "gateway-name", name)
			return errors.New(errMessage)
		}

		// Define the gateway rules
		rules, err := gatewayParams.RulesGenerator(name, namespace)
		if err != nil {

			errMessage := fmt.Sprintf("error templating %s gateway rules",
				gatewayParams.GatewayType)
			c.log.Error(errMessage, "gateway-name", name,
				"namespace", namespace, "err", err)
			return errors.New(errMessage)
		}

		// The names in the Helm chart are specified by users and so may not contain
		// the words "ingress-gateway" or "terminating-gateway". We need to create unique names for tokens
		// across all gateway types and so must suffix with either `-ingress-gateway` of `-terminating-gateway`.
		serviceAccountName := c.withPrefix(name)
		err = c.createACLPolicyRoleAndBindingRule(name, rules,
			gatewayParams.ConsulDC, gatewayParams.PrimaryDC, localPolicy,
			gatewayParams.Primary, gatewayParams.AuthMethodName, serviceAccountName, client)
		if err != nil {
			c.log.Error(err.Error())
			return err
		}
	}
	return nil
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

// configureSecretsBackend configures either the Kubernetes or Vault
// secrets backend based on flags.
func (c *Command) configureSecretsBackend() error {
	if c.backend != nil {
		// support a fake backend in unit tests
		return nil
	}
	secretName := c.flagBootstrapTokenSecretName
	if secretName == "" {
		secretName = c.withPrefix("bootstrap-acl-token")
	}

	secretKey := c.flagBootstrapTokenSecretKey
	if secretKey == "" {
		secretKey = common.ACLTokenSecretKey
	}

	switch c.flagSecretsBackend {
	case SecretsBackendTypeKubernetes:
		c.backend = &KubernetesSecretsBackend{
			ctx:          c.ctx,
			clientset:    c.clientset,
			k8sNamespace: c.flagK8sNamespace,
			secretName:   secretName,
			secretKey:    secretKey,
		}
		return nil
	case SecretsBackendTypeVault:
		cfg := vaultApi.DefaultConfig()
		cfg.Address = ""
		cfg.AgentAddress = "http://127.0.0.1:8200"
		vaultClient, err := vaultApi.NewClient(cfg)
		if err != nil {
			return fmt.Errorf("Error initializing Vault client: %w", err)
		}

		c.vaultClient = vaultClient // must set this for c.quitVaultAgent.
		c.backend = &VaultSecretsBackend{
			vaultClient: c.vaultClient,
			secretName:  secretName,
			secretKey:   secretKey,
		}
		return nil
	default:
		validValues := []SecretsBackendType{SecretsBackendTypeKubernetes, SecretsBackendTypeVault}
		return fmt.Errorf("Invalid value for -secrets-backend: %q. Valid values are %v.", c.flagSecretsBackend, validValues)
	}
}

// untilSucceeds runs op until it returns a nil error.
// If c.timeoutDuration is reached it will exit so that the command can be retried with updated server settings
// If c.cmdTimeout is cancelled it will exit.
func (c *Command) untilSucceeds(opName string, op func() error) error {
	timeoutCh := time.After(c.apiTimeoutDuration)
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
		case <-timeoutCh:
			return errors.New("reached api timeout")
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
func (c *Command) consulDatacenterList(client *consul.DynamicClient) (string, string, error) {
	var agentCfg map[string]map[string]interface{}
	err := c.untilSucceeds("calling /agent/self to get datacenter",
		func() error {
			var opErr error
			opErr = client.RefreshClient()
			if opErr != nil {
				c.log.Error("could not refresh client", opErr)
			}
			agentCfg, opErr = client.ConsulClient.Agent().Self()
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
			(c.flagConnectInject && c.flagFederation))
}

func (c *Command) validateFlags() error {
	if c.consulFlags.Addresses == "" {
		return errors.New("-addresses must be set")
	}

	if c.flagResourcePrefix == "" {
		return errors.New("-resource-prefix must be set")
	}

	// For the Consul node name to be discoverable via DNS, it must contain only
	// dashes and alphanumeric characters. Length is also constrained.
	// These restrictions match those defined in Consul's agent definition.
	invalidDnsRe := regexp.MustCompile(`[^A-Za-z0-9\\-]+`)
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

	if c.consulFlags.APITimeout <= 0 {
		return errors.New("-consul-api-timeout must be set to a value greater than 0")
	}

	//if c.flagVaultNamespace != "" && c.flagSecretsBackend != SecretsBackendTypeVault {
	//	return fmt.Errorf("-vault-namespace not supported for -secrets-backend=%q", c.flagSecretsBackend)
	//}

	return nil
}

func loadTokenFromFile(tokenFile string) (string, error) {
	// Load the bootstrap token from file.
	tokenBytes, err := os.ReadFile(tokenFile)
	if err != nil {
		return "", fmt.Errorf("unable to read token from file %q: %s", tokenFile, err)
	}
	if len(tokenBytes) == 0 {
		return "", fmt.Errorf("token file %q is empty", tokenFile)
	}
	return strings.TrimSpace(string(tokenBytes)), nil
}

func (c *Command) quitVaultAgent() {
	if c.vaultClient == nil {
		return
	}

	// Tell the Vault agent sidecar to quit. Without this, the Job does not
	// complete because the Vault agent does not stop. This retries because it
	// does not know exactly when the Vault agent sidecar will start.
	err := c.untilSucceeds("tell Vault agent to quit", func() error {
		// TODO: RawRequest is deprecated, but there is also not a high level
		// method for this in the Vault client.
		// nolint:staticcheck // SA1004 ignore
		_, err := c.vaultClient.RawRequest(
			c.vaultClient.NewRequest("POST", "/agent/v1/quit"),
		)
		return err
	})
	if err != nil {
		c.log.Error("Error telling Vault agent to quit", "error", err)
	}
}

// serverIPAddresses attempts to refresh the server IPs using netaddrs methods. These 'raw' IPs are used
// when boostrapping ACLs and before consul-server-connection-manager runs.
func (c *Command) serverIPAddresses() ([]net.IPAddr, error) {
	var ipAddrs []net.IPAddr
	var err error
	if err = backoff.Retry(func() error {
		ipAddrs, err = netaddrs.IPAddrs(c.ctx, c.consulFlags.Addresses, c.log)
		if err != nil {
			c.log.Error("Error resolving IP Address", "err", err)
			return err
		}
		c.log.Info("Refreshing server IP addresses", "addresses", ipAddrs)
		return nil
	}, exponentialBackoffWithMaxInterval()); err != nil {
		return nil, err
	}
	return ipAddrs, nil
}

const (
	consulDefaultNamespace = "default"
	consulDefaultPartition = "default"
	globalPolicy           = true
	localPolicy            = false
	synopsis               = "Initialize ACLs on Consul servers and other components."
	help                   = `
Usage: consul-k8s-control-plane server-acl-init [options]

  Bootstraps servers with ACLs and creates policies and ACL tokens for other
  components as Kubernetes Secrets.
  It will run indefinitely until all tokens have been created. It is idempotent
  and safe to run multiple times.

`
)
