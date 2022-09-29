package status

import (
	"errors"
	"fmt"
	"strconv"
	"sync"

	"github.com/posener/complete"
	"helm.sh/helm/v3/pkg/release"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hashicorp/consul-k8s/cli/common"
	"github.com/hashicorp/consul-k8s/cli/common/flag"
	"github.com/hashicorp/consul-k8s/cli/common/terminal"
	"github.com/hashicorp/consul-k8s/cli/helm"
	"helm.sh/helm/v3/pkg/action"
	helmCLI "helm.sh/helm/v3/pkg/cli"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"
)

const (
	flagNameKubeConfig  = "kubeconfig"
	flagNameKubeContext = "context"
)

type Command struct {
	*common.BaseCommand

	kubernetes kubernetes.Interface

	set *flag.Sets

	flagKubeConfig  string
	flagKubeContext string

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

	c.help = c.set.Help()
}

// Run checks the status of a Consul installation on Kubernetes.
func (c *Command) Run(args []string) int {
	c.once.Do(c.init)

	// The logger is initialized in main with the name cli. Here, we reset the name to status so log lines would be prefixed with status.
	c.Log.ResetNamed("status")

	defer common.CloseWithError(c.BaseCommand)

	if err := c.set.Parse(args); err != nil {
		c.UI.Output(err.Error())
		return 1
	}

	if err := c.validateFlags(); err != nil {
		c.UI.Output(err.Error())
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

	// Setup logger to stream Helm library logs.
	var uiLogger = func(s string, args ...interface{}) {
		logMsg := fmt.Sprintf(s, args...)
		c.UI.Output(logMsg, terminal.WithLibraryStyle())
	}

	c.UI.Output("Consul Status Summary", terminal.WithHeaderStyle())

	releaseName, namespace, err := common.CheckForInstallations(settings, uiLogger)
	if err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}

	if err := c.checkHelmInstallation(settings, uiLogger, releaseName, namespace); err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}

	if s, err := c.checkConsulServers(namespace); err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	} else {
		c.UI.Output(s, terminal.WithSuccessStyle())
	}

	if s, err := c.checkConsulClients(namespace); err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	} else {
		c.UI.Output(s, terminal.WithSuccessStyle())
	}

	return 0
}

// validateFlags checks the command line flags and values for errors.
func (c *Command) validateFlags() error {
	if len(c.set.Args()) > 0 {
		return errors.New("should have no non-flag arguments")
	}
	return nil
}

// AutocompleteFlags returns a mapping of supported flags and autocomplete
// options for this command. The map key for the Flags map should be the
// complete flag such as "-foo" or "--foo".
func (c *Command) AutocompleteFlags() complete.Flags {
	return complete.Flags{
		fmt.Sprintf("-%s", flagNameKubeConfig):  complete.PredictFiles("*"),
		fmt.Sprintf("-%s", flagNameKubeContext): complete.PredictNothing,
	}
}

// AutocompleteArgs returns the argument predictor for this command.
// Since argument completion is not supported, this will return
// complete.PredictNothing.
func (c *Command) AutocompleteArgs() complete.Predictor {
	return complete.PredictNothing
}

