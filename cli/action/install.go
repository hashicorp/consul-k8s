package action

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/hashicorp/consul-k8s/cli/common"
	"github.com/hashicorp/consul-k8s/cli/common/terminal"
	"github.com/hashicorp/consul-k8s/cli/helm"
	"github.com/hashicorp/consul-k8s/cli/validation"
	helmCLI "helm.sh/helm/v3/pkg/cli"
	"k8s.io/client-go/kubernetes"
)

type Install struct {
	Namespace     string
	Configuration helm.Values
	KubeContext   string
	KubeConfig    string
	UI            terminal.UI

	kubernetes kubernetes.Interface
	settings   *helmCLI.EnvSettings

	once sync.Once
}

func (i *Install) Run(ctx context.Context) error {
	// TODO change this to once
	i.init()

	return nil
}

func (i *Install) DryRun(ctx context.Context) error {
	// TODO change this to once
	i.init()

	return nil
}

func (i *Install) Precheck(ctx context.Context) error {
	// TODO change this to once
	i.init()

	// TODO LOG Checking if Consul can be installed.

	// Ensure there is not an existing Consul installation which would cause a conflict.
	// TODO actually pass a logger
	if name, ns, err := common.CheckForInstallations(i.settings, nil); err == nil {
		// c.UI.Output("Cannot install Consul. A Consul cluster is already installed in namespace %s with name %s.", ns, name, terminal.WithErrorStyle())
		// c.UI.Output("Use the command `consul-k8s uninstall` to uninstall Consul from the cluster.", terminal.WithInfoStyle())
		return fmt.Errorf(`cannot install Consul. A Consul cluster is already installed in namespace %s with name %s.\n
			Use the command "consul-k8s uninstall" to uninstall Consul from Kubernetes`, ns, name)
	}

	// TODO log "No existing Consul installations found."

	// Verify that no existing Consul PVCs are installed.
	if consulPvcs, err := validation.ListConsulPVCs(ctx, i.kubernetes); err != nil {
		return err
	} else if len(consulPvcs) > 0 {
		var pvcNames []string
		for _, pvc := range consulPvcs {
			pvcNames = append(pvcNames, fmt.Sprintf("%s/%s", pvc.Namespace, pvc.Name))
		}
		return fmt.Errorf("existing Consul Persistent Volume Claims found, possibly from a previous installation.\n"+
			"Please delete the following PVCs before installing Consul: %s", strings.Join(pvcNames, ", "))
	}
	// TODO log "No existing Consul PVCs found."

	// Verify that no existing Consul secrets are installed.
	if consulSecrets, err := validation.ListConsulSecrets(ctx, i.kubernetes); err != nil {
		return err
	} else if len(consulSecrets.Items) > 0 {
		var secretNames []string
		for _, secret := range consulSecrets.Items {
			secretNames = append(secretNames, fmt.Sprintf("%s/%s", secret.Namespace, secret.Name))
		}
		return fmt.Errorf("existing Consul secrets found, possibly from a previous installation.\n"+
			"Please delete the following secrets before installing Consul: %s", strings.Join(secretNames, ", "))
	}
	// TODO log "No existing Consul secrets found."

	// If no enterprise license secret was provided, the checks are complete.
	if i.Configuration.Global.EnterpriseLicense.SecretName == "" {
		return nil
	}

	// If an enterprise license secret was provided, verify that it exists.
	isEnt, err := validation.IsValidEnterprise(ctx, i.kubernetes, i.Namespace, i.Configuration.Global.EnterpriseLicense.SecretName)
	if err != nil {
		return err
	} else if !isEnt {
		return fmt.Errorf("the provided enterprise license secret %s/%s does not exist", i.Namespace, i.Configuration.Global.EnterpriseLicense.SecretName)
	}
	// TODO log "Valid enterprise license secret found."

	// If an enterprise license secret was provided, verify that the Consul
	// image is the enterprise image.
	if !validation.IsConsulEnterpriseImage(i.Configuration.Global.Image) {
		// TODO log "Consul image is not enterprise."
	}

	return nil
}

func (i *Install) init() error {
	// helmCLI.New() will create a settings object which is used by the Helm Go SDK calls.
	i.settings = helmCLI.New()

	// Any overrides by our kubeconfig and kubecontext flags is done here. The Kube client that
	// is created will use this command's flags first, then the HELM_KUBECONTEXT environment variable,
	// then call out to genericclioptions.ConfigFlag
	if i.KubeConfig != "" {
		i.settings.KubeConfig = i.KubeConfig
	}
	if i.KubeContext != "" {
		i.settings.KubeContext = i.KubeContext
	}

	// Set up the kubernetes client to use for non Helm SDK calls to the Kubernetes API
	// The Helm SDK will use settings.RESTClientGetter for its calls as well, so this will
	// use a consistent method to target the right cluster for both Helm SDK and non Helm SDK calls.
	if i.kubernetes == nil {
		restConfig, err := i.settings.RESTClientGetter().ToRESTConfig()
		if err != nil {
			return err
		}
		i.kubernetes, err = kubernetes.NewForConfig(restConfig)
		if err != nil {
			return err
		}
	}

	/*





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

		// Read the embedded chart files into []*loader.BufferedFile.
		chartFiles, err := helm.ReadChartFiles(consulChart.ConsulHelmChart, common.TopLevelChartDirName)
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

		// Run the install.
		if _, err = install.Run(chart, vals); err != nil {
			c.UI.Output(err.Error(), terminal.WithErrorStyle())
			return 1
		}

		c.UI.Output("Consul installed in namespace %q.", c.flagNamespace, terminal.WithSuccessStyle())// // Set up the kubernetes client to use for non Helm SDK calls to the Kubernetes API
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

		// Ensure there's no previous bootstrap secret lying around.
		if err := c.checkForPreviousSecrets(); err != nil {
			c.UI.Output(err.Error(), terminal.WithErrorStyle())
			return 1
		}
		c.UI.Output("No existing Consul secrets found", terminal.WithSuccessStyle())

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

		// If an enterprise license secret was provided, check that the secret exists and that the enterprise Consul image is set.
		if v.Global.EnterpriseLicense.SecretName != "" {
			if err := c.checkValidEnterprise(v.Global.EnterpriseLicense.SecretName); err != nil {
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

		// Read the embedded chart files into []*loader.BufferedFile.
		chartFiles, err := helm.ReadChartFiles(consulChart.ConsulHelmChart, common.TopLevelChartDirName)
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

		// Run the install.
		if _, err = install.Run(chart, vals); err != nil {
			c.UI.Output(err.Error(), terminal.WithErrorStyle())
			return 1
		}

		c.UI.Output("Consul installed in namespace %q.", c.flagNamespace, terminal.WithSuccessStyle())
	*/

	return nil
}
