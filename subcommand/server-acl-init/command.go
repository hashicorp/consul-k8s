package serveraclinit

import (
	"context"
	"errors"
	"flag"
	"os"
	"strings"
	"sync"
	"time"

	"fmt"
	"github.com/hashicorp/consul-k8s/subcommand"
	k8sflags "github.com/hashicorp/consul-k8s/subcommand/flags"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/command/flags"
	"github.com/hashicorp/go-hclog"
	"github.com/mitchellh/cli"
	apiv1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type Command struct {
	UI cli.Ui

	flags                        *flag.FlagSet
	k8s                          *k8sflags.K8SFlags
	flagReleaseName              string
	flagServerLabelSelector      string
	flagResourcePrefix           string
	flagReplicas                 int
	flagNamespace                string
	flagAllowDNS                 bool
	flagCreateClientToken        bool
	flagCreateSyncToken          bool
	flagCreateInjectToken        bool
	flagCreateInjectAuthMethod   bool
	flagBindingRuleSelector      string
	flagCreateEntLicenseToken    bool
	flagCreateSnapshotAgentToken bool
	flagCreateMeshGatewayToken   bool
	flagConsulCACert             string
	flagConsulTLSServerName      string
	flagUseHTTPS                 bool

	// Flags to support namespaces
	flagEnableNamespaces    bool   // Use namespacing on all components
	flagConsulSyncNamespace string // Consul namespace to register all catalog sync services into if not mirroring
	flagEnableNSMirroring   bool   // Enables mirroring of k8s namespaces into Consul
	flagMirroringPrefix     string // Prefix added to Consul namespaces created when mirroring

	flagLogLevel string
	flagTimeout  string

	clientset kubernetes.Interface
	// cmdTimeout is cancelled when the command timeout is reached.
	cmdTimeout    context.Context
	retryDuration time.Duration

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
	c.flags.StringVar(&c.flagNamespace, "k8s-namespace", "",
		"Name of Kubernetes namespace where the servers are deployed")
	c.flags.BoolVar(&c.flagAllowDNS, "allow-dns", false,
		"Toggle for updating the anonymous token to allow DNS queries to work")
	c.flags.BoolVar(&c.flagCreateClientToken, "create-client-token", true,
		"Toggle for creating a client agent token")
	c.flags.BoolVar(&c.flagCreateSyncToken, "create-sync-token", false,
		"Toggle for creating a catalog sync token")
	c.flags.BoolVar(&c.flagCreateInjectToken, "create-inject-namespace-token", false,
		"Toggle for creating a connect injector token. Only required when namespaces are enabled")
	c.flags.BoolVar(&c.flagCreateInjectAuthMethod, "create-inject-auth-method", false,
		"Toggle for creating a connect inject auth method. Deprecated: use -create-inject-auth-method instead.")
	c.flags.BoolVar(&c.flagCreateInjectAuthMethod, "create-inject-token", false,
		"Toggle for creating a connect inject auth method")
	c.flags.StringVar(&c.flagBindingRuleSelector, "acl-binding-rule-selector", "",
		"Selector string for connectInject ACL Binding Rule")
	c.flags.BoolVar(&c.flagCreateEntLicenseToken, "create-enterprise-license-token", false,
		"Toggle for creating a token for the enterprise license job")
	c.flags.BoolVar(&c.flagCreateSnapshotAgentToken, "create-snapshot-agent-token", false,
		"Toggle for creating a token for the Consul snapshot agent deployment (enterprise only)")
	c.flags.BoolVar(&c.flagCreateMeshGatewayToken, "create-mesh-gateway-token", false,
		"Toggle for creating a token for a Connect mesh gateway")
	c.flags.StringVar(&c.flagConsulCACert, "consul-ca-cert", "",
		"Path to the PEM-encoded CA certificate of the Consul cluster.")
	c.flags.StringVar(&c.flagConsulTLSServerName, "consul-tls-server-name", "",
		"The server name to set as the SNI header when sending HTTPS requests to Consul.")
	c.flags.BoolVar(&c.flagUseHTTPS, "use-https", false,
		"Toggle for using HTTPS for all API calls to Consul.")
	c.flags.BoolVar(&c.flagEnableNamespaces, "enable-namespaces", false,
		"Enables namespaces, in either a single Consul namespace or mirrored [Enterprise only feature]")
	c.flags.StringVar(&c.flagConsulSyncNamespace, "consul-sync-namespace", "default",
		"Defines which Consul namespace to have catalog sync register services into. If `-enable-namespace-mirroring` "+
			"is true, this is not used.")
	c.flags.BoolVar(&c.flagEnableNSMirroring, "enable-namespace-mirroring", false, "Enables namespace mirroring")
	c.flags.StringVar(&c.flagMirroringPrefix, "mirroring-prefix", "",
		"Prefix that will be added to all k8s namespaces mirrored into Consul if mirroring is enabled.")
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

	// Configure our logger.
	level := hclog.LevelFromString(c.flagLogLevel)
	if level == hclog.NoLevel {
		c.UI.Error(fmt.Sprintf("Unknown log level: %s", c.flagLogLevel))
		return 1
	}
	logger := hclog.New(&hclog.LoggerOptions{
		Level:  level,
		Output: os.Stderr,
	})

	// The ClientSet might already be set if we're in a test.
	if c.clientset == nil {
		if err := c.configureKubeClient(); err != nil {
			logger.Error(err.Error())
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
		statefulset, err := c.clientset.AppsV1().StatefulSets(c.flagNamespace).Get(ssName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if statefulset.Status.CurrentRevision == statefulset.Status.UpdateRevision {
			return nil
		}
		return fmt.Errorf("rollout is in progress (CurrentRevision=%s UpdateRevision=%s)",
			statefulset.Status.CurrentRevision, statefulset.Status.UpdateRevision)
	}, logger)
	if err != nil {
		logger.Error(err.Error())
		return 1
	}

	// Check if we've already been bootstrapped.
	bootTokenSecretName := c.withPrefix("bootstrap-acl-token")
	bootstrapToken, err := c.getBootstrapToken(logger, bootTokenSecretName)
	if err != nil {
		logger.Error(fmt.Sprintf("Unexpected error looking for preexisting bootstrap Secret: %s", err))
		return 1
	}

	if bootstrapToken != "" {
		logger.Info(fmt.Sprintf("ACLs already bootstrapped - retrieved bootstrap token from Secret %q", bootTokenSecretName))
	} else {
		logger.Info("No bootstrap token from previous installation found, continuing on to bootstrapping")
		bootstrapToken, err = c.bootstrapServers(logger, bootTokenSecretName, scheme)
		if err != nil {
			logger.Error(err.Error())
			return 1
		}
	}

	// For all of the next operations we'll need a Consul client.
	serverPods, err := c.getConsulServers(logger, 1, scheme)
	if err != nil {
		logger.Error(err.Error())
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
		logger.Error(fmt.Sprintf("Error creating Consul client for addr %q: %s", serverAddr, err))
		return 1
	}

	if c.flagCreateClientToken {
		agentRules, err := c.agentRules()
		if err != nil {
			logger.Error("Error templating client agent rules", "err", err)
			return 1
		}

		err = c.createACL("client", agentRules, consulClient, logger)
		if err != nil {
			logger.Error(err.Error())
			return 1
		}
	}

	if c.flagAllowDNS {
		err := c.configureDNSPolicies(logger, consulClient)
		if err != nil {
			logger.Error(err.Error())
			return 1
		}
	}

	if c.flagCreateSyncToken {
		syncRules, err := c.syncRules()
		if err != nil {
			logger.Error("Error templating sync rules", "err", err)
			return 1
		}

		err = c.createACL("catalog-sync", syncRules, consulClient, logger)
		if err != nil {
			logger.Error(err.Error())
			return 1
		}
	}

	if c.flagCreateInjectToken {
		injectRules, err := c.injectRules()
		if err != nil {
			logger.Error("Error templating inject rules", "err", err)
			return 1
		}

		err = c.createACL("connect-inject", injectRules, consulClient, logger)
		if err != nil {
			logger.Error(err.Error())
			return 1
		}
	}

	if c.flagCreateEntLicenseToken {
		err := c.createACL("enterprise-license", entLicenseRules, consulClient, logger)
		if err != nil {
			logger.Error(err.Error())
			return 1
		}
	}

	if c.flagCreateSnapshotAgentToken {
		err := c.createACL("client-snapshot-agent", snapshotAgentRules, consulClient, logger)
		if err != nil {
			logger.Error(err.Error())
			return 1
		}
	}

	if c.flagCreateMeshGatewayToken {
		meshGatewayRules, err := c.meshGatewayRules()
		if err != nil {
			logger.Error("Error templating dns rules", "err", err)
			return 1
		}

		err = c.createACL("mesh-gateway", meshGatewayRules, consulClient, logger)
		if err != nil {
			logger.Error(err.Error())
			return 1
		}
	}

	if c.flagCreateInjectAuthMethod {
		err := c.configureConnectInject(logger, consulClient)
		if err != nil {
			logger.Error(err.Error())
			return 1
		}
	}

	logger.Info("server-acl-init completed successfully")
	return 0
}

// getBootstrapToken returns the existing bootstrap token if there is one by
// reading the Kubernetes Secret with name secretName.
// If there is no bootstrap token yet, then it returns an empty string (not an error).
func (c *Command) getBootstrapToken(logger hclog.Logger, secretName string) (string, error) {
	secret, err := c.clientset.CoreV1().Secrets(c.flagNamespace).Get(secretName, metav1.GetOptions{})
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

// getConsulServers returns n Consul server pods with their http addresses.
// If there are less server pods than 'n' then the function will wait.
func (c *Command) getConsulServers(logger hclog.Logger, n int, scheme string) ([]podAddr, error) {
	var serverPods *apiv1.PodList
	err := c.untilSucceeds("discovering Consul server pods",
		func() error {
			var err error
			serverPods, err = c.clientset.CoreV1().Pods(c.flagNamespace).List(metav1.ListOptions{LabelSelector: c.flagServerLabelSelector})
			if err != nil {
				return err
			}

			if len(serverPods.Items) == 0 {
				return fmt.Errorf("no server pods with labels %q found", c.flagServerLabelSelector)
			}

			if len(serverPods.Items) < n {
				return fmt.Errorf("found %d servers, require %d", len(serverPods.Items), n)
			}

			for _, pod := range serverPods.Items {
				if pod.Status.PodIP == "" {
					return fmt.Errorf("pod %s has no IP", pod.Name)
				}
			}
			return nil
		}, logger)
	if err != nil {
		return nil, err
	}

	var podAddrs []podAddr
	for _, pod := range serverPods.Items {
		var httpPort int32
		for _, p := range pod.Spec.Containers[0].Ports {
			if p.Name == scheme {
				httpPort = p.ContainerPort
			}
		}
		if httpPort == 0 {
			return nil, fmt.Errorf("pod %s has no port labeled '%s'", pod.Name, scheme)
		}
		addr := fmt.Sprintf("%s:%d", pod.Status.PodIP, httpPort)
		podAddrs = append(podAddrs, podAddr{
			Name: pod.Name,
			Addr: addr,
		})
	}
	return podAddrs, nil
}

// bootstrapServers bootstraps ACLs and ensures each server has an ACL token.
func (c *Command) bootstrapServers(logger hclog.Logger, bootTokenSecretName, scheme string) (string, error) {
	serverPods, err := c.getConsulServers(logger, c.flagReplicas, scheme)
	if err != nil {
		return "", err
	}
	logger.Info(fmt.Sprintf("Found %d Consul server Pods", len(serverPods)))

	// Pick the first pod to connect to for bootstrapping and set up connection.
	firstServerAddr := serverPods[0].Addr
	consulClient, err := api.NewClient(&api.Config{
		Address: firstServerAddr,
		Scheme:  scheme,
		TLSConfig: api.TLSConfig{
			Address: c.flagConsulTLSServerName,
			CAFile:  c.flagConsulCACert,
		},
	})
	if err != nil {
		return "", fmt.Errorf("creating Consul client for address %s: %s", firstServerAddr, err)
	}

	// Call bootstrap ACLs API.
	var bootstrapToken []byte
	var unrecoverableErr error
	err = c.untilSucceeds("bootstrapping ACLs - PUT /v1/acl/bootstrap",
		func() error {
			bootstrapResp, _, err := consulClient.ACL().Bootstrap()
			if err == nil {
				bootstrapToken = []byte(bootstrapResp.SecretID)
				return nil
			}

			// Check if already bootstrapped.
			if strings.Contains(err.Error(), "Unexpected response code: 403") {
				unrecoverableErr = errors.New("ACLs already bootstrapped but the ACL token was not written to a Kubernetes secret." +
					" We can't proceed because the bootstrap token is lost." +
					" You must reset ACLs.")
				return nil
			}

			if isNoLeaderErr(err) {
				// Return a more descriptive error in the case of no leader
				// being elected.
				return fmt.Errorf("no leader elected: %s", err)
			}
			return err
		}, logger)
	if unrecoverableErr != nil {
		return "", unrecoverableErr
	}
	if err != nil {
		return "", err
	}

	// Write bootstrap token to a Kubernetes secret.
	err = c.untilSucceeds(fmt.Sprintf("writing bootstrap Secret %q", bootTokenSecretName),
		func() error {
			secret := &apiv1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: bootTokenSecretName,
				},
				Data: map[string][]byte{
					"token": bootstrapToken,
				},
			}
			_, err := c.clientset.CoreV1().Secrets(c.flagNamespace).Create(secret)
			return err
		}, logger)
	if err != nil {
		return "", err
	}

	// Override our original client with a new one that has the bootstrap token
	// set.
	consulClient, err = api.NewClient(&api.Config{
		Address: firstServerAddr,
		Scheme:  scheme,
		Token:   string(bootstrapToken),
		TLSConfig: api.TLSConfig{
			Address: c.flagConsulTLSServerName,
			CAFile:  c.flagConsulCACert,
		},
	})
	if err != nil {
		return "", fmt.Errorf("creating Consul client for address %s: %s", firstServerAddr, err)
	}

	// Create new tokens for each server and apply them.
	if err := c.setServerTokens(logger, consulClient, serverPods, string(bootstrapToken), scheme); err != nil {
		return "", err
	}
	return string(bootstrapToken), nil
}

// setServerTokens creates policies and associated ACL token for each server
// and then provides the token to the server.
func (c *Command) setServerTokens(logger hclog.Logger, consulClient *api.Client,
	serverPods []podAddr, bootstrapToken, scheme string) error {

	agentRules, err := c.agentRules()
	if err != nil {
		logger.Error("Error templating server agent rules", "err", err)
		return err
	}

	// Create agent policy.
	agentPolicy := api.ACLPolicy{
		Name:        "agent-token",
		Description: "Agent Token Policy",
		Rules:       agentRules,
	}
	err = c.untilSucceeds("creating agent policy - PUT /v1/acl/policy",
		func() error {
			_, _, err := consulClient.ACL().PolicyCreate(&agentPolicy, nil)
			if isPolicyExistsErr(err, agentPolicy.Name) {
				logger.Info(fmt.Sprintf("Policy %q already exists", agentPolicy.Name))
				return nil
			}
			return err
		}, logger)
	if err != nil {
		return err
	}

	// Create agent token for each server agent.
	var serverTokens []api.ACLToken
	for _, pod := range serverPods {
		var token *api.ACLToken
		err := c.untilSucceeds(fmt.Sprintf("creating server token for %s - PUT /v1/acl/token", pod.Name),
			func() error {
				tokenReq := api.ACLToken{
					Description: fmt.Sprintf("Server Token for %s", pod.Name),
					Policies:    []*api.ACLTokenPolicyLink{{Name: agentPolicy.Name}},
				}
				var err error
				token, _, err = consulClient.ACL().TokenCreate(&tokenReq, nil)
				return err
			}, logger)
		if err != nil {
			return err
		}
		serverTokens = append(serverTokens, *token)
	}

	// Pass out agent tokens to servers.
	for i, pod := range serverPods {
		// We create a new client for each server because we need to call each
		// server specifically.
		serverClient, err := api.NewClient(&api.Config{
			Address: pod.Addr,
			Scheme:  scheme,
			Token:   bootstrapToken,
			TLSConfig: api.TLSConfig{
				Address: c.flagConsulTLSServerName,
				CAFile:  c.flagConsulCACert,
			},
		})
		if err != nil {
			return fmt.Errorf(" creating Consul client for address %q: %s", pod.Addr, err)
		}
		podName := pod.Name

		// Update token.
		err = c.untilSucceeds(fmt.Sprintf("updating server token for %s - PUT /v1/agent/token/agent", podName),
			func() error {
				_, err := serverClient.Agent().UpdateAgentACLToken(serverTokens[i].SecretID, nil)
				return err
			}, logger)
		if err != nil {
			return err
		}
	}
	return nil
}

// createACL creates a policy with rules and name, creates an ACL token for that
// policy and then writes the token to a Kubernetes secret.
func (c *Command) createACL(name, rules string, consulClient *api.Client, logger hclog.Logger) error {
	// Check if the secret already exists, if so, we assume the ACL has already been created.
	secretName := c.withPrefix(name + "-acl-token")
	_, err := c.clientset.CoreV1().Secrets(c.flagNamespace).Get(secretName, metav1.GetOptions{})
	if err == nil {
		logger.Info(fmt.Sprintf("Secret %q already exists", secretName))
		return nil
	}

	// Create policy with the given rules.
	policyTmpl := api.ACLPolicy{
		Name:        fmt.Sprintf("%s-token", name),
		Description: fmt.Sprintf("%s Token Policy", name),
		Rules:       rules,
	}
	err = c.untilSucceeds(fmt.Sprintf("creating %s policy", policyTmpl.Name),
		func() error {
			_, _, err := consulClient.ACL().PolicyCreate(&policyTmpl, &api.WriteOptions{})
			if isPolicyExistsErr(err, policyTmpl.Name) {
				logger.Info(fmt.Sprintf("Policy %q already exists", policyTmpl.Name))
				return nil
			}
			return err
		}, logger)
	if err != nil {
		return err
	}

	// Create token for the policy.
	tokenTmpl := api.ACLToken{
		Description: fmt.Sprintf("%s Token", name),
		Policies:    []*api.ACLTokenPolicyLink{{Name: policyTmpl.Name}},
	}
	var token string
	err = c.untilSucceeds(fmt.Sprintf("creating token for policy %s", policyTmpl.Name),
		func() error {
			createdToken, _, err := consulClient.ACL().TokenCreate(&tokenTmpl, &api.WriteOptions{})
			if err == nil {
				token = createdToken.SecretID
			}
			return err
		}, logger)
	if err != nil {
		return err
	}

	// Write token to a Kubernetes secret.
	return c.untilSucceeds(fmt.Sprintf("writing Secret for token %s", policyTmpl.Name),
		func() error {
			secret := &apiv1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: secretName,
				},
				Data: map[string][]byte{
					"token": []byte(token),
				},
			}
			_, err := c.clientset.CoreV1().Secrets(c.flagNamespace).Create(secret)
			return err
		}, logger)
}

