package controller

import (
	"flag"
	"fmt"
	"sync"

	"github.com/hashicorp/consul-k8s/api/common"
	"github.com/hashicorp/consul-k8s/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/controller"
	"github.com/hashicorp/consul-k8s/subcommand/flags"
	"github.com/mitchellh/cli"
	"go.uber.org/zap/zapcore"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

type Command struct {
	UI cli.Ui

	flagSet   *flag.FlagSet
	k8s       *flags.K8SFlags
	httpFlags *flags.HTTPFlags

	flagWebhookTLSCertDir    string
	flagEnableLeaderElection bool
	flagEnableWebhooks       bool
	flagDatacenter           string
	flagLogLevel             string

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
	c.flagSet.BoolVar(&c.flagEnableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller. "+
			"Enabling this will ensure there is only one active controller manager.")
	c.flagSet.StringVar(&c.flagDatacenter, "datacenter", "",
		"Name of the Consul datacenter the controller is operating in. This is added as metadata on managed custom resources.")
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
	c.flagSet.BoolVar(&c.flagEnableWebhooks, "enable-webhooks", true,
		"Enable webhooks. Disable when running locally since Kube API server won't be able to route to local server.")
	c.flagSet.StringVar(&c.flagLogLevel, "log-level", zapcore.InfoLevel.String(),
		fmt.Sprintf("Log verbosity level. Supported values (in order of detail) are "+
			"%q, %q, %q, and %q.", zapcore.DebugLevel.String(), zapcore.InfoLevel.String(), zapcore.WarnLevel.String(), zapcore.ErrorLevel.String()))

	c.httpFlags = &flags.HTTPFlags{}
	flags.Merge(c.flagSet, c.httpFlags.Flags())
	c.help = flags.Usage(help, c.flagSet)
}

func (c *Command) Run(args []string) int {
	c.once.Do(c.init)
	if err := c.flagSet.Parse(args); err != nil {
		c.UI.Error(fmt.Sprintf("Parsing flagset: %s", err.Error()))
		return 1
	}
	if len(c.flagSet.Args()) > 0 {
		c.UI.Error("Invalid arguments: should have no non-flag arguments")
		return 1
	}
	if c.flagEnableWebhooks && c.flagWebhookTLSCertDir == "" {
		c.UI.Error("Invalid arguments: -webhook-tls-cert-dir must be set")
		return 1
	}
	if c.flagDatacenter == "" {
		c.UI.Error("Invalid arguments: -datacenter must be set")
		return 1
	}

	var zapLevel zapcore.Level
	if err := zapLevel.UnmarshalText([]byte(c.flagLogLevel)); err != nil {
		c.UI.Error(fmt.Sprintf("Error parsing -log-level %q: %s", c.flagLogLevel, err.Error()))
		return 1
	}
	// We set UseDevMode to true because we don't want our logs json
	// formatted.
	logger := zap.New(zap.UseDevMode(true), zap.Level(zapLevel))
	ctrl.SetLogger(logger)
	klog.SetLogger(logger)

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:           scheme,
		Port:             9443,
		LeaderElection:   c.flagEnableLeaderElection,
		LeaderElectionID: "consul.hashicorp.com",
		Logger:           logger,
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

	configEntryReconciler := &controller.ConfigEntryController{
		ConsulClient:               consulClient,
		DatacenterName:             c.flagDatacenter,
		EnableConsulNamespaces:     c.flagEnableNamespaces,
		ConsulDestinationNamespace: c.flagConsulDestinationNamespace,
		EnableNSMirroring:          c.flagEnableNSMirroring,
		NSMirroringPrefix:          c.flagNSMirroringPrefix,
		CrossNSACLPolicy:           c.flagCrossNSACLPolicy,
	}
	if err = (&controller.ServiceDefaultsController{
		ConfigEntryController: configEntryReconciler,
		Client:                mgr.GetClient(),
		Log:                   ctrl.Log.WithName("controller").WithName(common.ServiceDefaults),
		Scheme:                mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", common.ServiceDefaults)
		return 1
	}
	if err = (&controller.ServiceResolverController{
		ConfigEntryController: configEntryReconciler,
		Client:                mgr.GetClient(),
		Log:                   ctrl.Log.WithName("controller").WithName(common.ServiceResolver),
		Scheme:                mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", common.ServiceResolver)
		return 1
	}
	if err = (&controller.ProxyDefaultsController{
		ConfigEntryController: configEntryReconciler,
		Client:                mgr.GetClient(),
		Log:                   ctrl.Log.WithName("controller").WithName(common.ProxyDefaults),
		Scheme:                mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", common.ProxyDefaults)
		return 1
	}
	if err = (&controller.ServiceRouterController{
		ConfigEntryController: configEntryReconciler,
		Client:                mgr.GetClient(),
		Log:                   ctrl.Log.WithName("controller").WithName(common.ServiceRouter),
		Scheme:                mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", common.ServiceRouter)
		return 1
	}
	if err = (&controller.ServiceSplitterController{
		ConfigEntryController: configEntryReconciler,
		Client:                mgr.GetClient(),
		Log:                   ctrl.Log.WithName("controller").WithName(common.ServiceSplitter),
		Scheme:                mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", common.ServiceSplitter)
		return 1
	}
	if err = (&controller.ServiceIntentionsController{
		ConfigEntryController: configEntryReconciler,
		Client:                mgr.GetClient(),
		Log:                   ctrl.Log.WithName("controller").WithName(common.ServiceIntentions),
		Scheme:                mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", common.ServiceIntentions)
		return 1
	}
	if err = (&controller.IngressGatewayController{
		ConfigEntryController: configEntryReconciler,
		Client:                mgr.GetClient(),
		Log:                   ctrl.Log.WithName("controller").WithName(common.IngressGateway),
		Scheme:                mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", common.IngressGateway)
		return 1
	}

	if c.flagEnableWebhooks {
		// This webhook server sets up a Cert Watcher on the CertDir. This watches for file changes and updates the webhook certificates
		// automatically when new certificates are available.
		mgr.GetWebhookServer().CertDir = c.flagWebhookTLSCertDir

		// Note: The path here should be identical to the one on the kubebuilder
		// annotation in each webhook file.
		mgr.GetWebhookServer().Register("/mutate-v1alpha1-servicedefaults",
			&webhook.Admission{Handler: &v1alpha1.ServiceDefaultsWebhook{
				Client:                 mgr.GetClient(),
				ConsulClient:           consulClient,
				Logger:                 ctrl.Log.WithName("webhooks").WithName(common.ServiceDefaults),
				EnableConsulNamespaces: c.flagEnableNamespaces,
				EnableNSMirroring:      c.flagEnableNSMirroring,
			}})
		mgr.GetWebhookServer().Register("/mutate-v1alpha1-serviceresolver",
			&webhook.Admission{Handler: &v1alpha1.ServiceResolverWebhook{
				Client:                 mgr.GetClient(),
				ConsulClient:           consulClient,
				Logger:                 ctrl.Log.WithName("webhooks").WithName(common.ServiceResolver),
				EnableConsulNamespaces: c.flagEnableNamespaces,
				EnableNSMirroring:      c.flagEnableNSMirroring,
			}})
		mgr.GetWebhookServer().Register("/mutate-v1alpha1-proxydefaults",
			&webhook.Admission{Handler: &v1alpha1.ProxyDefaultsWebhook{
				Client:                 mgr.GetClient(),
				ConsulClient:           consulClient,
				Logger:                 ctrl.Log.WithName("webhooks").WithName(common.ProxyDefaults),
				EnableConsulNamespaces: c.flagEnableNamespaces,
				EnableNSMirroring:      c.flagEnableNSMirroring,
			}})
		mgr.GetWebhookServer().Register("/mutate-v1alpha1-servicerouter",
			&webhook.Admission{Handler: &v1alpha1.ServiceRouterWebhook{
				Client:                 mgr.GetClient(),
				ConsulClient:           consulClient,
				Logger:                 ctrl.Log.WithName("webhooks").WithName(common.ServiceRouter),
				EnableConsulNamespaces: c.flagEnableNamespaces,
				EnableNSMirroring:      c.flagEnableNSMirroring,
			}})
		mgr.GetWebhookServer().Register("/mutate-v1alpha1-servicesplitter",
			&webhook.Admission{Handler: &v1alpha1.ServiceSplitterWebhook{
				Client:                 mgr.GetClient(),
				ConsulClient:           consulClient,
				Logger:                 ctrl.Log.WithName("webhooks").WithName(common.ServiceSplitter),
				EnableConsulNamespaces: c.flagEnableNamespaces,
				EnableNSMirroring:      c.flagEnableNSMirroring,
			}})
		mgr.GetWebhookServer().Register("/mutate-v1alpha1-serviceintentions",
			&webhook.Admission{Handler: &v1alpha1.ServiceIntentionsWebhook{
				Client:                     mgr.GetClient(),
				ConsulClient:               consulClient,
				Logger:                     ctrl.Log.WithName("webhooks").WithName(common.ServiceIntentions),
				EnableConsulNamespaces:     c.flagEnableNamespaces,
				EnableNSMirroring:          c.flagEnableNSMirroring,
				ConsulDestinationNamespace: c.flagConsulDestinationNamespace,
				NSMirroringPrefix:          c.flagNSMirroringPrefix,
			}})
		mgr.GetWebhookServer().Register("/mutate-v1alpha1-ingressgateway",
			&webhook.Admission{Handler: &v1alpha1.IngressGatewayWebhook{
				Client:                 mgr.GetClient(),
				ConsulClient:           consulClient,
				Logger:                 ctrl.Log.WithName("webhooks").WithName(common.IngressGateway),
				EnableConsulNamespaces: c.flagEnableNamespaces,
				EnableNSMirroring:      c.flagEnableNSMirroring,
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
