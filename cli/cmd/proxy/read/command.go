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
	"github.com/posener/complete"
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

	flagNameNamespace   = "namespace"
	flagNameOutput      = "output"
	flagNameClusters    = "clusters"
	flagNameListeners   = "listeners"
	flagNameRoutes      = "routes"
	flagNameEndpoints   = "endpoints"
	flagNameSecrets     = "secrets"
	flagNameFQDN        = "fqdn"
	flagNameAddress     = "address"
	flagNamePort        = "port"
	flagNameKubeConfig  = "kubeconfig"
	flagNameKubeContext = "context"
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
		Name:    flagNameNamespace,
		Target:  &c.flagNamespace,
		Usage:   "The namespace where the target Pod can be found.",
		Aliases: []string{"n"},
	})
	f.StringVar(&flag.StringVar{
		Name:    flagNameOutput,
		Target:  &c.flagOutput,
		Usage:   "Output the Envoy configuration as 'table', 'json', or 'raw'.",
		Default: Table,
		Aliases: []string{"o"},
	})

	f = c.set.NewSet("Output Filtering Options")
	f.BoolVar(&flag.BoolVar{
		Name:   flagNameClusters,
		Target: &c.flagClusters,
		Usage:  "Filter output to only show clusters.",
	})
	f.BoolVar(&flag.BoolVar{
		Name:   flagNameListeners,
		Target: &c.flagListeners,
		Usage:  "Filter output to only show listeners.",
	})
	f.BoolVar(&flag.BoolVar{
		Name:   flagNameRoutes,
		Target: &c.flagRoutes,
		Usage:  "Filter output to only show routes.",
	})
	f.BoolVar(&flag.BoolVar{
		Name:   flagNameEndpoints,
		Target: &c.flagEndpoints,
		Usage:  "Filter output to only show endpoints.",
	})
	f.BoolVar(&flag.BoolVar{
		Name:   flagNameSecrets,
		Target: &c.flagSecrets,
		Usage:  "Filter output to only show secrets.",
	})
	f.StringVar(&flag.StringVar{
		Name:   flagNameFQDN,
		Target: &c.flagFQDN,
		Usage:  "Filter cluster output to clusters with a fully qualified domain name which contains the given value. May be combined with -address and -port.",
	})
	f.StringVar(&flag.StringVar{
		Name:   flagNameAddress,
		Target: &c.flagAddress,
		Usage:  "Filter clusters, endpoints, and listeners output to those with addresses which contain the given value. May be combined with -fqdn and -port",
	})
	f.IntVar(&flag.IntVar{
		Name:    flagNamePort,
		Target:  &c.flagPort,
		Usage:   "Filter endpoints and listeners output to addresses with the given port number. May be combined with -fqdn and -address.",
		Default: -1,
	})

	f = c.set.NewSet("GlobalOptions")
	f.StringVar(&flag.StringVar{
		Name:    flagNameKubeConfig,
		Aliases: []string{"c"},
		Target:  &c.flagKubeConfig,
		Usage:   "Set the path to kubeconfig file.",
	})
	f.StringVar(&flag.StringVar{
		Name:   flagNameKubeContext,
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

// AutocompleteFlags returns a mapping of supported flags and autocomplete
// options for this command. The map key for the Flags map should be the
// complete flag such as "-foo" or "--foo".
func (c *ReadCommand) AutocompleteFlags() complete.Flags {
	return complete.Flags{
		fmt.Sprintf("-%s", flagNameNamespace):   complete.PredictNothing,
		fmt.Sprintf("-%s", flagNameOutput):      complete.PredictNothing,
		fmt.Sprintf("-%s", flagNameClusters):    complete.PredictNothing,
		fmt.Sprintf("-%s", flagNameListeners):   complete.PredictNothing,
		fmt.Sprintf("-%s", flagNameRoutes):      complete.PredictNothing,
		fmt.Sprintf("-%s", flagNameEndpoints):   complete.PredictNothing,
		fmt.Sprintf("-%s", flagNameSecrets):     complete.PredictNothing,
		fmt.Sprintf("-%s", flagNameFQDN):        complete.PredictNothing,
		fmt.Sprintf("-%s", flagNameAddress):     complete.PredictNothing,
		fmt.Sprintf("-%s", flagNamePort):        complete.PredictNothing,
		fmt.Sprintf("-%s", flagNameKubeConfig):  complete.PredictFiles("*"),
		fmt.Sprintf("-%s", flagNameKubeContext): complete.PredictNothing,
	}
}

// AutocompleteArgs returns the argument predictor for this command.
// Since argument completion is not supported, this will return
// complete.PredictNothing.
func (c *ReadCommand) AutocompleteArgs() complete.Predictor {
	return complete.PredictNothing
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

func (c *ReadCommand) initKubernetes() (err error) {
	settings := helmCLI.New()

	if c.flagKubeConfig != "" {
		settings.KubeConfig = c.flagKubeConfig
	}

	if c.flagKubeContext != "" {
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

// shouldPrintTable takes the flag passed in for that table. If the flag is true,
// the table should always be printed. Otherwise, it should only be printed if
// no other table filtering flags are passed in.
func (c *ReadCommand) shouldPrintTable(table bool) bool {
	if table {
		return table
	}

	// True if no other table filtering flags are passed in.
	return !(c.flagClusters || c.flagEndpoints || c.flagListeners || c.flagRoutes || c.flagSecrets)
}

// filterWarnings checks if the user has passed in a combination of field and
// table filters where the field in question is not present on the table and
// returns a warning.
// For example, if the user passes "-fqdn default -endpoints", a warning will
// be printed saying "The filter `-fqdn default` does not apply to the tables displayed.".
func (c *ReadCommand) filterWarnings() []string {
	var warnings []string

	// No table filters passed. Return early.
	if !(c.flagClusters || c.flagEndpoints || c.flagListeners || c.flagRoutes || c.flagSecrets) {
		return warnings
	}

	if c.flagFQDN != "" && !c.flagClusters {
		warnings = append(warnings, fmt.Sprintf("The filter `-fqdn %s` does not apply to the tables displayed.", c.flagFQDN))
	}

	if c.flagPort != -1 && !(c.flagClusters || c.flagEndpoints || c.flagListeners) {
		warnings = append(warnings, fmt.Sprintf("The filter `-port %d` does not apply to the tables displayed.", c.flagPort))
	}

	if c.flagAddress != "" && !(c.flagClusters || c.flagEndpoints || c.flagListeners) {
		warnings = append(warnings, fmt.Sprintf("The filter `-address %s` does not apply to the tables displayed.", c.flagAddress))
	}

	return warnings
}

func (c *ReadCommand) outputTables(configs map[string]*EnvoyConfig) error {
	if c.flagFQDN != "" || c.flagAddress != "" || c.flagPort != -1 {
		c.UI.Output("Filters applied", terminal.WithHeaderStyle())

		if c.flagFQDN != "" {
			c.UI.Output(fmt.Sprintf("Fully qualified domain names containing: %s", c.flagFQDN), terminal.WithInfoStyle())
		}
		if c.flagAddress != "" {
			c.UI.Output(fmt.Sprintf("Endpoint addresses containing: %s", c.flagAddress), terminal.WithInfoStyle())
		}
		if c.flagPort != -1 {
			c.UI.Output(fmt.Sprintf("Endpoint addresses with port number: %d", c.flagPort), terminal.WithInfoStyle())
		}

		for _, warning := range c.filterWarnings() {
			c.UI.Output(warning, terminal.WithWarningStyle())
		}

		c.UI.Output("")
	}

	for name, config := range configs {
		c.UI.Output(fmt.Sprintf("Envoy configuration for %s in namespace %s:", name, c.flagNamespace))

		c.outputClustersTable(FilterClusters(config.Clusters, c.flagFQDN, c.flagAddress, c.flagPort))
		c.outputEndpointsTable(FilterEndpoints(config.Endpoints, c.flagAddress, c.flagPort))
		c.outputListenersTable(FilterListeners(config.Listeners, c.flagAddress, c.flagPort))
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
		if c.shouldPrintTable(c.flagClusters) {
			cfg["clusters"] = FilterClusters(config.Clusters, c.flagFQDN, c.flagAddress, c.flagPort)
		}
		if c.shouldPrintTable(c.flagEndpoints) {
			cfg["endpoints"] = FilterEndpoints(config.Endpoints, c.flagAddress, c.flagPort)
		}
		if c.shouldPrintTable(c.flagListeners) {
			cfg["listeners"] = FilterListeners(config.Listeners, c.flagAddress, c.flagPort)
		}
		if c.shouldPrintTable(c.flagRoutes) {
			cfg["routes"] = config.Routes
		}
		if c.shouldPrintTable(c.flagSecrets) {
			cfg["secrets"] = config.Secrets
		}

		cfgs[name] = cfg
	}

	escaped, err := json.MarshalIndent(cfgs, "", "\t")
	if err != nil {
		return err
	}

	// Unescape `>` the cheap way.
	out := strings.ReplaceAll(string(escaped), "\\u003e", ">")

	c.UI.Output(out)

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
	if !c.shouldPrintTable(c.flagClusters) {
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
	if !c.shouldPrintTable(c.flagEndpoints) {
		return
	}

	c.UI.Output(fmt.Sprintf("Endpoints (%d)", len(endpoints)), terminal.WithHeaderStyle())
	c.UI.Table(formatEndpoints(endpoints))
}

func (c *ReadCommand) outputListenersTable(listeners []Listener) {
	if !c.shouldPrintTable(c.flagListeners) {
		return
	}

	c.UI.Output(fmt.Sprintf("Listeners (%d)", len(listeners)), terminal.WithHeaderStyle())
	c.UI.Table(formatListeners(listeners))
}

func (c *ReadCommand) outputRoutesTable(routes []Route) {
	if !c.shouldPrintTable(c.flagRoutes) {
		return
	}

	c.UI.Output(fmt.Sprintf("Routes (%d)", len(routes)), terminal.WithHeaderStyle())
	c.UI.Table(formatRoutes(routes))
}

func (c *ReadCommand) outputSecretsTable(secrets []Secret) {
	if !c.shouldPrintTable(c.flagSecrets) {
		return
	}

	c.UI.Output(fmt.Sprintf("Secrets (%d)", len(secrets)), terminal.WithHeaderStyle())
	c.UI.Table(formatSecrets(secrets))
}
