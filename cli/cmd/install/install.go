package install

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
	"github.com/hashicorp/consul-k8s/cli/release"
	"github.com/hashicorp/consul-k8s/cli/validation"
	"helm.sh/helm/v3/pkg/action"
	helmCLI "helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/cli/values"
	"helm.sh/helm/v3/pkg/getter"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
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
	// Store all the possible preset values in 'presetList'. Printed in the help message. This is a change
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
}

// Run installs Consul into a Kubernetes cluster.
func (c *Command) Run(args []string) int {
	c.once.Do(c.init)

	// The logger is initialized in main with the name cli. Here, we reset the name to install so log lines would be prefixed with install.
	c.Log.ResetNamed("install")

	defer common.CloseWithError(c.BaseCommand)

	if err := c.validateFlags(args); err != nil {
		c.UI.Output(err.Error())
		return 1
	}

	if c.flagDryRun {
		c.UI.Output("Performing dry run install. No changes will be made to the cluster.", terminal.WithHeaderStyle())
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
			c.UI.Output("Error retrieving Kubernetes authentication:\n%v", err, terminal.WithErrorStyle())
			return 1
		}
		c.kubernetes, err = kubernetes.NewForConfig(restConfig)
		if err != nil {
			c.UI.Output("Error initializing Kubernetes client:\n%v", err, terminal.WithErrorStyle())
			return 1
		}
	}

	c.UI.Output("Checking if Consul can be installed", terminal.WithHeaderStyle())

	// Ensure there is not an existing Consul installation which would cause a conflict.
	if name, ns, err := common.CheckForInstallations(settings, uiLogger); err == nil {
		c.UI.Output("Cannot install Consul. A Consul cluster is already installed in namespace %s with name %s.", ns, name, terminal.WithErrorStyle())
		c.UI.Output("Use the command `consul-k8s uninstall` to uninstall Consul from the cluster.", terminal.WithInfoStyle())
		return 1
	}
	c.UI.Output("No existing Consul installations found.", terminal.WithSuccessStyle())

	// Ensure there's no previous PVCs lying around.
	if err := c.checkForPreviousPVCs(); err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}
	c.UI.Output("No existing Consul persistent volume claims found", terminal.WithSuccessStyle())

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

	var values helm.Values
	err = yaml.Unmarshal(valuesYaml, &values)
	if err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}

	release := release.Release{
		Name:          "consul",
		Namespace:     c.flagNamespace,
		Configuration: values,
	}

	msg, err := c.checkForPreviousSecrets(release)
	if err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}
	c.UI.Output(msg, terminal.WithSuccessStyle())

	// If an enterprise license secret was provided, check that the secret exists and that the enterprise Consul image is set.
	if values.Global.EnterpriseLicense.SecretName != "" {
		if err := c.checkValidEnterprise(release.Configuration.Global.EnterpriseLicense.SecretName); err != nil {
			c.UI.Output(err.Error(), terminal.WithErrorStyle())
			return 1
		}
		c.UI.Output("Valid enterprise Consul secret found.", terminal.WithSuccessStyle())
	}

	// Print out the installation summary.
	if !c.flagAutoApprove {
		c.UI.Output("Consul Installation Summary", terminal.WithHeaderStyle())
		c.UI.Output("Name: %s", common.DefaultReleaseName, terminal.WithInfoStyle())
		c.UI.Output("Namespace: %s", c.flagNamespace, terminal.WithInfoStyle())

		if len(vals) == 0 {
			c.UI.Output("\nNo overrides provided, using the default Helm values.", terminal.WithInfoStyle())
		} else {
			c.UI.Output("\nHelm value overrides\n-------------------\n"+string(valuesYaml), terminal.WithInfoStyle())
		}
	}

	// Without informing the user, default global.name to consul if it hasn't been set already. We don't allow setting
	// the release name, and since that is hardcoded to "consul", setting global.name to "consul" makes it so resources
	// aren't double prefixed with "consul-consul-...".
	vals = common.MergeMaps(config.Convert(config.GlobalNameConsul), vals)

	if c.flagDryRun {
		c.UI.Output("Dry run complete. No changes were made to the Kubernetes cluster.\n"+
			"Installation can proceed with this configuration.", terminal.WithInfoStyle())
		return 0
	}

	if !c.flagAutoApprove {
		confirmation, err := c.UI.Input(&terminal.Input{
			Prompt: "Proceed with installation? (y/N)",
			Style:  terminal.InfoStyle,
			Secret: false,
		})

		if err != nil {
			c.UI.Output(err.Error(), terminal.WithErrorStyle())
			return 1
		}
		if common.Abort(confirmation) {
			c.UI.Output("Install aborted. Use the command `consul-k8s install -help` to learn how to customize your installation.",
				terminal.WithInfoStyle())
			return 1
		}
	}

	c.UI.Output("Installing Consul", terminal.WithHeaderStyle())

	// Setup action configuration for Helm Go SDK function calls.
	actionConfig := new(action.Configuration)
	actionConfig, err = helm.InitActionConfig(actionConfig, c.flagNamespace, settings, uiLogger)
	if err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}

	// Setup the installation action.
	install := action.NewInstall(actionConfig)
	install.ReleaseName = common.DefaultReleaseName
	install.Namespace = c.flagNamespace
	install.CreateNamespace = true
	install.Wait = c.flagWait
	install.Timeout = c.timeoutDuration

	// Load the Helm chart.
	chart, err := helm.LoadChart(consulChart.ConsulHelmChart, common.TopLevelChartDirName)
	if err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}
	c.UI.Output("Downloaded charts", terminal.WithSuccessStyle())

	// Run the install.
	if _, err = install.Run(chart, vals); err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}

	c.UI.Output("Consul installed in namespace %q.", c.flagNamespace, terminal.WithSuccessStyle())
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

