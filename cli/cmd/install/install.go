package install

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/consul-k8s/cli/action"
	"github.com/hashicorp/consul-k8s/cli/common"
	"github.com/hashicorp/consul-k8s/cli/common/flag"
	"github.com/hashicorp/consul-k8s/cli/common/terminal"
	"github.com/hashicorp/consul-k8s/cli/config"
	helmCLI "helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/cli/values"
	"helm.sh/helm/v3/pkg/getter"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
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

	flagNameNamespace = "namespace"

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

	// Command options
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

	// Global options
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
		Usage:   "Perform pre-install checks and display a summary of the installation.",
	})
	f.StringSliceVar(&flag.StringSliceVar{
		Name:    flagNameConfigFile,
		Aliases: []string{"f"},
		Target:  &c.flagValueFiles,
		Usage:   "Set the path to a file to customize the installation, such as Consul Helm chart values file. Can be specified multiple times.",
	})
	f.StringVar(&flag.StringVar{
		Name:    flagNameNamespace,
		Target:  &c.flagNamespace,
		Default: common.DefaultReleaseNamespace,
		Usage:   "Set the namespace for the Consul installation.",
	})
	f.StringVar(&flag.StringVar{
		Name:    flagNamePreset,
		Target:  &c.flagPreset,
		Default: defaultPreset,
		Usage:   fmt.Sprintf("Use an installation preset, one of %s. Defaults to none", strings.Join(presetList, ", ")),
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
		Usage:   "Set a timeout to wait for installation to be ready.",
	})
	f.BoolVar(&flag.BoolVar{
		Name:    flagNameVerbose,
		Aliases: []string{"v"},
		Target:  &c.flagVerbose,
		Default: defaultVerbose,
		Usage:   "Output verbose logs from the command with the status of resources being installed.",
	})
	f.BoolVar(&flag.BoolVar{
		Name:    flagNameWait,
		Target:  &c.flagWait,
		Default: defaultWait,
		Usage:   "Wait for Kubernetes resources in installation to be ready before exiting command.",
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

	// Call the embedded BaseCommand's initialization function.
	c.Init()
}

// TODO move this to a common location
type helmValues struct {
	Global globalValues `yaml:"global"`
}

type globalValues struct {
	EnterpriseLicense enterpriseLicense `yaml:"enterpriseLicense"`
}

type enterpriseLicense struct {
	SecretName string `yaml:"secretName"`
	SecretKey  string `yaml:"secretKey"`
}

// Run installs Consul into a Kubernetes cluster.
func (c *Command) Run(args []string) int {
	c.once.Do(c.init)
	c.Log.ResetNamed("install")
	defer common.CloseWithError(c.BaseCommand)

	if err := c.validateFlags(args); err != nil {
		c.UI.Output(err.Error())
		return 1
	}

	helmValues, err := c.helmValues()
	if err != nil {
		c.UI.Output(err.Error())
		return 1
	}

	// Print out the installation summary.
	if !c.flagAutoApprove {
		c.UI.Output("Consul Installation Summary", terminal.WithHeaderStyle())
		c.UI.Output("Name: %s", common.DefaultReleaseName, terminal.WithInfoStyle())
		c.UI.Output("Namespace: %s", c.flagNamespace, terminal.WithInfoStyle())

		if len(helmValues) == 0 {
			c.UI.Output("\nNo overrides provided, using the default Helm values.", terminal.WithInfoStyle())
		} else {
			// TODO implement
			var val string
			c.UI.Output("\nHelm value overrides\n-------------------\n"+string(val), terminal.WithInfoStyle())
		}
	}

	// Configure the installation.
	install := action.Install{
		Namespace:     c.flagNamespace,
		Configuration: helmValues,
		KubeContext:   c.flagKubeContext,
		KubeConfig:    c.flagKubeConfig,
	}

	// Perform the installation or dry run.
	if c.flagDryRun {
		c.UI.Output("Performing dry run install. No changes will be made to the cluster.", terminal.WithHeaderStyle())
		if err := install.DryRun(context.TODO()); err != nil {
			c.UI.Output(err.Error())
			return 1
		}
	} else {
		if err := install.Run(context.TODO()); err != nil {
			c.UI.Output(err.Error())
			return 1
		}
	}

	return 0
}

// Help returns a description of the command and how it is used.
func (c *Command) Help() string {
	c.once.Do(c.init)
	return c.Synopsis() + "\n\nUsage: consul-k8s install [flags]\n\n" + c.help
}

// Synopsis returns a one-line command summary.
func (c *Command) Synopsis() string {
	return "Install Consul on Kubernetes."
}

func (c *Command) helmValues() (map[string]interface{}, error) {
	// TODO
	/*
		// Handle preset, value files, and set values logic.
		vals, err := c.mergeValuesFlagsWithPrecedence(settings)
		if err != nil {
			c.UI.Output(err.Error(), terminal.WithErrorStyle())
			return 1
		}
		valuesYaml, err := yaml.Marshal(vals)
		if err != nil {
			c.UI.Output(err.Error(), terminal.WithErrorStyle())
			return 1
		}

		var v helmValues
		err = yaml.Unmarshal(valuesYaml, &v)
		if err != nil {
			c.UI.Output(err.Error(), terminal.WithErrorStyle())
			return 1
		}
	*/

	return map[string]interface{}{}, nil
}

// TODO move this to a common location
// mergeValuesFlagsWithPrecedence is responsible for merging all the values to determine the values file for the
// installation based on the following precedence order from lowest to highest:
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

// validateFlags checks the command line flags and values for errors.
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
				return fmt.Errorf("file '%s' does not exist", filename)
			}
		}
	}

	return nil
}