// checkHelmInstallation uses the helm Go SDK to depict the status of a named release. This function then prints
// the version of the release, it's status (unknown, deployed, uninstalled, ...), and the overwritten values.
func (c *Command) checkHelmInstallation(settings *helmCLI.EnvSettings, uiLogger action.DebugLog, releaseName, namespace string) error {
	// Need a specific action config to call helm status, where namespace comes from the previous call to list.
	statusConfig := new(action.Configuration)
	statusConfig, err := helm.InitActionConfig(statusConfig, namespace, settings, uiLogger)
	if err != nil {
		return err
	}

	statuser := action.NewStatus(statusConfig)
	rel, err := statuser.Run(releaseName)
	if err != nil {
		return fmt.Errorf("couldn't check for installations: %s", err)
	}

	timezone, _ := rel.Info.LastDeployed.Zone()

	tbl := terminal.NewTable("Name", "Namespace", "Status", "Chart Version", "AppVersion", "Revision", "Last Updated")
	tbl.AddRow([]string{releaseName, namespace, string(rel.Info.Status), rel.Chart.Metadata.Version,
		rel.Chart.Metadata.AppVersion, strconv.Itoa(rel.Version),
		rel.Info.LastDeployed.Format("2006/01/02 15:04:05") + " " + timezone}, []string{})
	c.UI.Table(tbl)

	valuesYaml, err := yaml.Marshal(rel.Config)
	c.UI.Output("Config:", terminal.WithHeaderStyle())
	if err != nil {
		c.UI.Output("%+v", err, terminal.WithInfoStyle())
	} else if len(rel.Config) == 0 {
		c.UI.Output(string(valuesYaml), terminal.WithInfoStyle())
	} else {
		c.UI.Output(string(valuesYaml), terminal.WithInfoStyle())
	}

	// Check the status of the hooks.
	if len(rel.Hooks) > 1 {
		c.UI.Output("Status Of Helm Hooks:", terminal.WithHeaderStyle())

		for _, hook := range rel.Hooks {
			// Remember that we only report the status of pre-install or pre-upgrade hooks.
			if validEvent(hook.Events) {
				c.UI.Output("%s %s: %s", hook.Name, hook.Kind, hook.LastRun.Phase.String())
			}
		}
		fmt.Println("")
	}

	return nil
}

// validEvent is a helper function that checks if the given hook's events are pre-install or pre-upgrade.
// Only pre-install and pre-upgrade hooks are expected to have run when using the status command against
// a running installation.
func validEvent(events []release.HookEvent) bool {
	for _, event := range events {
		if event.String() == "pre-install" || event.String() == "pre-upgrade" {
			return true
		}
	}
	return false
}

// checkConsulServers uses the Kubernetes list function to report if the consul servers are healthy.
func (c *Command) checkConsulServers(namespace string) (string, error) {
	servers, err := c.kubernetes.AppsV1().StatefulSets(namespace).List(c.Ctx,
		metav1.ListOptions{LabelSelector: "app=consul,chart=consul-helm,component=server"})
	if err != nil {
		return "", err
	} else if len(servers.Items) == 0 {
		return "", errors.New("no server stateful set found")
	} else if len(servers.Items) > 1 {
		return "", errors.New("found multiple server stateful sets")
	}

	desiredReplicas := int(*servers.Items[0].Spec.Replicas)
	readyReplicas := int(servers.Items[0].Status.ReadyReplicas)
	if readyReplicas < desiredReplicas {
		return "", fmt.Errorf("%d/%d Consul servers unhealthy", desiredReplicas-readyReplicas, desiredReplicas)
	}
	return fmt.Sprintf("Consul servers healthy (%d/%d)", readyReplicas, desiredReplicas), nil
}

// checkConsulClients uses the Kubernetes list function to report if the consul clients are healthy.
func (c *Command) checkConsulClients(namespace string) (string, error) {
	clients, err := c.kubernetes.AppsV1().DaemonSets(namespace).List(c.Ctx,
		metav1.ListOptions{LabelSelector: "app=consul,chart=consul-helm"})
	if err != nil {
		return "", err
	} else if len(clients.Items) == 0 {
		return "", errors.New("no client daemon set found")
	} else if len(clients.Items) > 1 {
		return "", errors.New("found multiple client daemon sets")
	}
	desiredReplicas := int(clients.Items[0].Status.DesiredNumberScheduled)
	readyReplicas := int(clients.Items[0].Status.NumberReady)
	if readyReplicas < desiredReplicas {
		return "", fmt.Errorf("%d/%d Consul clients unhealthy", desiredReplicas-readyReplicas, desiredReplicas)
	}
	return fmt.Sprintf("Consul clients healthy (%d/%d)", readyReplicas, desiredReplicas), nil
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