// configureDNSPolicies sets up policies and tokens so that Consul DNS will
// work.
func (c *Command) configureDNSPolicies(logger hclog.Logger, consulClient *api.Client) error {
	dnsRules, err := c.dnsRules()
	if err != nil {
		logger.Error("Error templating dns rules", "err", err)
		return err
	}

	// Create policy for the anonymous token
	dnsPolicy := api.ACLPolicy{
		Name:        "dns-policy",
		Description: "DNS Policy",
		Rules:       dnsRules,
	}

	err = c.untilSucceeds("creating dns policy - PUT /v1/acl/policy",
		func() error {
			_, _, err := consulClient.ACL().PolicyCreate(&dnsPolicy, nil)
			if isPolicyExistsErr(err, dnsPolicy.Name) {
				logger.Info(fmt.Sprintf("Policy %q already exists", dnsPolicy.Name))
				return nil
			}
			return err
		}, logger)
	if err != nil {
		return err
	}

	// Create token to get sent to TokenUpdate
	aToken := api.ACLToken{
		AccessorID: "00000000-0000-0000-0000-000000000002",
		Policies:   []*api.ACLTokenPolicyLink{{Name: dnsPolicy.Name}},
	}

	// Update anonymous token to include this policy
	return c.untilSucceeds("updating anonymous token with DNS policy",
		func() error {
			_, _, err := consulClient.ACL().TokenUpdate(&aToken, &api.WriteOptions{})
			return err
		}, logger)
}

