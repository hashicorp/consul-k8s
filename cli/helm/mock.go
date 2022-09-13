package helm

import (
	"embed"

	"github.com/hashicorp/consul-k8s/cli/common"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/release"
)

type MockActionRunner struct {
	CheckForInstallationsFunc         func(options *CheckForInstallationsOptions) (bool, string, string, error)
	GetStatusFunc                     func(status *action.Status, name string) (*release.Release, error)
	InstallFunc                       func(install *action.Install, chrt *chart.Chart, vals map[string]interface{}) (*release.Release, error)
	LoadChartFunc                     func(chrt embed.FS, chartDirName string) (*chart.Chart, error)
	UninstallFunc                     func(uninstall *action.Uninstall, name string) (*release.UninstallReleaseResponse, error)
	UpgradeFunc                       func(upgrade *action.Upgrade, name string, chart *chart.Chart, vals map[string]interface{}) (*release.Release, error)
	CheckedForConsulInstallations     bool
	CheckedForConsulDemoInstallations bool
	GotStatusConsulRelease            bool
	GotStatusConsulDemoRelease        bool
	ConsulInstalled                   bool
	ConsulUninstalled                 bool
	ConsulUpgraded                    bool
	ConsulDemoInstalled               bool
	ConsulDemoUninstalled             bool
	ConsulDemoUpgraded                bool
}

func (m *MockActionRunner) Install(install *action.Install, chrt *chart.Chart, vals map[string]interface{}) (*release.Release, error) {
	var installFunc func(install *action.Install, chrt *chart.Chart, vals map[string]interface{}) (*release.Release, error)
	if m.InstallFunc == nil {
		installFunc = func(install *action.Install, chrt *chart.Chart, vals map[string]interface{}) (*release.Release, error) {
			return &release.Release{}, nil
		}
	} else {
		installFunc = m.InstallFunc
	}

	release, err := installFunc(install, chrt, vals)
	if err == nil {
		if install.ReleaseName == common.DefaultReleaseName {
			m.ConsulInstalled = true
		} else if install.ReleaseName == common.ConsulDemoAppReleaseName {
			m.ConsulDemoInstalled = true
		}
	}
	return release, err
}

func (m *MockActionRunner) Uninstall(uninstall *action.Uninstall, name string) (*release.UninstallReleaseResponse, error) {
	var uninstallFunc func(uninstall *action.Uninstall, name string) (*release.UninstallReleaseResponse, error)

	if m.UninstallFunc == nil {
		uninstallFunc = func(uninstall *action.Uninstall, name string) (*release.UninstallReleaseResponse, error) {
			return &release.UninstallReleaseResponse{}, nil
		}
	} else {
		uninstallFunc = m.UninstallFunc
	}

	release, err := uninstallFunc(uninstall, name)
	if err == nil {
		if name == common.DefaultReleaseName {
			m.ConsulUninstalled = true
		} else if name == common.ConsulDemoAppReleaseName {
			m.ConsulDemoUninstalled = true
		}
	}
	return release, err
}

func (m *MockActionRunner) CheckForInstallations(options *CheckForInstallationsOptions) (bool, string, string, error) {
	if options.ReleaseName == common.DefaultReleaseName {
		m.CheckedForConsulInstallations = true
	} else if options.ReleaseName == common.ConsulDemoAppReleaseName {
		m.CheckedForConsulDemoInstallations = true
	}

	if m.CheckForInstallationsFunc == nil {
		return false, "", "", nil
	}
	return m.CheckForInstallationsFunc(options)
}

func (m *MockActionRunner) GetStatus(status *action.Status, name string) (*release.Release, error) {
	if name == common.DefaultReleaseName {
		m.GotStatusConsulRelease = true
	} else if name == common.ConsulDemoAppReleaseName {
		m.GotStatusConsulDemoRelease = true
	}

	if m.GetStatusFunc == nil {
		return &release.Release{}, nil
	}
	return m.GetStatusFunc(status, name)
}

func (m *MockActionRunner) Upgrade(upgrade *action.Upgrade, name string, chrt *chart.Chart, vals map[string]interface{}) (*release.Release, error) {
	var upgradeFunc func(upgrade *action.Upgrade, name string, chrt *chart.Chart, vals map[string]interface{}) (*release.Release, error)

	if m.UpgradeFunc == nil {
		upgradeFunc = func(upgrade *action.Upgrade, name string, chrt *chart.Chart, vals map[string]interface{}) (*release.Release, error) {
			return &release.Release{}, nil
		}
	} else {
		upgradeFunc = m.UpgradeFunc
	}

	release, err := upgradeFunc(upgrade, name, chrt, vals)
	if err == nil {
		if name == common.DefaultReleaseName {
			m.ConsulUpgraded = true
		} else if name == common.ConsulDemoAppReleaseName {
			m.ConsulDemoUpgraded = true
		}
	}
	return release, err
}

func (m *MockActionRunner) LoadChart(chrt embed.FS, chartDirName string) (*chart.Chart, error) {
	var loadChartFunc func(chrt embed.FS, chartDirName string) (*chart.Chart, error)

	if m.LoadChartFunc == nil {
		loadChartFunc = func(chrt embed.FS, chartDirName string) (*chart.Chart, error) {
			return &chart.Chart{}, nil
		}
	} else {
		loadChartFunc = m.LoadChartFunc
	}

	release, err := loadChartFunc(chrt, chartDirName)
	return release, err
}
