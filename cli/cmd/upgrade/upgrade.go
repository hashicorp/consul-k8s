// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package upgrade

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	consulChart "github.com/hashicorp/consul-k8s/charts"
	"github.com/hashicorp/consul-k8s/cli/common"
	"github.com/hashicorp/consul-k8s/cli/common/flag"
	"github.com/hashicorp/consul-k8s/cli/common/terminal"
	"github.com/hashicorp/consul-k8s/cli/config"
	"github.com/hashicorp/consul-k8s/cli/helm"
	"github.com/hashicorp/consul-k8s/cli/preset"
	"github.com/posener/complete"
	helmCLI "helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/cli/values"
	"helm.sh/helm/v3/pkg/getter"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/strings/slices"
)

const (
	flagNamePreset = "preset"
	defaultPreset  = ""

	flagNameConfigFile      = "config-file"
	flagNameSetStringValues = "set-string"
	flagNameSetValues       = "set"
	flagNameFileValues      = "set-file"

	flagNameDryRun = "dry-run"
	defaultDryRun  = false

	flagNameAutoApprove = "auto-approve"
	defaultAutoApprove  = false

	flagNameTimeout = "timeout"
	defaultTimeout  = "10m"

	flagNameVerbose = "verbose"
	defaultVerbose  = false

	flagNameWait = "wait"
	defaultWait  = true

	flagNameContext    = "context"
	flagNameKubeconfig = "kubeconfig"

	flagNameDemo = "demo"
	defaultDemo  = false

	flagNameHCPResourceID = "hcp-resource-id"

	consulDemoChartPath = "demo"
)

type Command struct {
	*common.BaseCommand

	helmActionsRunner helm.HelmActionsRunner

	kubernetes kubernetes.Interface

	httpClient *http.Client

	set *flag.Sets

	flagPreset            string
	flagDryRun            bool
	flagAutoApprove       bool
	flagValueFiles        []string
	flagSetStringValues   []string
	flagSetValues         []string
	flagFileValues        []string
	flagTimeout           string
	timeoutDuration       time.Duration
	flagVerbose           bool
	flagWait              bool
	flagNameHCPResourceID string
	flagDemo              bool

	flagKubeConfig  string
	flagKubeContext string

	once sync.Once
	help string
}

