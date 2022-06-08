package list

import (
	"errors"
	"fmt"
	"sync"

	"github.com/hashicorp/consul-k8s/cli/common"
	"github.com/hashicorp/consul-k8s/cli/common/flag"
	"github.com/hashicorp/consul-k8s/cli/common/terminal"
	helmCLI "helm.sh/helm/v3/pkg/cli"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
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
		Name:    "namespace",
		Target:  &c.flagNamespace,
		Default: "default",
		Usage:   "The namespace to list proxies in.",
		Aliases: []string{"n"},
	})
	f.BoolVar(&flag.BoolVar{
		Name:    "all-namespaces",
		Target:  &c.flagAllNamespaces,
		Default: false,
		Usage:   "List pods in all namespaces.",
		Aliases: []string{"A"},
	})

	f = c.set.NewSet("Global Options")
	f.StringVar(&flag.StringVar{
		Name:    "kubeconfig",
		Aliases: []string{"c"},
		Target:  &c.flagKubeConfig,
		Default: "",
		Usage:   "Set the path to kubeconfig file.",
	})
	f.StringVar(&flag.StringVar{
		Name:    "context",
		Target:  &c.flagKubeContext,
		Default: "",
		Usage:   "Set the Kubernetes context to use.",
	})

	c.help = c.set.Help()
	c.Init()
}

// Run executes the list command.
func (c *ListCommand) Run(args []string) int {
	c.once.Do(c.init)
	c.Log.ResetNamed("list")
	defer common.CloseWithError(c.BaseCommand)

	// Parse the command line flags.
	if err := c.set.Parse(args); err != nil {
		c.UI.Output("Error parsing arguments:\n%v", err.Error())
		return 1
	}

	// Validate the command line flags.
	if err := c.validateFlags(); err != nil {
		c.UI.Output("Invalid argument:\n%v", err.Error())
		return 1
	}

	if c.kubernetes == nil {
		if err := c.initKubernetes(); err != nil {
			c.UI.Output("Error initializing Kubernetes client", err.Error())
			return 1
		}
	}

	if c.flagAllNamespaces {
		c.flagNamespace = ""
	}

	pods, err := c.fetchPods()
	if err != nil {
		c.UI.Output("Error fetching pods:\n%v", err, terminal.WithErrorStyle())
		return 1
	}

	c.output(pods)
	return 0
}

// Help returns a description of the command and how it is used.
func (c *ListCommand) Help() string {
	c.once.Do(c.init)
	return c.Synopsis() + "\n\nUsage: consul-k8s proxy list [flags]\n\n" + c.help
}

// Synopsis returns a one-line command summary.
func (c *ListCommand) Synopsis() string {
	return "List all Pods running proxies managed by Consul."
}

// validateFlags ensures that the flags passed in by the can be used.
func (c *ListCommand) validateFlags() error {
	if len(c.set.Args()) > 0 {
		return errors.New("should have no non-flag arguments")
	}
	if !common.IsValidLabel(c.flagNamespace) {
		return fmt.Errorf("'%s' is an invalid namespace. Namespaces follow the RFC 1123 label convention and must "+
			"consist of a lower case alphanumeric character or '-' and must start/end with an alphanumeric character", c.flagNamespace)
	}
	return nil
}

// initKubernetes initializes the Kubernetes client with the users config.
func (c *ListCommand) initKubernetes() error {
	settings := helmCLI.New()

	restConfig, err := settings.RESTClientGetter().ToRESTConfig()
	if err != nil {
		return fmt.Errorf("error retrieving Kubernetes authentication %v", err)
	}
	c.kubernetes, err = kubernetes.NewForConfig(restConfig)

	return err
}

// fetchPods fetches all pods in flagNamespace which run Consul proxies.
func (c *ListCommand) fetchPods() ([]v1.Pod, error) {
	var pods []v1.Pod

	gatewaypods, err := c.kubernetes.CoreV1().Pods(c.flagNamespace).List(c.Ctx, metav1.ListOptions{
		LabelSelector: "component in (ingress-gateway, mesh-gateway, terminating-gateway)",
	})
	if err != nil {
		return nil, err
	}
	pods = append(pods, gatewaypods.Items...)

	apigatewaypods, err := c.kubernetes.CoreV1().Pods(c.flagNamespace).List(c.Ctx, metav1.ListOptions{
		LabelSelector: "api-gateway.consul.hashicorp.com/managed=true",
	})
	if err != nil {
		return nil, err
	}
	pods = append(pods, apigatewaypods.Items...)

	sidecarpods, err := c.kubernetes.CoreV1().Pods(c.flagNamespace).List(c.Ctx, metav1.ListOptions{
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
	if c.flagNamespace == "" {
		c.UI.Output("Namespace: All Namespaces\n")
	} else {
		c.UI.Output("Namespace: %s\n", c.flagNamespace)
	}

	var tbl *terminal.Table
	if c.flagNamespace == "" {
		tbl = terminal.NewTable("Namespace", "Name", "Type")
	} else {
		tbl = terminal.NewTable("Name", "Type")
	}

	for _, pod := range pods {
		var proxyType string
		switch pod.Labels["component"] {
		case "ingress-gateway":
			proxyType = "Ingress Gateway"
		case "mesh-gateway":
			proxyType = "Mesh Gateway"
		case "terminating-gateway":
			proxyType = "Terminating Gateway"
		case "api-gateway":
			proxyType = "API Gateway"
		default:
			proxyType = "Sidecar"
		}

		if c.flagNamespace == "" {
			tbl.AddRow([]string{pod.Namespace, pod.Name, proxyType}, []string{})
		} else {
			tbl.AddRow([]string{pod.Name, proxyType}, []string{})
		}
	}

	c.UI.Table(tbl)
}
