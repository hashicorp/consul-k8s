package serveraclinit

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"sync"

	"github.com/hashicorp/consul-k8s/subcommand"
	k8sflags "github.com/hashicorp/consul-k8s/subcommand/flags"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/command/flags"
	"github.com/hashicorp/go-hclog"
	"github.com/mitchellh/cli"
	apiv1 "k8s.io/api/core/v1"
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

	clientset *kubernetes.Clientset

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

func (c *Command) Run(args []string) int {
	c.once.Do(c.init)
	if err := c.flags.Parse(args); err != nil {
		return 1
	}
	if len(c.flags.Args()) > 0 {
		c.UI.Error(fmt.Sprintf("Should have no non-flag arguments."))
		return 1
	}

	config, err := subcommand.K8SConfig(c.k8s.KubeConfig())
	if err != nil {
		c.UI.Error(fmt.Sprintf("Error retrieving Kubernetes auth: %s", err))
		return 1
	}

	// Create the Kubernetes clientset
	c.clientset, err = kubernetes.NewForConfig(config)
	if err != nil {
		c.UI.Error(fmt.Sprintf("Error initializing Kubernetes client: %s", err))
		return 1
	}

	level := hclog.LevelFromString(c.flagLogLevel)
	if level == hclog.NoLevel {
		c.UI.Error(fmt.Sprintf("Unknown log level: %s", c.flagLogLevel))
		return 1
	}

	logger := hclog.New(&hclog.LoggerOptions{
		Level:  level,
		Output: os.Stderr,
	})

	// Use the client to get statefulset pods
	labelSelector := fmt.Sprintf("component=server, app=consul, release=%s", c.flagReleaseName)

	serverPods, err := c.clientset.CoreV1().Pods(c.flagNamespace).List(metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		logger.Error(err.Error())
		return 1
	}

	if len(serverPods.Items) == 0 {
		logger.Error("No pods were found")
		return 1
	}

	// Pull the addresses out of each pod
	var podAddresses []string
	for _, pod := range serverPods.Items {
		address := fmt.Sprintf("%s:8500", pod.Status.PodIP)
		if pod.Status.PodIP != "" {
			podAddresses = append(podAddresses, address)
			logger.Info(address)
		}
	}

	if len(podAddresses) < c.flagReplicas {
		logger.Error(fmt.Sprintf("Not enough pod addresses were found: %d", len(podAddresses)))
		return 1
	}

	// Pick the first pod to connect to for bootstrapping & set up connection
	consulConfig := api.Config{
		Address: podAddresses[0],
		Scheme:  "http",
	}

	consulClient, err := api.NewClient(&consulConfig)
	if err != nil {
		c.UI.Error(fmt.Sprintf("Error connecting to Consul agent: %s", err))
		return 1
	}

	// Bootstrap the ACLs
	bootstrapToken, _, err := consulClient.ACL().Bootstrap()
	if err != nil {
		c.UI.Error(fmt.Sprintf("Error bootstrapping Consul agent: %s", err))
		return 1
	}

	// Write bootstrap token to a Kubernetes secret
	_, err = c.clientset.CoreV1().Secrets(c.flagNamespace).Create(&apiv1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s-consul-bootstrap-acl-token", c.flagReleaseName),
		},
		StringData: map[string]string{
			"token": bootstrapToken.SecretID,
		},
	})
	if err != nil {
		c.UI.Error(fmt.Sprintf("Error creating bootstrap token secret: %s", err))
		return 1
	}

	// Create agent policy
	agentPolicy := api.ACLPolicy{
		Name:        "agent-token",
		Description: "Agent Token Policy",
		Rules:       agentRules,
	}

	aclAgentPolicy, _, err := consulClient.ACL().PolicyCreate(&agentPolicy, &api.WriteOptions{Token: bootstrapToken.SecretID})
	if err != nil {
		c.UI.Error(fmt.Sprintf("Error creating agent policy: %s", err))
		return 1
	}

	// Create agent token for each server agent
	var serverTokens []api.ACLToken

	for i := 0; i < len(podAddresses); i++ {
		// Include the pod name into the token description
		token := api.ACLToken{
			Description: fmt.Sprintf("Server Agent Token for %s", serverPods.Items[i].Name),
			Policies:    []*api.ACLTokenPolicyLink{&api.ACLTokenPolicyLink{Name: aclAgentPolicy.Name}},
		}

		newToken, _, err := consulClient.ACL().TokenCreate(&token, &api.WriteOptions{Token: bootstrapToken.SecretID})
		if err != nil {
			c.UI.Error(fmt.Sprintf("Error creating agent token %v: %s", i, err))
			return 1
		}
		serverTokens = append(serverTokens, *newToken)
	}

	// Pass out agent tokens and restart the servers
	for i := 0; i < len(podAddresses); i++ {
		// Connect to other pods
		consulConfig := api.Config{
			Address: podAddresses[i],
			Scheme:  "http",
			Token:   bootstrapToken.SecretID,
		}

		consulClient, err = api.NewClient(&consulConfig)
		if err != nil {
			c.UI.Error(fmt.Sprintf("Error connecting to Consul agent %v: %s", i, err))
			return 1
		}

		// Apply token
		_, err = consulClient.Agent().UpdateAgentACLToken(serverTokens[i].SecretID, &api.WriteOptions{})
		if err != nil {
			c.UI.Error(fmt.Sprintf("Error applying agent token %v: %s", i, err))
			return 1
		}

		// Restart
		err = consulClient.Agent().Reload()
		if err != nil {
			c.UI.Error(fmt.Sprintf("Error restarting agent %v: %s", i, err))
			return 1
		}
	}

	// Create client agent token
	if c.flagCreateClientToken {
		if err := c.createACL("client", agentRules, consulClient); err != nil {
			c.UI.Error(err.Error())
			return 1
		}
	}

	// Update anonymous token to allow DNS to work
	if c.flagAllowDNS {
		// Create policy for the anonymous token
		dnsPolicy := api.ACLPolicy{
			Name:        "dns-policy",
			Description: "DNS Policy",
			Rules:       dnsRules,
		}

		aclDNSPolicy, _, err := consulClient.ACL().PolicyCreate(&dnsPolicy, &api.WriteOptions{})
		if err != nil {
			c.UI.Error(fmt.Sprintf("Error creating dns policy: %s", err))
			return 1
		}

		// Create token to get sent to TokenUpdate
		aToken := api.ACLToken{
			AccessorID: "00000000-0000-0000-0000-000000000002",
			Policies:   []*api.ACLTokenPolicyLink{&api.ACLTokenPolicyLink{Name: aclDNSPolicy.Name}},
		}

		// Update anonymous token to include this policy
		_, _, err = consulClient.ACL().TokenUpdate(&aToken, &api.WriteOptions{})
		if err != nil {
			c.UI.Error(fmt.Sprintf("Error updating anonymous token: %s", err))
			return 1
		}
	}

	// Create catalog sync token if necessary
	if c.flagCreateSyncToken {
		if err := c.createACL("catalog-sync", syncRules, consulClient); err != nil {
			c.UI.Error(err.Error())
			return 1
		}
	}

	// Create enterprise license token if necessary
	if c.flagCreateEntLicenseToken {
		if err := c.createACL("enterprise-license", entLicenseRules, consulClient); err != nil {
			c.UI.Error(err.Error())
			return 1
		}
	}

	// Create snapshot agent token if necessary
	if c.flagCreateSnapshotAgentToken {
		if err := c.createACL("client-snapshot-agent", snapshotAgentRules, consulClient); err != nil {
			c.UI.Error(err.Error())
			return 1
		}
	}

	if c.flagCreateMeshGatewayToken {
		if err := c.createACL("mesh-gateway", meshGatewayRules, consulClient); err != nil {
			c.UI.Error(err.Error())
			return 1
		}
	}

	// Support ConnectInject using Kubernetes as an auth method
	if c.flagCreateInjectAuthMethod {
		// Get the Kubernetes service IP address - kubernetes.default.svc
		k8sService, err := c.clientset.CoreV1().Services("default").Get("kubernetes", metav1.GetOptions{})
		if err != nil {
			c.UI.Error(fmt.Sprintf("Error getting kubernetes service: %s", err))
			return 1
		}

		// Get auth method service account JWT
		saName := fmt.Sprintf("%s-consul-connect-injector-authmethod-svc-account", c.flagReleaseName)
		amServiceAccount, err := c.clientset.CoreV1().ServiceAccounts(c.flagNamespace).Get(saName, metav1.GetOptions{})
		if err != nil {
			c.UI.Error(fmt.Sprintf("Error getting service account: %s", err))
			return 1
		}

		// Assume the jwt is the first secret attached to the service account
		saSecret, err := c.clientset.CoreV1().Secrets(c.flagNamespace).Get(amServiceAccount.Secrets[0].Name, metav1.GetOptions{})
		if err != nil {
			c.UI.Error(fmt.Sprintf("Error getting service account JWT secret: %s", err))
			return 1
		}

		// Set up auth method
		aam := api.ACLAuthMethod{
			Name:        fmt.Sprintf("%s-consul-k8s-auth-method", c.flagReleaseName),
			Description: fmt.Sprintf("Consul %s default Kubernetes AuthMethod", c.flagReleaseName),
			Type:        "kubernetes",
			Config: map[string]interface{}{
				"Host":              fmt.Sprintf("https://%s:443", k8sService.Spec.ClusterIP),
				"CACert":            string(saSecret.Data["ca.crt"]),
				"ServiceAccountJWT": string(saSecret.Data["token"]),
			},
		}

		authMethod, _, err := consulClient.ACL().AuthMethodCreate(&aam, &api.WriteOptions{})
		if err != nil {
			c.UI.Error(fmt.Sprintf("Error creating auth method: %s", err))
			return 1
		}

		// Register binding rule
		abr := api.ACLBindingRule{
			Description: fmt.Sprintf("Consul %s default binding rule", c.flagReleaseName),
			AuthMethod:  authMethod.Name,
			BindType:    api.BindingRuleBindTypeService,
			BindName:    "${serviceaccount.name}",
			Selector:    c.flagBindingRuleSelector,
		}

		_, _, err = consulClient.ACL().BindingRuleCreate(&abr, nil)
		if err != nil {
			c.UI.Error(fmt.Sprintf("Error creating binding rule: %s", err))
			return 1
		}
	}

	return 0
}