func (c *Command) init() {
	c.set = flag.NewSets()
	f := c.set.NewSet("Command Options")
	f.BoolVar(&flag.BoolVar{
		Name:    flagNameAutoApprove,
		Target:  &c.flagAutoApprove,
		Default: defaultAutoApprove,
		Usage:   "Skip confirmation prompt.",
	})
	f.BoolVar(&flag.BoolVar{
		Name:    flagNameDryRun,
		Target:  &c.flagDryRun,
		Default: defaultDryRun,
		Usage:   "Perform pre-upgrade checks and display summary of upgrade.",
	})
	f.StringSliceVar(&flag.StringSliceVar{
		Name:    flagNameConfigFile,
		Aliases: []string{"f"},
		Target:  &c.flagValueFiles,
		Usage:   "Set the path to a file to customize the upgrade, such as Consul Helm chart values file. Can be specified multiple times.",
	})
	f.StringVar(&flag.StringVar{
		Name:    flagNamePreset,
		Target:  &c.flagPreset,
		Default: defaultPreset,
		Usage:   fmt.Sprintf("Use an upgrade preset, one of %s. Defaults to none", strings.Join(preset.Presets, ", ")),
	})
	f.StringSliceVar(&flag.StringSliceVar{
		Name:   flagNameSetValues,
		Target: &c.flagSetValues,
		Usage:  "Set a value to customize. Can be specified multiple times. Supports Consul Helm chart values.",
	})
	f.StringSliceVar(&flag.StringSliceVar{
		Name:   flagNameFileValues,
		Target: &c.flagFileValues,
		Usage: "Set a value to customize using a file. The contents of the file will be set as the value." +
			"Can be specified multiple times. Supports Consul Helm chart values.",
	})
	f.StringSliceVar(&flag.StringSliceVar{
		Name:   flagNameSetStringValues,
		Target: &c.flagSetStringValues,
		Usage:  "Set a string value to customize. Can be specified multiple times. Supports Consul Helm chart values.",
	})
	f.StringVar(&flag.StringVar{
		Name:    flagNameTimeout,
		Target:  &c.flagTimeout,
		Default: defaultTimeout,
		Usage:   "Set a timeout to wait for upgrade to be ready.",
	})
	f.BoolVar(&flag.BoolVar{
		Name:    flagNameVerbose,
		Aliases: []string{"v"},
		Target:  &c.flagVerbose,
		Default: defaultVerbose,
		Usage:   "Output verbose logs from the command with the status of resources being upgraded.",
	})
	f.BoolVar(&flag.BoolVar{
		Name:    flagNameWait,
		Target:  &c.flagWait,
		Default: defaultWait,
		Usage:   "Wait for Kubernetes resources in upgrade to be ready before exiting command.",
	})

	f = c.set.NewSet("Global Options")
	f.StringVar(&flag.StringVar{
		Name:    flagNameKubeconfig,
		Aliases: []string{"c"},
		Target:  &c.flagKubeConfig,
		Default: "",
		Usage:   "Set the path to kubeconfig file.",
	})
	f.StringVar(&flag.StringVar{
		Name:    flagNameContext,
		Target:  &c.flagKubeContext,
		Default: "",
		Usage:   "Set the Kubernetes context to use.",
	})
	f.StringVar(&flag.StringVar{
		Name:    flagNameHCPResourceID,
		Target:  &c.flagNameHCPResourceID,
		Default: "",
		Usage:   "Set the HCP resource_id when using the 'cloud' preset.",
	})
	f.BoolVar(&flag.BoolVar{
		Name:    flagNameDemo,
		Target:  &c.flagDemo,
		Default: defaultDemo,
		Usage: fmt.Sprintf("Install %s immediately after installing %s.",
			common.ReleaseTypeConsulDemo, common.ReleaseTypeConsul),
	})

	c.help = c.set.Help()
}

