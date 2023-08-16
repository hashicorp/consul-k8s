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

func TestInstallDemoApp(t *testing.T) {
	cases := map[string]struct {
		messages          []string
		helmActionsRunner *MockActionRunner
		expectError       bool
	}{
		"basic success": {
			messages: []string{
				"\n==> Consul Demo Application Installation Summary\n    Name: consul-demo\n    Namespace: default\n    \n    \n",
				"\n==> Installing Consul\n ✓ Downloaded charts.\n ✓ Consul installed in namespace \"consul-namespace\".\n",
				"\n==> Accessing Consul Demo Application UI\n    kubectl port-forward service/nginx 8080:80 --namespace consul-namespace\n    Browse to http://localhost:8080.\n",
			},
			helmActionsRunner: &MockActionRunner{},
		},
		"failure because LoadChart returns failure": {
			messages: []string{
				"\n==> Consul Demo Application Installation Summary\n    Name: consul-demo\n    Namespace: default\n    \n    \n\n==> Installing Consul\n",
			},
			helmActionsRunner: &MockActionRunner{
				LoadChartFunc: func(chrt embed.FS, chartDirName string) (*chart.Chart, error) {
					return nil, errors.New("sad trombone!")
				},
			},
			expectError: true,
		},
		"failure because Install returns failure": {
			messages: []string{
				"\n==> Consul Demo Application Installation Summary\n    Name: consul-demo\n    Namespace: default\n    \n    \n\n==> Installing Consul\n",
			},
			helmActionsRunner: &MockActionRunner{
				InstallFunc: func(install *action.Install, chrt *chart.Chart, vals map[string]interface{}) (*release.Release, error) {
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
			options := &InstallOptions{
				HelmActionsRunner: mock,
				UI:                terminal.NewUI(context.Background(), buf),
				UILogger:          func(format string, v ...interface{}) {},
				ReleaseName:       "consul-release",
				ReleaseType:       common.ReleaseTypeConsul,
				Namespace:         "consul-namespace",
				Settings:          helmCLI.New(),
				AutoApprove:       true,
			}
			err := InstallDemoApp(options)
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
