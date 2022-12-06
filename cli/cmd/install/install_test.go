package install

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
	"github.com/hashicorp/consul-k8s/cli/release"
	"github.com/hashicorp/go-hclog"
	"github.com/posener/complete"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	helmRelease "helm.sh/helm/v3/pkg/release"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
)

func TestCheckForPreviousPVCs(t *testing.T) {
	c := getInitializedCommand(t, nil)
	c.kubernetes = fake.NewSimpleClientset()

	createPVC(t, "consul-server-test1", "default", c.kubernetes)
	createPVC(t, "consul-server-test2", "default", c.kubernetes)

	err := c.checkForPreviousPVCs()
	require.Error(t, err)
	require.Equal(t, err.Error(), "found persistent volume claims from previous installations, delete before reinstalling: default/consul-server-test1,default/consul-server-test2")

	// Clear out the client and make sure the check now passes.
	c.kubernetes = fake.NewSimpleClientset()
	err = c.checkForPreviousPVCs()
	require.NoError(t, err)

	// Add a new irrelevant PVC and make sure the check continues to pass.
	createPVC(t, "irrelevant-pvc", "default", c.kubernetes)
	err = c.checkForPreviousPVCs()
	require.NoError(t, err)
}

func TestCheckForPreviousSecrets(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		releaseName string
		helmValues  helm.Values
		secret      *v1.Secret
		expectMsg   bool
		expectErr   bool
	}{
		"No secrets, none expected": {
			releaseName: "consul",
			helmValues:  helm.Values{},
			secret:      nil,
			expectMsg:   true,
			expectErr:   false,
		},
		"Non-Consul secrets, none expected": {
			releaseName: "consul",
			helmValues:  helm.Values{},
			secret: &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "non-consul-secret",
				},
			},
			expectMsg: true,
			expectErr: false,
		},
		"Consul secrets, none expected": {
			releaseName: "consul",
			helmValues:  helm.Values{},
			secret: &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "consul-secret",
					Labels: map[string]string{common.CLILabelKey: common.CLILabelValue},
				},
			},
			expectMsg: false,
			expectErr: true,
		},
		"Federation secret, expected": {
			releaseName: "consul",
			helmValues: helm.Values{
				Global: helm.Global{
					Datacenter: "dc2",
					Federation: helm.Federation{
						Enabled:                true,
						PrimaryDatacenter:      "dc1",
						CreateFederationSecret: false,
					},
					Acls: helm.Acls{
						ReplicationToken: helm.ReplicationToken{
							SecretName: "consul-federation",
						},
					},
				},
			},
			secret: &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "consul-federation",
					Labels: map[string]string{common.CLILabelKey: common.CLILabelValue},
				},
			},
			expectMsg: true,
			expectErr: false,
		},
		"No federation secret, but expected": {
			releaseName: "consul",
			helmValues: helm.Values{
				Global: helm.Global{
					Datacenter: "dc2",
					Federation: helm.Federation{
						Enabled:                true,
						PrimaryDatacenter:      "dc1",
						CreateFederationSecret: false,
					},
					Acls: helm.Acls{
						ReplicationToken: helm.ReplicationToken{
							SecretName: "consul-federation",
						},
					},
				},
			},
			secret:    nil,
			expectMsg: false,
			expectErr: true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c := getInitializedCommand(t, nil)
			c.kubernetes = fake.NewSimpleClientset()

			c.kubernetes.CoreV1().Secrets("consul").Create(context.Background(), tc.secret, metav1.CreateOptions{})

			release := release.Release{Name: tc.releaseName, Configuration: tc.helmValues}
			msg, err := c.checkForPreviousSecrets(release)

			require.Equal(t, tc.expectMsg, msg != "")
			require.Equal(t, tc.expectErr, err != nil)
		})
	}
}

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
			"Should error on an invalid namespace. If this failed, TestValidLabel() probably did too.",
			[]string{"-namespace=\" nsWithSpace\""},
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

