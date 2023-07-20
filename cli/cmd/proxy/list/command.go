package list

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/hashicorp/consul-k8s/cli/common"
	"github.com/hashicorp/consul-k8s/cli/common/flag"
	"github.com/hashicorp/consul-k8s/cli/common/terminal"
	"github.com/posener/complete"
	helmCLI "helm.sh/helm/v3/pkg/cli"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/validation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	flagNameNamespace     = "namespace"
	flagNameAllNamespaces = "all-namespaces"
	flagNameKubeConfig    = "kubeconfig"
	flagNameKubeContext   = "context"
)

// ListCommand is the command struct for the proxy list command.
type ListCommand struct {
	*common.BaseCommand

	kubernetes kubernetes.Interface

	set *flag.Sets

	flagNamespace     string
	flagAllNamespaces bool

	flagKubeConfig  string
	flagKubeContext string

	once sync.Once
	help string
}

// init sets up flags and help text for the command.
func (c *ListCommand) init() {
	c.set = flag.NewSets()

	f := c.set.NewSet("Command Options")
	f.StringVar(&flag.StringVar{
		Name:    flagNameNamespace,
		Target:  &c.flagNamespace,
		Usage:   "The namespace to list proxies in.",
		Aliases: []string{"n"},
	})
	f.BoolVar(&flag.BoolVar{
		Name:    flagNameAllNamespaces,
		Target:  &c.flagAllNamespaces,
		Default: false,
		Usage:   "List pods in all namespaces.",
		Aliases: []string{"A"},
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

	c.help = c.set.Help()
}

// Run executes the list command.
func (c *ListCommand) Run(args []string) int {
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
			c.UI.Output("Error initializing Kubernetes client", err.Error(), terminal.WithErrorStyle())
			return 1
		}
	}

	pods, err := c.fetchPods()
	if err != nil {
		c.UI.Output("Error fetching pods:", err.Error(), terminal.WithErrorStyle())
		return 1
	}

	c.output(pods)
	return 0
}

// Help returns a description of the command and how it is used.
func (c *ListCommand) Help() string {
	c.once.Do(c.init)
	return fmt.Sprintf("%s\n\nUsage: consul-k8s proxy list [flags]\n\n%s", c.Synopsis(), c.help)
}

// Synopsis returns a one-line command summary.
func (c *ListCommand) Synopsis() string {
	return "List all Pods running proxies managed by Consul."
}

// AutocompleteFlags returns a mapping of supported flags and autocomplete
// options for this command. The map key for the Flags map should be the
// complete flag such as "-foo" or "--foo".
func (c *ListCommand) AutocompleteFlags() complete.Flags {
	return complete.Flags{
		fmt.Sprintf("-%s", flagNameNamespace):     complete.PredictNothing,
		fmt.Sprintf("-%s", flagNameAllNamespaces): complete.PredictNothing,
		fmt.Sprintf("-%s", flagNameKubeConfig):    complete.PredictFiles("*"),
		fmt.Sprintf("-%s", flagNameKubeContext):   complete.PredictNothing,
	}
}

// AutocompleteArgs returns the argument predictor for this command.
// Since argument completion is not supported, this will return
// complete.PredictNothing.
func (c *ListCommand) AutocompleteArgs() complete.Predictor {
	return complete.PredictNothing
}

// validateFlags ensures that the flags passed in by the can be used.
func (c *ListCommand) validateFlags() error {
	if len(c.set.Args()) > 0 {
		return errors.New("should have no non-flag arguments")
	}
	if errs := validation.ValidateNamespaceName(c.flagNamespace, false); c.flagNamespace != "" && len(errs) > 0 {
		return fmt.Errorf("invalid namespace name passed for -namespace/-n: %v", strings.Join(errs, "; "))
	}

	return nil
}

