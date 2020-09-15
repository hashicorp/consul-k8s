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
	UI cli.Ui

	flagSet   *flag.FlagSet
	k8s       *flags.K8SFlags
	httpFlags *flags.HTTPFlags

	flagMetricsAddr          string
	flagWebhookTLSCertDir    string
	flagEnableLeaderElection bool

	// Flags to support Consul Enterprise namespaces.
	flagEnableNamespaces           bool
	flagConsulDestinationNamespace string
	flagEnableNSMirroring          bool
	flagNSMirroringPrefix          string
	flagCrossNSACLPolicy           string

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
	c.flagSet.BoolVar(&c.flagEnableNamespaces, "enable-namespaces", false,
		"[Enterprise Only] Enables Consul Enterprise namespaces, in either a single Consul namespace or mirrored.")
	c.flagSet.StringVar(&c.flagConsulDestinationNamespace, "consul-destination-namespace", "default",
		"[Enterprise Only] Defines which Consul namespace to create all config entries in, regardless of their source Kubernetes namespace."+
			" If '-enable-k8s-namespace-mirroring' is true, this is not used.")
	c.flagSet.BoolVar(&c.flagEnableNSMirroring, "enable-k8s-namespace-mirroring", false, "[Enterprise Only] Enables "+
		"k8s namespace mirroring.")
	c.flagSet.StringVar(&c.flagNSMirroringPrefix, "k8s-namespace-mirroring-prefix", "",
		"[Enterprise Only] Prefix that will be added to all k8s namespaces mirrored into Consul if mirroring is enabled.")
	c.flagSet.StringVar(&c.flagCrossNSACLPolicy, "consul-cross-namespace-acl-policy", "",
		"[Enterprise Only] Name of the ACL policy to attach to all created Consul namespaces to allow service "+
			"discovery across Consul namespaces. Only necessary if ACLs are enabled.")
	c.flagSet.StringVar(&c.flagWebhookTLSCertDir, "webhook-tls-cert-dir", "",
		"Directory that contains the TLS cert and key required for the webhook. The cert and key files must be named 'tls.crt' and 'tls.key' respectively.")

	c.httpFlags = &flags.HTTPFlags{}
	flags.Merge(c.flagSet, c.httpFlags.Flags())
	c.help = flags.Usage(help, c.flagSet)
}

func (c *Command) Run(args []string) int {
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))
	c.once.Do(c.init)
	if err := c.flagSet.Parse(args); err != nil {
		c.UI.Error(fmt.Sprintf("Parsing flagset: %s", err.Error()))
		return 1
	}
	if len(c.flagSet.Args()) > 0 {
		c.UI.Error("Invalid arguments: should have no non-flag arguments")
		return 1
	}
	if c.flagWebhookTLSCertDir == "" {
		c.UI.Error("Invalid arguments: -webhook-tls-cert-dir must be set")
		return 1
	}

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

	configEntryReconciler := &controllers.ConfigEntryReconciler{
		ConsulClient:               consulClient,
		EnableConsulNamespaces:     c.flagEnableNamespaces,
		ConsulDestinationNamespace: c.flagConsulDestinationNamespace,
		EnableNSMirroring:          c.flagEnableNSMirroring,
		NSMirroringPrefix:          c.flagNSMirroringPrefix,
		CrossNSACLPolicy:           c.flagCrossNSACLPolicy,
	}
	if err = (&controllers.ServiceDefaultsReconciler{
		ConfigEntryReconciler: configEntryReconciler,
		Client:                mgr.GetClient(),
		Log:                   ctrl.Log.WithName("controllers").WithName("ServiceDefaults"),
		Scheme:                mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ServiceDefaults")
		return 1
	}
	if err = (&controllers.ServiceResolverReconciler{
		ConfigEntryReconciler: configEntryReconciler,
		Client:                mgr.GetClient(),
		Log:                   ctrl.Log.WithName("controllers").WithName("ServiceResolver"),
		Scheme:                mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ServiceResolver")
		return 1
	}

	if os.Getenv("ENABLE_WEBHOOKS") != "false" {
		// This webhook server sets up a Cert Watcher on the CertDir. This watches for file changes and updates the webhook certificates
		// automatically when new certificates are available.
		mgr.GetWebhookServer().CertDir = c.flagWebhookTLSCertDir

		// Note: The path here should be identical to the one on the kubebuilder
		// annotation in each webhook file.
		mgr.GetWebhookServer().Register("/mutate-v1alpha1-servicedefaults",
			&webhook.Admission{Handler: controllers.NewServiceDefaultsValidator(mgr.GetClient(), consulClient, ctrl.Log.WithName("webhooks").WithName("ServiceDefaults"))})
		mgr.GetWebhookServer().Register("/mutate-v1alpha1-serviceresolver",
			&webhook.Admission{Handler: controllers.NewServiceResolverValidator(mgr.GetClient(), consulClient, ctrl.Log.WithName("webhooks").WithName("ServiceResolver"))})
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
