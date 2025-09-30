// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package status

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/posener/complete"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/release"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hashicorp/consul-k8s/cli/common"
	"github.com/hashicorp/consul-k8s/cli/common/flag"
	"github.com/hashicorp/consul-k8s/cli/common/terminal"
	"github.com/hashicorp/consul-k8s/cli/helm"
	helmCLI "helm.sh/helm/v3/pkg/cli"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"
)

var tableHeaderForConsulComponents = []string{"NAME", "READY", "AGE", "CONTAINERS", "IMAGES"}

const (
	flagNameKubeConfig  = "kubeconfig"
	flagNameKubeContext = "context"
)

type Command struct {
	*common.BaseCommand

	helmActionsRunner helm.HelmActionsRunner

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
	if c.helmActionsRunner == nil {
		c.helmActionsRunner = &helm.ActionRunner{}
	}

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

	_, releaseName, namespace, err := c.helmActionsRunner.CheckForInstallations(&helm.CheckForInstallationsOptions{
		Settings:    settings,
		ReleaseName: common.DefaultReleaseName,
		DebugLog:    uiLogger,
	})
	if err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}

	if err := c.checkHelmInstallation(settings, uiLogger, releaseName, namespace); err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}

	err = c.checkConsulComponentsStatus(namespace)
	if err != nil {
		return 1
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
	rel, err := c.helmActionsRunner.GetStatus(statuser, releaseName)
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

// checkConsulComponentsStatus fetch and prints the status of different consul components
// like Consul Clients, Consul Servers, and Consul Deployments, in the given namespace of the cluster.
func (c *Command) checkConsulComponentsStatus(namespace string) error {
	var err error
	var tbl *terminal.Table
	tbl, err = c.getConsulClientsTable(namespace)
	c.printComponentStatus(tbl, err, "Consul Clients")
	tbl, err = c.getConsulServersTable(namespace)
	c.printComponentStatus(tbl, err, "Consul Servers")
	tbl, err = c.getConsulDeploymentsTable(namespace)
	c.printComponentStatus(tbl, err, "Consul Deployments")
	return err
}

// printComponentStatus prints the status of a given component (Consul Clients, Consul Servers, or Consul Deployments).
func (c *Command) printComponentStatus(tbl *terminal.Table, err error, component string) {
	c.UI.Output(fmt.Sprintf("%s status: ", component), terminal.WithHeaderStyle())
	if err != nil {
		c.UI.Output("unable to list %s: %s", component, err, terminal.WithErrorStyle())
	}
	if tbl != nil {
		c.UI.Table(tbl)
	} else {
		c.UI.Output(fmt.Sprintf("No %s found in Kubernetes cluster.", component))
	}
}

// getConsulClientsTable returns the table instance with the Consul Clients
// and their ready status (number of pods ready/desired)
func (c *Command) getConsulClientsTable(namespace string) (*terminal.Table, error) {
	clients, err := c.kubernetes.AppsV1().DaemonSets(namespace).List(c.Ctx, metav1.ListOptions{LabelSelector: "app=consul,chart=consul-helm,component=client"})
	if err != nil {
		return nil, err
	}
	var tbl *terminal.Table
	if len(clients.Items) != 0 {
		tbl = terminal.NewTable(tableHeaderForConsulComponents...)
		for _, c := range clients.Items {
			age := time.Since(c.CreationTimestamp.Time).Round(time.Minute)
			readyStatus := fmt.Sprintf("%d/%d", c.Status.NumberReady, c.Status.DesiredNumberScheduled)

			var containers, images []string
			for _, container := range c.Spec.Template.Spec.Containers {
				containers = append(containers, fmt.Sprintf("%s", container.Name))
				images = append(images, fmt.Sprintf("%s", container.Image))
			}
			imagesString := strings.Join(images, ", ")
			containersString := strings.Join(containers, ", ")
			if c.Status.NumberReady != c.Status.DesiredNumberScheduled {
				colourCode := []string{terminal.Red, terminal.Red, "", "", ""}
				tbl.AddRow([]string{c.Name, readyStatus, age.String(), containersString, imagesString}, colourCode)
			} else {
				tbl.AddRow([]string{c.Name, readyStatus, age.String(), containersString, imagesString}, []string{})
			}
		}
	}
	return tbl, nil
}

// getConsulServersTable returns the table instance with the Consul Servers
// and their ready status (number of pods ready/desired),
func (c *Command) getConsulServersTable(namespace string) (*terminal.Table, error) {
	servers, err := c.kubernetes.AppsV1().StatefulSets(namespace).List(c.Ctx, metav1.ListOptions{LabelSelector: "app=consul,chart=consul-helm,component=server"})
	if err != nil {
		return nil, err
	}
	var tbl *terminal.Table
	if len(servers.Items) != 0 {
		tbl = terminal.NewTable(tableHeaderForConsulComponents...)
		for _, s := range servers.Items {
			age := time.Since(s.CreationTimestamp.Time).Round(time.Minute)
			readyStatus := fmt.Sprintf("%d/%d", s.Status.ReadyReplicas, *s.Spec.Replicas)

			var containers, images []string
			for _, container := range s.Spec.Template.Spec.Containers {
				containers = append(containers, fmt.Sprintf("%s", container.Name))
				images = append(images, fmt.Sprintf("%s", container.Image))
			}
			imagesString := strings.Join(images, ", ")
			containersString := strings.Join(containers, ", ")
			if s.Status.ReadyReplicas != *s.Spec.Replicas {
				colourCode := []string{terminal.Red, terminal.Red, "", "", ""}
				tbl.AddRow([]string{s.Name, readyStatus, age.String(), containersString, imagesString}, colourCode)
			} else {
				tbl.AddRow([]string{s.Name, readyStatus, age.String(), containersString, imagesString}, []string{})
			}
		}
	}
	return tbl, nil
}

// getConsulDeploymentsTable returns the table instance with the Consul Deployed Deployments
// and their ready status (number of pods ready/desired),
func (c *Command) getConsulDeploymentsTable(namespace string) (*terminal.Table, error) {
	deployments, err := c.kubernetes.AppsV1().Deployments(namespace).List(c.Ctx, metav1.ListOptions{LabelSelector: "app=consul,chart=consul-helm"})
	if err != nil {
		return nil, err
	}
	var tbl *terminal.Table
	if len(deployments.Items) != 0 {
		tbl = terminal.NewTable(tableHeaderForConsulComponents...)
		for _, d := range deployments.Items {
			age := time.Since(d.CreationTimestamp.Time).Round(time.Minute)
			readyStatus := fmt.Sprintf("%d/%d", d.Status.ReadyReplicas, *d.Spec.Replicas)

			var containers, images []string
			for _, container := range d.Spec.Template.Spec.Containers {
				containers = append(containers, fmt.Sprintf("%s", container.Name))
				images = append(images, fmt.Sprintf("%s", container.Image))
			}
			imagesString := strings.Join(images, ", ")
			containersString := strings.Join(containers, ", ")
			if d.Status.ReadyReplicas != *d.Spec.Replicas {
				colourCode := []string{terminal.Red, terminal.Red, "", "", ""}
				tbl.AddRow([]string{d.Name, readyStatus, age.String(), containersString, imagesString}, colourCode)
			} else {
				tbl.AddRow([]string{d.Name, readyStatus, age.String(), containersString, imagesString}, []string{})
			}
		}
	}
	return tbl, nil
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
