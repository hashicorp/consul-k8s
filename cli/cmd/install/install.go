package install

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/consul-k8s/cli/cmd/common"
	"github.com/hashicorp/consul-k8s/cli/cmd/common/flag"
	"github.com/hashicorp/consul-k8s/cli/cmd/common/terminal"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	helmCLI "helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/cli/values"
	"helm.sh/helm/v3/pkg/getter"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"sigs.k8s.io/yaml"
)

const (
	flagNamePreset = "preset"
	defaultPreset  = ""

	defaultReleaseName = "consul"

	flagNameConfigFile      = "config-file"
	flagNameSetStringValues = "set-string"
	flagNameSetValues       = "set"
	flagNameFileValues      = "set-file"

	flagNameDryRun = "dry-run"
	defaultDryRun  = false

	flagNameAutoApprove = "auto-approve"
	defaultAutoApprove  = false

	flagNameNamespace = "namespace"
	defaultNamespace  = "consul"

	flagNameTimeout = "timeout"
	defaultTimeout  = "10m"

	flagNameWait = "wait"
	defaultWait  = true

	helmRepository = "https://helm.releases.hashicorp.com"
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
	flagWait            bool

	flagKubeConfig  string
	flagKubeContext string

	once sync.Once
	help string
}

func (c *Command) init() {
	// Store all the possible preset values in 'presetList'. Printed in the help message.
	var presetList []string
	for name := range presets {
		presetList = append(presetList, name)
	}

	c.set = flag.NewSets()
	{
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
			Usage:   "Run pre-install checks and display summary of installation.",
		})
		f.StringSliceVar(&flag.StringSliceVar{
			Name:    flagNameConfigFile,
			Aliases: []string{"f"},
			Target:  &c.flagValueFiles,
			Usage:   "Path to a file to customize the installation, such as Consul Helm chart values file. Can be specified multiple times.",
		})
		f.StringVar(&flag.StringVar{
			Name:    flagNameNamespace,
			Target:  &c.flagNamespace,
			Default: defaultNamespace,
			Usage:   "Namespace for the Consul installation.",
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
			Usage:   "Timeout to wait for installation to be ready.",
		})
		f.BoolVar(&flag.BoolVar{
			Name:    flagNameWait,
			Target:  &c.flagWait,
			Default: defaultWait,
			Usage:   "Determines whether to wait for resources in installation to be ready before exiting command.",
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
	}

	c.help = c.set.Help()

	// c.Init() calls the embedded BaseCommand's initialization function.
	c.Init()
}

