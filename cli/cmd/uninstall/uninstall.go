package uninstall

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/hashicorp/consul-k8s/cli/cmd/common"
	"github.com/hashicorp/consul-k8s/cli/cmd/common/flag"
	"github.com/hashicorp/consul-k8s/cli/cmd/common/terminal"
	"helm.sh/helm/v3/pkg/action"
	helmCLI "helm.sh/helm/v3/pkg/cli"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	flagAutoApprove    = "auto-approve"
	defaultAutoApprove = false

	flagNamespace        = "namespace"
	defaultAllNamespaces = ""

	flagReleaseName       = "name"
	defaultAnyReleaseName = ""

	flagWipeData    = "wipe-data"
	defaultWipeData = false

	flagSkipWipeData    = "skip-wipe-data"
	defaultSkipWipeData = false
)

type Command struct {
	*common.BaseCommand

	kubernetes kubernetes.Interface

	set *flag.Sets

	flagNamespace    string
	flagReleaseName  string
	flagAutoApprove  bool
	flagWipeData     bool
	flagSkipWipeData bool

	flagKubeConfig  string
	flagKubeContext string

	once sync.Once
	help string
}

func (c *Command) init() {
	c.set = flag.NewSets()
	f := c.set.NewSet("Command Options")
	f.BoolVar(&flag.BoolVar{
		Name:    flagAutoApprove,
		Target:  &c.flagAutoApprove,
		Default: defaultAutoApprove,
		Usage:   "Skip approval prompt for uninstalling Consul.",
	})
	// This is like the auto-approve to wipe all data without prompting for non-interactive environments that want
	// to remove everything.
	f.BoolVar(&flag.BoolVar{
		Name:    flagWipeData,
		Target:  &c.flagWipeData,
		Default: defaultWipeData,
		Usage:   "Delete all PVCs, Secrets, and Service Accounts associated with Consul Helm installation without prompting for approval to delete. Only use this when persisted data from previous installations is no longer necessary.",
	})
	// This is like the auto-approve to NOT wipe all data without prompting for non-interactive environments that
	// only want to remove the Consul Helm installation but keep the data.
	f.BoolVar(&flag.BoolVar{
		Name:    flagSkipWipeData,
		Target:  &c.flagSkipWipeData,
		Default: defaultSkipWipeData,
		Usage:   "Skip deleting all PVCs, Secrets, and Service Accounts associated with Consul Helm installation without prompting for approval to delete.",
	})
	f.StringVar(&flag.StringVar{
		Name:    flagNamespace,
		Target:  &c.flagNamespace,
		Default: defaultAllNamespaces,
		Usage:   "Namespace for the Consul installation.",
	})
	f.StringVar(&flag.StringVar{
		Name:    flagReleaseName,
		Target:  &c.flagReleaseName,
		Default: defaultAnyReleaseName,
		Usage:   "Name of the installation. This can be used to uninstall and/or delete the resources of a specific Helm release.",
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

	// Setup logger to stream Helm library logs
	var uiLogger = func(s string, args ...interface{}) {
		logMsg := fmt.Sprintf(s, args...)
		c.UI.Output(logMsg, terminal.WithLibraryStyle())
	}

	c.UI.Output("Existing Installation", terminal.WithHeaderStyle())

	// Search for Consul installation by calling `helm list`. Depends on what's already specified.
	actionConfig := new(action.Configuration)
	actionConfig, err := c.initActionConfig(actionConfig, c.flagNamespace, settings, uiLogger)
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
	} else {
		c.UI.Output("Existing Consul installation found.", terminal.WithSuccessStyle())
		c.UI.Output("Consul Un-Install Summary", terminal.WithHeaderStyle())
		c.UI.Output("Installation name: %s", foundReleaseName, terminal.WithInfoStyle())
		c.UI.Output("Namespace: %s", foundReleaseNamespace, terminal.WithInfoStyle())

		// Prompt for approval to uninstall Helm release.
		if !c.flagAutoApprove {
			confirmation, err := c.UI.Input(&terminal.Input{
				Prompt: "Proceed with uninstall? (y/N)",
				Style:  terminal.InfoStyle,
				Secret: false,
			})
			if err != nil {
				c.UI.Output(err.Error(), terminal.WithErrorStyle())
				return 1
			}
			confirmation = strings.TrimSuffix(confirmation, "\n")
			if !(strings.ToLower(confirmation) == "y" || strings.ToLower(confirmation) == "yes") {
				c.UI.Output("Un-install aborted. To learn how to customize the uninstall, run:\nconsul-k8s uninstall --help", terminal.WithInfoStyle())
				return 1
			}
		}
		// Actually call out to `helm delete`.
		actionConfig, err = c.initActionConfig(actionConfig, foundReleaseNamespace, settings, uiLogger)
		if err != nil {
			c.UI.Output(err.Error(), terminal.WithErrorStyle())
			return 1
		}
		uninstaller := action.NewUninstall(actionConfig)
		res, err := uninstaller.Run(foundReleaseName)
		if err != nil {
			c.UI.Output("unable to uninstall: %s", err, terminal.WithErrorStyle())
			return 1
		} else if res != nil && res.Info != "" {
			c.UI.Output("uninstall result: %s", res.Info, terminal.WithErrorStyle())
			return 1
		} else {
			c.UI.Output("Successfully uninstalled Consul Helm release", terminal.WithSuccessStyle())
		}
	}

	if c.flagSkipWipeData {
		c.UI.Output("Skipping deleting PVCs, secrets, and service accounts.", terminal.WithSuccessStyle())
		return 0
	}

	// At this point, even if no Helm release was found and uninstalled, there could
	// still be PVCs, Secrets, and Service Accounts left behind from a previous installation.
	// If there isn't a foundReleaseName and foundReleaseNamespace, we'll use the values of the
	// flags c.flagReleaseName and c.flagNamespace. If those are empty we'll prompt the user that
	// those should be provided to fully clean up the installation.
	if !found {
		if c.flagReleaseName == "" || c.flagNamespace == "" {
			c.UI.Output("No existing Consul Helm installation was found. To search for existing PVCs, secrets, and service accounts left behind by a Consul installation, please provide -release-name and -namespace.", terminal.WithInfoStyle())
			return 0
		}
		foundReleaseName = c.flagReleaseName
		foundReleaseNamespace = c.flagNamespace
	}

	// Prompt with a warning for approval before deleting PVCs, Secrets and ServiceAccounts. If flagWipeData is true,
	// then it will proceed to delete those without a prompt.
	if !c.flagWipeData {
		confirmation, err := c.UI.Input(&terminal.Input{
			Prompt: "WARNING: Proceed with deleting PVCs, Secrets, and ServiceAccounts? \n Only approve if all data from previous installation can be deleted (y/N)",
			Style:  terminal.WarningStyle,
			Secret: false,
		})
		if err != nil {
			c.UI.Output(err.Error(), terminal.WithErrorStyle())
			return 1
		}
		confirmation = strings.TrimSuffix(confirmation, "\n")
		if !(strings.ToLower(confirmation) == "y" || strings.ToLower(confirmation) == "yes") {
			c.UI.Output("Uninstall aborted without deleting PVCs, Secrets, and ServiceAccounts.", terminal.WithInfoStyle())
			return 1
		}
	}

	err = c.deletePVCs(foundReleaseName, foundReleaseNamespace)
	if err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}

	err = c.deleteSecrets(foundReleaseName, foundReleaseNamespace)
	if err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}

	err = c.deleteServiceAccounts(foundReleaseName, foundReleaseNamespace)
	if err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}

	return 0
}

