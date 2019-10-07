package serveraclinit

import (
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

	// Get Consul Server Pods and construct their addresses.
	serverPods := c.getConsulServers(logger)
	var serverPodAddrs []string
	for _, pod := range serverPods.Items {
		var httpPort int32
		for _, p := range pod.Spec.Containers[0].Ports {
			if p.Name == "http" {
				httpPort = p.ContainerPort
			}
		}
		if httpPort == 0 {
			c.UI.Error(fmt.Sprintf("pod %s has no port labeled 'http'", pod.Name))
			return 1
		}
		addr := fmt.Sprintf("%s:%d", pod.Status.PodIP, httpPort)
		serverPodAddrs = append(serverPodAddrs, addr)
	}
	logger.Info(fmt.Sprintf("Found %d Consul servers", len(serverPodAddrs)),
		"addrs", strings.Join(serverPodAddrs, ","))

	// Pick the first pod to connect to for bootstrapping and set up connection.
	firstServerAddr := serverPodAddrs[0]
	consulClient, err := api.NewClient(&api.Config{
		Address: firstServerAddr,
		Scheme:  "http",
	})
	if err != nil {
		// This should not happen but if it does it's likely unrecoverable.
		c.UI.Error(fmt.Sprintf("Error creating Consul API client: %s", err))
		return 1
	}

	// Bootstrap ACLs.
	bootstrapToken, err := c.bootstrapACLs(logger, consulClient)
	if err != nil {
		c.UI.Error(err.Error())
		return 1
	}

	// Override our original client with a new one that has the bootstrap token
	// set.
	consulClient, err = api.NewClient(&api.Config{
		Address: firstServerAddr,
		Scheme:  "http",
		Token:   string(bootstrapToken),
	})
	if err != nil {
		c.UI.Error(fmt.Sprintf("Error creating Consul client for addr %q: %s", firstServerAddr, err))
		return 1
	}

	// Create new tokens for each server and apply them.
	if err := c.setServerTokens(logger, consulClient, serverPods, serverPodAddrs, string(bootstrapToken)); err != nil {
		c.UI.Error(err.Error())
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

func (c *Command) getConsulServers(logger hclog.Logger) *apiv1.PodList {
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

			if len(serverPods.Items) < c.flagReplicas {
				return fmt.Errorf("found %d servers, require %d", len(serverPods.Items), c.flagReplicas)
			}

			for _, pod := range serverPods.Items {
				if pod.Status.PodIP == "" {
					return fmt.Errorf("pod %s has no IP", pod.Name)
				}
			}
			return nil
		}, logger)
	return serverPods
}

func (c *Command) bootstrapACLs(logger hclog.Logger, consulClient *api.Client) ([]byte, error) {
	// Bootstrap the ACLs unless already bootstrapped.
	alreadyBootstrapped := false
	var bootstrapToken []byte
	c.untilSucceeds("bootstrapping ACLs - PUT /v1/acl/bootstrap",
		func() error {
			bootstrapResp, _, err := consulClient.ACL().Bootstrap()
			if err == nil {
				bootstrapToken = []byte(bootstrapResp.SecretID)
				return nil
			}

			// Check if already bootstrapped.
			if strings.Contains(err.Error(), "Unexpected response code: 403") {
				alreadyBootstrapped = true
				logger.Info("ACLs already bootstrapped")
				return nil
			}

			if isNoLeaderErr(err) {
				// Return a more descriptive error in the case of no leader
				// being elected.
				return fmt.Errorf("no leader elected: %s", err)
			}
			return err
		}, logger)

	bootTokenSecretName := fmt.Sprintf("%s-consul-bootstrap-acl-token", c.flagReleaseName)
	if alreadyBootstrapped {
		// Retrieve the bootstrap token from the Kubernetes secret.
		secret, err := c.clientset.CoreV1().Secrets(c.flagNamespace).Get(bootTokenSecretName, metav1.GetOptions{})
		if err != nil {
			if k8serrors.IsNotFound(err) {
				return nil, fmt.Errorf("Bootstrap token secret %q was not found."+
					" We can't proceed because the bootstrap token is lost."+
					" You must reset ACLs.", bootTokenSecretName)
			}
			return nil, fmt.Errorf("Error getting bootstrap token Secret %q: %s", bootTokenSecretName, err)
		}
		var ok bool
		bootstrapToken, ok = secret.Data["token"]
		if !ok {
			return nil, fmt.Errorf("Secret %q does not have data key 'token'", bootTokenSecretName)
		}
		logger.Info(fmt.Sprintf("Got bootstrap token from Secret %q", bootTokenSecretName))
	} else {
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
				if k8serrors.IsAlreadyExists(err) {
					// If it already exists, update it.
					logger.Info(fmt.Sprintf("Secret %q already exists, updating it with new token", bootTokenSecretName))
					_, err = c.clientset.CoreV1().Secrets(c.flagNamespace).Update(secret)
					return err
				}
				return err
			}, logger)
	}
	return bootstrapToken, nil
}

