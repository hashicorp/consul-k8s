// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package helm

import (
	"embed"
	"strings"
	"time"

	"github.com/hashicorp/consul-k8s/cli/common"
	"github.com/hashicorp/consul-k8s/cli/common/terminal"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"helm.sh/helm/v3/pkg/action"
	helmCLI "helm.sh/helm/v3/pkg/cli"
)

// UpgradeOptions is used when calling UpgradeHelmRelease.
type UpgradeOptions struct {
	// ReleaseName is the name of the installed Helm release to upgrade.
	ReleaseName string
	// ReleaseType is the helm upgrade type - consul vs consul-demo.
	ReleaseType string
	// ReleaseTypeName is a user friendly version of ReleaseType.  The values
	// are consul and consul demo application.
	ReleaseTypeName string
	// Namespace is the Kubernetes namespace where the release is installed.
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
	// DryRun specifies whether the upgrade should actually modify the
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

// UpgradeHelmRelease handles downloading the embedded helm chart, loading the
// values, showing the diff between new and installed values, and runnning the
// Helm install command.
func UpgradeHelmRelease(options *UpgradeOptions) error {
	options.UI.Output("%s Upgrade Summary", cases.Title(language.English).String(options.ReleaseTypeName), terminal.WithHeaderStyle())

	chart, err := options.HelmActionsRunner.LoadChart(options.EmbeddedChart, options.ChartDirName)
	if err != nil {
		return err
	}
	options.UI.Output("Downloaded charts.", terminal.WithSuccessStyle())

	currentChartValues, err := FetchChartValues(options.HelmActionsRunner,
		options.Namespace, options.ReleaseName, options.Settings, options.UILogger)
	if err != nil {
		return err
	}

	// Print out the upgrade summary.
	if err = printDiff(currentChartValues, options.Values, options.UI); err != nil {
		options.UI.Output("Could not print the different between current and upgraded charts: %v", err, terminal.WithErrorStyle())
		return err
	}

	// Check if the user is OK with the upgrade unless the auto approve or dry run flags are true.
	if !options.AutoApprove && !options.DryRun {
		confirmation, err := options.UI.Input(&terminal.Input{
			Prompt: "Proceed with upgrade? (Y/n)",
			Style:  terminal.InfoStyle,
			Secret: false,
		})

		if err != nil {
			return err
		}
		// The upgrade will proceed if the user presses enter or responds with "y"/"yes" (case-insensitive).
		if confirmation != "" && common.Abort(confirmation) {
			options.UI.Output("Upgrade aborted. Use the command `consul-k8s upgrade -help` to learn how to customize your upgrade.",
				terminal.WithInfoStyle())
			return err
		}
	}

	if !options.DryRun {
		options.UI.Output("Upgrading %s", options.ReleaseTypeName, terminal.WithHeaderStyle())
	} else {
		options.UI.Output("Performing Dry Run Upgrade", terminal.WithHeaderStyle())
		return nil
	}

	// Setup action configuration for Helm Go SDK function calls.
	actionConfig := new(action.Configuration)
	actionConfig, err = InitActionConfig(actionConfig, options.Namespace, options.Settings, options.UILogger)
	if err != nil {
		return err
	}

	// Setup the upgrade action.
	upgrade := action.NewUpgrade(actionConfig)
	upgrade.Namespace = options.Namespace
	upgrade.DryRun = options.DryRun
	upgrade.Wait = options.Wait
	upgrade.Timeout = options.Timeout

	// Run the upgrade. Note that the dry run config is passed into the upgrade action, so upgrade.Run is called even during a dry run.
	_, err = options.HelmActionsRunner.Upgrade(upgrade, options.ReleaseName, chart, options.Values)
	if err != nil {
		return err
	}
	options.UI.Output("%s upgraded in namespace %q.", cases.Title(language.English).String(options.ReleaseTypeName), options.Namespace, terminal.WithSuccessStyle())
	return nil
}

// printDiff marshals both maps to YAML and prints the diff between the two.
func printDiff(old, new map[string]interface{}, ui terminal.UI) error {
	diff, err := common.Diff(old, new)
	if err != nil {
		return err
	}

	ui.Output("\nDifference between user overrides for current and upgraded charts"+
		"\n-----------------------------------------------------------------", terminal.WithInfoStyle())
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "+") {
			ui.Output(line, terminal.WithDiffAddedStyle())
		} else if strings.HasPrefix(line, "-") {
			ui.Output(line, terminal.WithDiffRemovedStyle())
		} else {
			ui.Output(line, terminal.WithDiffUnchangedStyle())
		}
	}

	return nil
}
