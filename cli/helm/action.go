// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package helm

import (
	"embed"
	"fmt"
	"os"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	helmCLI "helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/release"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

// InitActionConfig initializes a Helm Go SDK action configuration. This
// function currently uses a hack to override the namespace field that gets set
// in the K8s client set up by the SDK.
func InitActionConfig(actionConfig *action.Configuration, namespace string, settings *helmCLI.EnvSettings, logger action.DebugLog) (*action.Configuration, error) {
	getter := settings.RESTClientGetter()
	configFlags := getter.(*genericclioptions.ConfigFlags)
	configFlags.Namespace = &namespace
	err := actionConfig.Init(settings.RESTClientGetter(), namespace,
		os.Getenv("HELM_DRIVER"), logger)
	if err != nil {
		return nil, fmt.Errorf("error setting up helm action configuration to find existing installations: %s", err)
	}
	return actionConfig, nil
}

// HelmActionsRunner is a thin interface over existing Helm actions that normally
// require a Kubernetes cluster.  This interface allows us to mock it in tests
// and get better coverage of CLI commands.
type HelmActionsRunner interface {
	// A thin wrapper around the Helm list function.
	CheckForInstallations(options *CheckForInstallationsOptions) (bool, string, string, error)
	// A thin wrapper around the Helm status function.
	GetStatus(status *action.Status, name string) (*release.Release, error)
	// A thin wrapper around the Helm install function.
	Install(install *action.Install, chrt *chart.Chart, vals map[string]interface{}) (*release.Release, error)
	// A thin wrapper around the LoadChart function in consul-k8s CLI that reads the charts withing the embedded fle system.
	LoadChart(chart embed.FS, chartDirName string) (*chart.Chart, error)
	// A thin wrapper around the Helm uninstall function.
	Uninstall(uninstall *action.Uninstall, name string) (*release.UninstallReleaseResponse, error)
	// A thin wrapper around the Helm upgrade function.
	Upgrade(upgrade *action.Upgrade, name string, chart *chart.Chart, vals map[string]interface{}) (*release.Release, error)
}

// ActionRunner is the implementation of HelmActionsRunner interface that
// truly calls Helm sdk functions and requires a real Kubernetes cluster. It
// is the non-mock implementation of HelmActionsRunner that is used in the CLI.
type ActionRunner struct{}

func (h *ActionRunner) Uninstall(uninstall *action.Uninstall, name string) (*release.UninstallReleaseResponse, error) {
	return uninstall.Run(name)
}

func (h *ActionRunner) Install(install *action.Install, chrt *chart.Chart, vals map[string]interface{}) (*release.Release, error) {
	return install.Run(chrt, vals)
}

type CheckForInstallationsOptions struct {
	Settings              *helmCLI.EnvSettings
	ReleaseName           string
	DebugLog              action.DebugLog
	SkipErrorWhenNotFound bool
}

// CheckForInstallations uses the helm Go SDK to find helm releases in all namespaces where the chart name is
// "consul", and returns the release name and namespace if found, or an error if not found.
func (h *ActionRunner) CheckForInstallations(options *CheckForInstallationsOptions) (bool, string, string, error) {
	// Need a specific action config to call helm list, where namespace is NOT specified.
	listConfig := new(action.Configuration)
	if err := listConfig.Init(options.Settings.RESTClientGetter(), "",
		os.Getenv("HELM_DRIVER"), options.DebugLog); err != nil {
		return false, "", "", fmt.Errorf("couldn't initialize helm config: %s", err)
	}

	lister := action.NewList(listConfig)
	lister.AllNamespaces = true
	lister.StateMask = action.ListAll
	res, err := lister.Run()
	if err != nil {
		return false, "", "", fmt.Errorf("couldn't check for installations: %s", err)
	}

	for _, rel := range res {
		if rel.Chart.Metadata.Name == options.ReleaseName {
			return true, rel.Name, rel.Namespace, nil
		}
	}
	var notFoundError error
	if !options.SkipErrorWhenNotFound {
		notFoundError = fmt.Errorf("couldn't find installation named '%s'", options.ReleaseName)
	}
	return false, "", "", notFoundError
}

func (h *ActionRunner) GetStatus(status *action.Status, name string) (*release.Release, error) {
	return status.Run(name)
}

func (h *ActionRunner) Upgrade(upgrade *action.Upgrade, name string, chart *chart.Chart, vals map[string]interface{}) (*release.Release, error) {
	return upgrade.Run(name, chart, vals)
}

func (h *ActionRunner) LoadChart(chart embed.FS, chartDirName string) (*chart.Chart, error) {
	return LoadChart(chart, chartDirName)
}
