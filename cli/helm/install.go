package helm

import (
	"embed"
	"fmt"
	"time"

	"github.com/hashicorp/consul-k8s/cli/common"
	"github.com/hashicorp/consul-k8s/cli/common/terminal"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"helm.sh/helm/v3/pkg/action"
	helmCLI "helm.sh/helm/v3/pkg/cli"
)

// InstallOptions is used when calling InstallHelmRelease.
type InstallOptions struct {
	// ReleaseName is the name of the Helm release to be installed.
	ReleaseName string
	// ReleaseType is the helm upgrade type - consul vs consul-demo.
	ReleaseType string
	// Namespace is the Kubernetes namespace where the release is to be
	// installed.
	Namespace string
	// Values the Helm chart values in a map form.
	Values map[string]interface{}
	// Settings is the Helm CLI environment settings.
	Settings *helmCLI.EnvSettings
	// Embedded chart specifies the Consul or Consul Demo Helm chart that has
	// been embedded into the consul-k8s CLI.
	EmbeddedChart embed.FS
	// ChartDirName is the top level directory name fo the EmbeddedChart.
	ChartDirName string
	// UILogger is a DebugLog used to return messages from Helm to the UI.
	UILogger action.DebugLog
	// DryRun specifies whether the install/upgrade should actually modify the
	// Kubernetes cluster.
	DryRun bool
	// AutoApprove will bypass any terminal prompts with an automatic yes.
	AutoApprove bool
	// Wait specifies whether the Helm install should wait until all pods
	// are ready.
	Wait bool
	// Timeout is the duration that Helm will wait for the command to complete
	// before it throws an error.
	Timeout time.Duration
	// UI is the terminal output representation that is used to prompt the user
	// and output messages.
	UI terminal.UI
	// HelmActionsRunner is a thin interface around Helm actions for install,
	// upgrade, and uninstall.
	HelmActionsRunner HelmActionsRunner
}

// InstallDemoApp will perform the following actions
// - Print out the installation summary.
// - Setup action configuration for Helm Go SDK function calls.
// - Setup the installation action.
// - Load the Helm chart.
// - Run the install.
func InstallDemoApp(options *InstallOptions) error {
	options.UI.Output(fmt.Sprintf("%s Installation Summary",
		cases.Title(language.English).String(common.ReleaseTypeConsulDemo)),
		terminal.WithHeaderStyle())
	options.UI.Output("Name: %s", common.ConsulDemoAppReleaseName, terminal.WithInfoStyle())
	options.UI.Output("Namespace: %s", options.Settings.Namespace(), terminal.WithInfoStyle())
	options.UI.Output("\n", terminal.WithInfoStyle())

	err := InstallHelmRelease(options)
	if err != nil {
		return err
	}

	options.UI.Output("Accessing %s UI", cases.Title(language.English).String(common.ReleaseTypeConsulDemo), terminal.WithHeaderStyle())
	port := "8080"
	portForwardCmd := fmt.Sprintf("kubectl port-forward service/nginx %s:80", port)
	if options.Settings.Namespace() != "default" {
		portForwardCmd += fmt.Sprintf(" --namespace %s", options.Settings.Namespace())
	}
	options.UI.Output(portForwardCmd, terminal.WithInfoStyle())
	options.UI.Output("Browse to http://localhost:%s.", port, terminal.WithInfoStyle())
	return nil
}

// InstallHelmRelease handles downloading the embedded helm chart, loading the
// values and runnning the Helm install command.
func InstallHelmRelease(options *InstallOptions) error {
	if options.DryRun {
		return nil
	}

	if !options.AutoApprove {
		confirmation, err := options.UI.Input(&terminal.Input{
			Prompt: "Proceed with installation? (y/N)",
			Style:  terminal.InfoStyle,
			Secret: false,
		})

		if err != nil {
			return err
		}
		if common.Abort(confirmation) {
			options.UI.Output("Install aborted. Use the command `consul-k8s install -help` to learn how to customize your installation.",
				terminal.WithInfoStyle())
			return err
		}
	}

	options.UI.Output("Installing %s", options.ReleaseType, terminal.WithHeaderStyle())

	// Setup action configuration for Helm Go SDK function calls.
	actionConfig := new(action.Configuration)
	actionConfig, err := InitActionConfig(actionConfig, options.Namespace, options.Settings, options.UILogger)
	if err != nil {
		return err
	}

	// Setup the installation action.
	install := action.NewInstall(actionConfig)
	install.ReleaseName = options.ReleaseName
	install.Namespace = options.Namespace
	install.CreateNamespace = true
	install.Wait = options.Wait
	install.Timeout = options.Timeout

	// Load the Helm chart.
	chart, err := options.HelmActionsRunner.LoadChart(options.EmbeddedChart, options.ChartDirName)
	if err != nil {
		return err
	}
	options.UI.Output("Downloaded charts.", terminal.WithSuccessStyle())

	// Run the install.
	if _, err = options.HelmActionsRunner.Install(install, chart, options.Values); err != nil {
		return err
	}

	options.UI.Output("%s installed in namespace %q.", options.ReleaseType, options.Namespace, terminal.WithSuccessStyle())
	return nil
}
