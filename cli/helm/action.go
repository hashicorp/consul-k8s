package helm

import (
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

type HelmActionsRunner interface {
	Install(install *action.Install, chrt *chart.Chart, vals map[string]interface{}) (*release.Release, error)
	Uninstall(uninstall *action.Uninstall, name string) (*release.UninstallReleaseResponse, error)
	CheckForInstallations(options *CheckForInstallationsOptions) (bool, string, string, error)
}

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
