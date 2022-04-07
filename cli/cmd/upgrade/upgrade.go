package upgrade

import (
	"errors"
	"fmt"
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
	"helm.sh/helm/v3/pkg/action"
	helmCLI "helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/cli/values"
	"helm.sh/helm/v3/pkg/getter"
	"k8s.io/client-go/kubernetes"
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
)

type Command struct {
	*common.BaseCommand

	kubernetes kubernetes.Interface

	set *flag.Sets

	flagPreset          string
	flagDryRun          bool
	flagAutoApprove     bool
	flagValueFiles      []string
	flagSetStringValues []string
	flagSetValues       []string
	flagFileValues      []string
	flagTimeout         string
	timeoutDuration     time.Duration
	flagVerbose         bool
	flagWait            bool

	flagKubeConfig  string
	flagKubeContext string

	once sync.Once
	help string
}

func (c *Command) init() {
	// Store all the possible preset values in 'presetList'. Printed in the help message.
	var presetList []string
	for name := range config.Presets {
		presetList = append(presetList, name)
	}

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
		Usage:   fmt.Sprintf("Use an upgrade preset, one of %s. Defaults to none", strings.Join(presetList, ", ")),
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
		Name:    "kubeconfig",
		Aliases: []string{"c"},
		Target:  &c.flagKubeConfig,
		Default: "",
		Usage:   "Set the path to kubeconfig file.",
	})
	f.StringVar(&flag.StringVar{
		Name:    "context",
		Target:  &c.flagKubeContext,
		Default: "",
		Usage:   "Set the Kubernetes context to use.",
	})

	c.help = c.set.Help()

	// c.Init() calls the embedded BaseCommand's initialization function.
	c.Init()
}

func (c *Command) Run(args []string) int {
	c.once.Do(c.init)
	c.Log.ResetNamed("upgrade")

	defer common.CloseWithError(c.BaseCommand)

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
	name, namespace, err := common.CheckForInstallations(settings, uiLogger)
	if err != nil {
		c.UI.Output("Cannot upgrade Consul. Existing Consul installation not found. Use the command `consul-k8s install` to install Consul.", terminal.WithErrorStyle())
		return 1
	}
	c.UI.Output("Existing Consul installation found to be upgraded.", terminal.WithSuccessStyle())
	c.UI.Output("Name: %s\nNamespace: %s", name, namespace, terminal.WithInfoStyle())

	chart, err := helm.LoadChart(consulChart.ConsulHelmChart, common.TopLevelChartDirName)
	if err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}
	c.UI.Output("Loaded charts", terminal.WithSuccessStyle())

	currentChartValues, err := helm.FetchChartValues(namespace, name, settings, uiLogger)
	if err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}

	// Handle preset, value files, and set values logic.
	chartValues, err := c.mergeValuesFlagsWithPrecedence(settings)
	if err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}

	// Without informing the user, default global.name to consul if it hasn't been set already. We don't allow setting
	// the release name, and since that is hardcoded to "consul", setting global.name to "consul" makes it so resources
	// aren't double prefixed with "consul-consul-...".
	chartValues = common.MergeMaps(config.Convert(config.GlobalNameConsul), chartValues)

	// Print out the upgrade summary.
	if err = c.printDiff(currentChartValues, chartValues); err != nil {
		c.UI.Output("Could not print the different between current and upgraded charts: %v", err, terminal.WithErrorStyle())
		return 1
	}

	// Check if the user is OK with the upgrade unless the auto approve or dry run flags are true.
	if !c.flagAutoApprove && !c.flagDryRun {
		confirmation, err := c.UI.Input(&terminal.Input{
			Prompt: "Proceed with upgrade? (y/N)",
			Style:  terminal.InfoStyle,
			Secret: false,
		})

		if err != nil {
			c.UI.Output(err.Error(), terminal.WithErrorStyle())
			return 1
		}
		if common.Abort(confirmation) {
			c.UI.Output("Upgrade aborted. Use the command `consul-k8s upgrade -help` to learn how to customize your upgrade.",
				terminal.WithInfoStyle())
			return 1
		}
	}

	if !c.flagDryRun {
		c.UI.Output("Upgrading Consul", terminal.WithHeaderStyle())
	} else {
		c.UI.Output("Performing Dry Run Upgrade", terminal.WithHeaderStyle())
	}

	// Setup action configuration for Helm Go SDK function calls.
	actionConfig := new(action.Configuration)
	actionConfig, err = helm.InitActionConfig(actionConfig, namespace, settings, uiLogger)
	if err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}

	// Setup the upgrade action.
	upgrade := action.NewUpgrade(actionConfig)
	upgrade.Namespace = namespace
	upgrade.DryRun = c.flagDryRun
	upgrade.Wait = c.flagWait
	upgrade.Timeout = c.timeoutDuration

	// Run the upgrade. Note that the dry run config is passed into the upgrade action, so upgrade.Run is called even during a dry run.
	_, err = upgrade.Run(common.DefaultReleaseName, chart, chartValues)
	if err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}

	if c.flagDryRun {
		c.UI.Output("Dry run complete. No changes were made to the Kubernetes cluster.\n"+
			"Upgrade can proceed with this configuration.", terminal.WithInfoStyle())
		return 0
	}

	c.UI.Output("Consul upgraded in namespace %q.", namespace, terminal.WithSuccessStyle())
	return 0
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
	if _, ok := config.Presets[c.flagPreset]; c.flagPreset != defaultPreset && !ok {
		return fmt.Errorf("'%s' is not a valid preset", c.flagPreset)
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
func (c *Command) mergeValuesFlagsWithPrecedence(settings *helmCLI.EnvSettings) (map[string]interface{}, error) {
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
		presetMap := config.Presets[c.flagPreset].(map[string]interface{})
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

// printDiff marshals both maps to YAML and prints the diff between the two.
func (c *Command) printDiff(old, new map[string]interface{}) error {
	diff, err := common.Diff(old, new)
	if err != nil {
		return err
	}

	c.UI.Output("\nDifference between user overrides for current and upgraded charts"+
		"\n--------------------------------------------------------------", terminal.WithInfoStyle())
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "+") {
			c.UI.Output(line, terminal.WithDiffAddedStyle())
		} else if strings.HasPrefix(line, "-") {
			c.UI.Output(line, terminal.WithDiffRemovedStyle())
		} else {
			c.UI.Output(line, terminal.WithDiffUnchangedStyle())
		}
	}

	return nil
}
