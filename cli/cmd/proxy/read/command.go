package read

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/hashicorp/consul-k8s/cli/common"
	"github.com/hashicorp/consul-k8s/cli/common/flag"
	"github.com/hashicorp/consul-k8s/cli/common/terminal"
	helmCLI "helm.sh/helm/v3/pkg/cli"
	"k8s.io/apimachinery/pkg/api/validation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/utils/strings/slices"
)

// defaultAdminPort is the port where the Envoy admin API is exposed.
const defaultAdminPort int = 19000

const (
	Table = "table"
	JSON  = "json"
	Raw   = "raw"
)

type ReadCommand struct {
	*common.BaseCommand

	kubernetes kubernetes.Interface

	set *flag.Sets

	// Command Flags
	flagNamespace string
	flagPodName   string
	flagOutput    string

	// Output Filtering Opts
	flagClusters  bool
	flagListeners bool
	flagRoutes    bool
	flagEndpoints bool
	flagSecrets   bool

	// Global Flags
	flagKubeConfig  string
	flagKubeContext string

	fetchConfig func(context.Context, common.PortForwarder) (*EnvoyConfig, error)

	restConfig *rest.Config

	once sync.Once
	help string
}

func (c *ReadCommand) init() {
	if c.fetchConfig == nil {
		c.fetchConfig = FetchConfig
	}

	c.set = flag.NewSets()
	f := c.set.NewSet("Command Options")
	f.StringVar(&flag.StringVar{
		Name:    "namespace",
		Target:  &c.flagNamespace,
		Usage:   "The namespace where the target Pod can be found.",
		Aliases: []string{"n"},
	})
	f.StringVar(&flag.StringVar{
		Name:    "output",
		Target:  &c.flagOutput,
		Usage:   "Output the Envoy configuration as 'table', 'json', or 'raw'.",
		Default: Table,
		Aliases: []string{"o"},
	})

	f = c.set.NewSet("Output Filtering Options")
	f.BoolVar(&flag.BoolVar{
		Name:   "clusters",
		Target: &c.flagClusters,
		Usage:  "Filter output to only show clusters.",
	})
	f.BoolVar(&flag.BoolVar{
		Name:   "listeners",
		Target: &c.flagListeners,
		Usage:  "Filter output to only show listeners.",
	})
	f.BoolVar(&flag.BoolVar{
		Name:   "routes",
		Target: &c.flagRoutes,
		Usage:  "Filter output to only show routes.",
	})
	f.BoolVar(&flag.BoolVar{
		Name:   "endpoints",
		Target: &c.flagEndpoints,
		Usage:  "Filter output to only show endpoints.",
	})
	f.BoolVar(&flag.BoolVar{
		Name:   "secrets",
		Target: &c.flagSecrets,
		Usage:  "Filter output to only show secrets.",
	})

	f = c.set.NewSet("GlobalOptions")
	f.StringVar(&flag.StringVar{
		Name:    "kubeconfig",
		Aliases: []string{"c"},
		Target:  &c.flagKubeConfig,
		Usage:   "Set the path to kubeconfig file.",
	})
	f.StringVar(&flag.StringVar{
		Name:   "context",
		Target: &c.flagKubeContext,
		Usage:  "Set the Kubernetes context to use.",
	})

	c.help = c.set.Help()
}

func (c *ReadCommand) Run(args []string) int {
	c.once.Do(c.init)
	c.Log.ResetNamed("read")
	defer common.CloseWithError(c.BaseCommand)

	if err := c.parseFlags(args); err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		c.UI.Output("\n" + c.Help())
		return 1
	}

	if err := c.validateFlags(); err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		c.UI.Output("\n" + c.Help())
		return 1
	}

	if err := c.initKubernetes(); err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}

	adminPorts, err := c.fetchAdminPorts()
	if err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}

	configs, err := c.fetchConfigs(adminPorts)
	if err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}

	err = c.outputConfigs(configs)
	if err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}

	return 0
}