func (c *Command) Run(args []string) int {
	c.once.Do(c.init)
	c.Log.ResetNamed("upgrade")

	defer common.CloseWithError(c.BaseCommand)

	if c.helmActionsRunner == nil {
		c.helmActionsRunner = &helm.ActionRunner{}
	}

	err := c.validateFlags(args)
	if err != nil {
		c.UI.Output(err.Error())
		return 1
	}

	if c.flagDryRun {
		c.UI.Output("Performing dry run upgrade. No changes will be made to the cluster.", terminal.WithInfoStyle())
	}

	c.timeoutDuration, err = time.ParseDuration(c.flagTimeout)
	if err != nil {
		c.UI.Output(fmt.Sprintf("Invalid timeout: %s", err))
		return 1
	}

	// helmCLI.New() will create a settings object which is used by the Helm Go SDK calls.
	settings := helmCLI.New()

	// Any overrides by our kubeconfig and kubecontext flags is done here. The Kube client that
	// is created will use this command's flags first, then the HELM_KUBECONTEXT environment variable,
	// then call out to genericclioptions.ConfigFlag
	if c.flagKubeConfig != "" {
		settings.KubeConfig = c.flagKubeConfig
	}
	if c.flagKubeContext != "" {
		settings.KubeContext = c.flagKubeContext
	}

	// Set up the kubernetes client to use for non Helm SDK calls to the Kubernetes API
	// The Helm SDK will use settings.RESTClientGetter for its calls as well, so this will
	// use a consistent method to target the right cluster for both Helm SDK and non Helm SDK calls.
	if c.kubernetes == nil {
		restConfig, err := settings.RESTClientGetter().ToRESTConfig()
		if err != nil {
			c.UI.Output("Error retrieving Kubernetes authentication:\n%v", err, terminal.WithErrorStyle())
			return 1
		}
		c.kubernetes, err = kubernetes.NewForConfig(restConfig)
		if err != nil {
			c.UI.Output("Error initializing Kubernetes client:\n%v", err, terminal.WithErrorStyle())
			return 1
		}
	}

	c.UI.Output("Checking if Consul can be upgraded", terminal.WithHeaderStyle())
	uiLogger := c.createUILogger()
	found, consulName, consulNamespace, err := c.helmActionsRunner.CheckForInstallations(&helm.CheckForInstallationsOptions{
		Settings:    settings,
		ReleaseName: common.DefaultReleaseName,
		DebugLog:    uiLogger,
	})

	if err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}
	if !found {
		c.UI.Output("Cannot upgrade Consul. Existing Consul installation not found. Use the command `consul-k8s install` to install Consul.", terminal.WithErrorStyle())
		return 1
	} else {
		c.UI.Output("Existing %s installation found to be upgraded.", common.ReleaseTypeConsul, terminal.WithSuccessStyle())
		c.UI.Output("Name: %s\nNamespace: %s", consulName, consulNamespace, terminal.WithInfoStyle())
	}

	c.UI.Output(fmt.Sprintf("Checking if %s can be upgraded", common.ReleaseTypeConsulDemo), terminal.WithHeaderStyle())
	// Ensure there is not an existing Consul demo installation which would cause a conflict.
	foundDemo, demoName, demoNamespace, _ := c.helmActionsRunner.CheckForInstallations(&helm.CheckForInstallationsOptions{
		Settings:    settings,
		ReleaseName: common.ConsulDemoAppReleaseName,
		DebugLog:    uiLogger,
	})
	if foundDemo {
		c.UI.Output("Existing %s installation found to be upgraded.", common.ReleaseTypeConsulDemo, terminal.WithSuccessStyle())
		c.UI.Output("Name: %s\nNamespace: %s", demoName, demoNamespace, terminal.WithInfoStyle())
	} else {
		if c.flagDemo {
			c.UI.Output("No existing %s installation found, but -demo flag provided. %s will be installed in namespace %s.",
				common.ConsulDemoAppReleaseName, common.ConsulDemoAppReleaseName, consulNamespace, terminal.WithInfoStyle())
		} else {
			c.UI.Output("No existing %s installation found.", common.ReleaseTypeConsulDemo, terminal.WithInfoStyle())
		}
	}

	// Handle preset, value files, and set values logic.
	chartValues, err := c.mergeValuesFlagsWithPrecedence(settings, consulNamespace)
	if err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}

	// Without informing the user, default global.name to consul if it hasn't been set already. We don't allow setting
	// the release name, and since that is hardcoded to "consul", setting global.name to "consul" makes it so resources
	// aren't double prefixed with "consul-consul-...".
	chartValues = common.MergeMaps(config.ConvertToMap(config.GlobalNameConsul), chartValues)

	timeout, err := time.ParseDuration(c.flagTimeout)
	if err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}
	options := &helm.UpgradeOptions{
		ReleaseName:       consulName,
		ReleaseType:       common.ReleaseTypeConsul,
		ReleaseTypeName:   common.ReleaseTypeConsul,
		Namespace:         consulNamespace,
		Values:            chartValues,
		Settings:          settings,
		EmbeddedChart:     consulChart.ConsulHelmChart,
		ChartDirName:      common.TopLevelChartDirName,
		UILogger:          uiLogger,
		DryRun:            c.flagDryRun,
		AutoApprove:       c.flagAutoApprove,
		Wait:              c.flagWait,
		Timeout:           timeout,
		UI:                c.UI,
		HelmActionsRunner: c.helmActionsRunner,
	}

	err = helm.UpgradeHelmRelease(options)
	if err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}

	timeout, err = time.ParseDuration(c.flagTimeout)
	if err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}

	if foundDemo {
		options := &helm.UpgradeOptions{
			ReleaseName:       demoName,
			ReleaseType:       common.ReleaseTypeConsulDemo,
			ReleaseTypeName:   common.ConsulDemoAppReleaseName,
			Namespace:         demoNamespace,
			Values:            make(map[string]interface{}),
			Settings:          settings,
			EmbeddedChart:     consulChart.DemoHelmChart,
			ChartDirName:      consulDemoChartPath,
			UILogger:          uiLogger,
			DryRun:            c.flagDryRun,
			AutoApprove:       c.flagAutoApprove,
			Wait:              c.flagWait,
			Timeout:           timeout,
			UI:                c.UI,
			HelmActionsRunner: c.helmActionsRunner,
		}

		err = helm.UpgradeHelmRelease(options)
		if err != nil {
			c.UI.Output(err.Error(), terminal.WithErrorStyle())
			return 1
		}
	} else if c.flagDemo {

		options := &helm.InstallOptions{
			ReleaseName:       common.ConsulDemoAppReleaseName,
			ReleaseType:       common.ReleaseTypeConsulDemo,
			Namespace:         settings.Namespace(),
			Values:            make(map[string]interface{}),
			Settings:          settings,
			EmbeddedChart:     consulChart.DemoHelmChart,
			ChartDirName:      consulDemoChartPath,
			UILogger:          uiLogger,
			DryRun:            c.flagDryRun,
			AutoApprove:       c.flagAutoApprove,
			Wait:              c.flagWait,
			Timeout:           timeout,
			UI:                c.UI,
			HelmActionsRunner: c.helmActionsRunner,
		}
		err = helm.InstallDemoApp(options)
		if err != nil {
			c.UI.Output(err.Error(), terminal.WithErrorStyle())
			return 1
		}
	}

	if c.flagDryRun {
		c.UI.Output("Dry run complete. No changes were made to the Kubernetes cluster.\n"+
			"Upgrade can proceed with this configuration.", terminal.WithInfoStyle())
		return 0
	}
	return 0
}