// configureConnectInject sets up auth methods so that connect injection will
// work.
func (c *Command) configureConnectInject(logger hclog.Logger, consulClient *api.Client) error {
	// First, check if there's already an acl binding rule. If so, then this
	// work is already done.
	authMethodName := c.withPrefix("k8s-auth-method")
	var existingRules []*api.ACLBindingRule
	err := c.untilSucceeds(fmt.Sprintf("listing binding rules for auth method %s", authMethodName),
		func() error {
			var err error
			existingRules, _, err = consulClient.ACL().BindingRuleList(authMethodName, nil)
			return err
		}, logger)
	if err != nil {
		return err
	}
	if len(existingRules) > 0 {
		logger.Info(fmt.Sprintf("Binding rule for %s already exists", authMethodName))
		return nil
	}

	var kubeSvc *apiv1.Service
	err = c.untilSucceeds("getting kubernetes service IP",
		func() error {
			var err error
			kubeSvc, err = c.clientset.CoreV1().Services("default").Get("kubernetes", metav1.GetOptions{})
			return err
		}, logger)
	if err != nil {
		return err
	}

	// Get the Secret name for the auth method ServiceAccount.
	var authMethodServiceAccount *apiv1.ServiceAccount
	saName := c.withPrefix("connect-injector-authmethod-svc-account")
	err = c.untilSucceeds(fmt.Sprintf("getting %s ServiceAccount", saName),
		func() error {
			var err error
			authMethodServiceAccount, err = c.clientset.CoreV1().ServiceAccounts(c.flagNamespace).Get(saName, metav1.GetOptions{})
			return err
		}, logger)
	if err != nil {
		return err
	}

	// ServiceAccounts always have a secret name. The secret
	// contains the JWT token.
	saSecretName := authMethodServiceAccount.Secrets[0].Name

	// Get the secret that will contain the ServiceAccount JWT token.
	var saSecret *apiv1.Secret
	err = c.untilSucceeds(fmt.Sprintf("getting %s Secret", saSecretName),
		func() error {
			var err error
			saSecret, err = c.clientset.CoreV1().Secrets(c.flagNamespace).Get(saSecretName, metav1.GetOptions{})
			return err
		}, logger)
	if err != nil {
		return err
	}

	// Now we're ready to set up Consul's auth method.
	authMethodTmpl := api.ACLAuthMethod{
		Name:        authMethodName,
		Description: "Kubernetes AuthMethod",
		Type:        "kubernetes",
		Config: map[string]interface{}{
			"Host":              fmt.Sprintf("https://%s:443", kubeSvc.Spec.ClusterIP),
			"CACert":            string(saSecret.Data["ca.crt"]),
			"ServiceAccountJWT": string(saSecret.Data["token"]),
		},
	}

	// Add options for mirroring namespaces
	if c.flagEnableNSMirroring {
		authMethodTmpl.Config["MapNamespaces"] = true
		authMethodTmpl.Config["ConsulNamespacePrefix"] = c.flagMirroringPrefix
	}

	// Set up the auth method in the specific namespace if not mirroring
	// If namespaces and mirroring are enabled, this is not necessary because
	// the auth method will fall back to being created in the Consul `default`
	// namespace automatically, as is necessary for mirroring.
	writeOptions := api.WriteOptions{}
	if c.flagEnableNamespaces && !c.flagEnableNSMirroring {
		writeOptions.Namespace = c.flagConsulSyncNamespace
	}

	var authMethod *api.ACLAuthMethod
	err = c.untilSucceeds(fmt.Sprintf("creating auth method %s", authMethodTmpl.Name),
		func() error {
			var err error
			authMethod, _, err = consulClient.ACL().AuthMethodCreate(&authMethodTmpl, &writeOptions)
			return err
		}, logger)
	if err != nil {
		return err
	}

	// Create the binding rule.
	abr := api.ACLBindingRule{
		Description: "Kubernetes binding rule",
		AuthMethod:  authMethod.Name,
		BindType:    api.BindingRuleBindTypeService,
		BindName:    "${serviceaccount.name}",
		Selector:    c.flagBindingRuleSelector,
	}

	// Add a namespace if appropriate
	// If namespaces and mirroring are enabled, this is not necessary because
	// the binding rule will fall back to being created in the Consul `default`
	// namespace automatically, as is necessary for mirroring.
	if c.flagEnableNamespaces && !c.flagEnableNSMirroring {
		abr.Namespace = c.flagConsulSyncNamespace
	}

	return c.untilSucceeds(fmt.Sprintf("creating acl binding rule for %s", authMethodTmpl.Name),
		func() error {
			_, _, err := consulClient.ACL().BindingRuleCreate(&abr, nil)
			return err
		}, logger)
}