func (c *ReadCommand) Help() string {
	c.once.Do(c.init)
	return fmt.Sprintf("%s\n\nUsage: consul-k8s proxy read <pod-name> [flags]\n\n%s", c.Synopsis(), c.help)
}

func (c *ReadCommand) Synopsis() string {
	return "Inspect the Envoy configuration for a given Pod."
}

func (c *ReadCommand) parseFlags(args []string) error {
	// Separate positional arguments from keyed arguments.
	positional := []string{}
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			break
		}
		positional = append(positional, arg)
	}
	keyed := args[len(positional):]

	if len(positional) != 1 {
		return fmt.Errorf("Exactly one positional argument is required: <pod-name>")
	}
	c.flagPodName = positional[0]

	if err := c.set.Parse(keyed); err != nil {
		return err
	}

	return nil
}

func (c *ReadCommand) validateFlags() error {
	if errs := validation.ValidateNamespaceName(c.flagNamespace, false); c.flagNamespace != "" && len(errs) > 0 {
		return fmt.Errorf("invalid namespace name passed for -namespace/-n: %v", strings.Join(errs, "; "))
	}
	if outputs := []string{Table, JSON, Raw}; !slices.Contains(outputs, c.flagOutput) {
		return fmt.Errorf("-output must be one of %s.", strings.Join(outputs, ", "))
	}
	return nil
}

// filtersPassed returns true if the user has passed a flag which filters the
// output.
func (c *ReadCommand) filtersPassed() bool {
	return c.flagClusters || c.flagEndpoints || c.flagListeners || c.flagRoutes || c.flagSecrets
}

func (c *ReadCommand) initKubernetes() (err error) {
	settings := helmCLI.New()

	if c.flagKubeConfig == "" {
		settings.KubeConfig = c.flagKubeConfig
	}

	if c.flagKubeContext == "" {
		settings.KubeContext = c.flagKubeContext
	}

	if c.restConfig == nil {
		if c.restConfig, err = settings.RESTClientGetter().ToRESTConfig(); err != nil {
			return fmt.Errorf("error creating Kubernetes REST config %v", err)
		}
	}

	if c.kubernetes == nil {
		if c.kubernetes, err = kubernetes.NewForConfig(c.restConfig); err != nil {
			return fmt.Errorf("error creating Kubernetes client %v", err)
		}
	}

	if c.flagNamespace == "" {
		c.flagNamespace = settings.Namespace()
	}

	return nil
}

func (c *ReadCommand) fetchAdminPorts() (map[string]int, error) {
	adminPorts := make(map[string]int, 0)

	pod, err := c.kubernetes.CoreV1().Pods(c.flagNamespace).Get(c.Ctx, c.flagPodName, metav1.GetOptions{})
	if err != nil {
		return adminPorts, err
	}

	connectService, isMultiport := pod.Annotations["consul.hashicorp.com/connect-service"]

	if !isMultiport {
		// Return the default port configuration.
		adminPorts[c.flagPodName] = defaultAdminPort
		return adminPorts, nil
	}

	for index, service := range strings.Split(connectService, ",") {
		adminPorts[service] = defaultAdminPort + index
	}

	return adminPorts, nil
}

func (c *ReadCommand) fetchConfigs(adminPorts map[string]int) (map[string]*EnvoyConfig, error) {
	configs := make(map[string]*EnvoyConfig, 0)

	for name, adminPort := range adminPorts {
		pf := common.PortForward{
			Namespace:  c.flagNamespace,
			PodName:    c.flagPodName,
			RemotePort: adminPort,
			KubeClient: c.kubernetes,
			RestConfig: c.restConfig,
		}

		config, err := c.fetchConfig(c.Ctx, &pf)
		if err != nil {
			return configs, err
		}

		configs[name] = config
	}

	return configs, nil
}

