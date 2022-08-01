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
	"k8s.io/client-go/kubernetes"
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

	// Global Flags
	flagKubeConfig  string
	flagKubeContext string

	fetchConfig func(context.Context, common.PortForwarder) (*EnvoyConfig, error)

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
		Usage:   "The namespace to list proxies in.",
		Aliases: []string{"n"},
	})
	f.BoolVar(&flag.BoolVar{
		Name:    "json",
		Target:  &c.flagJSON,
		Default: false,
		Usage:   "Output the whole Envoy Config as JSON.",
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

	if err := c.set.Parse(args); err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}

	if arguments := c.set.Args(); len(arguments) > 0 {
		c.flagPodName = arguments[0]
	}

	if c.flagPodName == "" {
		c.UI.Output("Missing pod name.")
		return 1
	}

	if err := c.validateFlags(); err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}

	if c.kubernetes == nil {
		if err := c.initKubernetes(); err != nil {
			c.UI.Output(err.Error(), terminal.WithErrorStyle())
			return 1
		}
	}

	pf := common.PortForward{
		Namespace:   c.flagNamespace,
		PodName:     c.flagPodName,
		RemotePort:  adminPort,
		KubeClient:  c.kubernetes,
		KubeConfig:  c.flagKubeConfig,
		KubeContext: c.flagKubeContext,
	}

	config, err := c.fetchConfig(c.Ctx, &pf)
	if err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}

	c.outputConfig(config)

	return 0
}

func (c *ReadCommand) Synopsis() string {
	return ""
}

func (c *ReadCommand) Help() string {
	return ""
}

func (c *ReadCommand) validateFlags() error {
	return nil
}

func (c *ReadCommand) initKubernetes() error {
	settings := helmCLI.New()

	restConfig, err := settings.RESTClientGetter().ToRESTConfig()
	if err != nil {
		return fmt.Errorf("error retrieving Kubernetes authentication %v", err)
	}
	if c.kubernetes, err = kubernetes.NewForConfig(restConfig); err != nil {
		return fmt.Errorf("error creating Kubernetes client %v", err)
	}
	if c.flagKubeConfig == "" {
		c.flagKubeConfig = settings.KubeConfig
	}
	if c.flagKubeContext == "" {
		c.flagKubeContext = settings.KubeContext
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

	c.UI.Output("Clusters:")
	clusters := terminal.NewTable("Name", "FQDN", "Endpoints", "Type", "Last Updated")
	for _, cluster := range config.Clusters {
		clusters.AddRow([]string{cluster.Name, cluster.FullyQualifiedDomainName, strings.Join(cluster.Endpoints, ", "),
			cluster.Type, cluster.LastUpdated}, []string{})
	}

	c.UI.Output("Endpoints:")
	endpoints := terminal.NewTable("Endpoint", "Cluster", "Weight", "Status")
	for _, endpoint := range config.Endpoints {
		endpoints.AddRow([]string{endpoint.Address, endpoint.Cluster, fmt.Sprintf("%f", endpoint.Weight), endpoint.Status}, []string{})
	}
}