// untilSucceeds runs op until it returns a nil error.
// If c.cmdTimeout is cancelled it will exit.
func (c *Command) untilSucceeds(opName string, op func() error, logger hclog.Logger) error {
	for {
		err := op()
		if err == nil {
			logger.Info(fmt.Sprintf("Success: %s", opName))
			break
		}
		logger.Error(fmt.Sprintf("Failure: %s", opName), "err", err)
		logger.Info("Retrying in " + c.retryDuration.String())
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

// isNoLeaderErr returns true if err is due to trying to call the
// bootstrap ACLs API when there is no leader elected.
func isNoLeaderErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "Unexpected response code: 500") &&
		strings.Contains(err.Error(), "The ACL system is currently in legacy mode.")
}

// isPolicyExistsErr returns true if err is due to trying to call the
// policy create API when the policy already exists.
func isPolicyExistsErr(err error, policyName string) bool {
	return err != nil &&
		strings.Contains(err.Error(), "Unexpected response code: 500") &&
		strings.Contains(err.Error(), fmt.Sprintf("Invalid Policy: A Policy with Name %q already exists", policyName))
}

// podAddr is a convenience struct for passing around pod names and
// addresses for Consul servers.
type podAddr struct {
	// Name is the name of the pod.
	Name string
	// Addr is in the form "<ip>:<port>".
	Addr string
}

const synopsis = "Initialize ACLs on Consul servers and other components."
const help = `
Usage: consul-k8s server-acl-init [options]

  Bootstraps servers with ACLs and creates policies and ACL tokens for other
  components as Kubernetes Secrets.
  It will run indefinitely until all tokens have been created. It is idempotent
  and safe to run multiple times.

`
