package shared

import (
	"fmt"

	"github.com/hashicorp/consul-k8s/cli/common"
	"github.com/hashicorp/consul-k8s/cli/common/terminal"
	"github.com/hashicorp/consul-k8s/cli/helm"
	"helm.sh/helm/v3/pkg/action"
	helmCLI "helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/release"
)

// type helmConfigOptions struct {
// 	*common.BaseCommand
// }

// GetHelmRelease returns the [ Helm release object, releaseName, namespace, error ] for the Consul installation in the specified k8s namespace.
func GetHelmRelease(settings *helmCLI.EnvSettings, helmActionsRunner helm.HelmActionsRunner) (*release.Release, string, string, error) {

	// Setup logger to stream Helm library logs.
	var c common.BaseCommand
	var uiLogger = func(s string, args ...interface{}) {
		logMsg := fmt.Sprintf(s, args...)
		c.UI.Output(logMsg, terminal.WithLibraryStyle())
	}

	_, releaseName, namespace, err := helmActionsRunner.CheckForInstallations(&helm.CheckForInstallationsOptions{
		Settings:    settings,
		ReleaseName: common.DefaultReleaseName,
		DebugLog:    uiLogger,
	})
	if err != nil {
		return nil, "", "", fmt.Errorf("couldn't find the helm releases: %w", err)
	}

	statusConfig := new(action.Configuration)
	statusConfig, err = helm.InitActionConfig(statusConfig, namespace, settings, uiLogger)
	if err != nil {
		return nil, releaseName, namespace, fmt.Errorf("couldn't intialise helm go SDK action configuration: %s", err)
	}

	statuser := action.NewStatus(statusConfig)
	rel, err := helmActionsRunner.GetStatus(statuser, releaseName)
	if err != nil {
		return nil, releaseName, namespace, fmt.Errorf("couldn't get the helm release: %s", err)
	}
	return rel, releaseName, namespace, nil
}