func TestCheckValidEnterprise(t *testing.T) {
	c := getInitializedCommand(t, nil)
	c.kubernetes = fake.NewSimpleClientset()
	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "consul-secret",
		},
	}
	secret2 := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "consul-secret2",
		},
	}

	// Enterprise secret is valid.
	createSecret(t, secret, "consul", c.kubernetes)
	err := c.checkValidEnterprise(secret.Name)
	require.NoError(t, err)

	// Enterprise secret does not exist.
	err = c.checkValidEnterprise("consul-unrelated-secret")
	require.Error(t, err)
	require.Contains(t, err.Error(), "please make sure that the secret exists")

	// Enterprise secret exists in a different namespace.
	c.kubernetes.CoreV1().Secrets("unrelated").Create(context.Background(), secret2, metav1.CreateOptions{})
	err = c.checkValidEnterprise(secret2.Name)
	require.Error(t, err)
	require.Contains(t, err.Error(), "please make sure that the secret exists")
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
				os.Unsetenv("HCP_CLIENT_ID")
				os.Unsetenv("HCP_CLIENT_SECRET")
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
			if testCase.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
		defer testCase.postProcessingFunc()
	}
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
			p, err := c.getPreset(tc.presetName)
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

func TestInstall(t *testing.T) {
	var k8s kubernetes.Interface
	licenseSecretName := "consul-license"
	cases := map[string]struct {
		input                                   []string
		messages                                []string
		helmActionsRunner                       *helm.MockActionRunner
		preProcessingFunc                       func()
		expectedReturnCode                      int
		expectCheckedForConsulInstallations     bool
		expectCheckedForConsulDemoInstallations bool
		expectConsulInstalled                   bool
		expectConsulDemoInstalled               bool
	}{
		"install with no arguments returns success": {
			input: []string{},
			messages: []string{
				"\n==> Checking if Consul can be installed\n ✓ No existing Consul installations found.\n ✓ No existing Consul persistent volume claims found\n ✓ No existing Consul secrets found.\n",
				"\n==> Consul Installation Summary\n    Name: consul\n    Namespace: consul\n    \n    No overrides provided, using the default Helm values.\n",
				"\n==> Installing Consul\n ✓ Downloaded charts.\n ✓ Consul installed in namespace \"consul\".\n",
			},
			helmActionsRunner:                       &helm.MockActionRunner{},
			expectedReturnCode:                      0,
			expectCheckedForConsulInstallations:     true,
			expectCheckedForConsulDemoInstallations: false,
			expectConsulInstalled:                   true,
			expectConsulDemoInstalled:               false,
		},
		"install when consul installation errors returns error": {
			input: []string{},
			messages: []string{
				"\n==> Checking if Consul can be installed\n ✓ No existing Consul installations found.\n ✓ No existing Consul persistent volume claims found\n ✓ No existing Consul secrets found.\n",
				"\n==> Consul Installation Summary\n    Name: consul\n    Namespace: consul\n    \n    No overrides provided, using the default Helm values.\n",
				"\n==> Installing Consul\n ✓ Downloaded charts.\n ! Helm returned an error.\n",
			},
			helmActionsRunner: &helm.MockActionRunner{
				InstallFunc: func(install *action.Install, chrt *chart.Chart, vals map[string]interface{}) (*helmRelease.Release, error) {
					return nil, errors.New("Helm returned an error.")
				},
			},
			expectedReturnCode:                      1,
			expectCheckedForConsulInstallations:     true,
			expectCheckedForConsulDemoInstallations: false,
			expectConsulInstalled:                   false,
			expectConsulDemoInstalled:               false,
		},
		"install with no arguments when consul installation already exists returns error": {
			input: []string{
				"--auto-approve",
			},
			messages: []string{
				"\n==> Checking if Consul can be installed\n ! Cannot install Consul. A Consul cluster is already installed in namespace consul with name consul.\n    Use the command `consul-k8s uninstall` to uninstall Consul from the cluster.\n",
			},
			helmActionsRunner: &helm.MockActionRunner{
				CheckForInstallationsFunc: func(options *helm.CheckForInstallationsOptions) (bool, string, string, error) {
					return true, "consul", "consul", nil
				},
			},
			expectedReturnCode:                      1,
			expectCheckedForConsulInstallations:     true,
			expectCheckedForConsulDemoInstallations: false,
			expectConsulInstalled:                   false,
			expectConsulDemoInstalled:               false,
		},
		"install with no arguments when PVCs exist returns error": {
			input: []string{},
			messages: []string{
				"\n==> Checking if Consul can be installed\n ✓ No existing Consul installations found.\n ! found persistent volume claims from previous installations, delete before reinstalling: consul/consul-server-test1\n",
			},
			helmActionsRunner: &helm.MockActionRunner{},
			preProcessingFunc: func() {
				createPVC(t, "consul-server-test1", "consul", k8s)
			},
			expectedReturnCode:                      1,
			expectCheckedForConsulInstallations:     true,
			expectCheckedForConsulDemoInstallations: false,
			expectConsulInstalled:                   false,
			expectConsulDemoInstalled:               false,
		},
		"install with no arguments when secrets exist returns error": {
			input: []string{
				"--auto-approve",
			},
			messages: []string{
				"\n==> Checking if Consul can be installed\n ✓ No existing Consul installations found.\n ✓ No existing Consul persistent volume claims found\n ! Found Consul secrets, possibly from a previous installation.\nDelete existing Consul secrets from Kubernetes:\n\nkubectl delete secret consul-secret --namespace consul\n\n",
			},
			helmActionsRunner: &helm.MockActionRunner{},
			preProcessingFunc: func() {
				secret := &v1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "consul-secret",
						Labels: map[string]string{common.CLILabelKey: common.CLILabelValue},
					},
				}
				createSecret(t, secret, "consul", k8s)
			},
			expectedReturnCode:                      1,
			expectCheckedForConsulInstallations:     true,
			expectCheckedForConsulDemoInstallations: false,
			expectConsulInstalled:                   false,
			expectConsulDemoInstalled:               false,
		},
		"enterprise install when license secret exists returns success": {
			input: []string{
				"--set", fmt.Sprintf("global.enterpriseLicense.secretName=%s", licenseSecretName),
			},
			messages: []string{
				"\n==> Checking if Consul can be installed\n ✓ No existing Consul installations found.\n ✓ No existing Consul persistent volume claims found\n ✓ No existing Consul secrets found.\n ✓ Valid enterprise Consul secret found.\n",
				"\n==> Consul Installation Summary\n    Name: consul\n    Namespace: consul\n    \n    Helm value overrides\n    --------------------\n    global:\n      enterpriseLicense:\n        secretName: consul-license\n    \n",
				"\n==> Installing Consul\n ✓ Downloaded charts.\n ✓ Consul installed in namespace \"consul\".\n",
			},
			helmActionsRunner: &helm.MockActionRunner{},
			preProcessingFunc: func() {
				secret := &v1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name: licenseSecretName,
					},
				}
				createSecret(t, secret, "consul", k8s)
			},
			expectedReturnCode:                      0,
			expectCheckedForConsulInstallations:     true,
			expectCheckedForConsulDemoInstallations: false,
			expectConsulInstalled:                   true,
			expectConsulDemoInstalled:               false,
		},
		"enterprise install when license secret does not exist returns error": {
			input: []string{
				"--set", fmt.Sprintf("global.enterpriseLicense.secretName=%s", licenseSecretName),
			},
			messages: []string{
				"\n==> Checking if Consul can be installed\n ✓ No existing Consul installations found.\n ✓ No existing Consul persistent volume claims found\n ✓ No existing Consul secrets found.\n ! enterprise license secret \"consul-license\" is not found in the \"consul\" namespace; please make sure that the secret exists in the \"consul\" namespace\n"},
			helmActionsRunner:                       &helm.MockActionRunner{},
			expectedReturnCode:                      1,
			expectCheckedForConsulInstallations:     true,
			expectCheckedForConsulDemoInstallations: false,
			expectConsulInstalled:                   false,
			expectConsulDemoInstalled:               false,
		},
		"install for quickstart preset returns success": {
			input: []string{
				"-preset", "quickstart",
			},
			messages: []string{
				"\n==> Checking if Consul can be installed\n ✓ No existing Consul installations found.\n ✓ No existing Consul persistent volume claims found\n ✓ No existing Consul secrets found.\n",
				"\n==> Consul Installation Summary\n    Name: consul\n    Namespace: consul\n    \n    Helm value overrides\n    --------------------\n    connectInject:\n      enabled: true\n      metrics:\n        defaultEnableMerging: true\n        defaultEnabled: true\n        enableGatewayMetrics: true\n    global:\n      metrics:\n        enableAgentMetrics: true\n        enabled: true\n      name: consul\n    prometheus:\n      enabled: true\n    server:\n      replicas: 1\n    ui:\n      enabled: true\n      service:\n        enabled: true\n    \n",
				"\n==> Installing Consul\n ✓ Downloaded charts.\n ✓ Consul installed in namespace \"consul\".\n",
			},
			helmActionsRunner:                       &helm.MockActionRunner{},
			expectedReturnCode:                      0,
			expectCheckedForConsulInstallations:     true,
			expectCheckedForConsulDemoInstallations: false,
			expectConsulInstalled:                   true,
			expectConsulDemoInstalled:               false,
		},
		"install for secure preset returns success": {
			input: []string{
				"-preset", "secure",
			},
			messages: []string{
				"\n==> Checking if Consul can be installed\n ✓ No existing Consul installations found.\n ✓ No existing Consul persistent volume claims found\n ✓ No existing Consul secrets found.\n",
				"\n==> Consul Installation Summary\n    Name: consul\n    Namespace: consul\n    \n    Helm value overrides\n    --------------------\n    connectInject:\n      enabled: true\n    global:\n      acls:\n        manageSystemACLs: true\n      gossipEncryption:\n        autoGenerate: true\n      name: consul\n      tls:\n        enableAutoEncrypt: true\n        enabled: true\n    server:\n      replicas: 1\n    \n",
				"\n==> Installing Consul\n ✓ Downloaded charts.\n ✓ Consul installed in namespace \"consul\".\n",
			},
			helmActionsRunner:                       &helm.MockActionRunner{},
			expectedReturnCode:                      0,
			expectCheckedForConsulInstallations:     true,
			expectCheckedForConsulDemoInstallations: false,
			expectConsulInstalled:                   true,
			expectConsulDemoInstalled:               false,
		},
		"install with demo flag returns success": {
			input: []string{
				"-demo",
			},
			messages: []string{
				"\n==> Checking if Consul can be installed\n ✓ No existing Consul installations found.\n ✓ No existing Consul persistent volume claims found\n ✓ No existing Consul secrets found.\n",
				"\n==> Checking if Consul Demo Application can be installed\n ✓ No existing Consul demo application installations found.\n",
				"\n==> Consul Installation Summary\n    Name: consul\n    Namespace: consul\n    \n    No overrides provided, using the default Helm values.\n",
				"\n==> Installing Consul\n ✓ Downloaded charts.\n ✓ Consul installed in namespace \"consul\".\n",
				"\n==> Consul Demo Application Installation Summary\n    Name: consul-demo\n    Namespace: consul\n    \n    \n",
				"\n==> Installing Consul demo application\n ✓ Downloaded charts.\n ✓ Consul demo application installed in namespace \"consul\".\n",
				"\n==> Accessing Consul Demo Application UI\n    kubectl port-forward service/nginx 8080:80 --namespace consul\n    Browse to http://localhost:8080.\n",
			},
			helmActionsRunner:                       &helm.MockActionRunner{},
			expectedReturnCode:                      0,
			expectCheckedForConsulInstallations:     true,
			expectCheckedForConsulDemoInstallations: true,
			expectConsulInstalled:                   true,
			expectConsulDemoInstalled:               true,
		},
		"install with demo flag when consul demo installation errors returns error": {
			input: []string{
				"-demo",
			},
			messages: []string{
				"\n==> Checking if Consul can be installed\n ✓ No existing Consul installations found.\n ✓ No existing Consul persistent volume claims found\n ✓ No existing Consul secrets found.\n",
				"\n==> Checking if Consul Demo Application can be installed\n ✓ No existing Consul demo application installations found.\n",
				"\n==> Consul Installation Summary\n    Name: consul\n    Namespace: consul\n    \n    No overrides provided, using the default Helm values.\n",
				"\n==> Installing Consul\n ✓ Downloaded charts.\n ✓ Consul installed in namespace \"consul\".\n",
				"\n==> Consul Demo Application Installation Summary\n    Name: consul-demo\n    Namespace: consul\n    \n    \n",
				"\n==> Installing Consul demo application\n ✓ Downloaded charts.\n ! Helm returned an error.\n",
			},
			helmActionsRunner: &helm.MockActionRunner{
				InstallFunc: func(install *action.Install, chrt *chart.Chart, vals map[string]interface{}) (*helmRelease.Release, error) {
					if install.ReleaseName == "consul" {
						return &helmRelease.Release{Name: install.ReleaseName}, nil
					}
					return nil, errors.New("Helm returned an error.")
				},
			},
			expectedReturnCode:                      1,
			expectCheckedForConsulInstallations:     true,
			expectCheckedForConsulDemoInstallations: true,
			expectConsulInstalled:                   true,
			expectConsulDemoInstalled:               false,
		},
		"install with demo flag when demo is already installed returns error and does not install consul or the demo": {
			input: []string{
				"-demo",
			},
			messages: []string{
				"\n==> Checking if Consul can be installed\n ✓ No existing Consul installations found.\n ✓ No existing Consul persistent volume claims found\n ✓ No existing Consul secrets found.\n",
				"\n==> Checking if Consul Demo Application can be installed\n ! Cannot install Consul demo application. A Consul demo application cluster is already installed in namespace consul-demo with name consul-demo.\n    Use the command `consul-k8s uninstall` to uninstall the Consul demo application from the cluster.\n",
			},
			helmActionsRunner: &helm.MockActionRunner{
				CheckForInstallationsFunc: func(options *helm.CheckForInstallationsOptions) (bool, string, string, error) {
					if options.ReleaseName == "consul" {
						return false, "", "", nil
					} else {
						return true, "consul-demo", "consul-demo", nil
					}
				},
			},
			expectedReturnCode:                      1,
			expectCheckedForConsulInstallations:     true,
			expectCheckedForConsulDemoInstallations: true,
			expectConsulInstalled:                   false,
			expectConsulDemoInstalled:               false,
		},
		"install with --dry-run flag returns success": {
			input: []string{
				"--dry-run",
			},
			messages: []string{
				"\n==> Performing dry run install. No changes will be made to the cluster.\n",
				"\n==> Checking if Consul can be installed\n ✓ No existing Consul installations found.\n ✓ No existing Consul persistent volume claims found\n ✓ No existing Consul secrets found.\n",
				"\n==> Consul Installation Summary\n    Name: consul\n    Namespace: consul\n    \n    No overrides provided, using the default Helm values.\n    Dry run complete. No changes were made to the Kubernetes cluster.\n    Installation can proceed with this configuration.\n",
			},
			helmActionsRunner:                       &helm.MockActionRunner{},
			expectedReturnCode:                      0,
			expectCheckedForConsulInstallations:     true,
			expectCheckedForConsulDemoInstallations: false,
			expectConsulInstalled:                   false,
			expectConsulDemoInstalled:               false,
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
			require.Equal(t, tc.expectConsulInstalled, mock.ConsulInstalled)
			require.Equal(t, tc.expectConsulDemoInstalled, mock.ConsulDemoInstalled)
			output := buf.String()
			for _, msg := range tc.messages {
				require.Contains(t, output, msg)
			}
		})
	}
}

func createPVC(t *testing.T, name string, namespace string, k8s kubernetes.Interface) {
	t.Helper()

	pvc := &v1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	_, err := k8s.CoreV1().PersistentVolumeClaims(namespace).Create(context.Background(), pvc, metav1.CreateOptions{})
	require.NoError(t, err)
}

func createSecret(t *testing.T, secret *v1.Secret, namespace string, k8s kubernetes.Interface) {
	t.Helper()
	_, err := k8s.CoreV1().Secrets(namespace).Create(context.Background(), secret, metav1.CreateOptions{})
	require.NoError(t, err)
}