// AutocompleteFlags returns a mapping of supported flags and autocomplete
// options for this command. The map key for the Flags map should be the
// complete flag such as "-foo" or "--foo".
func (c *Command) AutocompleteFlags() complete.Flags {
	return complete.Flags{
		fmt.Sprintf("-%s", flagNamePreset):          complete.PredictNothing,
		fmt.Sprintf("-%s", flagNameConfigFile):      complete.PredictFiles("*"),
		fmt.Sprintf("-%s", flagNameSetStringValues): complete.PredictNothing,
		fmt.Sprintf("-%s", flagNameSetValues):       complete.PredictNothing,
		fmt.Sprintf("-%s", flagNameFileValues):      complete.PredictFiles("*"),
		fmt.Sprintf("-%s", flagNameDryRun):          complete.PredictNothing,
		fmt.Sprintf("-%s", flagNameAutoApprove):     complete.PredictNothing,
		fmt.Sprintf("-%s", flagNameTimeout):         complete.PredictNothing,
		fmt.Sprintf("-%s", flagNameVerbose):         complete.PredictNothing,
		fmt.Sprintf("-%s", flagNameWait):            complete.PredictNothing,
		fmt.Sprintf("-%s", flagNameContext):         complete.PredictNothing,
		fmt.Sprintf("-%s", flagNameKubeconfig):      complete.PredictFiles("*"),
		fmt.Sprintf("-%s", flagNameDemo):            complete.PredictNothing,
		fmt.Sprintf("-%s", flagNameHCPResourceID):   complete.PredictNothing,
	}
}

// AutocompleteArgs returns the argument predictor for this command.
// Since argument completion is not supported, this will return
// complete.PredictNothing.
func (c *Command) AutocompleteArgs() complete.Predictor {
	return complete.PredictNothing
}

// validateFlags checks that the user's provided flags are valid.
func (c *Command) validateFlags(args []string) error {
	if err := c.set.Parse(args); err != nil {
		return err
	}
	if len(c.set.Args()) > 0 {
		return errors.New("should have no non-flag arguments")
	}
	if len(c.flagValueFiles) != 0 && c.flagPreset != defaultPreset {
		return fmt.Errorf("cannot set both -%s and -%s", flagNameConfigFile, flagNamePreset)
	}
	if ok := slices.Contains(preset.Presets, c.flagPreset); c.flagPreset != defaultPreset && !ok {
		return fmt.Errorf("'%s' is not a valid preset (valid presets: %s)", c.flagPreset, strings.Join(preset.Presets, ", "))
	}
	if _, err := time.ParseDuration(c.flagTimeout); err != nil {
		return fmt.Errorf("unable to parse -%s: %s", flagNameTimeout, err)
	}
	if len(c.flagValueFiles) != 0 {
		for _, filename := range c.flagValueFiles {
			if _, err := os.Stat(filename); err != nil && os.IsNotExist(err) {
				return fmt.Errorf("file '%s' does not exist", filename)
			}
		}
	}

	if c.flagPreset == preset.PresetCloud {
		clientID := os.Getenv(preset.EnvHCPClientID)
		clientSecret := os.Getenv(preset.EnvHCPClientSecret)
		if clientID == "" {
			return fmt.Errorf("When '%s' is specified as the preset, the '%s' environment variable must also be set", preset.PresetCloud, preset.EnvHCPClientID)
		} else if clientSecret == "" {
			return fmt.Errorf("When '%s' is specified as the preset, the '%s' environment variable must also be set", preset.PresetCloud, preset.EnvHCPClientSecret)
		} else if c.flagNameHCPResourceID == "" {
			return fmt.Errorf("When '%s' is specified as the preset, the '%s' flag must also be provided", preset.PresetCloud, flagNameHCPResourceID)
		}
	} else if c.flagNameHCPResourceID != "" {
		return fmt.Errorf("The '%s' flag can only be used with the '%s' preset", flagNameHCPResourceID, preset.PresetCloud)
	}

	return nil
}

