package proxy

import (
	"fmt"
	"net"
	"strings"
	"sync"

	"github.com/hashicorp/consul-k8s/cli/common"
	"github.com/hashicorp/consul-k8s/cli/common/flag"
	"github.com/hashicorp/consul-k8s/cli/common/terminal"
	troubleshoot "github.com/hashicorp/consul/troubleshoot/proxy"
	"github.com/posener/complete"
	helmCLI "helm.sh/helm/v3/pkg/cli"
	"k8s.io/apimachinery/pkg/api/validation"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	defaultAdminPort        int = 19000
	flagNameKubeConfig          = "kubeconfig"
	flagNameKubeContext         = "context"
	flagNameNamespace           = "namespace"
	flagNamePod                 = "pod"
	flagNameUpstreamEnvoyID     = "upstream-envoy-id"
	flagNameUpstreamIP          = "upstream-ip"
	DebugColor                  = "\033[0;36m%s\033[0m"
)

type ProxyCommand struct {
	*common.BaseCommand

	kubernetes kubernetes.Interface

	set *flag.Sets

	flagKubeConfig  string
	flagKubeContext string
	flagNamespace   string

	flagPod             string
	flagUpstreamEnvoyID string
	flagUpstreamIP      string

	restConfig *rest.Config

	once sync.Once
	help string
}

// init sets up flags and help text for the command.
func (c *ProxyCommand) init() {
	c.set = flag.NewSets()
	f := c.set.NewSet("Command Options")

	f.StringVar(&flag.StringVar{
		Name:    flagNamePod,
		Target:  &c.flagPod,
		Usage:   "The pod to port-forward to.",
		Aliases: []string{"p"},
	})

	f.StringVar(&flag.StringVar{
		Name:    flagNameUpstreamEnvoyID,
		Target:  &c.flagUpstreamEnvoyID,
		Usage:   "The envoy identifier of the upstream service that receives the communication. (explicit upstreams only)",
		Aliases: []string{"id"},
	})

	f.StringVar(&flag.StringVar{
		Name:    flagNameUpstreamIP,
		Target:  &c.flagUpstreamIP,
		Usage:   "The IP address of the upstream service that receives the communication. (transparent proxy only)",
		Aliases: []string{"ip"},
	})

	f = c.set.NewSet("Global Options")
	f.StringVar(&flag.StringVar{
		Name:    flagNameKubeConfig,
		Aliases: []string{"c"},
		Target:  &c.flagKubeConfig,
		Default: "",
		Usage:   "Set the path to kubeconfig file.",
	})
	f.StringVar(&flag.StringVar{
		Name:    flagNameKubeContext,
		Target:  &c.flagKubeContext,
		Default: "",
		Usage:   "Set the Kubernetes context to use.",
	})

	f.StringVar(&flag.StringVar{
		Name:    flagNameNamespace,
		Target:  &c.flagNamespace,
		Usage:   "The namespace the pod is in.",
		Aliases: []string{"n"},
	})

	c.help = c.set.Help()
}

// Run executes the list command.
func (c *ProxyCommand) Run(args []string) int {
	c.once.Do(c.init)
	c.Log.ResetNamed("list")
	defer common.CloseWithError(c.BaseCommand)

	// Parse the command line flags.
	if err := c.set.Parse(args); err != nil {
		c.UI.Output("Error parsing arguments: %v", err.Error(), terminal.WithErrorStyle())
		return 1
	}

	// Validate the command line flags.
	if err := c.validateFlags(); err != nil {
		c.UI.Output("Invalid argument: %v", err.Error(), terminal.WithErrorStyle())
		return 1
	}

	if c.kubernetes == nil {
		if err := c.initKubernetes(); err != nil {
			c.UI.Output("Error initializing Kubernetes client: %v", err.Error(), terminal.WithErrorStyle())
			return 1
		}
	}

	if err := c.Troubleshoot(); err != nil {
		c.UI.Output("Error running troubleshoot: %v", err.Error(), terminal.WithErrorStyle())
		return 1
	}

	return 0
}

