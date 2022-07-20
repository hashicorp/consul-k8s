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
	flagFQDN      string
	flagAddress   string
	flagPort      int

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
	f.StringVar(&flag.StringVar{
		Name:   "fqdn",
		Target: &c.flagFQDN,
		Usage:  "Filter cluster output to only clusters with a fully qualified domain name which contains the given value.",
	})
	f.StringVar(&flag.StringVar{
		Name:   "address",
		Target: &c.flagAddress,
		Usage:  "Filter clusters, endpoints, and listeners output to only those with endpoint addresses which contain the given value.",
	})
	f.IntVar(&flag.IntVar{
		Name:    "port",
		Target:  &c.flagPort,
		Usage:   "Filter endpoints output to only endpoints with the given port number.",
		Default: -1,
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

// areTablesFiltered returns true if a table filtering flag was passed in.
func (c *ReadCommand) areTablesFiltered() bool {
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

<<<<<<< HEAD
func (c *ReadCommand) outputTables(configs map[string]*EnvoyConfig) error {
	for name, config := range configs {
		c.UI.Output(fmt.Sprintf("Envoy configuration for %s in namespace %s:", name, c.flagNamespace))

		c.outputClustersTable(FilterFQDN(config.Clusters, c.flagFQDN))
		c.outputEndpointsTable(config.Endpoints)
		c.outputListenersTable(config.Listeners)
		c.outputRoutesTable(config.Routes)
		c.outputSecretsTable(config.Secrets)
		c.UI.Output("\n")
=======
func (c *ReadCommand) outputAsJSON(config *EnvoyConfig) error {
	cfg := make(map[string]interface{})
	if !c.areTablesFiltered() || c.flagClusters {
		cfg["clusters"] = FilterClusters(config.Clusters, c.flagFQDN, c.flagAddress, c.flagPort)
	}
	if !c.areTablesFiltered() || c.flagEndpoints {
		cfg["endpoints"] = FilterEndpoints(config.Endpoints, c.flagAddress, c.flagPort)
	}
	if !c.areTablesFiltered() || c.flagListeners {
		cfg["listeners"] = FilterListeners(config.Listeners, c.flagAddress, c.flagPort)
	}
	if !c.areTablesFiltered() || c.flagRoutes {
		cfg["routes"] = config.Routes
	}
	if !c.areTablesFiltered() || c.flagSecrets {
		cfg["secrets"] = config.Secrets
>>>>>>> f11c95d0 (Add port filtering)
	}

	return nil
}

func (c *ReadCommand) outputJSON(configs map[string]*EnvoyConfig) error {
	cfgs := make(map[string]interface{})
	for name, config := range configs {
		cfg := make(map[string]interface{})
		if !c.tableFiltersPassed() || c.flagClusters {
			cfg["clusters"] = FilterFQDN(config.Clusters, c.flagFQDN)
		}
		if !c.tableFiltersPassed() || c.flagEndpoints {
			cfg["endpoints"] = config.Endpoints
		}
		if !c.tableFiltersPassed() || c.flagListeners {
			cfg["listeners"] = config.Listeners
		}
		if !c.tableFiltersPassed() || c.flagRoutes {
			cfg["routes"] = config.Routes
		}
		if !c.tableFiltersPassed() || c.flagSecrets {
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

<<<<<<< HEAD
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
=======
func (c *ReadCommand) outputAsTables(config *EnvoyConfig) {
	c.UI.Output(fmt.Sprintf("Envoy configuration for %s in namespace %s:", c.flagPodName, c.flagNamespace))
	if c.flagFQDN != "" || c.flagAddress != "" || c.flagPort != -1 {
		c.UI.Output("Filters applied", terminal.WithHeaderStyle())

		if c.flagFQDN != "" {
			c.UI.Output(fmt.Sprintf("Fully qualified domain names must contain `%s`", c.flagFQDN), terminal.WithInfoStyle())
		}
		if c.flagAddress != "" {
			c.UI.Output(fmt.Sprintf("Endpoint addresses must contain `%s`", c.flagAddress), terminal.WithInfoStyle())
		}
		if c.flagPort != -1 {
			c.UI.Output(fmt.Sprintf("Endpoint addresses must have the port `%d`", c.flagPort), terminal.WithInfoStyle())
		}
	}

	c.outputClustersTable(FilterClusters(config.Clusters, c.flagFQDN, c.flagAddress, c.flagPort))
	c.outputEndpointsTable(FilterEndpoints(config.Endpoints, c.flagAddress, c.flagPort))
	c.outputListenersTable(FilterListeners(config.Listeners, c.flagAddress, c.flagPort))
	c.outputRoutesTable(config.Routes)
	c.outputSecretsTable(config.Secrets)
>>>>>>> f11c95d0 (Add port filtering)
}

func (c *ReadCommand) outputClustersTable(clusters []Cluster) {
	if c.areTablesFiltered() && !c.flagClusters {
		return
	}

	c.UI.Output(fmt.Sprintf("Clusters (%d)", len(clusters)), terminal.WithHeaderStyle())
	table := terminal.NewTable("Name", "FQDN", "Endpoints", "Type", "Last Updated")
	for _, cluster := range clusters {
		table.AddRow([]string{cluster.Name, cluster.FullyQualifiedDomainName, strings.Join(cluster.Endpoints, ", "),
			cluster.Type, cluster.LastUpdated}, []string{})
	}
	c.UI.Table(table)
	c.UI.Output("")
}

func (c *ReadCommand) outputEndpointsTable(endpoints []Endpoint) {
	if c.areTablesFiltered() && !c.flagEndpoints {
		return
	}

	c.UI.Output(fmt.Sprintf("Endpoints (%d)", len(endpoints)), terminal.WithHeaderStyle())
	c.UI.Table(formatEndpoints(endpoints))
}

func (c *ReadCommand) outputListenersTable(listeners []Listener) {
	if c.areTablesFiltered() && !c.flagListeners {
		return
	}

	c.UI.Output(fmt.Sprintf("Listeners (%d)", len(listeners)), terminal.WithHeaderStyle())
	c.UI.Table(formatListeners(listeners))
}

func (c *ReadCommand) outputRoutesTable(routes []Route) {
	if c.areTablesFiltered() && !c.flagRoutes {
		return
	}

	c.UI.Output(fmt.Sprintf("Routes (%d)", len(routes)), terminal.WithHeaderStyle())
	c.UI.Table(formatRoutes(routes))
}

func (c *ReadCommand) outputSecretsTable(secrets []Secret) {
	if c.areTablesFiltered() && !c.flagSecrets {
		return
	}

	c.UI.Output(fmt.Sprintf("Secrets (%d)", len(secrets)), terminal.WithHeaderStyle())
	c.UI.Table(formatSecrets(secrets))
}
