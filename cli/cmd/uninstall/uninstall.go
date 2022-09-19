package uninstall

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/hashicorp/consul-k8s/cli/common"
	"github.com/hashicorp/consul-k8s/cli/common/flag"
	"github.com/hashicorp/consul-k8s/cli/common/terminal"
	"github.com/hashicorp/consul-k8s/cli/helm"
	"github.com/posener/complete"
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

	flagTimeout    = "timeout"
	defaultTimeout = "10m"

	flagContext    = "context"
	flagKubeconfig = "kubeconfig"
)

type Command struct {
	*common.BaseCommand

	kubernetes kubernetes.Interface

	set *flag.Sets

	flagNamespace   string
	flagReleaseName string
	flagAutoApprove bool
	flagWipeData    bool
	flagTimeout     string
	timeoutDuration time.Duration

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
	f.BoolVar(&flag.BoolVar{
		Name:    flagWipeData,
		Target:  &c.flagWipeData,
		Default: defaultWipeData,
		Usage:   "When used in combination with -auto-approve, all persisted data (PVCs and Secrets) from previous installations will be deleted. Only set this to true when data from previous installations is no longer necessary.",
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
	f.StringVar(&flag.StringVar{
		Name:    flagTimeout,
		Target:  &c.flagTimeout,
		Default: defaultTimeout,
		Usage:   "Timeout to wait for uninstall.",
	})

	f = c.set.NewSet("Global Options")
	f.StringVar(&flag.StringVar{
		Name:    flagKubeconfig,
		Aliases: []string{"c"},
		Target:  &c.flagKubeConfig,
		Default: "",
		Usage:   "Path to kubeconfig file.",
	})
	f.StringVar(&flag.StringVar{
		Name:    flagContext,
		Target:  &c.flagKubeContext,
		Default: "",
		Usage:   "Kubernetes context to use.",
	})

	c.help = c.set.Help()
}

func (c *Command) Run(args []string) int {
	c.once.Do(c.init)

	// The logger is initialized in main with the name cli. Here, we reset the name to uninstall so log lines would be prefixed with uninstall.
	c.Log.ResetNamed("uninstall")

	defer func() {
		if err := c.Close(); err != nil {
			c.Log.Error(err.Error())
			os.Exit(1)
		}
	}()

	if err := c.set.Parse(args); err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}
	if len(c.set.Args()) > 0 {
		c.UI.Output("Should have no non-flag arguments.", terminal.WithErrorStyle())
		return 1
	}
	if c.flagWipeData && !c.flagAutoApprove {
		c.UI.Output("Can't set -wipe-data alone. Omit this flag to interactively uninstall, or use it with -auto-approve to wipe all data during the uninstall.", terminal.WithErrorStyle())
		return 1
	}
	duration, err := time.ParseDuration(c.flagTimeout)
	if err != nil {
		c.UI.Output("unable to parse -%s: %s", flagTimeout, err, terminal.WithErrorStyle())
		return 1
	}
	c.timeoutDuration = duration

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
			return 1
		}
		c.kubernetes, err = kubernetes.NewForConfig(restConfig)
		if err != nil {
			c.UI.Output("initializing Kubernetes client: %v", err, terminal.WithErrorStyle())
			return 1
		}
	}

	// Setup logger to stream Helm library logs.
	var uiLogger = func(s string, args ...interface{}) {
		logMsg := fmt.Sprintf(s, args...)
		c.UI.Output(logMsg, terminal.WithLibraryStyle())
	}

	c.UI.Output("Existing Installation", terminal.WithHeaderStyle())

	// Search for Consul installation by calling `helm list`. Depends on what's already specified.
	actionConfig := new(action.Configuration)
	actionConfig, err = helm.InitActionConfig(actionConfig, c.flagNamespace, settings, uiLogger)
	if err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}

	found, foundReleaseName, foundReleaseNamespace, err := c.findExistingInstallation(settings, uiLogger)
	if err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	} else {
		c.UI.Output("Existing Consul installation found.", terminal.WithSuccessStyle())
		c.UI.Output("Consul Uninstall Summary", terminal.WithHeaderStyle())
		c.UI.Output("Name: %s", foundReleaseName, terminal.WithInfoStyle())
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
			if common.Abort(confirmation) {
				c.UI.Output("Uninstall aborted. To learn how to customize the uninstall, run:\nconsul-k8s uninstall --help", terminal.WithInfoStyle())
				return 1
			}
		}

		// Actually call out to `helm delete`.
		actionConfig, err = helm.InitActionConfig(actionConfig, foundReleaseNamespace, settings, uiLogger)
		if err != nil {
			c.UI.Output(err.Error(), terminal.WithErrorStyle())
			return 1
		}

		uninstaller := action.NewUninstall(actionConfig)
		uninstaller.Timeout = c.timeoutDuration
		res, err := uninstaller.Run(foundReleaseName)
		if err != nil {
			c.UI.Output("unable to uninstall: %s", err, terminal.WithErrorStyle())
			return 1
		}
		if res != nil && res.Info != "" {
			c.UI.Output("Uninstall result: %s", res.Info, terminal.WithInfoStyle())
		}
		c.UI.Output("Successfully uninstalled Consul Helm release", terminal.WithSuccessStyle())
	}

	// If -auto-approve=true and -wipe-data=false, we should only uninstall the release, and skip deleting resources.
	if c.flagAutoApprove && !c.flagWipeData {
		c.UI.Output("Skipping deleting PVCs, secrets, and service accounts.", terminal.WithSuccessStyle())
		return 0
	}

	// At this point, even if no Helm release was found and uninstalled, there could
	// still be PVCs, Secrets, and Service Accounts left behind from a previous installation.
	// If there isn't a foundReleaseName and foundReleaseNamespace, we'll use the values of the
	// flags c.flagReleaseName and c.flagNamespace. If those are empty we'll fall back to defaults "consul" for the
	// installation name and "consul" for the namespace.
	if !found {
		if c.flagReleaseName == "" || c.flagNamespace == "" {
			foundReleaseName = common.DefaultReleaseName
			foundReleaseNamespace = common.DefaultReleaseNamespace
		} else {
			foundReleaseName = c.flagReleaseName
			foundReleaseNamespace = c.flagNamespace
		}
	}

	c.UI.Output("Other Consul Resources", terminal.WithHeaderStyle())
	if c.flagAutoApprove {
		c.UI.Output("Deleting data for installation: ", terminal.WithInfoStyle())
		c.UI.Output("Name: %s", foundReleaseName, terminal.WithInfoStyle())
		c.UI.Output("Namespace %s", foundReleaseNamespace, terminal.WithInfoStyle())
	}
	// Prompt with a warning for approval before deleting PVCs, Secrets, Service Accounts, Roles, Role Bindings,
	// Jobs, Cluster Roles, and Cluster Role Bindings.
	if !c.flagAutoApprove {
		confirmation, err := c.UI.Input(&terminal.Input{
			Prompt: fmt.Sprintf("WARNING: Proceed with deleting PVCs, Secrets, Service Accounts, Roles, Role Bindings, Jobs, Cluster Roles, and Cluster Role Bindings for the following installation? \n\n   Name: %s \n   Namespace: %s \n\n   Only approve if all data from this installation can be deleted. (y/N)", foundReleaseName, foundReleaseNamespace),
			Style:  terminal.WarningStyle,
			Secret: false,
		})
		if err != nil {
			c.UI.Output(err.Error(), terminal.WithErrorStyle())
			return 1
		}
		if common.Abort(confirmation) {
			c.UI.Output("Uninstall aborted without deleting PVCs and Secrets.", terminal.WithInfoStyle())
			return 1
		}
	}

	if err := c.deletePVCs(foundReleaseName, foundReleaseNamespace); err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}

	if err := c.deleteSecrets(foundReleaseNamespace); err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}

	if err := c.deleteServiceAccounts(foundReleaseName, foundReleaseNamespace); err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}

	if err := c.deleteRoles(foundReleaseName, foundReleaseNamespace); err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}

	if err := c.deleteRoleBindings(foundReleaseName, foundReleaseNamespace); err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}

	if err := c.deleteJobs(foundReleaseName, foundReleaseNamespace); err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}

	if err := c.deleteClusterRoles(foundReleaseName); err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}

	if err := c.deleteClusterRoleBindings(foundReleaseName); err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}

	return 0
}