func (c *Command) Help() string {
	c.once.Do(c.init)
	s := "Usage: kubectl consul uninstall [options]" + "\n" + "Uninstall Consul with options to delete data and resources associated with Consul installation." + "\n\n" + c.help
	return s
}

func (c *Command) Synopsis() string {
	return "Uninstall Consul deployment."
}

func (c *Command) initActionConfig(actionConfig *action.Configuration, namespace string, settings *helmCLI.EnvSettings, logger action.DebugLog) (*action.Configuration, error) {
	var err error
	if namespace == defaultAllNamespaces {
		err = actionConfig.Init(settings.RESTClientGetter(), "",
			os.Getenv("HELM_DRIVER"), logger)
	} else {
		err = actionConfig.Init(settings.RESTClientGetter(), namespace,
			os.Getenv("HELM_DRIVER"), logger)
	}
	if err != nil {
		return nil, fmt.Errorf("error setting up helm action configuration to find existing installations: %s", err)
	}
	return actionConfig, nil
}

func (c *Command) findExistingInstallation(actionConfig *action.Configuration) (bool, string, string, error) {
	lister := action.NewList(actionConfig)
	if c.flagNamespace == defaultAllNamespaces {
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
			if c.flagNamespace != defaultAllNamespaces {
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

func (c *Command) deletePVCs(foundReleaseName, foundReleaseNamespace string) error {
	var pvcNames []string
	pvcSelector := metav1.ListOptions{LabelSelector: fmt.Sprintf("release=%s", foundReleaseName)}
	pvcs, err := c.kubernetes.CoreV1().PersistentVolumeClaims(foundReleaseNamespace).List(c.Ctx, pvcSelector)
	if err != nil {
		return fmt.Errorf("deletePVCs: %s", err)
	}
	for _, pvc := range pvcs.Items {
		err := c.kubernetes.CoreV1().PersistentVolumeClaims(foundReleaseNamespace).Delete(c.Ctx, pvc.Name, metav1.DeleteOptions{})
		if err != nil {
			return fmt.Errorf("deletePVCs: error deleting PVC %q: %s", pvc.Name, err)
		}
		pvcNames = append(pvcNames, pvc.Name)
	}
	err = backoff.Retry(func() error {
		pvcs, err := c.kubernetes.CoreV1().PersistentVolumeClaims(foundReleaseNamespace).List(c.Ctx, pvcSelector)
		if err != nil {
			return fmt.Errorf("deletePVCs: %s", err)
		}
		if len(pvcs.Items) > 0 {
			err = fmt.Errorf("deletePVCs: pvcs still exist")
			return err
		}
		return err
	}, backoff.WithMaxRetries(backoff.NewConstantBackOff(100*time.Millisecond), 1800))
	if err != nil {
		return fmt.Errorf("deletePVCs: timed out waiting for PVCs to be deleted")
	}
	if len(pvcNames) > 0 {
		c.UI.Output(common.PrefixLines(" Deleted PVC => ", strings.Join(pvcNames, "\n")), terminal.WithSuccessStyle())
		c.UI.Output("PVCs deleted.", terminal.WithSuccessStyle())
	} else {
		c.UI.Output("No PVCs found.", terminal.WithSuccessStyle())
	}
	return nil
}

// deleteSecrets deletes any secrets that have foundReleaseName in their name.
func (c *Command) deleteSecrets(foundReleaseName, foundReleaseNamespace string) error {
	var secretNames []string
	secrets, err := c.kubernetes.CoreV1().Secrets(foundReleaseNamespace).List(c.Ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("deleteSecrets: %s", err)
	}
	for _, secret := range secrets.Items {
		if strings.HasPrefix(secret.Name, foundReleaseName) {
			err := c.kubernetes.CoreV1().Secrets(foundReleaseNamespace).Delete(c.Ctx, secret.Name, metav1.DeleteOptions{})
			if err != nil {
				return fmt.Errorf("deleteSecrets: error deleting Secret %q: %s", secret.Name, err)
			}
			secretNames = append(secretNames, secret.Name)
		}
	}
	if len(secretNames) > 0 {
		c.UI.Output("Consul secrets deleted.", terminal.WithSuccessStyle())
	} else {
		c.UI.Output("No Consul secrets found.", terminal.WithSuccessStyle())
	}
	return nil
}

// deleteServiceAccounts deletes service accounts that have foundReleaseName in their name.
func (c *Command) deleteServiceAccounts(foundReleaseName, foundReleaseNamespace string) error {
	var serviceAccountNames []string
	sas, err := c.kubernetes.CoreV1().ServiceAccounts(foundReleaseNamespace).List(c.Ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("deleteServiceAccounts: %s", err)
	}
	for _, sa := range sas.Items {
		if strings.HasPrefix(sa.Name, foundReleaseName) {
			err := c.kubernetes.CoreV1().ServiceAccounts(foundReleaseNamespace).Delete(c.Ctx, sa.Name, metav1.DeleteOptions{})
			if err != nil {
				return fmt.Errorf("deleteServiceAccounts: error deleting ServiceAccount %q: %s", sa.Name, err)
			}
			serviceAccountNames = append(serviceAccountNames, sa.Name)
		}
	}
	if len(serviceAccountNames) > 0 {
		c.UI.Output("Consul service accounts deleted.", terminal.WithSuccessStyle())
	} else {
		c.UI.Output("No Consul service accounts found.", terminal.WithSuccessStyle())
	}
	return nil
}
