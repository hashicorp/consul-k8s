package upgrade

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/hashicorp/consul-k8s/cli/common"
	cmnFlag "github.com/hashicorp/consul-k8s/cli/common/flag"
	"github.com/hashicorp/consul-k8s/cli/common/terminal"
	"github.com/hashicorp/consul-k8s/cli/helm"
	"github.com/hashicorp/consul-k8s/cli/preset"
	"github.com/hashicorp/go-hclog"
	"github.com/posener/complete"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	helmRelease "helm.sh/helm/v3/pkg/release"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
)

// TestValidateFlags tests the validate flags function.
func TestValidateFlags(t *testing.T) {
	// The following cases should all error, if they fail to this test fails.
	testCases := []struct {
		description string
		input       []string
	}{
		{
			"Should disallow non-flag arguments.",
			[]string{"foo", "-auto-approve"},
		},
		{
			"Should disallow specifying both values file AND presets.",
			[]string{"-f='f.txt'", "-preset=demo"},
		},
		{
			"Should error on invalid presets.",
			[]string{"-preset=foo"},
		},
		{
			"Should error on invalid timeout.",
			[]string{"-timeout=invalid-timeout"},
		},
		{
			"Should have errored on a non-existant file.",
			[]string{"-f=\"does_not_exist.txt\""},
		},
	}

	for _, testCase := range testCases {
		c := getInitializedCommand(t, nil)
		t.Run(testCase.description, func(t *testing.T) {
			if err := c.validateFlags(testCase.input); err == nil {
				t.Errorf("Test case should have failed.")
			}
		})
	}
}

// getInitializedCommand sets up a command struct for tests.
func getInitializedCommand(t *testing.T, buf io.Writer) *Command {
	t.Helper()
	log := hclog.New(&hclog.LoggerOptions{
		Name:   "cli",
		Level:  hclog.Info,
		Output: os.Stdout,
	})
	var ui terminal.UI
	if buf != nil {
		ui = terminal.NewUI(context.Background(), buf)
	} else {
		ui = terminal.NewBasicUI(context.Background())
	}
	baseCommand := &common.BaseCommand{
		Log: log,
		UI:  ui,
	}

	c := &Command{
		BaseCommand: baseCommand,
	}
	c.init()
	return c
}

func TestTaskCreateCommand_AutocompleteFlags(t *testing.T) {
	t.Parallel()
	cmd := getInitializedCommand(t, nil)

	predictor := cmd.AutocompleteFlags()

	// Test that we get the expected number of predictions
	args := complete.Args{Last: "-"}
	res := predictor.Predict(args)

	// Grab the list of flags from the Flag object
	flags := make([]string, 0)
	cmd.set.VisitSets(func(name string, set *cmnFlag.Set) {
		set.VisitAll(func(flag *flag.Flag) {
			flags = append(flags, fmt.Sprintf("-%s", flag.Name))
		})
	})

	// Verify that there is a prediction for each flag associated with the command
	assert.Equal(t, len(flags), len(res))
	assert.ElementsMatch(t, flags, res, "flags and predictions didn't match, make sure to add "+
		"new flags to the command AutoCompleteFlags function")
}

func TestTaskCreateCommand_AutocompleteArgs(t *testing.T) {
	cmd := getInitializedCommand(t, nil)
	c := cmd.AutocompleteArgs()
	assert.Equal(t, complete.PredictNothing, c)
}

func TestGetPreset(t *testing.T) {
	testCases := []struct {
		description string
		presetName  string
	}{
		{
			"'cloud' should return a CloudPreset'.",
			preset.PresetCloud,
		},
		{
			"'quickstart' should return a QuickstartPreset'.",
			preset.PresetQuickstart,
		},
		{
			"'secure' should return a SecurePreset'.",
			preset.PresetSecure,
		},
	}

	for _, tc := range testCases {
		c := getInitializedCommand(t, nil)
		t.Run(tc.description, func(t *testing.T) {
			p, err := c.getPreset(tc.presetName, "consul")
			require.NoError(t, err)
			switch p.(type) {
			case *preset.CloudPreset:
				require.Equal(t, preset.PresetCloud, tc.presetName)
			case *preset.QuickstartPreset:
				require.Equal(t, preset.PresetQuickstart, tc.presetName)
			case *preset.SecurePreset:
				require.Equal(t, preset.PresetSecure, tc.presetName)
			}
		})
	}
}