// checkForPreviousPVCs checks for existing Kubernetes persistent volume claims with a name containing "consul-server"
// and returns an error with a list of PVCs it finds if any match.
func (c *Command) checkForPreviousPVCs() error {
	pvcs, err := c.kubernetes.CoreV1().PersistentVolumeClaims("").List(c.Ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("error listing persistent volume claims: %s", err)
	}
	var previousPVCs []string
	for _, pvc := range pvcs.Items {
		if strings.Contains(pvc.Name, "consul-server") {
			previousPVCs = append(previousPVCs, fmt.Sprintf("%s/%s", pvc.Namespace, pvc.Name))
		}
	}

	if len(previousPVCs) > 0 {
		return fmt.Errorf("found persistent volume claims from previous installations, delete before reinstalling: %s",
			strings.Join(previousPVCs, ","))
	}
	return nil
}

// checkForPreviousSecrets checks for Consul secrets that exist in the cluster
// and returns a message if the secret configuration is ok or an error if
// the secret configuration could cause a conflict.
func (c *Command) checkForPreviousSecrets(release release.Release) (string, error) {
	secrets, err := validation.ListConsulSecrets(c.Ctx, c.kubernetes, release.Namespace)
	if err != nil {
		return "", fmt.Errorf("Error listing Consul secrets: %s", err)
	}

	// If the Consul configuration is a secondary DC, only one secret should
	// exist, the Consul federation secret.
	fedSecret := release.FedSecret()
	if release.ShouldExpectFederationSecret() {
		if len(secrets.Items) == 1 && secrets.Items[0].Name == fedSecret {
			return fmt.Sprintf("Found secret %s for Consul federation.", fedSecret), nil
		} else if len(secrets.Items) == 0 {
			return "", fmt.Errorf("Missing secret %s for Consul federation.\n"+
				"Please refer to the Consul Secondary Cluster configuration docs:\nhttps://www.consul.io/docs/k8s/installation/multi-cluster/kubernetes#secondary-cluster-s", fedSecret)
		}
	}

	// If not a secondary DC for federation, no Consul secrets should exist.
	if len(secrets.Items) > 0 {
		// Nicely format the delete commands for existing Consul secrets.
		namespacedSecrets := make(map[string][]string)
		for _, secret := range secrets.Items {
			namespacedSecrets[secret.Namespace] = append(namespacedSecrets[secret.Namespace], secret.Name)
		}

		var deleteCmds string
		for namespace, secretNames := range namespacedSecrets {
			deleteCmds += fmt.Sprintf("kubectl delete secret %s --namespace %s\n", strings.Join(secretNames, " "), namespace)
		}

		return "", fmt.Errorf("Found Consul secrets, possibly from a previous installation.\n"+
			"Delete existing Consul secrets from Kubernetes:\n\n%s", deleteCmds)
	}

	return "No existing Consul secrets found.", nil
}

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

// checkValidEnterprise checks and validates an enterprise installation.
// When an enterprise license secret is provided, check that the secret exists in the "consul" namespace.
func (c *Command) checkValidEnterprise(secretName string) error {

	_, err := c.kubernetes.CoreV1().Secrets(c.flagNamespace).Get(c.Ctx, secretName, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		return fmt.Errorf("enterprise license secret %q is not found in the %q namespace; please make sure that the secret exists in the %q namespace", secretName, c.flagNamespace, c.flagNamespace)
	} else if err != nil {
		return fmt.Errorf("error getting the enterprise license secret %q in the %q namespace: %s", secretName, c.flagNamespace, err)
	}
	return nil
}
