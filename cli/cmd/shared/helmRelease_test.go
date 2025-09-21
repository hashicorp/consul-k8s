package shared

import (
	"errors"
	"testing"

	"github.com/hashicorp/consul-k8s/cli/helm"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	helmCLI "helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/release"
	helmTime "helm.sh/helm/v3/pkg/time"
)

func TestGetHelmRelease(t *testing.T) {
	nowTime := helmTime.Now()

	cases := map[string]struct {
		helmActionsRunner  *helm.MockActionRunner
		expectedRelease    *release.Release
		expectedErrContent string
	}{
		"success with empty config": {
			helmActionsRunner: &helm.MockActionRunner{
				CheckForInstallationsFunc: func(options *helm.CheckForInstallationsOptions) (bool, string, string, error) {
					return true, "consul-release", "consul-ns", nil
				},
				GetStatusFunc: func(status *action.Status, name string) (*release.Release, error) {
					return &release.Release{
						Name:      "consul-release",
						Namespace: "consul-ns",
						Info:      &release.Info{LastDeployed: nowTime, Status: "READY"},
						Chart:     &chart.Chart{Metadata: &chart.Metadata{Version: "1.0.0"}},
						Config:    make(map[string]interface{}),
					}, nil
				},
			},
			expectedRelease: &release.Release{
				Name:      "consul-release",
				Namespace: "consul-ns",
				Info:      &release.Info{LastDeployed: nowTime, Status: "READY"},
				Chart:     &chart.Chart{Metadata: &chart.Metadata{Version: "1.0.0"}},
				Config:    make(map[string]interface{}),
			},
		},
		"success with some config": {
			helmActionsRunner: &helm.MockActionRunner{
				CheckForInstallationsFunc: func(options *helm.CheckForInstallationsOptions) (bool, string, string, error) {
					return true, "consul", "consul", nil
				},
				GetStatusFunc: func(status *action.Status, name string) (*release.Release, error) {
					return &release.Release{
						Name:      "consul",
						Namespace: "consul",
						Info:      &release.Info{LastDeployed: nowTime, Status: "READY"},
						Chart:     &chart.Chart{Metadata: &chart.Metadata{Version: "1.0.0"}},
						Config:    map[string]interface{}{"global": "true"},
					}, nil
				},
			},
			expectedRelease: &release.Release{
				Name:      "consul",
				Namespace: "consul",
				Info:      &release.Info{LastDeployed: nowTime, Status: "READY"},
				Chart:     &chart.Chart{Metadata: &chart.Metadata{Version: "1.0.0"}},
				Config:    map[string]interface{}{"global": "true"},
			},
		},
		"error on CheckForInstallations": {
			helmActionsRunner: &helm.MockActionRunner{
				CheckForInstallationsFunc: func(opts *helm.CheckForInstallationsOptions) (bool, string, string, error) {
					return false, "", "", errors.New("helm CheckForInstallations test failed")
				},
				// GetStatusFunc is not needed here as the function will exit early.
			},
			expectedErrContent: "couldn't find the helm releases",
		},
		"error on GetStatus": {
			helmActionsRunner: &helm.MockActionRunner{
				CheckForInstallationsFunc: func(opts *helm.CheckForInstallationsOptions) (bool, string, string, error) {
					return true, "consul-release", "consul-ns", nil
				},
				GetStatusFunc: func(status *action.Status, name string) (*release.Release, error) {
					return nil, errors.New("helm GetStatus test failed")
				},
			},
			expectedErrContent: "couldn't get the helm release",
		},
		// 		GetStatusFunc: func(status *action.Status, name string) (*release.Release, error) {
		// 			return nil, errors.New("error")
		// 		},
		// 	},
		// 	expectedReturnCode: 1,
		// },
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			settings := helmCLI.New()
			rel, releaseName, namespace, err := GetHelmRelease(settings, tc.helmActionsRunner)
			if tc.expectedErrContent != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.expectedErrContent)
				require.Nil(t, rel)
			} else {
				require.NoError(t, err)
				require.NotNil(t, rel)
				require.Equal(t, tc.expectedRelease.Name, rel.Name)
				require.Equal(t, tc.expectedRelease.Name, releaseName)
				require.Equal(t, tc.expectedRelease.Namespace, namespace)
				require.Equal(t, tc.expectedRelease.Info.Status, rel.Info.Status)
				require.Equal(t, tc.expectedRelease.Info.LastDeployed, rel.Info.LastDeployed)
				require.Equal(t, tc.expectedRelease.Chart.Metadata.Version, rel.Chart.Metadata.Version)
				require.Equal(t, tc.expectedRelease.Config, rel.Config)
			}
		})
	}
}