func (c *Command) Run(args []string) int {
	c.once.Do(c.init)

	defer func() {
		if err := c.Close(); err != nil {
			c.UI.Output(err.Error())
		}
	}()

	// The logger is initialized in main with the name cli. Here, we reset the name to install so log lines would be prefixed with install.
	c.Log.ResetNamed("install")

	if err := c.validateFlags(args); err != nil {
		c.UI.Output(err.Error())
		return 1
	}

	// A hack to set namespace via the HELM_NAMESPACE env var until we merge a PR that will allow us to use the latest
	// Helm templates.
	prevHelmNSEnv := os.Getenv("HELM_NAMESPACE")
	os.Setenv("HELM_NAMESPACE", c.flagNamespace)
	// helmCLI.New() will create a settings object which is used by the Helm Go SDK calls.
	// Any overrides by our kubeconfig and kubecontext flags is done here. The Kube client that
	// is created will use this command's flags first, then the HELM_KUBECONTEXT environment variable,
	// then call out to genericclioptions.ConfigFlag
	settings := helmCLI.New()
	os.Setenv("HELM_NAMESPACE", prevHelmNSEnv)

	if c.flagKubeConfig != "" {
		settings.KubeConfig = c.flagKubeConfig
	}
	if c.flagKubeContext != "" {
		settings.KubeContext = c.flagKubeContext
	}

	// Setup logger to stream Helm library logs
	var uiLogger = func(s string, args ...interface{}) {
		logMsg := fmt.Sprintf(s, args...)
		c.UI.Output(logMsg, terminal.WithLibraryStyle())
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

	c.UI.Output("Pre-Install Checks", terminal.WithHeaderStyle())

	if err := c.checkForPreviousInstallations(settings, uiLogger); err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}

	// Ensure there's no previous PVCs lying around.
	if err := c.checkForPreviousPVCs(); err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}

	// Ensure there's no previous bootstrap secret lying around.
	if err := c.checkForPreviousSecrets(); err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
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

	// Print out the installation summary.
	if !c.flagAutoApprove {
		c.UI.Output("Consul Installation Summary", terminal.WithHeaderStyle())
		c.UI.Output("Installation name: %s", defaultReleaseName, terminal.WithInfoStyle())
		c.UI.Output("Namespace: %s", c.flagNamespace, terminal.WithInfoStyle())

		if len(vals) == 0 {
			c.UI.Output("Overrides: "+string(valuesYaml), terminal.WithInfoStyle())
		} else {
			c.UI.Output("Overrides:"+"\n"+string(valuesYaml), terminal.WithInfoStyle())
		}
	}

	// Without informing the user, default global.name to consul if it hasn't been set already. We don't allow setting
	// the release name, and since that is hardcoded to "consul", setting global.name to "consul" makes it so resources
	// aren't double prefixed with "consul-consul-...".
	vals = mergeMaps(convert(globalNameConsul), vals)

	// Dry Run should exit here, no need to actual locate/download the charts.
	if c.flagDryRun {
		c.UI.Output("Dry run complete - installation can proceed.", terminal.WithInfoStyle())
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
		confirmation = strings.TrimSuffix(confirmation, "\n")
		if !(strings.ToLower(confirmation) == "y" || strings.ToLower(confirmation) == "yes") {
			c.UI.Output("Install aborted. To learn how to customize your installation, run:\nconsul-k8s install --help", terminal.WithInfoStyle())
			return 1
		}
	}

	c.UI.Output("Running Installation", terminal.WithHeaderStyle())

	// Setup action configuration for Helm Go SDK function calls.
	actionConfig := new(action.Configuration)
	if err := actionConfig.Init(settings.RESTClientGetter(), c.flagNamespace,
		os.Getenv("HELM_DRIVER"), uiLogger); err != nil {
		c.UI.Output(err.Error())
		return 1
	}

	// Setup the installation action.
	install := action.NewInstall(actionConfig)
	install.ReleaseName = defaultReleaseName
	install.Namespace = c.flagNamespace
	install.CreateNamespace = true
	install.ChartPathOptions.RepoURL = helmRepository
	install.Wait = c.flagWait
	install.Timeout = c.timeoutDuration

	// Locate the chart, install it in some cache locally.
	chartPath, err := install.ChartPathOptions.LocateChart("consul", settings)
	if err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}

	// Actually load the chart into memory.
	chart, err := loader.Load(chartPath)
	if err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}
	c.UI.Output("Downloaded charts", terminal.WithSuccessStyle())

	// Run the install.
	_, err = install.Run(chart, vals)
	if err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}
	c.UI.Output("Consul installed into namespace %q", c.flagNamespace, terminal.WithSuccessStyle())

	return 0
}
func (c *Command) Help() string {
	c.once.Do(c.init)
	s := "Usage: consul-k8s install [flags]" + "\n" + "Install Consul onto a Kubernetes cluster." + "\n"
	return s + "\n" + c.help
}

func (c *Command) Synopsis() string {
	return "Install Consul on Kubernetes."
}

// checkForPreviousInstallations uses the helm Go SDK to find helm releases in all namespaces where the chart name is
// "consul", and returns an error if there is an existing installation.
// Note that this function is tricky to test because mocking out the action.Configuration struct requires a
// RegistryClient field that is from an internal helm package, so we are not unit testing it.
func (c *Command) checkForPreviousInstallations(settings *helmCLI.EnvSettings, uiLogger action.DebugLog) error {
	// Need a specific action config to call helm list, where namespace is NOT specified.
	listConfig := new(action.Configuration)
	if err := listConfig.Init(settings.RESTClientGetter(), "",
		os.Getenv("HELM_DRIVER"), uiLogger); err != nil {
		return fmt.Errorf("couldn't initialize helm config: %s", err)
	}

	lister := action.NewList(listConfig)
	lister.AllNamespaces = true
	res, err := lister.Run()
	if err != nil {
		return fmt.Errorf("couldn't check for installations: %s", err)
	}

	for _, rel := range res {
		if rel.Chart.Metadata.Name == "consul" {
			// TODO: In the future the user will be prompted with our own uninstall command.
			return fmt.Errorf("existing Consul installation found (name=%s, namespace=%s) - run helm "+
				"delete %s -n %s if you wish to re-install",
				rel.Name, rel.Namespace, rel.Name, rel.Namespace)
		}
	}
	c.UI.Output("No existing installations found", terminal.WithSuccessStyle())
	return nil
}