// TestValidateCloudPresets tests the validate flags function when passed the cloud preset.
func TestValidateCloudPresets(t *testing.T) {
	testCases := []struct {
		description        string
		input              []string
		preProcessingFunc  func()
		postProcessingFunc func()
		expectError        bool
	}{
		{
			"Should not error on cloud preset when HCP_CLIENT_ID and HCP_CLIENT_SECRET envvars are present and hcp-resource-id parameter is provided.",
			[]string{"-preset=cloud", "-hcp-resource-id=foobar"},
			func() {
				os.Setenv("HCP_CLIENT_ID", "foo")
				os.Setenv("HCP_CLIENT_SECRET", "bar")
			},
			func() {
				os.Setenv("HCP_CLIENT_ID", "")
				os.Setenv("HCP_CLIENT_SECRET", "")
			},
			false,
		},
		{
			"Should error on cloud preset when HCP_CLIENT_ID is not provided.",
			[]string{"-preset=cloud", "-hcp-resource-id=foobar"},
			func() {
				os.Unsetenv("HCP_CLIENT_ID")
				os.Setenv("HCP_CLIENT_SECRET", "bar")
			},
			func() {
				os.Unsetenv("HCP_CLIENT_ID")
				os.Unsetenv("HCP_CLIENT_SECRET")
			},
			true,
		},
		{
			"Should error on cloud preset when HCP_CLIENT_SECRET is not provided.",
			[]string{"-preset=cloud", "-hcp-resource-id=foobar"},
			func() {
				os.Setenv("HCP_CLIENT_ID", "foo")
				os.Unsetenv("HCP_CLIENT_SECRET")
			},
			func() {
				os.Unsetenv("HCP_CLIENT_ID")
				os.Unsetenv("HCP_CLIENT_SECRET")
			},
			true,
		},
		{
			"Should error on cloud preset when -hcp-resource-id flag is not provided.",
			[]string{"-preset=cloud"},
			func() {
				os.Setenv("HCP_CLIENT_ID", "foo")
				os.Setenv("HCP_CLIENT_SECRET", "bar")
			},
			func() {
				os.Unsetenv("HCP_CLIENT_ID")
				os.Unsetenv("HCP_CLIENT_SECRET")
			},
			true,
		},
		{
			"Should error when -hcp-resource-id flag is provided but cloud preset is not specified.",
			[]string{"-hcp-resource-id=foobar"},
			func() {
				os.Setenv("HCP_CLIENT_ID", "foo")
				os.Setenv("HCP_CLIENT_SECRET", "bar")
			},
			func() {
				os.Unsetenv("HCP_CLIENT_ID")
				os.Unsetenv("HCP_CLIENT_SECRET")
			},
			true,
		},
	}

	for _, testCase := range testCases {
		testCase.preProcessingFunc()
		c := getInitializedCommand(t, nil)
		t.Run(testCase.description, func(t *testing.T) {
			err := c.validateFlags(testCase.input)
			if testCase.expectError && err == nil {
				t.Errorf("Test case should have failed.")
			} else if !testCase.expectError && err != nil {
				t.Errorf("Test case should not have failed.")
			}
		})
		testCase.postProcessingFunc()
	}
}