// mergeValuesFlagsWithPrecedence is responsible for merging all the values to determine the values file for the
// upgrade based on the following precedence order from lowest to highest:
// 1. -preset
// 2. -f values-file
// 3. -set
// 4. -set-string
// 5. -set-file
// For example, -set-file will override a value provided via -set.
// Within each of these groups the rightmost flag value has the highest precedence.
func (c *Command) mergeValuesFlagsWithPrecedence(settings *helmCLI.EnvSettings, namespace string) (map[string]interface{}, error) {
	p := getter.All(settings)
	v := &values.Options{
		ValueFiles:   c.flagValueFiles,
		StringValues: c.flagSetStringValues,
		Values:       c.flagSetValues,
		FileValues:   c.flagFileValues,
	}
	vals, err := v.MergeValues(p)
	if err != nil {
		return nil, fmt.Errorf("error merging values: %s", err)
	}
	if c.flagPreset != defaultPreset {
		// Note the ordering of the function call, presets have lower precedence than set vals.
		p, err := c.getPreset(c.flagPreset, namespace)
		if err != nil {
			return nil, fmt.Errorf("error getting preset provider: %s", err)
		}
		presetMap, err := p.GetValueMap()
		if err != nil {
			return nil, fmt.Errorf("error getting preset values: %s", err)
		}
		vals = common.MergeMaps(presetMap, vals)
	}
	return vals, err
}

// Help returns a description of the command and how it is used.
func (c *Command) Help() string {
	c.once.Do(c.init)
	return c.Synopsis() + "\n\nUsage: consul-k8s upgrade [flags]\n\n" + c.help
}

// Synopsis returns a one-line command summary.
func (c *Command) Synopsis() string {
	return "Upgrade Consul on Kubernetes from an existing installation."
}

// createUILogger creates a logger that will write to the UI.
func (c *Command) createUILogger() func(string, ...interface{}) {
	return func(s string, args ...interface{}) {
		logMsg := fmt.Sprintf(s, args...)

		if c.flagVerbose {
			// Only output all logs when verbose is enabled
			c.UI.Output(logMsg, terminal.WithLibraryStyle())
		} else {
			// When verbose is not enabled, output all logs except not ready messages for resources
			if !strings.Contains(logMsg, "not ready") {
				c.UI.Output(logMsg, terminal.WithLibraryStyle())
			}
		}
	}
}

// getPreset is a factory function that, given a string, produces a struct that
// implements the Preset interface.  If the string is not recognized an error is
// returned.
func (c *Command) getPreset(name string, namespace string) (preset.Preset, error) {
	hcpConfig := preset.GetHCPPresetFromEnv(c.flagNameHCPResourceID)
	getPresetConfig := &preset.GetPresetConfig{
		Name: name,
		CloudPreset: &preset.CloudPreset{
			KubernetesClient:    c.kubernetes,
			KubernetesNamespace: namespace,
			SkipSavingSecrets:   true,
			UI:                  c.UI,
			HTTPClient:          c.httpClient,
			HCPConfig:           hcpConfig,
			Context:             c.Ctx,
		},
	}
	return preset.GetPreset(getPresetConfig)
}