// checkForPreviousPVCs checks for existing PVCs with a name containing "consul-server" and returns an error and lists
// the PVCs it finds matches.
func (c *Command) checkForPreviousPVCs() error {
	pvcs, err := c.kubernetes.CoreV1().PersistentVolumeClaims("").List(c.Ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("error listing PVCs: %s", err)
	}
	var previousPVCs []string
	for _, pvc := range pvcs.Items {
		if strings.Contains(pvc.Name, "consul-server") {
			previousPVCs = append(previousPVCs, fmt.Sprintf("%s/%s", pvc.Namespace, pvc.Name))
		}
	}

	if len(previousPVCs) > 0 {
		return fmt.Errorf("found PVCs from previous installations (%s), delete before re-installing",
			strings.Join(previousPVCs, ","))
	}
	c.UI.Output("No previous persistent volume claims found", terminal.WithSuccessStyle())
	return nil
}

// checkForPreviousSecrets checks for the bootstrap token and returns an error if found.
func (c *Command) checkForPreviousSecrets() error {
	secrets, err := c.kubernetes.CoreV1().Secrets("").List(c.Ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("error listing secrets: %s", err)
	}
	for _, secret := range secrets.Items {
		// future TODO: also check for federation secret
		if strings.Contains(secret.Name, "consul-bootstrap-acl-token") {
			return fmt.Errorf("found consul-acl-bootstrap-token secret from previous installations: %q in namespace %q. To delete, run kubectl delete secret %s --namespace %s",
				secret.Name, secret.Namespace, secret.Name, secret.Namespace)
		}
	}
	c.UI.Output("No previous secrets found", terminal.WithSuccessStyle())
	return nil
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
		presetMap := presets[c.flagPreset].(map[string]interface{})
		vals = mergeMaps(presetMap, vals)
	}
	return vals, err
}

// mergeMaps is a helper function used in Run. Merges two maps giving b precedent.
// @source: https://github.com/helm/helm/blob/main/pkg/cli/values/options.go
func mergeMaps(a, b map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(a))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		if v, ok := v.(map[string]interface{}); ok {
			if bv, ok := out[k]; ok {
				if bv, ok := bv.(map[string]interface{}); ok {
					out[k] = mergeMaps(bv, v)
					continue
				}
			}
		}
		out[k] = v
	}
	return out
}

// validateFlags is a helper function that performs sanity checks on the user's provided flags.
func (c *Command) validateFlags(args []string) error {
	if err := c.set.Parse(args); err != nil {
		return err
	}
	if len(c.set.Args()) > 0 {
		return errors.New("should have no non-flag arguments")
	}
	if len(c.flagValueFiles) != 0 && c.flagPreset != defaultPreset {
		return errors.New(fmt.Sprintf("Cannot set both -%s and -%s", flagNameConfigFile, flagNamePreset))
	}
	if _, ok := presets[c.flagPreset]; c.flagPreset != defaultPreset && !ok {
		return errors.New(fmt.Sprintf("'%s' is not a valid preset", c.flagPreset))
	}
	if !validLabel(c.flagNamespace) {
		return errors.New(fmt.Sprintf("'%s' is an invalid namespace. Namespaces follow the RFC 1123 label convention and must "+
			"consist of a lower case alphanumeric character or '-' and must start/end with an alphanumeric.", c.flagNamespace))
	}
	duration, err := time.ParseDuration(c.flagTimeout)
	if err != nil {
		return fmt.Errorf("unable to parse -%s: %s", flagNameTimeout, err)
	}
	c.timeoutDuration = duration
	if len(c.flagValueFiles) != 0 {
		for _, filename := range c.flagValueFiles {
			if _, err := os.Stat(filename); err != nil && os.IsNotExist(err) {
				return errors.New(fmt.Sprintf("File '%s' does not exist.", filename))
			}
		}
	}

	if c.flagDryRun {
		c.UI.Output("Performing dry run installation.", terminal.WithInfoStyle())
	}
	return nil
}

// validLabel is a helper function that checks if a string follows RFC 1123 labels.
func validLabel(s string) bool {
	for i, c := range s {
		alphanum := ('a' <= c && c <= 'z') || ('0' <= c && c <= '9')
		// If the character is not the last or first, it can be a dash.
		if i != 0 && i != (len(s)-1) {
			alphanum = alphanum || (c == '-')
		}
		if !alphanum {
			return false
		}
	}
	return true
}