func (c *Command) Help() string {
	c.once.Do(c.init)
	s := "Usage: consul-k8s uninstall [flags]" + "\n" + "Uninstall Consul with options to delete data and resources associated with Consul installation." + "\n\n" + c.help
	return s
}

func (c *Command) Synopsis() string {
	return "Uninstall Consul deployment."
}

// AutocompleteFlags returns a mapping of supported flags and autocomplete
// options for this command. The map key for the Flags map should be the
// complete flag such as "-foo" or "--foo".
func (c *Command) AutocompleteFlags() complete.Flags {
	return complete.Flags{
		fmt.Sprintf("-%s", flagAutoApprove): complete.PredictNothing,
		fmt.Sprintf("-%s", flagNamespace):   complete.PredictNothing,
		fmt.Sprintf("-%s", flagReleaseName): complete.PredictNothing,
		fmt.Sprintf("-%s", flagWipeData):    complete.PredictNothing,
		fmt.Sprintf("-%s", flagTimeout):     complete.PredictNothing,
		fmt.Sprintf("-%s", flagContext):     complete.PredictNothing,
		fmt.Sprintf("-%s", flagKubeconfig):  complete.PredictFiles("*"),
	}
}

// AutocompleteArgs returns the argument predictor for this command.
// Since argument completion is not supported, this will return
// complete.PredictNothing.
func (c *Command) AutocompleteArgs() complete.Predictor {
	return complete.PredictNothing
}

