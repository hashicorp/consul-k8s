package envoy_stats

import (
	"errors"
	"fmt"
	"github.com/hashicorp/consul-k8s/cli/common"
	"github.com/hashicorp/consul-k8s/cli/common/flag"
	"github.com/hashicorp/consul-k8s/cli/common/terminal"
	"github.com/hashicorp/consul-k8s/cli/helm"
	helmCLI "helm.sh/helm/v3/pkg/cli"
	"io"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"net/http"
	"strconv"
	"sync"
)

const envoyAdminPort = 19000

type Command struct {
	*common.BaseCommand

	helmActionsRunner helm.HelmActionsRunner

	kubernetes kubernetes.Interface

	restConfig *rest.Config

	set *flag.Sets

	flagKubeConfig  string
	flagKubeContext string
	flagNamespace   string
	flagPod         string

	once sync.Once
	help string
}

func (c *Command) init() {
	c.set = flag.NewSets()

	f := c.set.NewSet("Global Options")
	f.StringVar(&flag.StringVar{
		Name:    "kubeconfig",
		Aliases: []string{"c"},
		Target:  &c.flagKubeConfig,
		Default: "",
		Usage:   "Path to kubeconfig file.",
	})
	f.StringVar(&flag.StringVar{
		Name:    "context",
		Target:  &c.flagKubeContext,
		Default: "",
		Usage:   "Kubernetes context to use.",
	})
	f.StringVar(&flag.StringVar{
		Name:    "namespace",
		Target:  &c.flagNamespace,
		Default: "",
		Usage:   "Namespace of pod",
	})
	f.StringVar(&flag.StringVar{
		Name:    "pod",
		Target:  &c.flagPod,
		Default: "",
		Usage:   "Pod for which envoy stats need to be captured",
	})

	c.help = c.set.Help()
}

// validateFlags checks the command line flags and values for errors.
func (c *Command) validateFlags() error {
	if len(c.set.Args()) > 0 {
		return errors.New("should have no non-flag arguments")
	}
	return nil
}

func (c *Command) Run(args []string) int {
	c.once.Do(c.init)

	if err := c.set.Parse(args); err != nil {
		c.UI.Output(err.Error())
		return 1
	}

	if err := c.validateFlags(); err != nil {
		c.UI.Output(err.Error())
		return 1
	}

	if c.flagPod == "" || c.flagNamespace == "" {
		c.UI.Output("namespace and pod name are required")
		return 1
	}

	// helmCLI.New() will create a settings object which is used by the Helm Go SDK calls.
	settings := helmCLI.New()
	if c.flagKubeConfig != "" {
		settings.KubeConfig = c.flagKubeConfig
	}
	if c.flagKubeContext != "" {
		settings.KubeContext = c.flagKubeContext
	}

	if err := c.setupKubeClient(settings); err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}

	if c.restConfig == nil {
		var err error
		if c.restConfig, err = settings.RESTClientGetter().ToRESTConfig(); err != nil {
			c.UI.Output("error setting rest config")
			return 1
		}
	}

	pf := common.PortForward{
		Namespace:  c.flagNamespace,
		PodName:    c.flagPod,
		RemotePort: envoyAdminPort,
		KubeClient: c.kubernetes,
		RestConfig: c.restConfig,
	}

	_, err := pf.Open(c.Ctx)
	if err != nil {
		c.UI.Output("error port forwarding %s", err)
		return 1
	}
	defer pf.Close()

	resp, err := http.Get(fmt.Sprintf("http://localhost:%s/stats", strconv.Itoa(pf.GetLocalPort())))
	if err != nil {
		c.UI.Output("error hitting stats endpoint of envoy %s", err)
		return 1
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		c.UI.Output("error reading body of http response %s", err)
		return 1
	}

	c.UI.Output(string(bodyBytes))
	defer resp.Body.Close()
	return 0
}

// setupKubeClient to use for non Helm SDK calls to the Kubernetes API The Helm SDK will use
// settings.RESTClientGetter for its calls as well, so this will use a consistent method to
// target the right cluster for both Helm SDK and non Helm SDK calls.
func (c *Command) setupKubeClient(settings *helmCLI.EnvSettings) error {
	if c.kubernetes == nil {
		restConfig, err := settings.RESTClientGetter().ToRESTConfig()
		if err != nil {
			c.UI.Output("Error retrieving Kubernetes authentication: %v", err, terminal.WithErrorStyle())
			return err
		}
		c.kubernetes, err = kubernetes.NewForConfig(restConfig)
		if err != nil {
			c.UI.Output("Error initializing Kubernetes client: %v", err, terminal.WithErrorStyle())
			return err
		}
	}

	return nil
}

// Help returns a description of the command and how it is used.
func (c *Command) Help() string {
	c.once.Do(c.init)
	return c.Synopsis() + "\n\nUsage: consul-k8s status [flags]\n\n" + c.help
}

// Synopsis returns a one-line command summary.
func (c *Command) Synopsis() string {
	return "Check the status of a Consul installation on Kubernetes."
}