// initKubernetes initializes the Kubernetes client.
func (c *ListCommand) initKubernetes() error {
	settings := helmCLI.New()

	if c.flagKubeConfig != "" {
		settings.KubeConfig = c.flagKubeConfig
	}

	if c.flagKubeContext != "" {
		settings.KubeContext = c.flagKubeContext
	}

	restConfig, err := settings.RESTClientGetter().ToRESTConfig()
	if err != nil {
		return fmt.Errorf("error retrieving Kubernetes authentication %v", err)
	}
	if c.kubernetes, err = kubernetes.NewForConfig(restConfig); err != nil {
		return fmt.Errorf("error creating Kubernetes client %v", err)
	}

	return nil
}

func (c *ListCommand) namespace() string {
	settings := helmCLI.New()

	if c.flagAllNamespaces {
		return "" // An empty namespace means all namespaces.
	} else if c.flagNamespace != "" {
		return c.flagNamespace
	} else {
		return settings.Namespace()
	}
}

// fetchPods fetches all pods in flagNamespace which run Consul proxies.
func (c *ListCommand) fetchPods() ([]v1.Pod, error) {
	var pods []v1.Pod

	// Fetch all pods in the namespace with labels matching the gateway component names.
	gatewaypods, err := c.kubernetes.CoreV1().Pods(c.namespace()).List(c.Ctx, metav1.ListOptions{
		LabelSelector: "component in (ingress-gateway, mesh-gateway, terminating-gateway), chart=consul-helm",
	})
	if err != nil {
		return nil, err
	}
	pods = append(pods, gatewaypods.Items...)

	// Fetch all pods in the namespace with a label indicating they are an API gateway.
	apigatewaypods, err := c.kubernetes.CoreV1().Pods(c.namespace()).List(c.Ctx, metav1.ListOptions{
		LabelSelector: "api-gateway.consul.hashicorp.com/managed=true",
	})
	if err != nil {
		return nil, err
	}
	pods = append(pods, apigatewaypods.Items...)

	// Fetch all pods in the namespace with a label indicating they are a service networked by Consul.
	sidecarpods, err := c.kubernetes.CoreV1().Pods(c.namespace()).List(c.Ctx, metav1.ListOptions{
		LabelSelector: "consul.hashicorp.com/connect-inject-status=injected",
	})
	if err != nil {
		return nil, err
	}
	pods = append(pods, sidecarpods.Items...)

	return pods, nil
}

// output prints a table of pods to the terminal.
func (c *ListCommand) output(pods []v1.Pod) {
	if len(pods) == 0 {
		if c.flagAllNamespaces {
			c.UI.Output("No proxies found across all namespaces.")
		} else {
			c.UI.Output("No proxies found in %s namespace.", c.namespace())
		}
		return
	}

	if c.flagAllNamespaces {
		c.UI.Output("Namespace: all namespaces\n")
	} else {
		c.UI.Output("Namespace: %s\n", c.namespace())
	}

	var tbl *terminal.Table
	if c.flagAllNamespaces {
		tbl = terminal.NewTable("Namespace", "Name", "Type")
	} else {
		tbl = terminal.NewTable("Name", "Type")
	}

	for _, pod := range pods {
		var proxyType string

		// Get the type for ingress, mesh, and terminating gateways.
		switch pod.Labels["component"] {
		case "ingress-gateway":
			proxyType = "Ingress Gateway"
		case "mesh-gateway":
			proxyType = "Mesh Gateway"
		case "terminating-gateway":
			proxyType = "Terminating Gateway"
		}

		// Determine if the pod is an API Gateway.
		if pod.Labels["api-gateway.consul.hashicorp.com/managed"] == "true" {
			proxyType = "API Gateway"
		}

		// Fallback to "Sidecar" as a default
		if proxyType == "" {
			proxyType = "Sidecar"
		}

		if c.flagAllNamespaces {
			tbl.AddRow([]string{pod.Namespace, pod.Name, proxyType}, []string{})
		} else {
			tbl.AddRow([]string{pod.Name, proxyType}, []string{})
		}
	}

	c.UI.Table(tbl)
}
