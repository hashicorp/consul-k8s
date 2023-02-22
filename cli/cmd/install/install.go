package install

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
	"github.com/hashicorp/consul-k8s/cli/release"
	"github.com/hashicorp/consul-k8s/cli/validation"
	"github.com/posener/complete"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"helm.sh/helm/v3/pkg/action"
	helmCLI "helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/cli/values"
	"helm.sh/helm/v3/pkg/getter"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/utils/strings/slices"
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

	flagNameContext    = "context"
	flagNameKubeconfig = "kubeconfig"

	flagNameHCPResourceID = "hcp-resource-id"

	flagNameDemo = "demo"
	defaultDemo  = false
)

type Command struct {
	*common.BaseCommand

	kubernetes kubernetes.Interface

	helmActionsRunner helm.HelmActionsRunner

	httpClient *http.Client

	set *flag.Sets

	flagPreset            string
	flagNamespace         string
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
	flagDemo              bool
	flagNameHCPResourceID string

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
		Usage:   fmt.Sprintf("Use an installation preset, one of %s. Defaults to none", strings.Join(preset.Presets, ", ")),
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
	f.BoolVar(&flag.BoolVar{
		Name:    flagNameDemo,
		Target:  &c.flagDemo,
		Default: defaultDemo,
		Usage: fmt.Sprintf("Install %s immediately after installing %s.",
			common.ReleaseTypeConsulDemo, common.ReleaseTypeConsul),
	})
	f.StringVar(&flag.StringVar{
		Name:    flagNameHCPResourceID,
		Target:  &c.flagNameHCPResourceID,
		Default: "",
		Usage:   "Set the HCP resource_id when using the 'cloud' preset.",
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

	c.help = c.set.Help()
}

// Run installs Consul into a Kubernetes cluster.
func (c *Command) Run(args []string) int {
	c.once.Do(c.init)
	if c.helmActionsRunner == nil {
		c.helmActionsRunner = &helm.ActionRunner{}
	}

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
	if found, name, ns, _ := c.helmActionsRunner.CheckForInstallations(&helm.CheckForInstallationsOptions{
		Settings:    settings,
		ReleaseName: common.DefaultReleaseName,
		DebugLog:    uiLogger,
	}); found {
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

	release := release.Release{
		Name:      common.DefaultReleaseName,
		Namespace: c.flagNamespace,
	}

	msg, err := c.checkForPreviousSecrets(release)
	if err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}
	c.UI.Output(msg, terminal.WithSuccessStyle())

	if c.flagDemo {
		c.UI.Output("Checking if %s can be installed",
			cases.Title(language.English).String(common.ReleaseTypeConsulDemo),
			terminal.WithHeaderStyle())

		// Ensure there is not an existing Consul demo installation which would cause a conflict.
		if found, name, ns, _ := c.helmActionsRunner.CheckForInstallations(&helm.CheckForInstallationsOptions{
			Settings:    settings,
			ReleaseName: common.ConsulDemoAppReleaseName,
			DebugLog:    uiLogger,
		}); found {
			c.UI.Output("Cannot install %s. A %s cluster is already installed in namespace %s with name %s.",
				common.ReleaseTypeConsulDemo, common.ReleaseTypeConsulDemo, ns, name, terminal.WithErrorStyle())
			c.UI.Output("Use the command `consul-k8s uninstall` to uninstall the %s from the cluster.",
				common.ReleaseTypeConsulDemo, terminal.WithInfoStyle())
			return 1
		}
		c.UI.Output("No existing %s installations found.", common.ReleaseTypeConsulDemo, terminal.WithSuccessStyle())
	}

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

	var helmVals helm.Values
	err = yaml.Unmarshal(valuesYaml, &helmVals)
	if err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}

	release.Configuration = helmVals

	// If an enterprise license secret was provided, check that the secret exists and that the enterprise Consul image is set.
	if helmVals.Global.EnterpriseLicense.SecretName != "" {
		if err := c.checkValidEnterprise(release.Configuration.Global.EnterpriseLicense.SecretName); err != nil {
			c.UI.Output(err.Error(), terminal.WithErrorStyle())
			return 1
		}
		c.UI.Output("Valid enterprise Consul secret found.", terminal.WithSuccessStyle())
	}

	err = c.installConsul(valuesYaml, vals, settings, uiLogger)
	if err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}

	if c.flagDemo {
		timeout, err := time.ParseDuration(c.flagTimeout)
		if err != nil {
			c.UI.Output(err.Error(), terminal.WithErrorStyle())
			return 1
		}
		options := &helm.InstallOptions{
			ReleaseName:       common.ConsulDemoAppReleaseName,
			ReleaseType:       common.ReleaseTypeConsulDemo,
			Namespace:         c.flagNamespace,
			Values:            make(map[string]interface{}),
			Settings:          settings,
			EmbeddedChart:     consulChart.DemoHelmChart,
			ChartDirName:      "demo",
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
			"Installation can proceed with this configuration.", terminal.WithInfoStyle())
	}

	return 0
}