func (c *ReadCommand) outputConfigs(configs map[string]*EnvoyConfig) error {
	switch c.flagOutput {
	case Table:
		return c.outputTables(configs)
	case JSON:
		return c.outputJSON(configs)
	case Raw:
		return c.outputRaw(configs)
	}

	return nil
}

func (c *ReadCommand) outputTables(configs map[string]*EnvoyConfig) error {
	for name, config := range configs {
		c.UI.Output(fmt.Sprintf("Envoy configuration for %s in namespace %s:", name, c.flagNamespace))

		c.outputClustersTable(config.Clusters)
		c.outputEndpointsTable(config.Endpoints)
		c.outputListenersTable(config.Listeners)
		c.outputRoutesTable(config.Routes)
		c.outputSecretsTable(config.Secrets)
		c.UI.Output("\n")
	}

	return nil
}

func (c *ReadCommand) outputJSON(configs map[string]*EnvoyConfig) error {
	cfgs := make(map[string]interface{})
	for name, config := range configs {
		cfg := make(map[string]interface{})
		if !c.filtersPassed() || c.flagClusters {
			cfg["clusters"] = config.Clusters
		}
		if !c.filtersPassed() || c.flagEndpoints {
			cfg["endpoints"] = config.Endpoints
		}
		if !c.filtersPassed() || c.flagListeners {
			cfg["listeners"] = config.Listeners
		}
		if !c.filtersPassed() || c.flagRoutes {
			cfg["routes"] = config.Routes
		}
		if !c.filtersPassed() || c.flagSecrets {
			cfg["secrets"] = config.Secrets
		}

		cfgs[name] = cfg
	}

	out, err := json.MarshalIndent(cfgs, "", "\t")
	if err != nil {
		return err
	}

	c.UI.Output(string(out))

	return nil
}

func (c *ReadCommand) outputRaw(configs map[string]*EnvoyConfig) error {
	cfgs := make(map[string]interface{}, 0)
	for name, config := range configs {
		var cfg interface{}
		if err := json.Unmarshal(config.rawCfg, &cfg); err != nil {
			return err
		}

		cfgs[name] = cfg
	}

	out, err := json.MarshalIndent(cfgs, "", "\t")
	if err != nil {
		return err
	}

	c.UI.Output(string(out))

	return nil
}

func (c *ReadCommand) outputClustersTable(clusters []Cluster) {
	if c.filtersPassed() && !c.flagClusters {
		return
	}

	c.UI.Output(fmt.Sprintf("Clusters (%d)", len(clusters)), terminal.WithHeaderStyle())
	c.UI.Table(formatClusters(clusters))
}

func (c *ReadCommand) outputEndpointsTable(endpoints []Endpoint) {
	if c.filtersPassed() && !c.flagEndpoints {
		return
	}

	c.UI.Output(fmt.Sprintf("Endpoints (%d)", len(endpoints)), terminal.WithHeaderStyle())
	c.UI.Table(formatEndpoints(endpoints))
}

func (c *ReadCommand) outputListenersTable(listeners []Listener) {
	if c.filtersPassed() && !c.flagListeners {
		return
	}

	c.UI.Output(fmt.Sprintf("Listeners (%d)", len(listeners)), terminal.WithHeaderStyle())
	c.UI.Table(formatListeners(listeners))
}

func (c *ReadCommand) outputRoutesTable(routes []Route) {
	if c.filtersPassed() && !c.flagRoutes {
		return
	}

	c.UI.Output(fmt.Sprintf("Routes (%d)", len(routes)), terminal.WithHeaderStyle())
	c.UI.Table(formatRoutes(routes))
}

func (c *ReadCommand) outputSecretsTable(secrets []Secret) {
	if c.filtersPassed() && !c.flagSecrets {
		return
	}

	c.UI.Output(fmt.Sprintf("Secrets (%d)", len(secrets)), terminal.WithHeaderStyle())
	c.UI.Table(formatSecrets(secrets))
}
