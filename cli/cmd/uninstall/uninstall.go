package uninstall

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/consul-k8s/cli/cmd/common"
	"github.com/hashicorp/consul-k8s/cli/cmd/common/flag"
	"github.com/hashicorp/consul-k8s/cli/cmd/common/terminal"
	"helm.sh/helm/v3/pkg/action"
	helmCLI "helm.sh/helm/v3/pkg/cli"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	FlagSkipConfirm    = "skip-confirm"
	DefaultSkipConfirm = false

	FlagNamespace = "namespace"
	AllNamespaces = ""

	FlagReleaseName    = "name"
	DefaultReleaseName = ""
)

type Command struct {
	*common.BaseCommand

	kubernetes kubernetes.Interface

	set *flag.Sets

	flagNamespace   string
	flagReleaseName string
	flagSkipConfirm bool

	flagKubeConfig  string
	flagKubeContext string

	once sync.Once
	help string
}

func (c *Command) init() {
	c.set = flag.NewSets()
	{
		f := c.set.NewSet("Command Options")
		f.BoolVar(&flag.BoolVar{
			Name:    FlagSkipConfirm,
			Target:  &c.flagSkipConfirm,
			Default: DefaultSkipConfirm,
			Usage:   "Skip confirmation prompt.",
		})
		f.StringVar(&flag.StringVar{
			Name:    FlagNamespace,
			Target:  &c.flagNamespace,
			Default: AllNamespaces,
			Usage:   fmt.Sprintf("Namespace for the Consul installation. Defaults to \"%q\".", AllNamespaces),
		})
		f.StringVar(&flag.StringVar{
			Name:    FlagReleaseName,
			Target:  &c.flagReleaseName,
			Default: DefaultReleaseName,
			Usage:   "Name of the installation. This will be prefixed to resources installed on the cluster.",
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
}

func (c *Command) Run(args []string) int {
	c.once.Do(c.init)
	// Note that `c.init` and `c.Init` are NOT the same thing. One initializes the command struct,
	// the other the UI. It looks similar because BaseCommand is embedded in Command.
	c.Init()
	defer func() {
		if err := c.Close(); err != nil {
			c.UI.Output(err.Error())
			os.Exit(1)
		}
	}()

	// The logger is initialized in main with the name cli. Here, we reset the name to uninstall so log lines would be prefixed with uninstall.
	c.Log.ResetNamed("uninstall")

	if err := c.set.Parse(args); err != nil {
		c.UI.Output(err.Error())
		os.Exit(1)
	} else if len(c.set.Args()) > 0 {
		c.UI.Output("Should have no non-flag arguments.")
		os.Exit(1)
	}

	// helmCLI.New() will create a settings object which is used by the Helm Go SDK calls.
	settings := helmCLI.New()

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
			c.UI.Output("retrieving Kubernetes auth: %v", err, terminal.WithErrorStyle())
			os.Exit(1)
		}
		c.kubernetes, err = kubernetes.NewForConfig(restConfig)
		if err != nil {
			c.UI.Output("initializing Kubernetes client: %v", err, terminal.WithErrorStyle())
			return 1
		}
	}

	// Library functions should not log.
	var nopLogger = func(_ string, _ ...interface{}) {}
	// Setup logger to stream Helm library logs
	//var uiLogger = func(s string, args ...interface{}) {
	//	logMsg := fmt.Sprintf(s, args...)
	//	c.UI.Output(logMsg, terminal.WithInfoStyle())
	//}

	c.UI.Output("Verification Checks", terminal.WithHeaderStyle())
	// Search for Consul installation by calling `helm list`. Depends on what's already specified.

	//var actionConfig *action.Configuration
	actionConfig, err := c.createActionConfig(settings, nopLogger)
	if err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}

	found, foundReleaseName, foundReleaseNamespace, err := c.findExistingInstallation(actionConfig)
	if err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}
	if !found {
		c.UI.Output("No existing Consul installations.", terminal.WithSuccessStyle())
		return 0
		// TODO should we exit here? how do we continue allowing deleting pvc and secrets?
	}

	c.UI.Output("Existing Consul installation found.", terminal.WithSuccessStyle())

	c.UI.Output("Consul Un-Install Summary", terminal.WithHeaderStyle())
	c.UI.Output("Installation name: %s", foundReleaseName, terminal.WithInfoStyle())
	c.UI.Output("Namespace: %s", foundReleaseNamespace, terminal.WithInfoStyle())

	if !c.flagSkipConfirm {
		confirmation, err := c.UI.Input(&terminal.Input{
			Prompt: "Proceed with uninstall? (y/n)",
			Style:  terminal.InfoStyle,
			Secret: false,
		})
		if err != nil {
			c.UI.Output(err.Error(), terminal.WithErrorStyle())
			return 1
		}
		confirmation = strings.TrimSuffix(confirmation, "\n")
		if !(strings.ToLower(confirmation) == "y" || strings.ToLower(confirmation) == "yes") {
			c.UI.Output("Un-install aborted. To learn how to customize the uninstall, run:\nconsul-k8s install --help", terminal.WithInfoStyle())
			return 1
		}
	}

	c.UI.Output("Running Un-Install Steps", terminal.WithHeaderStyle())
	// Actually call out to `helm delete`
	// TODO: Commenting these out fixes it. But why?
	//actionConfig = new(action.Configuration)
	actionConfig.Init(settings.RESTClientGetter(), c.flagNamespace,
		os.Getenv("HELM_DRIVER"), nopLogger)
	uninstaller := action.NewUninstall(actionConfig)
	res, err := uninstaller.Run(c.flagReleaseName)
	if err != nil {
		c.UI.Output("unable to uninstall: %s", err, terminal.WithErrorStyle())
		return 1
	} else if res != nil && res.Info != "" {
		c.UI.Output("uninstall result: %s", res.Info, terminal.WithErrorStyle())
		return 1
	} else {
		c.UI.Output("Helm uninstall successful", terminal.WithSuccessStyle())
	}

	// Delete PVCs
	var pvcNames []string
	pvcSelector := metav1.ListOptions{LabelSelector: "release=" + c.flagReleaseName}
	pvcs, err := c.kubernetes.CoreV1().PersistentVolumeClaims(c.flagNamespace).List(c.Ctx, pvcSelector)
	if err != nil {
		c.UI.Output("error listing PVCs: %s", err, terminal.WithErrorStyle())
		return 1
	}
	for _, pvc := range pvcs.Items {
		err := c.kubernetes.CoreV1().PersistentVolumeClaims(c.flagNamespace).Delete(c.Ctx, pvc.Name, metav1.DeleteOptions{})
		if err != nil {
			c.UI.Output("error deleting PVC %s: %s", pvc.Name, err, terminal.WithErrorStyle())
			return 1
		}
		pvcNames = append(pvcNames, pvc.Name)
	}
	maxWait := 1800
	var i int
	for i = 0; i < maxWait; i++ {
		pvcs, err := c.kubernetes.CoreV1().PersistentVolumeClaims(c.flagNamespace).List(c.Ctx, pvcSelector)
		if err != nil {
			c.UI.Output("error listing PVCs: %s", err, terminal.WithErrorStyle())
			return 1
		}
		if len(pvcs.Items) == 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if i == maxWait {
		c.UI.Output("timed out waiting for PVCs to be deleted", terminal.WithErrorStyle())
		return 1
	}
	if len(pvcNames) > 0 {
		c.UI.Output(common.PrefixLines(" Deleted PVC => ", strings.Join(pvcNames, "\n")), terminal.WithSuccessStyle())
	}
	c.UI.Output("Persistent volume claims deleted.", terminal.WithSuccessStyle())

	// Delete any secrets that have releaseName in their name.
	var secretNames []string
	secrets, err := c.kubernetes.CoreV1().Secrets(c.flagNamespace).List(c.Ctx, metav1.ListOptions{})
	if err != nil {
		c.UI.Output("error listing Secrets: %s", err, terminal.WithErrorStyle())
		return 1
	}
	for _, secret := range secrets.Items {
		if strings.HasPrefix(secret.Name, c.flagReleaseName) {
			err := c.kubernetes.CoreV1().Secrets(c.flagNamespace).Delete(c.Ctx, secret.Name, metav1.DeleteOptions{})
			if err != nil {
				c.UI.Output("error deleting Secret %s: %s", secret.Name, err, terminal.WithErrorStyle())
				return 1
			}
			secretNames = append(secretNames, secret.Name)
		}
	}
	if len(secretNames) > 0 {
		c.UI.Output("Consul secrets deleted.", terminal.WithSuccessStyle())
	} else {
		c.UI.Output("No Consul secrets found.", terminal.WithSuccessStyle())
	}

	// Delete service accounts that have releaseName in their name.
	var serviceAccountNames []string
	sas, err := c.kubernetes.CoreV1().ServiceAccounts(c.flagNamespace).List(c.Ctx, metav1.ListOptions{})
	if err != nil {
		c.UI.Output("error listing ServiceAccounts: %s", err, terminal.WithErrorStyle())
		return 1
	}
	for _, sa := range sas.Items {
		if strings.HasPrefix(sa.Name, c.flagReleaseName) {
			err := c.kubernetes.CoreV1().ServiceAccounts(c.flagNamespace).Delete(c.Ctx, sa.Name, metav1.DeleteOptions{})
			if err != nil {
				c.UI.Output("error deleting Service Account %s: %s", sa.Name, err, terminal.WithErrorStyle())
				return 1
			}
			serviceAccountNames = append(serviceAccountNames, sa.Name)
		}
	}
	if len(serviceAccountNames) > 0 {
		c.UI.Output("Consul service accounts deleted.", terminal.WithSuccessStyle())
	} else {
		c.UI.Output("No Consul service accounts found.", terminal.WithSuccessStyle())
	}

	return 0
}

