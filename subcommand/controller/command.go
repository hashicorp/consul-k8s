package controller

import (
	"flag"
	"os"
	"sync"

	"github.com/hashicorp/consul-k8s/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/controllers"
	"github.com/hashicorp/consul-k8s/subcommand/flags"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

type Command struct {
	flags                *flag.FlagSet
	k8s                  *flags.K8SFlags
	httpFlags            *flags.HTTPFlags
	metricsAddr          string
	enableLeaderElection bool
	flagSecretName       string
	flagInitType         string
	flagNamespace        string
	flagACLDir           string
	flagTokenSinkFile    string

	once sync.Once
	help string
}

func (c *Command) Help() string {
	c.once.Do(c.init)
	return c.help
}

func (c *Command) Synopsis() string {
	return help
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
	c.flags = flag.NewFlagSet("", flag.ContinueOnError)
	c.flags.StringVar(&c.metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	c.flags.BoolVar(&c.enableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	c.httpFlags = &flags.HTTPFlags{}
	flags.Merge(c.flags, c.httpFlags.Flags())
	c.help = flags.Usage(help, c.flags)
}

func (c *Command) Run(_ []string) int {
	c.once.Do(c.init)

	if err := c.flags.Parse(nil); err != nil {
		setupLog.Error(err, "parsing flags")
		return 1
	}

	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: c.metricsAddr,
		Port:               9443,
		LeaderElection:     c.enableLeaderElection,
		LeaderElectionID:   "65a0bb41.my.domain",
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
		mgr.GetWebhookServer().Register("/mutate-v1alpha1-servicedefaults", &webhook.Admission{Handler: &v1alpha1.ServiceDefaultsValidator{
			Client:       mgr.GetClient(),
			ConsulClient: consulClient,
		}})
	}
	// +kubebuilder:scaffold:builder

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		return 1
	}
	return 0
}

const help = `
Usage: consul-k8s controller [options]

  Starts the consul kubernetes controller

`
