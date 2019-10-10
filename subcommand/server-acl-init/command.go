package serveraclinit

import (
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
	flagReplicas                 int
	flagNamespace                string
	flagAllowDNS                 bool
	flagCreateClientToken        bool
	flagCreateSyncToken          bool
	flagCreateInjectAuthMethod   bool
	flagBindingRuleSelector      string
	flagCreateEntLicenseToken    bool
	flagCreateSnapshotAgentToken bool
	flagCreateMeshGatewayToken   bool
	flagLogLevel                 string

	clientset kubernetes.Interface

	once sync.Once
	help string
}

func (c *Command) init() {
	c.flags = flag.NewFlagSet("", flag.ContinueOnError)
	c.flags.StringVar(&c.flagReleaseName, "release-name", "",
		"Name of Consul Helm release")
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
	c.flags.BoolVar(&c.flagCreateInjectAuthMethod, "create-inject-token", false,
		"Toggle for creating a connect inject token")
	c.flags.StringVar(&c.flagBindingRuleSelector, "acl-binding-rule-selector", "",
		"Selector string for connectInject ACL Binding Rule")
	c.flags.BoolVar(&c.flagCreateEntLicenseToken, "create-enterprise-license-token", false,
		"Toggle for creating a token for the enterprise license job")
	c.flags.BoolVar(&c.flagCreateSnapshotAgentToken, "create-snapshot-agent-token", false,
		"Toggle for creating a token for the Consul snapshot agent deployment (enterprise only)")
	c.flags.BoolVar(&c.flagCreateMeshGatewayToken, "create-mesh-gateway-token", false,
		"Toggle for creating a token for a Connect mesh gateway")
	c.flags.StringVar(&c.flagLogLevel, "log-level", "info",
		"Log verbosity level. Supported values (in order of detail) are \"trace\", "+
			"\"debug\", \"info\", \"warn\", and \"error\".")

	c.k8s = &k8sflags.K8SFlags{}
	flags.Merge(c.flags, c.k8s.Flags())
	c.help = flags.Usage(help, c.flags)
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

	// The ClientSet might already be set if we're in a test.
	if c.clientset == nil {
		if err := c.configureKubeClient(); err != nil {
			c.UI.Error(err.Error())
			return 1
		}
	}

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

	// Check if we've already been bootstrapped.
	bootTokenSecretName := fmt.Sprintf("%s-consul-bootstrap-acl-token", c.flagReleaseName)
	bootstrapToken, err := c.getBootstrapToken(logger, bootTokenSecretName)
	if err != nil {
		c.UI.Error(fmt.Sprintf("Unexpected error looking for preexisting bootstrap Secret: %s", err))
		return 1
	}

	if bootstrapToken != "" {
		logger.Info(fmt.Sprintf("ACLs already bootstrapped - retrieved bootstrap token from Secret %q", bootTokenSecretName))
	} else {
		logger.Info("No bootstrap token from previous installation found, continuing on to bootstrapping")
		bootstrapToken, err = c.bootstrapServers(logger, bootTokenSecretName)
		if err != nil {
			c.UI.Error(err.Error())
			return 1
		}
	}

	// For all of the next operations we'll need a Consul client.
	serverPods, err := c.getConsulServers(logger, 1)
	if err != nil {
		c.UI.Error(err.Error())
		return 1
	}
	serverAddr := serverPods[0].Addr
	consulClient, err := api.NewClient(&api.Config{
		Address: serverAddr,
		Scheme:  "http",
		Token:   string(bootstrapToken),
	})
	if err != nil {
		c.UI.Error(fmt.Sprintf("Error creating Consul client for addr %q: %s", serverAddr, err))
		return 1
	}

	if c.flagCreateClientToken {
		c.createACL("client", agentRules, consulClient, logger)
	}

	if c.flagAllowDNS {
		c.configureDNSPolicies(logger, consulClient)
	}

	if c.flagCreateSyncToken {
		c.createACL("catalog-sync", syncRules, consulClient, logger)
	}

	if c.flagCreateEntLicenseToken {
		c.createACL("enterprise-license", entLicenseRules, consulClient, logger)
	}

	if c.flagCreateSnapshotAgentToken {
		c.createACL("client-snapshot-agent", snapshotAgentRules, consulClient, logger)
	}

	if c.flagCreateMeshGatewayToken {
		c.createACL("mesh-gateway", meshGatewayRules, consulClient, logger)
	}

	if c.flagCreateInjectAuthMethod {
		c.configureConnectInject(logger, consulClient)
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
func (c *Command) getConsulServers(logger hclog.Logger, n int) ([]podAddr, error) {
	var serverPods *apiv1.PodList
	c.untilSucceeds("discovering Consul server pods",
		func() error {
			labelSelector := fmt.Sprintf("component=server, app=consul, release=%s", c.flagReleaseName)
			var err error
			serverPods, err = c.clientset.CoreV1().Pods(c.flagNamespace).List(metav1.ListOptions{LabelSelector: labelSelector})
			if err != nil {
				return err
			}

			if len(serverPods.Items) == 0 {
				return fmt.Errorf("no server pods with labels %q found", labelSelector)
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

	var podAddrs []podAddr
	for _, pod := range serverPods.Items {
		var httpPort int32
		for _, p := range pod.Spec.Containers[0].Ports {
			if p.Name == "http" {
				httpPort = p.ContainerPort
			}
		}
		if httpPort == 0 {
			return nil, fmt.Errorf("pod %s has no port labeled 'http'", pod.Name)
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
func (c *Command) bootstrapServers(logger hclog.Logger, bootTokenSecretName string) (string, error) {
	serverPods, err := c.getConsulServers(logger, c.flagReplicas)
	if err != nil {
		return "", err
	}
	logger.Info(fmt.Sprintf("Found %d Consul server Pods", len(serverPods)))

	// Pick the first pod to connect to for bootstrapping and set up connection.
	firstServerAddr := serverPods[0].Addr
	consulClient, err := api.NewClient(&api.Config{
		Address: firstServerAddr,
		Scheme:  "http",
	})
	if err != nil {
		return "", fmt.Errorf("creating Consul client for address %s: %s", firstServerAddr, err)
	}

	// Call bootstrap ACLs API.
	var bootstrapToken []byte
	var unrecoverableErr error
	c.untilSucceeds("bootstrapping ACLs - PUT /v1/acl/bootstrap",
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

	// Write bootstrap token to a Kubernetes secret.
	c.untilSucceeds(fmt.Sprintf("writing bootstrap Secret %q", bootTokenSecretName),
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

	// Override our original client with a new one that has the bootstrap token
	// set.
	consulClient, err = api.NewClient(&api.Config{
		Address: firstServerAddr,
		Scheme:  "http",
		Token:   string(bootstrapToken),
	})
	if err != nil {
		return "", fmt.Errorf("creating Consul client for address %s: %s", firstServerAddr, err)
	}

	// Create new tokens for each server and apply them.
	if err := c.setServerTokens(logger, consulClient, serverPods, string(bootstrapToken)); err != nil {
		return "", err
	}
	return string(bootstrapToken), nil
}

// setServerTokens creates policies and associated ACL token for each server
// and then provides the token to the server.
func (c *Command) setServerTokens(logger hclog.Logger, consulClient *api.Client,
	serverPods []podAddr, bootstrapToken string) error {
	// Create agent policy.
	agentPolicy := api.ACLPolicy{
		Name:        "agent-token",
		Description: "Agent Token Policy",
		Rules:       agentRules,
	}
	c.untilSucceeds("creating agent policy - PUT /v1/acl/policy",
		func() error {
			_, _, err := consulClient.ACL().PolicyCreate(&agentPolicy, nil)
			if isPolicyExistsErr(err, agentPolicy.Name) {
				logger.Info(fmt.Sprintf("Policy %q already exists", agentPolicy.Name))
				return nil
			}
			return err
		}, logger)

	// Create agent token for each server agent.
	var serverTokens []api.ACLToken
	for _, pod := range serverPods {
		var token *api.ACLToken
		c.untilSucceeds(fmt.Sprintf("creating server token for %s - PUT /v1/acl/token", pod.Name),
			func() error {
				tokenReq := api.ACLToken{
					Description: fmt.Sprintf("Server Token for %s", pod.Name),
					Policies:    []*api.ACLTokenPolicyLink{{Name: agentPolicy.Name}},
				}
				var err error
				token, _, err = consulClient.ACL().TokenCreate(&tokenReq, nil)
				return err
			}, logger)
		serverTokens = append(serverTokens, *token)
	}

	// Pass out agent tokens to servers.
	for i, pod := range serverPods {
		// We create a new client for each server because we need to call each
		// server specifically.
		serverClient, err := api.NewClient(&api.Config{
			Address: pod.Addr,
			Scheme:  "http",
			Token:   bootstrapToken,
		})
		if err != nil {
			return fmt.Errorf(" creating Consul client for address %q: %s", pod.Addr, err)
		}
		podName := pod.Name

		// Update token.
		c.untilSucceeds(fmt.Sprintf("updating server token for %s - PUT /v1/agent/token/agent", podName),
			func() error {
				_, err := serverClient.Agent().UpdateAgentACLToken(serverTokens[i].SecretID, nil)
				return err
			}, logger)
	}
	return nil
}

// createACL creates a policy with rules and name, creates an ACL token for that
// policy and then writes the token to a Kubernetes secret.
func (c *Command) createACL(name, rules string, consulClient *api.Client, logger hclog.Logger) {
	// Check if the secret already exists, if so, we assume the ACL has already been created.
	secretName := fmt.Sprintf("%s-consul-%s-acl-token", c.flagReleaseName, name)
	_, err := c.clientset.CoreV1().Secrets(c.flagNamespace).Get(secretName, metav1.GetOptions{})
	if err == nil {
		logger.Info(fmt.Sprintf("Secret %q already exists", secretName))
		return
	}

	// Create policy with the given rules.
	policyTmpl := api.ACLPolicy{
		Name:        fmt.Sprintf("%s-token", name),
		Description: fmt.Sprintf("%s Token Policy", name),
		Rules:       rules,
	}
	c.untilSucceeds(fmt.Sprintf("creating %s policy", policyTmpl.Name),
		func() error {
			_, _, err := consulClient.ACL().PolicyCreate(&policyTmpl, &api.WriteOptions{})
			if isPolicyExistsErr(err, policyTmpl.Name) {
				logger.Info(fmt.Sprintf("Policy %q already exists", policyTmpl.Name))
				return nil
			}
			return err
		}, logger)

	// Create token for the policy.
	tokenTmpl := api.ACLToken{
		Description: fmt.Sprintf("%s Token", name),
		Policies:    []*api.ACLTokenPolicyLink{{Name: policyTmpl.Name}},
	}
	var token string
	c.untilSucceeds(fmt.Sprintf("creating token for policy %s", policyTmpl.Name),
		func() error {
			createdToken, _, err := consulClient.ACL().TokenCreate(&tokenTmpl, &api.WriteOptions{})
			if err == nil {
				token = createdToken.SecretID
			}
			return err
		}, logger)

	// Write token to a Kubernetes secret.
	c.untilSucceeds(fmt.Sprintf("writing Secret for token %s", policyTmpl.Name),
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
func (c *Command) configureDNSPolicies(logger hclog.Logger, consulClient *api.Client) {
	// Create policy for the anonymous token
	dnsPolicy := api.ACLPolicy{
		Name:        "dns-policy",
		Description: "DNS Policy",
		Rules:       dnsRules,
	}

	c.untilSucceeds("creating dns policy - PUT /v1/acl/policy",
		func() error {
			_, _, err := consulClient.ACL().PolicyCreate(&dnsPolicy, nil)
			if isPolicyExistsErr(err, dnsPolicy.Name) {
				logger.Info(fmt.Sprintf("Policy %q already exists", dnsPolicy.Name))
				return nil
			}
			return err
		}, logger)

	// Create token to get sent to TokenUpdate
	aToken := api.ACLToken{
		AccessorID: "00000000-0000-0000-0000-000000000002",
		Policies:   []*api.ACLTokenPolicyLink{{Name: dnsPolicy.Name}},
	}

	// Update anonymous token to include this policy
	c.untilSucceeds("updating anonymous token with DNS policy",
		func() error {
			_, _, err := consulClient.ACL().TokenUpdate(&aToken, &api.WriteOptions{})
			return err
		}, logger)
}

// configureConnectInject sets up auth methods so that connect injection will
// work.
func (c *Command) configureConnectInject(logger hclog.Logger, consulClient *api.Client) {
	// First, check if there's already an acl binding rule. If so, then this
	// work is already done.
	authMethodName := fmt.Sprintf("%s-consul-k8s-auth-method", c.flagReleaseName)
	var existingRules []*api.ACLBindingRule
	c.untilSucceeds(fmt.Sprintf("listing binding rules for auth method %s", authMethodName),
		func() error {
			var err error
			existingRules, _, err = consulClient.ACL().BindingRuleList(authMethodName, nil)
			return err
		}, logger)
	if len(existingRules) > 0 {
		logger.Info(fmt.Sprintf("Binding rule for %s already exists", authMethodName))
		return
	}

	var kubeSvc *apiv1.Service
	c.untilSucceeds("getting kubernetes service IP",
		func() error {
			var err error
			kubeSvc, err = c.clientset.CoreV1().Services("default").Get("kubernetes", metav1.GetOptions{})
			return err
		}, logger)

	// Get the Secret name for the auth method ServiceAccount.
	var authMethodServiceAccount *apiv1.ServiceAccount
	saName := fmt.Sprintf("%s-consul-connect-injector-authmethod-svc-account", c.flagReleaseName)
	c.untilSucceeds(fmt.Sprintf("getting %s ServiceAccount", saName),
		func() error {
			var err error
			authMethodServiceAccount, err = c.clientset.CoreV1().ServiceAccounts(c.flagNamespace).Get(saName, metav1.GetOptions{})
			return err
		}, logger)

	// ServiceAccounts always have a secret name. The secret
	// contains the JWT token.
	saSecretName := authMethodServiceAccount.Secrets[0].Name

	// Get the secret that will contain the ServiceAccount JWT token.
	var saSecret *apiv1.Secret
	c.untilSucceeds(fmt.Sprintf("getting %s Secret", saSecretName),
		func() error {
			var err error
			saSecret, err = c.clientset.CoreV1().Secrets(c.flagNamespace).Get(saSecretName, metav1.GetOptions{})
			return err
		}, logger)

	// Now we're ready to set up Consul's auth method.
	authMethodTmpl := api.ACLAuthMethod{
		Name:        authMethodName,
		Description: fmt.Sprintf("Consul %s default Kubernetes AuthMethod", c.flagReleaseName),
		Type:        "kubernetes",
		Config: map[string]interface{}{
			"Host":              fmt.Sprintf("https://%s:443", kubeSvc.Spec.ClusterIP),
			"CACert":            string(saSecret.Data["ca.crt"]),
			"ServiceAccountJWT": string(saSecret.Data["token"]),
		},
	}
	var authMethod *api.ACLAuthMethod
	c.untilSucceeds(fmt.Sprintf("creating auth method %s", authMethodTmpl.Name),
		func() error {
			var err error
			authMethod, _, err = consulClient.ACL().AuthMethodCreate(&authMethodTmpl, &api.WriteOptions{})
			return err
		}, logger)

	// Create the binding rule.
	abr := api.ACLBindingRule{
		Description: fmt.Sprintf("Consul %s default binding rule", c.flagReleaseName),
		AuthMethod:  authMethod.Name,
		BindType:    api.BindingRuleBindTypeService,
		BindName:    "${serviceaccount.name}",
		Selector:    c.flagBindingRuleSelector,
	}
	c.untilSucceeds(fmt.Sprintf("creating acl binding rule for %s", authMethodTmpl.Name),
		func() error {
			_, _, err := consulClient.ACL().BindingRuleCreate(&abr, nil)
			return err
		}, logger)
}

// untilSucceeds runs op until it returns a nil error.
func (c *Command) untilSucceeds(opName string, op func() error, logger hclog.Logger) {
	for {
		err := op()
		if err == nil {
			logger.Info(fmt.Sprintf("Success: %s", opName))
			break
		}
		logger.Error(fmt.Sprintf("Failure: %s", opName), "err", err)
		logger.Info("Retrying in 1s")
		time.Sleep(1 * time.Second)
	}
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

// ACL rules
const agentRules = `node_prefix "" {
   policy = "write"
}
service_prefix "" {
   policy = "read"
}`

const dnsRules = `node_prefix "" {
   policy = "read"
}
service_prefix "" {
   policy = "read"
}`

const syncRules = `node_prefix "" {
   policy = "read"
}
node "k8s-sync" {
	policy = "write"
}
service_prefix "" {
   policy = "write"
}`

const snapshotAgentRules = `acl = "write"
key "consul-snapshot/lock" {
   policy = "write"
}
session_prefix "" {
   policy = "write"
}
service "consul-snapshot" {
   policy = "write"
}`

// This assumes users are using the default name for the service, i.e.
// "mesh-gateway".
const meshGatewayRules = `service_prefix "" {
   policy = "read"
}

service "mesh-gateway" {
   policy = "write"
}`

const entLicenseRules = `operator = "write"`
