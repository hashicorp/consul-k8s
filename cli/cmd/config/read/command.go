// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package read

import (
	"errors"
	"fmt"
	"sync"

	"github.com/posener/complete"

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

// ReadCommand represents the command to read the helm config of a Consul installation on Kubernetes.
type ReadCommand struct {
	*common.BaseCommand

	helmActionsRunner helm.HelmActionsRunner

	kubernetes kubernetes.Interface

	set *flag.Sets

	flagKubeConfig  string
	flagKubeContext string

	once sync.Once
	help string
}

func (c *ReadCommand) init() {
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
func (c *ReadCommand) Run(args []string) int {
	c.once.Do(c.init)
	if c.helmActionsRunner == nil {
		c.helmActionsRunner = &helm.ActionRunner{}
	}

	c.Log.ResetNamed("config read")
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

	return 0
}

// validateFlags checks the command line flags and values for errors.
func (c *ReadCommand) validateFlags() error {
	if len(c.set.Args()) > 0 {
		return errors.New("should have no non-flag arguments")
	}
	return nil
}

// AutocompleteFlags returns a mapping of supported flags and autocomplete
// options for this command. The map key for the Flags map should be the
// complete flag such as "-foo" or "--foo".
func (c *ReadCommand) AutocompleteFlags() complete.Flags {
	return complete.Flags{
		fmt.Sprintf("-%s", flagNameKubeConfig):  complete.PredictFiles("*"),
		fmt.Sprintf("-%s", flagNameKubeContext): complete.PredictNothing,
	}
}

// AutocompleteArgs returns the argument predictor for this command.
// Since argument completion is not supported, this will return
// complete.PredictNothing.
func (c *ReadCommand) AutocompleteArgs() complete.Predictor {
	return complete.PredictNothing
}

// checkHelmInstallation uses the helm Go SDK to depict the status of a named release. This function then prints
// the version of the release, it's status (unknown, deployed, uninstalled, ...), and the overwritten values.
func (c *ReadCommand) checkHelmInstallation(settings *helmCLI.EnvSettings, uiLogger action.DebugLog, releaseName, namespace string) error {
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

	valuesYaml, err := yaml.Marshal(rel.Config)
	if err != nil {
		return err
	}
	c.UI.Output(string(valuesYaml))

	return nil
}

// setupKubeClient to use for non Helm SDK calls to the Kubernetes API The Helm SDK will use
// settings.RESTClientGetter for its calls as well, so this will use a consistent method to
// target the right cluster for both Helm SDK and non Helm SDK calls.
func (c *ReadCommand) setupKubeClient(settings *helmCLI.EnvSettings) error {
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
func (c *ReadCommand) Help() string {
	c.once.Do(c.init)
	return c.Synopsis() + "\n\nUsage: consul-k8s config read [flags]\n\n" + c.help
}

// Synopsis returns a one-line command summary.
func (c *ReadCommand) Synopsis() string {
	return "Returns the helm config of a Consul installation on Kubernetes."
}