func (c *Command) findExistingInstallation(settings *helmCLI.EnvSettings, uiLogger action.DebugLog) (bool, string, string, error) {
	releaseName, namespace, err := common.CheckForInstallations(settings, uiLogger)
	if err != nil {
		return false, "", "", err
	} else if c.flagNamespace == defaultAllNamespaces || c.flagNamespace == namespace {
		return true, releaseName, namespace, nil
	} else {
		return false, "", "", fmt.Errorf("could not find consul installation in namespace %s", c.flagNamespace)
	}
}

// deletePVCs deletes any pvcs that have the label release={{foundReleaseName}} and waits for them to be deleted.
func (c *Command) deletePVCs(foundReleaseName, foundReleaseNamespace string) error {
	var pvcNames []string
	pvcSelector := metav1.ListOptions{LabelSelector: fmt.Sprintf("release=%s", foundReleaseName)}
	pvcs, err := c.kubernetes.CoreV1().PersistentVolumeClaims(foundReleaseNamespace).List(c.Ctx, pvcSelector)
	if err != nil {
		return fmt.Errorf("deletePVCs: %s", err)
	}
	if len(pvcs.Items) == 0 {
		c.UI.Output("No PVCs found.", terminal.WithSuccessStyle())
		return nil
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
			return fmt.Errorf("deletePVCs: pvcs still exist")
		}
		return nil
	}, backoff.WithMaxRetries(backoff.NewConstantBackOff(100*time.Millisecond), 1800))
	if err != nil {
		return fmt.Errorf("deletePVCs: timed out waiting for PVCs to be deleted")
	}
	if len(pvcNames) > 0 {
		for _, pvc := range pvcNames {
			c.UI.Output("Deleted PVC => %s", pvc, terminal.WithSuccessStyle())
		}
		c.UI.Output("PVCs deleted.", terminal.WithSuccessStyle())
	}
	return nil
}

// deleteSecrets deletes any secrets that have the label "managed-by" set to "consul-k8s".
func (c *Command) deleteSecrets(foundReleaseNamespace string) error {
	secrets, err := c.kubernetes.CoreV1().Secrets(foundReleaseNamespace).List(c.Ctx, metav1.ListOptions{
		LabelSelector: common.CLILabelKey + "=" + common.CLILabelValue,
	})
	if err != nil {
		return fmt.Errorf("deleteSecrets: %s", err)
	}
	if len(secrets.Items) == 0 {
		c.UI.Output("No Consul secrets found.", terminal.WithSuccessStyle())
		return nil
	}
	var secretNames []string
	for _, secret := range secrets.Items {
		err := c.kubernetes.CoreV1().Secrets(foundReleaseNamespace).Delete(c.Ctx, secret.Name, metav1.DeleteOptions{})
		if err != nil {
			return fmt.Errorf("deleteSecrets: error deleting Secret %q: %s", secret.Name, err)
		}
		secretNames = append(secretNames, secret.Name)
	}
	if len(secretNames) > 0 {
		for _, secret := range secretNames {
			c.UI.Output("Deleted Secret => %s", secret, terminal.WithSuccessStyle())
		}
		c.UI.Output("Consul secrets deleted.", terminal.WithSuccessStyle())
	}
	return nil
}

