package helm

import (
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/release"
)

type MockActionRunner struct {
	CheckForInstallationsReponse func(options *CheckForInstallationsOptions) (bool, string, string, error)
}

func (m *MockActionRunner) Install(install *action.Install, chrt *chart.Chart, vals map[string]interface{}) (*release.Release, error) {
	return &release.Release{}, nil
}

func (m *MockActionRunner) Uninstall(uninstall *action.Uninstall, name string) (*release.UninstallReleaseResponse, error) {
	return &release.UninstallReleaseResponse{}, nil
}

func (h *MockActionRunner) CheckForInstallations(options *CheckForInstallationsOptions) (bool, string, string, error) {
	if h.CheckForInstallationsReponse == nil {
		return false, "", "", nil
	}
	return h.CheckForInstallationsReponse(options)
}
