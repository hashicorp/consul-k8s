package helm

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"testing"

	"github.com/hashicorp/consul-k8s/cli/common"
	"github.com/hashicorp/consul-k8s/cli/common/terminal"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	helmCLI "helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/release"
)

func TestUpgrade(t *testing.T) {
	buf := new(bytes.Buffer)
	mock := &MockActionRunner{
		CheckForInstallationsFunc: func(options *CheckForInstallationsOptions) (bool, string, string, error) {
			if options.ReleaseName == "consul" {
				return false, "", "", nil
			} else {
				return true, "consul-demo", "consul-demo", nil
			}
		},
	}

	options := &UpgradeOptions{
		HelmActionsRunner: mock,
		UI:                terminal.NewUI(context.Background(), buf),
		UILogger:          func(format string, v ...interface{}) {},
		ReleaseName:       "consul-release",
		ReleaseType:       common.ReleaseTypeConsul,
		Namespace:         "consul-namespace",
		Settings:          helmCLI.New(),
		AutoApprove:       true,
	}

	expectedMessages := []string{
		"\n==>  Upgrade Summary\n ✓ Downloaded charts.\n    \n    Difference between user overrides for current and upgraded charts\n    --------------------------------------------------------------\n  \n",
		"\n==> Upgrading \n ✓  upgraded in namespace \"consul-namespace\".\n",
	}
	err := UpgradeHelmRelease(options)
	require.NoError(t, err)
	output := buf.String()
	for _, msg := range expectedMessages {
		require.Contains(t, output, msg)
	}
}

func TestUpgradeHelmRelease(t *testing.T) {
	cases := map[string]struct {
		messages          []string
		helmActionsRunner *MockActionRunner
		expectError       bool
	}{
		"basic success": {
			messages: []string{
				"\n==> Consul Upgrade Summary\n ✓ Downloaded charts.\n    \n    Difference between user overrides for current and upgraded charts\n    --------------------------------------------------------------\n  \n",
				"\n==> Upgrading Consul\n ✓ Consul upgraded in namespace \"consul-namespace\".\n",
			},
			helmActionsRunner: &MockActionRunner{},
		},
		"failure because LoadChart returns failure": {
			messages: []string{
				"\n==> Consul Upgrade Summary\n",
			},
			helmActionsRunner: &MockActionRunner{
				LoadChartFunc: func(chrt embed.FS, chartDirName string) (*chart.Chart, error) {
					return nil, errors.New("sad trombone!")
				},
			},
			expectError: true,
		},
		"failure because Upgrade returns failure": {
			messages: []string{
				"\n==> Consul Upgrade Summary\n",
			},
			helmActionsRunner: &MockActionRunner{
				UpgradeFunc: func(upgrade *action.Upgrade, name string, chart *chart.Chart, vals map[string]interface{}) (*release.Release, error) {
					return nil, errors.New("sad trombone!")
				},
			},
			expectError: true,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			buf := new(bytes.Buffer)
			mock := tc.helmActionsRunner
			options := &UpgradeOptions{
				HelmActionsRunner: mock,
				UI:                terminal.NewUI(context.Background(), buf),
				UILogger:          func(format string, v ...interface{}) {},
				ReleaseName:       "consul-release",
				ReleaseType:       common.ReleaseTypeConsul,
				ReleaseTypeName:   common.ReleaseTypeConsul,
				Namespace:         "consul-namespace",
				Settings:          helmCLI.New(),
				AutoApprove:       true,
			}
			err := UpgradeHelmRelease(options)
			if tc.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			output := buf.String()
			for _, msg := range tc.messages {
				require.Contains(t, output, msg)
			}
		})
	}
}