// deleteServiceAccounts deletes service accounts that have the label release={{foundReleaseName}}.
func (c *Command) deleteServiceAccounts(foundReleaseName, foundReleaseNamespace string) error {
	var serviceAccountNames []string
	saSelector := metav1.ListOptions{LabelSelector: fmt.Sprintf("release=%s", foundReleaseName)}
	sas, err := c.kubernetes.CoreV1().ServiceAccounts(foundReleaseNamespace).List(c.Ctx, saSelector)
	if err != nil {
		return fmt.Errorf("deleteServiceAccounts: %s", err)
	}
	if len(sas.Items) == 0 {
		c.UI.Output("No Consul service accounts found.", terminal.WithSuccessStyle())
		return nil
	}
	for _, sa := range sas.Items {
		err := c.kubernetes.CoreV1().ServiceAccounts(foundReleaseNamespace).Delete(c.Ctx, sa.Name, metav1.DeleteOptions{})
		if err != nil {
			return fmt.Errorf("deleteServiceAccounts: error deleting ServiceAccount %q: %s", sa.Name, err)
		}
		serviceAccountNames = append(serviceAccountNames, sa.Name)
	}
	if len(serviceAccountNames) > 0 {
		for _, sa := range serviceAccountNames {
			c.UI.Output("Deleted Service Account => %s", sa, terminal.WithSuccessStyle())
		}
		c.UI.Output("Consul service accounts deleted.", terminal.WithSuccessStyle())
	}
	return nil
}

// deleteRoles deletes roles that have the label release={{foundReleaseName}}.
func (c *Command) deleteRoles(foundReleaseName, foundReleaseNamespace string) error {
	var roleNames []string
	roleSelector := metav1.ListOptions{LabelSelector: fmt.Sprintf("release=%s", foundReleaseName)}
	roles, err := c.kubernetes.RbacV1().Roles(foundReleaseNamespace).List(c.Ctx, roleSelector)
	if err != nil {
		return fmt.Errorf("deleteRoles: %s", err)
	}
	if len(roles.Items) == 0 {
		c.UI.Output("No Consul roles found.", terminal.WithSuccessStyle())
		return nil
	}
	for _, role := range roles.Items {
		err := c.kubernetes.RbacV1().Roles(foundReleaseNamespace).Delete(c.Ctx, role.Name, metav1.DeleteOptions{})
		if err != nil {
			return fmt.Errorf("deleteRoles: error deleting Role %q: %s", role.Name, err)
		}
		roleNames = append(roleNames, role.Name)
	}
	if len(roleNames) > 0 {
		for _, role := range roleNames {
			c.UI.Output("Deleted Role => %s", role, terminal.WithSuccessStyle())
		}
		c.UI.Output("Consul roles deleted.", terminal.WithSuccessStyle())
	}
	return nil
}

// deleteRoleBindings deletes rolebindings that have the label release={{foundReleaseName}}.
func (c *Command) deleteRoleBindings(foundReleaseName, foundReleaseNamespace string) error {
	var rolebindingNames []string
	rolebindingSelector := metav1.ListOptions{LabelSelector: fmt.Sprintf("release=%s", foundReleaseName)}
	rolebindings, err := c.kubernetes.RbacV1().RoleBindings(foundReleaseNamespace).List(c.Ctx, rolebindingSelector)
	if err != nil {
		return fmt.Errorf("deleteRoleBindings: %s", err)
	}
	if len(rolebindings.Items) == 0 {
		c.UI.Output("No Consul rolebindings found.", terminal.WithSuccessStyle())
		return nil
	}
	for _, rolebinding := range rolebindings.Items {
		err := c.kubernetes.RbacV1().RoleBindings(foundReleaseNamespace).Delete(c.Ctx, rolebinding.Name, metav1.DeleteOptions{})
		if err != nil {
			return fmt.Errorf("deleteRoleBindings: error deleting Role %q: %s", rolebinding.Name, err)
		}
		rolebindingNames = append(rolebindingNames, rolebinding.Name)
	}
	if len(rolebindingNames) > 0 {
		for _, rolebinding := range rolebindingNames {
			c.UI.Output("Deleted Role Binding => %s", rolebinding, terminal.WithSuccessStyle())
		}
		c.UI.Output("Consul rolebindings deleted.", terminal.WithSuccessStyle())
	}
	return nil
}

