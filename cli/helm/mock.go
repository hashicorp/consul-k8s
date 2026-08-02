// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package helm

import (
	"embed"
	"time"

	"github.com/hashicorp/consul-k8s/cli/common"

	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart"
	"helm.sh/helm/v4/pkg/release"
)

type fakeReleaser struct{}

func (fakeReleaser) Name() string              { return "" }
func (fakeReleaser) Namespace() string         { return "" }
func (fakeReleaser) Version() int              { return 0 }
func (fakeReleaser) Hooks() []release.Hook     { return nil }
func (fakeReleaser) Manifest() string          { return "" }
func (fakeReleaser) Notes() string             { return "" }
func (fakeReleaser) Labels() map[string]string { return nil }
func (fakeReleaser) Chart() chart.Charter      { return nil }
func (fakeReleaser) Status() string            { return "" }
func (fakeReleaser) ApplyMethod() string       { return "" }
func (fakeReleaser) DeployedAt() time.Time     { return time.Time{} }

type fakeChart struct{}

type MockActionRunner struct {
	CheckForInstallationsFunc         func(options *CheckForInstallationsOptions) (bool, string, string, error)
	GetStatusFunc                     func(status *action.Status, name string) (release.Releaser, error)
	InstallFunc                       func(install *action.Install, chrt chart.Charter, vals map[string]interface{}) (release.Releaser, error)
	LoadChartFunc                     func(chrt embed.FS, chartDirName string) (chart.Charter, error)
	UninstallFunc                     func(uninstall *action.Uninstall, name string) (*release.UninstallReleaseResponse, error)
	UpgradeFunc                       func(upgrade *action.Upgrade, name string, chart chart.Charter, vals map[string]interface{}) (release.Releaser, error)
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

func (m *MockActionRunner) Install(install *action.Install, chrt chart.Charter, vals map[string]interface{}) (release.Releaser, error) {
	var installFunc func(install *action.Install, chrt chart.Charter, vals map[string]interface{}) (release.Releaser, error)
	if m.InstallFunc == nil {
		installFunc = func(install *action.Install, chrt chart.Charter, vals map[string]interface{}) (release.Releaser, error) {
			return fakeReleaser{}, nil
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

func (m *MockActionRunner) GetStatus(status *action.Status, name string) (release.Releaser, error) {
	if name == common.DefaultReleaseName {
		m.GotStatusConsulRelease = true
	} else if name == common.ConsulDemoAppReleaseName {
		m.GotStatusConsulDemoRelease = true
	}

	if m.GetStatusFunc == nil {
		return fakeReleaser{}, nil
	}
	return m.GetStatusFunc(status, name)
}

func (m *MockActionRunner) Upgrade(upgrade *action.Upgrade, name string, chrt chart.Charter, vals map[string]interface{}) (release.Releaser, error) {
	var upgradeFunc func(upgrade *action.Upgrade, name string, chrt chart.Charter, vals map[string]interface{}) (release.Releaser, error)

	if m.UpgradeFunc == nil {
		upgradeFunc = func(upgrade *action.Upgrade, name string, chrt chart.Charter, vals map[string]interface{}) (release.Releaser, error) {
			return fakeReleaser{}, nil
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

func (m *MockActionRunner) LoadChart(chrt embed.FS, chartDirName string) (chart.Charter, error) {
	var loadChartFunc func(chrt embed.FS, chartDirName string) (chart.Charter, error)

	if m.LoadChartFunc == nil {
		loadChartFunc = func(chrt embed.FS, chartDirName string) (chart.Charter, error) {
			return fakeChart{}, nil
		}
	} else {
		loadChartFunc = m.LoadChartFunc
	}

	release, err := loadChartFunc(chrt, chartDirName)
	return release, err
}