func (c *Command) installConsul(valuesYaml []byte, vals map[string]interface{}, settings *helmCLI.EnvSettings, uiLogger action.DebugLog) error {
	// Print out the installation summary.
	c.UI.Output("Consul Installation Summary", terminal.WithHeaderStyle())
	c.UI.Output("Name: %s", common.DefaultReleaseName, terminal.WithInfoStyle())
	c.UI.Output("Namespace: %s", c.flagNamespace, terminal.WithInfoStyle())

	if len(vals) == 0 {
		c.UI.Output("\nNo overrides provided, using the default Helm values.", terminal.WithInfoStyle())
	} else {
		c.UI.Output("\nHelm value overrides\n--------------------\n"+string(valuesYaml), terminal.WithInfoStyle())
	}

	// Without informing the user, default global.name to consul if it hasn't been set already. We don't allow setting
	// the release name, and since that is hardcoded to "consul", setting global.name to "consul" makes it so resources
	// aren't double prefixed with "consul-consul-...".
	vals = common.MergeMaps(config.ConvertToMap(config.GlobalNameConsul), vals)

	timeout, err := time.ParseDuration(c.flagTimeout)
	if err != nil {
		return err
	}
	installOptions := &helm.InstallOptions{
		ReleaseName:       common.DefaultReleaseName,
		ReleaseType:       common.ReleaseTypeConsul,
		Namespace:         c.flagNamespace,
		Values:            vals,
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

	err = helm.InstallHelmRelease(installOptions)
	if err != nil {
		return err
	}

	return nil
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

// AutocompleteFlags returns a mapping of supported flags and autocomplete
// options for this command. The map key for the Flags map should be the
// complete flag such as "-foo" or "--foo".
func (c *Command) AutocompleteFlags() complete.Flags {
	return complete.Flags{
		fmt.Sprintf("-%s", flagNamePreset):          complete.PredictNothing,
		fmt.Sprintf("-%s", flagNameNamespace):       complete.PredictNothing,
		fmt.Sprintf("-%s", flagNameDryRun):          complete.PredictNothing,
		fmt.Sprintf("-%s", flagNameAutoApprove):     complete.PredictNothing,
		fmt.Sprintf("-%s", flagNameConfigFile):      complete.PredictFiles("*"),
		fmt.Sprintf("-%s", flagNameSetStringValues): complete.PredictNothing,
		fmt.Sprintf("-%s", flagNameSetValues):       complete.PredictNothing,
		fmt.Sprintf("-%s", flagNameFileValues):      complete.PredictFiles("*"),
		fmt.Sprintf("-%s", flagNameTimeout):         complete.PredictNothing,
		fmt.Sprintf("-%s", flagNameVerbose):         complete.PredictNothing,
		fmt.Sprintf("-%s", flagNameWait):            complete.PredictNothing,
		fmt.Sprintf("-%s", flagNameContext):         complete.PredictNothing,
		fmt.Sprintf("-%s", flagNameKubeconfig):      complete.PredictNothing,
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
		p, err := c.getPreset(c.flagPreset)
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
	if ok := slices.Contains(preset.Presets, c.flagPreset); c.flagPreset != defaultPreset && !ok {
		return fmt.Errorf("'%s' is not a valid preset (valid presets: %s)", c.flagPreset, strings.Join(preset.Presets, ", "))
	}
	if !common.IsValidLabel(c.flagNamespace) {
		return fmt.Errorf("'%s' is an invalid namespace. Namespaces follow the RFC 1123 label convention and must "+
			"consist of a lower case alphanumeric character or '-' and must start/end with an alphanumeric character", c.flagNamespace)
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

// getPreset is a factory function that, given a string, produces a struct that
// implements the Preset interface.  If the string is not recognized an error is
// returned.
func (c *Command) getPreset(name string) (preset.Preset, error) {
	hcpConfig := preset.GetHCPPresetFromEnv(c.flagNameHCPResourceID)
	getPresetConfig := &preset.GetPresetConfig{
		Name: name,
		CloudPreset: &preset.CloudPreset{
			KubernetesClient:    c.kubernetes,
			KubernetesNamespace: c.flagNamespace,
			HCPConfig:           hcpConfig,
			UI:                  c.UI,
			HTTPClient:          c.httpClient,
			Context:             c.Ctx,
		},
	}
	return preset.GetPreset(getPresetConfig)
}