func (c *Command) Help() string {
	c.once.Do(c.init)
	s := "Usage: kubectl consul uninstall [options]" + "\n\n" + "Uninstall Consul and clean up all data." + "\n" +
		"Any data store in Consul will not be recoverable." + "\n" + c.help
	return s
}

func (c *Command) Synopsis() string {
	return "Uninstall Consul deployment."
}

const help = `
Usage: kubectl consul uninstall [options]

  Uninstall Consul and clean up all data.
  This is a destructive action. Any data store in Consul will
  not be recoverable.
`

func (c *Command) createActionConfig(settings *helmCLI.EnvSettings, logger action.DebugLog) (*action.Configuration, error) {
	actionConfig := new(action.Configuration)
	var err error
	if c.flagNamespace == AllNamespaces {
		err = actionConfig.Init(settings.RESTClientGetter(), "",
			os.Getenv("HELM_DRIVER"), logger)
	} else {
		err = actionConfig.Init(settings.RESTClientGetter(), c.flagNamespace,
			os.Getenv("HELM_DRIVER"), logger)
	}
	if err != nil {
		return nil, fmt.Errorf("error setting up helm action configuration to find existing installations: %s", err)
	}
	return actionConfig, nil
}

func (c *Command) findExistingInstallation(actionConfig *action.Configuration) (bool, string, string, error) {
	lister := action.NewList(actionConfig)
	if c.flagNamespace == AllNamespaces {
		lister.AllNamespaces = true
	}
	res, err := lister.Run()
	if err != nil {
		return false, "", "", fmt.Errorf("error finding existing installations: %s", err)
	}

	found := false
	foundReleaseName := ""
	foundReleaseNamespace := ""
	for _, rel := range res {
		if rel.Chart.Metadata.Name == "consul" {
			if c.flagNamespace != AllNamespaces {
				if c.flagNamespace == rel.Name {
					found = true
					foundReleaseName = rel.Name
					foundReleaseNamespace = rel.Namespace
					break
				}
			}
			found = true
			foundReleaseName = rel.Name
			foundReleaseNamespace = rel.Namespace
			break
		}
	}

	return found, foundReleaseName, foundReleaseNamespace, nil

}