func (c *Command) setServerTokens(logger hclog.Logger, consulClient *api.Client,
	serverPods *apiv1.PodList, serverPodAddrs []string, bootstrapToken string) error {
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
	for i := 0; i < len(serverPodAddrs); i++ {
		podName := serverPods.Items[i].Name

		var token *api.ACLToken
		c.untilSucceeds(fmt.Sprintf("creating server token for %s - PUT /v1/acl/token", podName),
			func() error {
				tokenReq := api.ACLToken{
					Description: fmt.Sprintf("Server Token for %s", podName),
					Policies:    []*api.ACLTokenPolicyLink{{Name: agentPolicy.Name}},
				}
				var err error
				token, _, err = consulClient.ACL().TokenCreate(&tokenReq, nil)
				return err
			}, logger)
		serverTokens = append(serverTokens, *token)
	}

	// Pass out agent tokens to servers.
	for i := 0; i < len(serverPodAddrs); i++ {
		// We create a new client for each server because we need to call each
		// server specifically.
		serverClient, err := api.NewClient(&api.Config{
			Address: serverPodAddrs[i],
			Scheme:  "http",
			Token:   bootstrapToken,
		})
		if err != nil {
			return fmt.Errorf("Error creating Consul client for addr %q: %s", serverPodAddrs[i], err)
		}
		podName := serverPods.Items[i].Name

		// Update token.
		c.untilSucceeds(fmt.Sprintf("updating server token for %s - PUT /v1/agent/token/agent", podName),
			func() error {
				_, err := serverClient.Agent().UpdateAgentACLToken(serverTokens[i].SecretID, nil)
				return err
			}, logger)
	}
	return nil
}

func (c *Command) createACL(name, rules string, consulClient *api.Client, logger hclog.Logger) {
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
					Name: fmt.Sprintf("%s-consul-%s-acl-token", c.flagReleaseName, name),
				},
				Data: map[string][]byte{
					"token": []byte(token),
				},
			}
			_, err := c.clientset.CoreV1().Secrets(c.flagNamespace).Create(secret)
			if k8serrors.IsAlreadyExists(err) {
				// If the secret already exists, update it.
				_, err := c.clientset.CoreV1().Secrets(c.flagNamespace).Update(secret)
				return err
			}
			return err
		}, logger)
}

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

func (c *Command) configureConnectInject(logger hclog.Logger, consulClient *api.Client) {
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
		Name:        fmt.Sprintf("%s-consul-k8s-auth-method", c.flagReleaseName),
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

func isNoLeaderErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "Unexpected response code: 500") &&
		strings.Contains(err.Error(), "The ACL system is currently in legacy mode.")
}

func isPolicyExistsErr(err error, policyName string) bool {
	return err != nil &&
		strings.Contains(err.Error(), "Unexpected response code: 500") &&
		strings.Contains(err.Error(), fmt.Sprintf("Invalid Policy: A Policy with Name %q already exists", policyName))
}

const synopsis = "Initialize ACLs on Consul servers."
const help = `
Usage: consul-k8s server-acl-init [options]

  Bootstraps servers with ACLs

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