// validateFlags ensures that the flags passed in by the can be used.
func (c *ProxyCommand) validateFlags() error {

	if (c.flagUpstreamEnvoyID == "" && c.flagUpstreamIP == "") || (c.flagUpstreamEnvoyID != "" && c.flagUpstreamIP != "") {
		return fmt.Errorf("-upstream-envoy-id OR -upstream-ip is required.\n Please run `consul troubleshoot upstreams` to find the corresponding upstream.")
	}

	if c.flagPod == "" {
		return fmt.Errorf("-pod flag is required")
	}

	if errs := validation.ValidateNamespaceName(c.flagNamespace, false); c.flagNamespace != "" && len(errs) > 0 {
		return fmt.Errorf("invalid namespace name passed for -namespace/-n: %v", strings.Join(errs, "; "))
	}

	return nil
}

// initKubernetes initializes the Kubernetes client.
func (c *ProxyCommand) initKubernetes() (err error) {
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

func (c *ProxyCommand) Troubleshoot() error {
	pf := common.PortForward{
		Namespace:  c.flagNamespace,
		PodName:    c.flagPod,
		RemotePort: defaultAdminPort,
		KubeClient: c.kubernetes,
		RestConfig: c.restConfig,
	}

	endpoint, err := pf.Open(c.Ctx)
	if err != nil {
		return err
	}
	defer pf.Close()

	adminAddr, adminPort, err := net.SplitHostPort(endpoint)
	if err != nil {
		return err
	}

	adminAddrIP, err := net.ResolveIPAddr("ip", adminAddr)
	if err != nil {
		return err
	}

	t, err := troubleshoot.NewTroubleshoot(adminAddrIP, adminPort)
	if err != nil {
		return err
	}

	// err = t.GetEnvoyConfigDump()
	// if err != nil {
	// 	return err
	// }

	messages, err := t.RunAllTests(c.flagUpstreamEnvoyID, c.flagUpstreamIP)
	if err != nil {
		return err
	}

	c.UI.Output("Validation", terminal.WithHeaderStyle())
	for _, o := range messages {
		if o.Success {
			c.UI.Output(o.Message, terminal.WithSuccessStyle())
		} else {
			c.UI.Output(o.Message, terminal.WithErrorStyle())
			if o.PossibleActions != "" {
				c.UI.Output(fmt.Sprintf("possible actions: %v", o.PossibleActions), terminal.WithInfoStyle())
			}
		}
	}

	return nil
}

// AutocompleteFlags returns a mapping of supported flags and autocomplete
// options for this command. The map key for the Flags map should be the
// complete flag such as "-foo" or "--foo".
func (c *ProxyCommand) AutocompleteFlags() complete.Flags {
	return complete.Flags{
		fmt.Sprintf("-%s", flagNameNamespace):   complete.PredictNothing,
		fmt.Sprintf("-%s", flagNameKubeConfig):  complete.PredictFiles("*"),
		fmt.Sprintf("-%s", flagNameKubeContext): complete.PredictNothing,
	}
}

// AutocompleteArgs returns the argument predictor for this command.
// Since argument completion is not supported, this will return
// complete.PredictNothing.
func (c *ProxyCommand) AutocompleteArgs() complete.Predictor {
	return complete.PredictNothing
}

func (c *ProxyCommand) Synopsis() string {
	return synopsis
}

func (c *ProxyCommand) Help() string {
	return help
}

const (
	synopsis = "Troubleshoots service mesh issues."
	help     = `
Usage: consul-k8s troubleshoot proxy [options]

  Connect to a pod with a proxy and troubleshoots service mesh communication issues.

  Requires a pod and upstream service SNI.

  Examples:
    $ consul-k8s troubleshoot proxy -pod pod1 -upstream foo
	
	where 'pod1' is the pod running a consul proxy and 'foo' is the upstream envoy ID which 
	can be obtained by running:
	$ consul-k8s troubleshoot upstreams [options]
`
)
