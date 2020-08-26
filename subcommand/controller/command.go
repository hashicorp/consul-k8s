package controller

import (
	"flag"
	"fmt"
	"os"
	"sync"

	"github.com/hashicorp/consul-k8s/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/controllers"
	"github.com/hashicorp/consul-k8s/subcommand/flags"
	"github.com/mitchellh/cli"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

type Command struct {
	UI        cli.Ui
	flagSet   *flag.FlagSet
	k8s       *flags.K8SFlags
	httpFlags *flags.HTTPFlags

	flagMetricsAddr          string
	flagEnableLeaderElection bool

	once sync.Once
	help string
}

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

func (c *Command) init() {
	c.flagSet = flag.NewFlagSet("", flag.ContinueOnError)
	c.flagSet.StringVar(&c.flagMetricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	c.flagSet.BoolVar(&c.flagEnableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller. "+
			"Enabling this will ensure there is only one active controller manager.")
	c.httpFlags = &flags.HTTPFlags{}
	flags.Merge(c.flagSet, c.httpFlags.Flags())
	c.help = flags.Usage(help, c.flagSet)
}

func (c *Command) Run(_ []string) int {
	c.once.Do(c.init)
	if err := c.flagSet.Parse(nil); err != nil {
		setupLog.Error(err, "parsing flagSet")
		return 1
	}
	if len(c.flagSet.Args()) > 0 {
		c.UI.Error(fmt.Sprintf("Should have no non-flag arguments."))
		return 1
	}

	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: c.flagMetricsAddr,
		Port:               9443,
		LeaderElection:     c.flagEnableLeaderElection,
		LeaderElectionID:   "consul.hashicorp.com",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		return 1
	}

	consulClient, err := c.httpFlags.APIClient()
	if err != nil {
		setupLog.Error(err, "connecting to Consul agent")
		return 1
	}

	if err = (&controllers.ServiceDefaultsReconciler{
		Client:       mgr.GetClient(),
		Log:          ctrl.Log.WithName("controllers").WithName("ServiceDefaults"),
		Scheme:       mgr.GetScheme(),
		ConsulClient: consulClient,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ServiceDefaults")
		return 1
	}

	if os.Getenv("ENABLE_WEBHOOKS") != "false" {
		//Note: The path here should be identical to the one on the kubebuilder annotation in file api/v1alpha1/servicedefaults_webhook.go
		mgr.GetWebhookServer().Register("/mutate-v1alpha1-servicedefaults",
			&webhook.Admission{Handler: v1alpha1.NewServiceDefaultsValidator(mgr.GetClient(), consulClient, ctrl.Log.WithName("webhooks").WithName("ServiceDefaults"))})
	}
	// +kubebuilder:scaffold:builder

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		return 1
	}
	return 0
}

func (c *Command) Help() string {
	c.once.Do(c.init)
	return c.help
}

func (c *Command) Synopsis() string {
	return synopsis
}

const synopsis = "Starts the Consul Kubernetes controller"
const help = `
Usage: consul-k8s controller [options]

  Starts the Consul Kubernetes controller that manages Consul Custom Resource Definitions

`
