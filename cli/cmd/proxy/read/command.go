package read

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/hashicorp/consul-k8s/cli/common"
	"github.com/hashicorp/consul-k8s/cli/common/flag"
	"github.com/hashicorp/consul-k8s/cli/common/terminal"
	helmCLI "helm.sh/helm/v3/pkg/cli"
	"k8s.io/apimachinery/pkg/api/validation"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// adminPort is the port where the Envoy admin API is exposed.
const adminPort int = 19000

type ReadCommand struct {
	*common.BaseCommand

	kubernetes kubernetes.Interface

	set *flag.Sets

	// Command Flags
	flagNamespace string
	flagPodName   string
	flagJSON      bool

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
	f.BoolVar(&flag.BoolVar{
		Name:    "json",
		Target:  &c.flagJSON,
		Default: false,
		Usage:   "Output the whole Envoy Config as JSON.",
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
	c.Init()
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

	pf := common.PortForward{
		Namespace:  c.flagNamespace,
		PodName:    c.flagPodName,
		RemotePort: adminPort,
		KubeClient: c.kubernetes,
		RestConfig: c.restConfig,
	}

	config, err := c.fetchConfig(c.Ctx, &pf)
	if err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}

	c.outputConfig(config)
	return 0
}

func (c *ReadCommand) Help() string {
	c.once.Do(c.init)
	return fmt.Sprintf("%s\n\nUsage: consul-k8s proxy read <pod-name> [flags]\n\n%s", c.Synopsis(), c.help)
}

func (c *ReadCommand) Synopsis() string {
	return "Print the Envoy configuration for a given Pod."
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
	return nil
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

func (c *ReadCommand) outputConfig(config *EnvoyConfig) {
	if c.flagJSON {
		c.UI.Output(string(config.rawCfg))
		return
	}

	c.UI.Output(fmt.Sprintf("Envoy configuration for %s in Namespace %s:", c.flagPodName, c.flagNamespace))

	// Track if any filters are passed in. If not, print everything; if so, only
	// print the filters that are passed in.
	filtersPassed := c.flagClusters || c.flagEndpoints || c.flagListeners || c.flagRoutes || c.flagSecrets

	if !filtersPassed || c.flagClusters {
		c.UI.Output(fmt.Sprintf("Clusters (%d)", len(config.Clusters)), terminal.WithHeaderStyle())
		clusters := terminal.NewTable("Name", "FQDN", "Endpoints", "Type", "Last Updated")
		for _, cluster := range config.Clusters {
			clusters.AddRow([]string{cluster.Name, cluster.FullyQualifiedDomainName, strings.Join(cluster.Endpoints, ", "),
				cluster.Type, cluster.LastUpdated}, []string{})
		}
		c.UI.Table(clusters)
		c.UI.Output("")
	}

	if !filtersPassed || c.flagEndpoints {
		c.UI.Output(fmt.Sprintf("Endpoints (%d)", len(config.Endpoints)), terminal.WithHeaderStyle())
		endpoints := terminal.NewTable("Address:Port", "Cluster", "Weight", "Status")
		for _, endpoint := range config.Endpoints {
			var statusColor string
			if endpoint.Status == "HEALTHY" {
				statusColor = "green"
			} else {
				statusColor = "red"
			}

			endpoints.AddRow(
				[]string{endpoint.Address, endpoint.Cluster, fmt.Sprintf("%f", endpoint.Weight), endpoint.Status},
				[]string{"", "", "", statusColor})
		}
		c.UI.Table(endpoints)
		c.UI.Output("")
	}

	if !filtersPassed || c.flagListeners {
		c.UI.Output(fmt.Sprintf("Listeners (%d)", len(config.Listeners)), terminal.WithHeaderStyle())
		listeners := terminal.NewTable("Name", "Address:Port", "Direction", "Filter Chain Match", "Filters")
		for _, listener := range config.Listeners {

			for index, filter := range listener.FilterChain {
				// Print each element of the filter chain in a separate line
				// without repeating the name, address, etc.
				filters := strings.Join(filter.Filters, "\n")
				if index == 0 {
					listeners.AddRow(
						[]string{listener.Name, listener.Address, listener.Direction, filter.FilterChainMatch, filters},
						[]string{})
				} else {
					listeners.AddRow(
						[]string{"", "", "", filter.FilterChainMatch, filters},
						[]string{})
				}
			}
		}
		c.UI.Table(listeners)
		c.UI.Output("")
	}

	if !filtersPassed || c.flagRoutes {
		c.UI.Output(fmt.Sprintf("Routes (%d)", len(config.Routes)), terminal.WithHeaderStyle())
		routes := terminal.NewTable("Name", "Destination Cluster", "Last Updated")
		for _, route := range config.Routes {
			routes.AddRow([]string{route.Name, route.DestinationCluster, route.LastUpdated}, []string{})
		}
		c.UI.Table(routes)
		c.UI.Output("")
	}

	if !filtersPassed || c.flagSecrets {
		c.UI.Output(fmt.Sprintf("Secrets (%d)", len(config.Secrets)), terminal.WithHeaderStyle())
		secrets := terminal.NewTable("Name", "Type", "Last Updated")
		for _, secret := range config.Secrets {
			secrets.AddRow([]string{secret.Name, secret.Type, secret.LastUpdated}, []string{})
		}

		c.UI.Table(secrets)
		c.UI.Output("")
	}
}