// deleteJobs deletes jobs that have the label release={{foundReleaseName}}.
func (c *Command) deleteJobs(foundReleaseName, foundReleaseNamespace string) error {
	var jobNames []string
	jobSelector := metav1.ListOptions{LabelSelector: fmt.Sprintf("release=%s", foundReleaseName)}
	jobs, err := c.kubernetes.BatchV1().Jobs(foundReleaseNamespace).List(c.Ctx, jobSelector)
	if err != nil {
		return fmt.Errorf("deleteJobs: %s", err)
	}
	if len(jobs.Items) == 0 {
		c.UI.Output("No Consul jobs found.", terminal.WithSuccessStyle())
		return nil
	}
	for _, job := range jobs.Items {
		err := c.kubernetes.BatchV1().Jobs(foundReleaseNamespace).Delete(c.Ctx, job.Name, metav1.DeleteOptions{})
		if err != nil {
			return fmt.Errorf("deleteJobs: error deleting Job %q: %s", job.Name, err)
		}
		jobNames = append(jobNames, job.Name)
	}
	if len(jobNames) > 0 {
		for _, job := range jobNames {
			c.UI.Output("Deleted Jobs => %s", job, terminal.WithSuccessStyle())
		}
		c.UI.Output("Consul jobs deleted.", terminal.WithSuccessStyle())
	}
	return nil
}

// deleteClusterRoles deletes clusterRoles that have the label release={{foundReleaseName}}.
func (c *Command) deleteClusterRoles(foundReleaseName string) error {
	var clusterRolesNames []string
	clusterRolesSelector := metav1.ListOptions{LabelSelector: fmt.Sprintf("release=%s", foundReleaseName)}
	clusterRoles, err := c.kubernetes.RbacV1().ClusterRoles().List(c.Ctx, clusterRolesSelector)
	if err != nil {
		return fmt.Errorf("deleteClusterRoles: %s", err)
	}
	if len(clusterRoles.Items) == 0 {
		c.UI.Output("No Consul cluster roles found.", terminal.WithSuccessStyle())
		return nil
	}
	for _, clusterRole := range clusterRoles.Items {
		err := c.kubernetes.RbacV1().ClusterRoles().Delete(c.Ctx, clusterRole.Name, metav1.DeleteOptions{})
		if err != nil {
			return fmt.Errorf("deleteClusterRoles: error deleting cluster role %q: %s", clusterRole.Name, err)
		}
		clusterRolesNames = append(clusterRolesNames, clusterRole.Name)
	}
	if len(clusterRolesNames) > 0 {
		for _, clusterRole := range clusterRolesNames {
			c.UI.Output("Deleted cluster role => %s", clusterRole, terminal.WithSuccessStyle())
		}
		c.UI.Output("Consul cluster roles deleted.", terminal.WithSuccessStyle())
	}
	return nil
}

// deleteClusterRoleBindings deletes clusterrolebindings that have the label release={{foundReleaseName}}.
func (c *Command) deleteClusterRoleBindings(foundReleaseName string) error {
	var clusterRoleBindingsNames []string
	clusterRoleBindingsSelector := metav1.ListOptions{LabelSelector: fmt.Sprintf("release=%s", foundReleaseName)}
	clusterRoleBindings, err := c.kubernetes.RbacV1().ClusterRoleBindings().List(c.Ctx, clusterRoleBindingsSelector)
	if err != nil {
		return fmt.Errorf("deleteClusterRoleBindings: %s", err)
	}
	if len(clusterRoleBindings.Items) == 0 {
		c.UI.Output("No Consul cluster role bindings found.", terminal.WithSuccessStyle())
		return nil
	}
	for _, clusterRoleBinding := range clusterRoleBindings.Items {
		err := c.kubernetes.RbacV1().ClusterRoleBindings().Delete(c.Ctx, clusterRoleBinding.Name, metav1.DeleteOptions{})
		if err != nil {
			return fmt.Errorf("deleteClusterRoleBindings: error deleting cluster role binding %q: %s", clusterRoleBinding.Name, err)
		}
		clusterRoleBindingsNames = append(clusterRoleBindingsNames, clusterRoleBinding.Name)
	}
	if len(clusterRoleBindingsNames) > 0 {
		for _, clusterRoleBinding := range clusterRoleBindingsNames {
			c.UI.Output("Deleted cluster role binding => %s", clusterRoleBinding, terminal.WithSuccessStyle())
		}
		c.UI.Output("Consul cluster role bindings deleted.", terminal.WithSuccessStyle())
	}
	return nil
}
