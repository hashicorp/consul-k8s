// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package list

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/posener/complete"
	"golang.org/x/exp/maps"
	helmCLI "helm.sh/helm/v3/pkg/cli"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/validation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	"github.com/hashicorp/consul-k8s/cli/common"
	"github.com/hashicorp/consul-k8s/cli/common/flag"
	"github.com/hashicorp/consul-k8s/cli/common/terminal"
)

const (
	flagNameNamespace     = "namespace"
	flagNameAllNamespaces = "all-namespaces"
	flagNameKubeConfig    = "kubeconfig"
	flagNameKubeContext   = "context"
	flagNameOutputFormat  = "output"
)

// ListCommand is the command struct for the proxy list command.
type ListCommand struct {
	*common.BaseCommand

	kubernetes kubernetes.Interface

	set *flag.Sets

	flagNamespace     string
	flagAllNamespaces bool
	flagOutputFormat  string

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
	f.StringVar(&flag.StringVar{
		Name:    flagNameOutputFormat,
		Default: "table",
		Target:  &c.flagOutputFormat,
		Usage:   "Output format",
		Aliases: []string{"o"},
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
		fmt.Sprintf("-%s", flagNameOutputFormat):  complete.PredictNothing,
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
	if outputs := []string{"table", "json", "archive"}; !slices.Contains(outputs, c.flagOutputFormat) {
		return fmt.Errorf("-output must be one of %s.", strings.Join(outputs, ", "))
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

// fetchPods fetches all pods in flagNamespace which run Consul proxies,
// making sure to return each pod only once even if multiple label selectors may
// return the same pod. The pods in the resulting list are grouped by proxy type
// and then sorted by namespace + name within each group.
func (c *ListCommand) fetchPods() ([]v1.Pod, error) {
	var (
		apiGateways         = make(map[types.NamespacedName]v1.Pod)
		ingressGateways     = make(map[types.NamespacedName]v1.Pod)
		meshGateways        = make(map[types.NamespacedName]v1.Pod)
		terminatingGateways = make(map[types.NamespacedName]v1.Pod)
		sidecars            = make(map[types.NamespacedName]v1.Pod)
	)

	// Map target map for each proxy type. Note that some proxy types
	// require multiple selectors and thus target the same map.
	proxySelectors := []struct {
		Target   map[types.NamespacedName]v1.Pod
		Selector string
	}{
		{Target: apiGateways, Selector: "component=api-gateway, gateway.consul.hashicorp.com/managed=true"},
		{Target: apiGateways, Selector: "api-gateway.consul.hashicorp.com/managed=true"}, // Legacy API gateways
		{Target: ingressGateways, Selector: "component=ingress-gateway, chart=consul-helm"},
		{Target: meshGateways, Selector: "component=mesh-gateway, chart=consul-helm"},
		{Target: terminatingGateways, Selector: "component=terminating-gateway, chart=consul-helm"},
		{Target: sidecars, Selector: "consul.hashicorp.com/connect-inject-status=injected"},
	}

	// Query all proxy types into their appropriate maps.
	for _, selector := range proxySelectors {
		pods, err := c.kubernetes.CoreV1().Pods(c.namespace()).List(c.Ctx, metav1.ListOptions{
			LabelSelector: selector.Selector,
		})
		if err != nil {
			return nil, err
		}

		for _, pod := range pods.Items {
			name := types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}
			selector.Target[name] = pod
		}
	}

	// Collect all proxies into a single list of Pods, ordered by proxy type.
	// Within each proxy type subgroup, order by namespace and then name for output readability.
	var pods []v1.Pod
	var podSources = []map[types.NamespacedName]v1.Pod{
		apiGateways, ingressGateways, meshGateways, terminatingGateways, sidecars,
	}
	for _, podSource := range podSources {
		names := maps.Keys(podSource)

		// Insert Pods ordered by their NamespacedName which amounts to "<namespace>/<name>".
		sort.SliceStable(names, func(i, j int) bool {
			return strings.Compare(names[i].String(), names[j].String()) < 0
		})

		for _, name := range names {
			pods = append(pods, podSource[name])
		}
	}

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

	var tbl *terminal.Table
	if c.flagAllNamespaces {
		tbl = terminal.NewTable("Namespace", "Name", "Type")
	} else {
		tbl = terminal.NewTable("Name", "Type")
	}

	for _, pod := range pods {
		var proxyType string

		// Get the type for api, ingress, mesh, and terminating gateways + sidecars.
		switch pod.Labels["component"] {
		case "api-gateway":
			proxyType = "API Gateway"
		case "ingress-gateway":
			proxyType = "Ingress Gateway"
		case "mesh-gateway":
			proxyType = "Mesh Gateway"
		case "terminating-gateway":
			proxyType = "Terminating Gateway"
		default:
			// Fallback to "Sidecar" as a default
			proxyType = "Sidecar"

			// Determine if deprecated API Gateway pod.
			if pod.Labels["api-gateway.consul.hashicorp.com/managed"] == "true" {
				proxyType = "API Gateway"
			}
		}

		if c.flagAllNamespaces {
			tbl.AddRow([]string{pod.Namespace, pod.Name, proxyType}, []string{})
		} else {
			tbl.AddRow([]string{pod.Name, proxyType}, []string{})
		}
	}

	if c.flagOutputFormat == "json" {
		tableJson := tbl.ToJson()
		jsonSt, err := json.MarshalIndent(tableJson, "", "    ")
		if err != nil {
			c.UI.Output("error converting table to json: %v", err.Error(), terminal.WithErrorStyle())
		} else {
			c.UI.Output(string(jsonSt))
		}
	} else if c.flagOutputFormat == "archive" {
		tableJson := tbl.ToJson()
		jsonSt, err := json.MarshalIndent(tableJson, "", "    ")
		if err != nil {
			c.UI.Output("error converting proxy list output to json: %v", err.Error(), terminal.WithErrorStyle())
		}

		// Create file path and directory for storing proxy list
		// NOTE: currently it is writing stats file in cwd '/proxy' only. Also, file contents will be overwritten
		// if the command is run multiple times or if file already exists.
		proxyListFilePath := filepath.Join("proxy", "proxy-list.json")
		err = os.MkdirAll(filepath.Dir(proxyListFilePath), 0755)
		if err != nil {
			fmt.Printf("error creating proxy list output directory: %v", err)
		}
		err = os.WriteFile(proxyListFilePath, jsonSt, 0644)
		if err != nil {
			// Note: Please do not delete the directory created above even if writing file fails.
			// This (/proxy) directory is used by all proxy read, log, list, stats command, for storing their outputs as archive.
			fmt.Printf("error writing proxy list output to json file: %v", err)
		}
		c.UI.Output("proxy list output saved to '%s'", proxyListFilePath, terminal.WithSuccessStyle())
	} else {
		if !c.flagAllNamespaces {
			c.UI.Output("Namespace: %s\n", c.namespace())
		}

		c.UI.Table(tbl)
	}

}