func TestUpgrade(t *testing.T) {
	var k8s kubernetes.Interface
	cases := map[string]struct {
		input                                   []string
		messages                                []string
		helmActionsRunner                       *helm.MockActionRunner
		preProcessingFunc                       func()
		expectedReturnCode                      int
		expectCheckedForConsulInstallations     bool
		expectCheckedForConsulDemoInstallations bool
		expectConsulUpgraded                    bool
		expectConsulDemoUpgraded                bool
		expectConsulDemoInstalled               bool
	}{
		"upgrade when consul installation exists returns success": {
			input: []string{},
			messages: []string{
				"\n==> Checking if Consul can be upgraded\n ✓ Existing Consul installation found to be upgraded.\n    Name: consul\n    Namespace: consul\n",
				"\n==> Checking if Consul demo application can be upgraded\n    No existing Consul demo application installation found.\n",
				"\n==> Consul Upgrade Summary\n ✓ Downloaded charts.\n    \n    Difference between user overrides for current and upgraded charts\n    --------------------------------------------------------------\n  + global:\n  +   name: consul\n  \n",
				"\n==> Upgrading Consul\n ✓ Consul upgraded in namespace \"consul\".\n",
			},
			helmActionsRunner: &helm.MockActionRunner{
				CheckForInstallationsFunc: func(options *helm.CheckForInstallationsOptions) (bool, string, string, error) {
					if options.ReleaseName == "consul" {
						return true, "consul", "consul", nil
					} else {
						return false, "", "", nil
					}
				},
			},
			expectedReturnCode:                      0,
			expectCheckedForConsulInstallations:     true,
			expectCheckedForConsulDemoInstallations: true,
			expectConsulUpgraded:                    true,
			expectConsulDemoUpgraded:                false,
		},
		"upgrade when consul installation does not exists returns error": {
			input: []string{},
			messages: []string{
				"\n==> Checking if Consul can be upgraded\n ! Cannot upgrade Consul. Existing Consul installation not found. Use the command `consul-k8s install` to install Consul.\n",
			},
			helmActionsRunner: &helm.MockActionRunner{
				CheckForInstallationsFunc: func(options *helm.CheckForInstallationsOptions) (bool, string, string, error) {
					if options.ReleaseName == "consul" {
						return false, "", "", nil
					} else {
						return false, "", "", nil
					}
				},
			},
			expectedReturnCode:                      1,
			expectCheckedForConsulInstallations:     true,
			expectCheckedForConsulDemoInstallations: false,
			expectConsulUpgraded:                    false,
			expectConsulDemoUpgraded:                false,
		},
		"upgrade when consul upgrade errors returns error": {
			input: []string{},
			messages: []string{
				"\n==> Checking if Consul can be upgraded\n ✓ Existing Consul installation found to be upgraded.\n    Name: consul\n    Namespace: consul\n",
				"\n==> Checking if Consul demo application can be upgraded\n    No existing Consul demo application installation found.\n",
				"\n==> Consul Upgrade Summary\n ✓ Downloaded charts.\n    \n    Difference between user overrides for current and upgraded charts\n    --------------------------------------------------------------\n  + global:\n  +   name: consul\n  \n\n==> Upgrading Consul\n ! Helm returned an error.\n",
			},
			helmActionsRunner: &helm.MockActionRunner{
				CheckForInstallationsFunc: func(options *helm.CheckForInstallationsOptions) (bool, string, string, error) {
					if options.ReleaseName == "consul" {
						return true, "consul", "consul", nil
					} else {
						return false, "", "", nil
					}
				},
				UpgradeFunc: func(upgrade *action.Upgrade, name string, chart *chart.Chart, vals map[string]interface{}) (*helmRelease.Release, error) {
					return nil, errors.New("Helm returned an error.")
				},
			},
			expectedReturnCode:                      1,
			expectCheckedForConsulInstallations:     true,
			expectCheckedForConsulDemoInstallations: true,
			expectConsulUpgraded:                    false,
			expectConsulDemoUpgraded:                false,
		},
		"upgrade when demo flag provided but no demo installation exists installs demo and returns success": {
			input: []string{
				"-demo",
			},
			messages: []string{
				"\n==> Checking if Consul can be upgraded\n ✓ Existing Consul installation found to be upgraded.\n    Name: consul\n    Namespace: consul\n",
				"\n==> Checking if Consul demo application can be upgraded\n    No existing consul-demo installation found, but -demo flag provided. consul-demo will be installed in namespace consul.\n",
				"\n==> Consul Upgrade Summary\n ✓ Downloaded charts.\n    \n    Difference between user overrides for current and upgraded charts\n    --------------------------------------------------------------\n  + global:\n  +   name: consul\n  \n",
				"\n==> Upgrading Consul\n ✓ Consul upgraded in namespace \"consul\".\n",
				"\n==> Consul Demo Application Installation Summary\n    Name: consul-demo\n    Namespace: consul\n    \n    \n",
				"\n==> Installing Consul demo application\n ✓ Downloaded charts.\n ✓ Consul demo application installed in namespace \"consul\".\n",
				"\n==> Accessing Consul Demo Application UI\n    kubectl port-forward service/nginx 8080:80 --namespace consul\n    Browse to http://localhost:8080.\n",
			},
			helmActionsRunner: &helm.MockActionRunner{
				CheckForInstallationsFunc: func(options *helm.CheckForInstallationsOptions) (bool, string, string, error) {
					if options.ReleaseName == "consul" {
						return true, "consul", "consul", nil
					} else {
						return false, "", "", nil
					}
				},
			},
			expectedReturnCode:                      0,
			expectCheckedForConsulInstallations:     true,
			expectCheckedForConsulDemoInstallations: true,
			expectConsulUpgraded:                    true,
			expectConsulDemoUpgraded:                false,
			expectConsulDemoInstalled:               true,
		},
		"upgrade when demo flag provided and demo installation exists upgrades demo and returns success": {
			input: []string{
				"-demo",
			},
			messages: []string{
				"\n==> Checking if Consul can be upgraded\n ✓ Existing Consul installation found to be upgraded.\n    Name: consul\n    Namespace: consul\n",
				"\n==> Checking if Consul demo application can be upgraded\n ✓ Existing Consul demo application installation found to be upgraded.\n    Name: consul-demo\n    Namespace: consul-demo\n",
				"\n==> Consul Upgrade Summary\n ✓ Downloaded charts.\n    \n    Difference between user overrides for current and upgraded charts\n    --------------------------------------------------------------\n  + global:\n  +   name: consul\n  \n",
				"\n==> Upgrading Consul\n ✓ Consul upgraded in namespace \"consul\".\n",
				"\n==> Consul-Demo Upgrade Summary\n ✓ Downloaded charts.\n    \n    Difference between user overrides for current and upgraded charts\n    --------------------------------------------------------------\n  \n",
				"\n==> Upgrading consul-demo\n ✓ Consul-Demo upgraded in namespace \"consul-demo\".\n",
			},
			helmActionsRunner: &helm.MockActionRunner{
				CheckForInstallationsFunc: func(options *helm.CheckForInstallationsOptions) (bool, string, string, error) {
					if options.ReleaseName == "consul" {
						return true, "consul", "consul", nil
					} else {
						return true, "consul-demo", "consul-demo", nil
					}
				},
			},
			expectedReturnCode:                      0,
			expectCheckedForConsulInstallations:     true,
			expectCheckedForConsulDemoInstallations: true,
			expectConsulUpgraded:                    true,
			expectConsulDemoUpgraded:                true,
			expectConsulDemoInstalled:               false,
		},
		"upgrade when demo flag not provided but demo installation exists upgrades demo and returns success": {
			input: []string{},
			messages: []string{
				"\n==> Checking if Consul can be upgraded\n ✓ Existing Consul installation found to be upgraded.\n    Name: consul\n    Namespace: consul\n",
				"\n==> Checking if Consul demo application can be upgraded\n ✓ Existing Consul demo application installation found to be upgraded.\n    Name: consul-demo\n    Namespace: consul-demo\n",
				"\n==> Consul Upgrade Summary\n ✓ Downloaded charts.\n    \n    Difference between user overrides for current and upgraded charts\n    --------------------------------------------------------------\n  + global:\n  +   name: consul\n  \n",
				"\n==> Upgrading Consul\n ✓ Consul upgraded in namespace \"consul\".\n",
				"\n==> Consul-Demo Upgrade Summary\n ✓ Downloaded charts.\n    \n    Difference between user overrides for current and upgraded charts\n    --------------------------------------------------------------\n  \n",
				"\n==> Upgrading consul-demo\n ✓ Consul-Demo upgraded in namespace \"consul-demo\".\n",
			},
			helmActionsRunner: &helm.MockActionRunner{
				CheckForInstallationsFunc: func(options *helm.CheckForInstallationsOptions) (bool, string, string, error) {
					if options.ReleaseName == "consul" {
						return true, "consul", "consul", nil
					} else {
						return true, "consul-demo", "consul-demo", nil
					}
				},
			},
			expectedReturnCode:                      0,
			expectCheckedForConsulInstallations:     true,
			expectCheckedForConsulDemoInstallations: true,
			expectConsulUpgraded:                    true,
			expectConsulDemoUpgraded:                true,
			expectConsulDemoInstalled:               false,
		},
		"upgrade when demo upgrade errors returns error with consul being upgraded but demo not being upgraded": {
			input: []string{},
			messages: []string{
				"\n==> Checking if Consul can be upgraded\n ✓ Existing Consul installation found to be upgraded.\n    Name: consul\n    Namespace: consul\n",
				"\n==> Checking if Consul demo application can be upgraded\n ✓ Existing Consul demo application installation found to be upgraded.\n    Name: consul-demo\n    Namespace: consul-demo\n",
				"\n==> Consul Upgrade Summary\n ✓ Downloaded charts.\n    \n    Difference between user overrides for current and upgraded charts\n    --------------------------------------------------------------\n  + global:\n  +   name: consul\n  \n",
				"\n==> Upgrading Consul\n ✓ Consul upgraded in namespace \"consul\".\n",
				"\n==> Consul-Demo Upgrade Summary\n ✓ Downloaded charts.\n    \n    Difference between user overrides for current and upgraded charts\n    --------------------------------------------------------------\n  \n",
				"\n==> Upgrading consul-demo\n ! Helm returned an error.\n",
			},
			helmActionsRunner: &helm.MockActionRunner{
				CheckForInstallationsFunc: func(options *helm.CheckForInstallationsOptions) (bool, string, string, error) {
					if options.ReleaseName == "consul" {
						return true, "consul", "consul", nil
					} else {
						return true, "consul-demo", "consul-demo", nil
					}
				},
				UpgradeFunc: func(upgrade *action.Upgrade, name string, chart *chart.Chart, vals map[string]interface{}) (*helmRelease.Release, error) {
					if name == "consul" {
						return &helmRelease.Release{}, nil
					} else {
						return nil, errors.New("Helm returned an error.")
					}
				},
			},
			expectedReturnCode:                      1,
			expectCheckedForConsulInstallations:     true,
			expectCheckedForConsulDemoInstallations: true,
			expectConsulUpgraded:                    true,
			expectConsulDemoUpgraded:                false,
		},
		"upgrade with quickstart preset when consul installation exists returns success": {
			input: []string{
				"-preset", "quickstart",
			},
			messages: []string{
				"\n==> Checking if Consul can be upgraded\n ✓ Existing Consul installation found to be upgraded.\n    Name: consul\n    Namespace: consul\n",
				"\n==> Checking if Consul demo application can be upgraded\n    No existing Consul demo application installation found.\n",
				"\n==> Consul Upgrade Summary\n ✓ Downloaded charts.\n    \n    Difference between user overrides for current and upgraded charts\n    --------------------------------------------------------------\n  + connectInject:\n  +   enabled: true\n  +   metrics:\n  +     defaultEnableMerging: true\n  +     defaultEnabled: true\n  +     enableGatewayMetrics: true\n  + controller:\n  +   enabled: true\n  + global:\n  +   metrics:\n  +     enableAgentMetrics: true\n  +     enabled: true\n  +   name: consul\n  + prometheus:\n  +   enabled: true\n  + server:\n  +   replicas: 1\n  + ui:\n  +   enabled: true\n  +   service:\n  +     enabled: true\n  \n",
				"\n==> Upgrading Consul\n ✓ Consul upgraded in namespace \"consul\".\n",
			},
			helmActionsRunner: &helm.MockActionRunner{
				CheckForInstallationsFunc: func(options *helm.CheckForInstallationsOptions) (bool, string, string, error) {
					if options.ReleaseName == "consul" {
						return true, "consul", "consul", nil
					} else {
						return false, "", "", nil
					}
				},
			},
			expectedReturnCode:                      0,
			expectCheckedForConsulInstallations:     true,
			expectCheckedForConsulDemoInstallations: true,
			expectConsulUpgraded:                    true,
			expectConsulDemoUpgraded:                false,
		},
		"upgrade with secure preset when consul installation exists returns success": {
			input: []string{
				"-preset", "secure",
			},
			messages: []string{
				"\n==> Checking if Consul can be upgraded\n ✓ Existing Consul installation found to be upgraded.\n    Name: consul\n    Namespace: consul\n",
				"\n==> Checking if Consul demo application can be upgraded\n    No existing Consul demo application installation found.\n",
				"\n==> Consul Upgrade Summary\n ✓ Downloaded charts.\n    \n    Difference between user overrides for current and upgraded charts\n    --------------------------------------------------------------\n  + connectInject:\n  +   enabled: true\n  + controller:\n  +   enabled: true\n  + global:\n  +   acls:\n  +     manageSystemACLs: true\n  +   gossipEncryption:\n  +     autoGenerate: true\n  +   name: consul\n  +   tls:\n  +     enableAutoEncrypt: true\n  +     enabled: true\n  + server:\n  +   replicas: 1\n  \n",
				"\n==> Upgrading Consul\n ✓ Consul upgraded in namespace \"consul\".\n",
			},
			helmActionsRunner: &helm.MockActionRunner{
				CheckForInstallationsFunc: func(options *helm.CheckForInstallationsOptions) (bool, string, string, error) {
					if options.ReleaseName == "consul" {
						return true, "consul", "consul", nil
					} else {
						return false, "", "", nil
					}
				},
			},
			expectedReturnCode:                      0,
			expectCheckedForConsulInstallations:     true,
			expectCheckedForConsulDemoInstallations: true,
			expectConsulUpgraded:                    true,
			expectConsulDemoUpgraded:                false,
		},
		"upgrade with --dry-run flag when consul installation exists returns success": {
			input: []string{
				"--dry-run",
			},
			messages: []string{
				"    Performing dry run upgrade. No changes will be made to the cluster.\n",
				"\n==> Checking if Consul can be upgraded\n ✓ Existing Consul installation found to be upgraded.\n    Name: consul\n    Namespace: consul\n",
				"\n==> Checking if Consul demo application can be upgraded\n    No existing Consul demo application installation found.\n",
				"\n==> Consul Upgrade Summary\n ✓ Downloaded charts.\n    \n    Difference between user overrides for current and upgraded charts\n    --------------------------------------------------------------\n  + global:\n  +   name: consul\n  \n",
				"\n==> Performing Dry Run Upgrade\n    Dry run complete. No changes were made to the Kubernetes cluster.\n    Upgrade can proceed with this configuration.\n",
			},
			helmActionsRunner: &helm.MockActionRunner{
				CheckForInstallationsFunc: func(options *helm.CheckForInstallationsOptions) (bool, string, string, error) {
					if options.ReleaseName == "consul" {
						return true, "consul", "consul", nil
					} else {
						return false, "", "", nil
					}
				},
			},
			expectedReturnCode:                      0,
			expectCheckedForConsulInstallations:     true,
			expectCheckedForConsulDemoInstallations: true,
			expectConsulUpgraded:                    false,
			expectConsulDemoUpgraded:                false,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			buf := new(bytes.Buffer)
			c := getInitializedCommand(t, buf)
			k8s = fake.NewSimpleClientset()
			c.kubernetes = k8s
			mock := tc.helmActionsRunner
			c.helmActionsRunner = mock
			if tc.preProcessingFunc != nil {
				tc.preProcessingFunc()
			}
			input := append([]string{
				"--auto-approve",
			}, tc.input...)
			returnCode := c.Run(input)
			require.Equal(t, tc.expectedReturnCode, returnCode)
			require.Equal(t, tc.expectCheckedForConsulInstallations, mock.CheckedForConsulInstallations)
			require.Equal(t, tc.expectCheckedForConsulDemoInstallations, mock.CheckedForConsulDemoInstallations)
			require.Equal(t, tc.expectConsulUpgraded, mock.ConsulUpgraded)
			require.Equal(t, tc.expectConsulDemoUpgraded, mock.ConsulDemoUpgraded)
			require.Equal(t, tc.expectConsulDemoInstalled, mock.ConsulDemoInstalled)
			output := buf.String()
			for _, msg := range tc.messages {
				require.Contains(t, output, msg)
			}
		})
	}
}
