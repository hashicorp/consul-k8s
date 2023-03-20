// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package upstreams

import (
	"fmt"
	"net"
	"sort"
	"strconv"
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
	defaultAdminPort    int = 19000
	flagNameKubeConfig      = "kubeconfig"
	flagNameKubeContext     = "context"
	flagNameNamespace       = "namespace"
	flagNamePod             = "pod"
)

type UpstreamsCommand struct {
	*common.BaseCommand

	kubernetes kubernetes.Interface

	set *flag.Sets

	flagKubeConfig  string
	flagKubeContext string
	flagNamespace   string

	flagPod string

	restConfig *rest.Config

	once sync.Once
	help string
}

// init sets up flags and help text for the command.
func (c *UpstreamsCommand) init() {
	c.set = flag.NewSets()
	f := c.set.NewSet("Command Options")

	f.StringVar(&flag.StringVar{
		Name:    flagNamePod,
		Target:  &c.flagPod,
		Usage:   "The pod to port-forward to.",
		Aliases: []string{"p"},
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
func (c *UpstreamsCommand) Run(args []string) int {
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
func (c *UpstreamsCommand) validateFlags() error {

	if c.flagPod == "" {
		return fmt.Errorf("-pod flag is required")
	}

	if errs := validation.ValidateNamespaceName(c.flagNamespace, false); c.flagNamespace != "" && len(errs) > 0 {
		return fmt.Errorf("invalid namespace name passed for -namespace/-n: %v", strings.Join(errs, "; "))
	}

	return nil
}

// initKubernetes initializes the Kubernetes client.
func (c *UpstreamsCommand) initKubernetes() (err error) {
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

func (c *UpstreamsCommand) Troubleshoot() error {
	pf := common.PortForward{
		Namespace:  c.flagNamespace,
		PodName:    c.flagPod,
		RemotePort: defaultAdminPort,
		KubeClient: c.kubernetes,
		RestConfig: c.restConfig,
	}

	endpoint, err := pf.Open(c.Ctx)
	if err != nil {
		return fmt.Errorf("error opening endpoint: %v", err)
	}
	defer pf.Close()

	adminAddr, adminPort, err := net.SplitHostPort(endpoint)
	if err != nil {
		return fmt.Errorf("error splitting hostport: %v", err)
	}

	adminAddrIP, err := net.ResolveIPAddr("ip", adminAddr)
	if err != nil {
		return fmt.Errorf("error resolving ip address: %v", err)
	}

	t, err := troubleshoot.NewTroubleshoot(adminAddrIP, adminPort)
	if err != nil {
		return fmt.Errorf("error creating new troubleshoot: %v", err)
	}

	envoyIDs, upstreamIPs, err := t.GetUpstreams()
	if err != nil {
		return fmt.Errorf("error getting upstreams: %v", err)
	}

	c.UI.Output(fmt.Sprintf("Upstreams (explicit upstreams only) (%v)", len(envoyIDs)), terminal.WithHeaderStyle())
	for _, e := range envoyIDs {
		c.UI.Output(e)
	}

	c.UI.Output(fmt.Sprintf("Upstream IPs (transparent proxy only) (%v)", len(upstreamIPs)), terminal.WithHeaderStyle())
	table := terminal.NewTable("IPs ", "Virtual ", "Cluster Names")
	for _, u := range upstreamIPs {
		table.AddRow([]string{formatIPs(u.IPs), strconv.FormatBool(u.IsVirtual), formatClusterNames(u.ClusterNames)}, []string{})
	}
	c.UI.Table(table)

	c.UI.Output("\nIf you cannot find the upstream address or cluster for a transparent proxy upstream:", terminal.WithInfoStyle())
	c.UI.Output("-> Check intentions: Transparent proxy upstreams are configured based on intentions. Make sure you "+
		"have configured intentions to allow traffic to your upstream.", terminal.WithInfoStyle())
	c.UI.Output("-> To check that the right cluster is being dialed, run a DNS lookup "+
		"for the upstream you are dialing. For example, run `dig backend.svc.consul` to return the IP address for the `backend` service. If the address you get from that is missing "+
		"from the upstream IPs, it means that your proxy may be misconfigured.", terminal.WithInfoStyle())

	return nil
}

// AutocompleteFlags returns a mapping of supported flags and autocomplete
// options for this command. The map key for the Flags map should be the
// complete flag such as "-foo" or "--foo".
func (c *UpstreamsCommand) AutocompleteFlags() complete.Flags {
	return complete.Flags{
		fmt.Sprintf("-%s", flagNameNamespace):   complete.PredictNothing,
		fmt.Sprintf("-%s", flagNameKubeConfig):  complete.PredictFiles("*"),
		fmt.Sprintf("-%s", flagNameKubeContext): complete.PredictNothing,
	}
}

// AutocompleteArgs returns the argument predictor for this command.
// Since argument completion is not supported, this will return
// complete.PredictNothing.
func (c *UpstreamsCommand) AutocompleteArgs() complete.Predictor {
	return complete.PredictNothing
}

func (c *UpstreamsCommand) Synopsis() string {
	return synopsis
}

func (c *UpstreamsCommand) Help() string {
	return help
}

func formatIPs(ips []string) string {
	return strings.Join(ips, ", ")
}

func formatClusterNames(names map[string]struct{}) string {
	var out []string
	for k := range names {
		out = append(out, k)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}

const (
	synopsis = "Connect to a pod with a proxy and gather upstream services."
	help     = `
Usage: consul-k8s troubleshoot upstreams [options]

  Connect to a pod with a proxy and gather upstream services.

  Requires a pod.
  
  Examples:
    $ consul-k8s troubleshoot upstreams -pod pod1
	
	where 'pod1' is the pod running a consul proxy 
`
)