func (c *Command) createACL(name, rules string, consulClient *api.Client) error {
	// Create policy
	policy := api.ACLPolicy{
		Name:        fmt.Sprintf("%s-token", name),
		Description: fmt.Sprintf("%s Token Policy", name),
		Rules:       rules,
	}

	createdPolicy, _, err := consulClient.ACL().PolicyCreate(&policy, &api.WriteOptions{})
	if err != nil {
		return fmt.Errorf("Error creating %s policy: %s", name, err)
	}

	token := api.ACLToken{
		Description: fmt.Sprintf("%s Token", name),
		Policies:    []*api.ACLTokenPolicyLink{&api.ACLTokenPolicyLink{Name: createdPolicy.Name}},
	}

	// Create token
	createdToken, _, err := consulClient.ACL().TokenCreate(&token, &api.WriteOptions{})
	if err != nil {
		return fmt.Errorf("Error creating %s token: %s", name, err)
	}

	// Write token to a Kubernetes secret
	_, err = c.clientset.CoreV1().Secrets(c.flagNamespace).Create(&apiv1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s-consul-%s-acl-token", c.flagReleaseName, name),
		},
		StringData: map[string]string{
			"token": createdToken.SecretID,
		},
	})
	if err != nil {
		return errors.New(fmt.Sprintf("Error creating %s token secret: %s", name, err))
	}

	return nil
}

func (c *Command) Synopsis() string { return synopsis }
func (c *Command) Help() string {
	c.once.Do(c.init)
	return c.help
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
