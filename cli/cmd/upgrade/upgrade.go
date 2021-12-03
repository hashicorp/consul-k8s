package upgrade

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"time"

	consulChart "github.com/hashicorp/consul-k8s/charts"
	"github.com/hashicorp/consul-k8s/cli/cmd/common"
	"github.com/hashicorp/consul-k8s/cli/cmd/common/flag"
	"github.com/hashicorp/consul-k8s/cli/cmd/common/terminal"
	"github.com/hashicorp/consul-k8s/cli/config"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	helmCLI "helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/cli/values"
	"helm.sh/helm/v3/pkg/getter"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"
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
	// action/upgrade
	// --install, --reset-values vs --reuse-values,
	// atomic
)

type Command struct {
	*common.BaseCommand

	kubernetes kubernetes.Interface

	set *flag.Sets

	flagPreset          string
	flagNamespace       string
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
	// TODO: I removed the releaseName flag, I think we might need to put it back
	// TODO: and give it as argument to the run of status.
	c.set = flag.NewSets()
	f := c.set.NewSet("Command Options")
	f.BoolVar(&flag.BoolVar{
		Name:    flagNameAutoApprove,
		Target:  &c.flagAutoApprove,
		Default: defaultAutoApprove,
		Usage:   "Skip confirmation prompt.",
	})
	f.StringVar(&flag.StringVar{
		Name:    flagNameNamespace,
		Target:  &c.flagNamespace,
		Default: defaultNamespace,
		Usage:   "Namespace where Consul is installed. Defaults to 'consul'.",
	})
	f.BoolVar(&flag.BoolVar{
		Name:    flagNameDryRun,
		Target:  &c.flagDryRun,
		Default: defaultDryRun,
		Usage:   "Run pre-upgrade checks and display summary of upgrade.",
	})
	f.StringSliceVar(&flag.StringSliceVar{
		Name:    flagNameConfigFile,
		Aliases: []string{"f"},
		Target:  &c.flagValueFiles,
		Usage:   "Path to a file to customize the upgrade, such as Consul Helm chart values file. Can be specified multiple times.",
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
		Usage: "Set a value to customize via a file. The contents of the file will be set as the value. Can be " +
			"specified multiple times. Supports Consul Helm chart values.",
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
		Usage:   "Timeout to wait for upgrade to be ready.",
	})
	f.BoolVar(&flag.BoolVar{
		Name:    flagNameVerbose,
		Aliases: []string{"v"},
		Target:  &c.flagVerbose,
		Default: defaultVerbose,
		Usage:   "Output verbose logs from the upgrade command with the status of resources being upgraded.",
	})
	f.BoolVar(&flag.BoolVar{
		Name:    flagNameWait,
		Target:  &c.flagWait,
		Default: defaultWait,
		Usage:   "Determines whether to wait for resources in upgrade to be ready before exiting command.",
	})

	f = c.set.NewSet("Global Options")
	f.StringVar(&flag.StringVar{
		Name:    "kubeconfig",
		Aliases: []string{"c"},
		Target:  &c.flagKubeConfig,
		Default: "",
		Usage:   "Path to kubeconfig file.",
	})
	f.StringVar(&flag.StringVar{
		Name:    "context",
		Target:  &c.flagKubeContext,
		Default: "",
		Usage:   "Kubernetes context to use.",
	})

	c.help = c.set.Help()

	// c.Init() calls the embedded BaseCommand's initialization function.
	c.Init()
}

func (c *Command) Run(args []string) int {
	c.once.Do(c.init)

	// The logger is initialized in main with the name cli. Here, we reset the name to install so log lines would be prefixed with install.
	c.Log.ResetNamed("upgrade")

	defer common.CloseWithError(c.BaseCommand)

	if err := c.validateFlags(args); err != nil {
		c.UI.Output(err.Error())
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

	// Setup logger to stream Helm library logs
	var uiLogger = func(s string, args ...interface{}) {
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

	// Set up the kubernetes client to use for non Helm SDK calls to the Kubernetes API
	// The Helm SDK will use settings.RESTClientGetter for its calls as well, so this will
	// use a consistent method to target the right cluster for both Helm SDK and non Helm SDK calls.
	if c.kubernetes == nil {
		restConfig, err := settings.RESTClientGetter().ToRESTConfig()
		if err != nil {
			c.UI.Output("Retrieving Kubernetes auth: %v", err, terminal.WithErrorStyle())
			return 1
		}
		c.kubernetes, err = kubernetes.NewForConfig(restConfig)
		if err != nil {
			c.UI.Output("Initializing Kubernetes client: %v", err, terminal.WithErrorStyle())
			return 1
		}
	}

	c.UI.Output("Pre-Upgrade Checks", terminal.WithHeaderStyle())

	name, namespace, err := common.CheckForInstallations(settings, uiLogger)
	if err != nil {
		c.UI.Output("Could not find existing Consul installation. Run 'consul-k8s install' to create one.")
		return 1
	} else {
		c.UI.Output("Existing installation found to be upgraded.", terminal.WithSuccessStyle())
		c.UI.Output("Name: %s", name, terminal.WithInfoStyle())
		c.UI.Output("Namespace: %s", ns, terminal.WithInfoStyle())
	}

	c.UI.Output("Existing installation found.", terminal.WithSuccessStyle())
	c.UI.Output("Name: %s\nNamespace: %s", name, namespace, terminal.WithInfoStyle())

	chart, err := common.LoadChart(consulChart.ConsulHelmChart, common.TopLevelChartDirName)
	if err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}

	// Create a *chart.Chart object from the files to run the installation from.
	chart, err := loader.LoadFiles(chartFiles)
	if err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}
	c.UI.Output("Downloaded charts", terminal.WithSuccessStyle())

	// Run a status to get the current chart values.
	statusConfig := new(action.Configuration)
	statusConfig, err = common.InitActionConfig(statusConfig, namespace, settings, uiLogger)
	if err != nil {
		return 1
	}

	statuser := action.NewStatus(statusConfig)
	_, err = statuser.Run(common.DefaultReleaseNamespace)
	if err != nil {
		return 1
	}
	c.UI.Output("Ran a `helm status` to gather current Chart values.", terminal.WithSuccessStyle())

	// Handle preset, value files, and set values logic.
	vals, err := c.mergeValuesFlagsWithPrecedence(settings)
	if err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}
	// TODO: We don't need this anymore.
	_, err = yaml.Marshal(vals)
	if err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}
	c.UI.Output("Succeeded in parsing CLI set values.")

	// Without informing the user, default global.name to consul if it hasn't been set already. We don't allow setting
	// the release name, and since that is hardcoded to "consul", setting global.name to "consul" makes it so resources
	// aren't double prefixed with "consul-consul-...".
	vals = common.MergeMaps(config.Convert(config.GlobalNameConsul), vals)

	// Print out the upgrade summary.
	if !c.flagAutoApprove {
		c.UI.Output("Consul Upgrade Summary", terminal.WithHeaderStyle())
		c.UI.Output("Installation name: %s", common.DefaultReleaseName, terminal.WithInfoStyle())
		c.UI.Output("Namespace: %s", namespace, terminal.WithInfoStyle())

		// Coalesce the user provided over-rides with the chart. This is basically what `upgrade` does anyways.
		// newChart := common.MergeMaps(chart.Values, vals)
	}

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
			c.UI.Output("Upgrade aborted. To learn how to customize your upgrade, run:\nconsul-k8s upgrade --help", terminal.WithInfoStyle())
			return 1
		}
	}

	if !c.flagDryRun {
		c.UI.Output("Running Upgrade", terminal.WithHeaderStyle())
	} else {
		c.UI.Output("Performing Dry Run Upgrade", terminal.WithHeaderStyle())
	}

	// Setup action configuration for Helm Go SDK function calls.
	actionConfig := new(action.Configuration)
	actionConfig, err = common.InitActionConfig(actionConfig, namespace, settings, uiLogger)
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

	// Read the embedded chart files into []*loader.BufferedFile.
	chartFiles, err := common.ReadChartFiles(consulChart.ConsulHelmChart, common.TopLevelChartDirName)
	if err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}

	// Create a *chart.Chart object from the files to run the upgrade from.
	chart, err := loader.LoadFiles(chartFiles)
	if err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}
	c.UI.Output("Loaded charts", terminal.WithSuccessStyle())

	// Run the upgrade. Note that the dry run config is passed into the upgrade action, so upgrade.Run is called even during a dry run.
	re, err := upgrade.Run(common.DefaultReleaseName, chart, vals)
	if err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}

	// Dry Run should exit here, printing the release's config.
	if c.flagDryRun {
		c.UI.Output("Dry run complete - upgrade can proceed.", terminal.WithInfoStyle())

		configYaml, err := yaml.Marshal(re.Config)
		if err != nil {
			c.UI.Output(err.Error(), terminal.WithErrorStyle())
			return 1
		}

		if len(re.Config) == 0 {
			c.UI.Output("Config: "+string(configYaml), terminal.WithInfoStyle())
		} else {
			c.UI.Output("Config:"+"\n"+string(configYaml), terminal.WithInfoStyle())
		}

		return 0
	}

	c.UI.Output("Upgraded Consul into namespace %q", namespace, terminal.WithSuccessStyle())

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
	if !common.IsValidLabel(c.flagNamespace) {
		return fmt.Errorf("'%s' is an invalid namespace. Namespaces follow the RFC 1123 label convention and must "+
			"consist of a lower case alphanumeric character or '-' and must start/end with an alphanumeric character", c.flagNamespace)
	}
	duration, err := time.ParseDuration(c.flagTimeout)
	if err != nil {
		return fmt.Errorf("unable to parse -%s: %s", flagNameTimeout, err)
	}
	c.timeoutDuration = duration
	if len(c.flagValueFiles) != 0 {
		for _, filename := range c.flagValueFiles {
			if _, err := os.Stat(filename); err != nil && os.IsNotExist(err) {
				return fmt.Errorf("File '%s' does not exist.", filename)
			}
		}
	}

	if c.flagDryRun {
		c.UI.Output("Performing dry run upgrade.", terminal.WithInfoStyle())
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

// Help returns a description of this command and how it can be used.
func (c *Command) Help() string {
	c.once.Do(c.init)
	return fmt.Sprintf("Usage: consul-k8s upgrade [flags]\n%s\n\n%s", c.Synopsis(), c.help)
}

// Synopsis returns a short string describing the command.
func (c *Command) Synopsis() string {
	return "Upgrade Consul on Kubernetes from an existing installation."
}

// MapDiff takes in two map to string interfaces and returns a list of differences.
func MapDiff(a, b map[string]interface{}) (map[string]interface{}, map[string]interface{}) {
	from, to := map[string]interface{}{}, map[string]interface{}{}

	for aKey, aVal := range a {
		//if aKey == "global" {
		//	fmt.Println("Break here!")
		//} else if aKey == "name" {
		//	fmt.Println("ok like really break here lmao")
		//}
		bVal, matchingKeys := b[aKey]
		if matchingKeys {
			// Spotted a difference!
			if !reflect.DeepEqual(aVal, bVal) {
				// Determine types of the interface{}
				aIsMap := false

				switch aVal.(type) {
				case map[string]interface{}:
					aIsMap = true
				}

				bIsMap := false

				switch bVal.(type) {
				case map[string]interface{}:
					bIsMap = true
				}

				if !aIsMap && !bIsMap {
					from[aKey] = aVal
					to[aKey] = bVal
				} else {
					from[aKey], to[aKey] = MapDiff(aVal.(map[string]interface{}), bVal.(map[string]interface{}))
				}
			}
		} else {
			from[aKey] = aVal
			to[aKey] = nil
		}
	}

	// We also need to iterate through b, in case it has keys not present in a.
	for bKey, bVal := range b {
		_, matchingKeys := a[bKey]
		if !matchingKeys {
			from[bKey] = nil
			to[bKey] = bVal
		}
	}

	return from, to
}
