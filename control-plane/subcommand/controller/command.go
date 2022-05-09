package controller

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"sync"

	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/controller"
	mutatingwebhookconfiguration "github.com/hashicorp/consul-k8s/control-plane/helper/mutating-webhook-configuration"
	cmdCommon "github.com/hashicorp/consul-k8s/control-plane/subcommand/common"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/flags"
	"github.com/hashicorp/consul/api"
	"github.com/mitchellh/cli"
	"go.uber.org/zap/zapcore"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

type Command struct {
	UI cli.Ui

	flagSet   *flag.FlagSet
	httpFlags *flags.HTTPFlags

	flagWebhookTLSCertDir     string
	flagEnableLeaderElection  bool
	flagEnableWebhooks        bool
	flagDatacenter            string
	flagLogLevel              string
	flagLogJSON               bool
	flagResourcePrefix        string
	flagEnableWebhookCAUpdate bool

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
	c.flagSet.StringVar(&c.flagResourcePrefix, "resource-prefix", "",
		"Release prefix of the Consul installation used to determine Consul DNS Service name.")
	c.flagSet.BoolVar(&c.flagEnableWebhookCAUpdate, "enable-webhook-ca-update", false,
		"Enables updating the CABundle on the webhook within this controller rather than using the web cert manager.")
	c.flagSet.StringVar(&c.flagLogLevel, "log-level", zapcore.InfoLevel.String(),
		fmt.Sprintf("Log verbosity level. Supported values (in order of detail) are "+
			"%q, %q, %q, and %q.", zapcore.DebugLevel.String(), zapcore.InfoLevel.String(), zapcore.WarnLevel.String(), zapcore.ErrorLevel.String()))
	c.flagSet.BoolVar(&c.flagLogJSON, "log-json", false,
		"Enable or disable JSON output format for logging.")

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
	// Validate flags
	if err := c.validateFlags(); err != nil {
		c.UI.Error(err.Error())
		return 1
	}

	zapLogger, err := cmdCommon.ZapLogger(c.flagLogLevel, c.flagLogJSON)
	if err != nil {
		c.UI.Error(fmt.Sprintf("Error setting up logging: %s", err.Error()))
		return 1
	}
	ctrl.SetLogger(zapLogger)
	klog.SetLogger(zapLogger)

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:           scheme,
		Port:             9443,
		LeaderElection:   c.flagEnableLeaderElection,
		LeaderElectionID: "consul.hashicorp.com",
		Logger:           zapLogger,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		return 1
	}

	cfg := api.DefaultConfig()
	c.httpFlags.MergeOntoConfig(cfg)
	consulClient, err := consul.NewClient(cfg, c.httpFlags.ConsulAPITimeout())
	if err != nil {
		setupLog.Error(err, "connecting to Consul agent")
		return 1
	}

	partitionsEnabled := c.httpFlags.Partition() != ""
	consulMeta := common.ConsulMeta{
		PartitionsEnabled:    partitionsEnabled,
		Partition:            c.httpFlags.Partition(),
		NamespacesEnabled:    c.flagEnableNamespaces,
		DestinationNamespace: c.flagConsulDestinationNamespace,
		Mirroring:            c.flagEnableNSMirroring,
		Prefix:               c.flagNSMirroringPrefix,
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
	if err = (&controller.MeshController{
		ConfigEntryController: configEntryReconciler,
		Client:                mgr.GetClient(),
		Log:                   ctrl.Log.WithName("controller").WithName(common.Mesh),
		Scheme:                mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", common.Mesh)
		return 1
	}
	if err = (&controller.ExportedServicesController{
		ConfigEntryController: configEntryReconciler,
		Client:                mgr.GetClient(),
		Log:                   ctrl.Log.WithName("controller").WithName(common.ExportedServices),
		Scheme:                mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", common.ExportedServices)
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
	if err = (&controller.TerminatingGatewayController{
		ConfigEntryController: configEntryReconciler,
		Client:                mgr.GetClient(),
		Log:                   ctrl.Log.WithName("controller").WithName(common.TerminatingGateway),
		Scheme:                mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", common.TerminatingGateway)
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
				Client:       mgr.GetClient(),
				ConsulClient: consulClient,
				Logger:       ctrl.Log.WithName("webhooks").WithName(common.ServiceDefaults),
				ConsulMeta:   consulMeta,
			}})
		mgr.GetWebhookServer().Register("/mutate-v1alpha1-serviceresolver",
			&webhook.Admission{Handler: &v1alpha1.ServiceResolverWebhook{
				Client:       mgr.GetClient(),
				ConsulClient: consulClient,
				Logger:       ctrl.Log.WithName("webhooks").WithName(common.ServiceResolver),
				ConsulMeta:   consulMeta,
			}})
		mgr.GetWebhookServer().Register("/mutate-v1alpha1-proxydefaults",
			&webhook.Admission{Handler: &v1alpha1.ProxyDefaultsWebhook{
				Client:       mgr.GetClient(),
				ConsulClient: consulClient,
				Logger:       ctrl.Log.WithName("webhooks").WithName(common.ProxyDefaults),
				ConsulMeta:   consulMeta,
			}})
		mgr.GetWebhookServer().Register("/mutate-v1alpha1-mesh",
			&webhook.Admission{Handler: &v1alpha1.MeshWebhook{
				Client:       mgr.GetClient(),
				ConsulClient: consulClient,
				Logger:       ctrl.Log.WithName("webhooks").WithName(common.Mesh),
			}})
		mgr.GetWebhookServer().Register("/mutate-v1alpha1-exportedservices",
			&webhook.Admission{Handler: &v1alpha1.ExportedServicesWebhook{
				Client:       mgr.GetClient(),
				ConsulClient: consulClient,
				Logger:       ctrl.Log.WithName("webhooks").WithName(common.ExportedServices),
				ConsulMeta:   consulMeta,
			}})
		mgr.GetWebhookServer().Register("/mutate-v1alpha1-servicerouter",
			&webhook.Admission{Handler: &v1alpha1.ServiceRouterWebhook{
				Client:       mgr.GetClient(),
				ConsulClient: consulClient,
				Logger:       ctrl.Log.WithName("webhooks").WithName(common.ServiceRouter),
				ConsulMeta:   consulMeta,
			}})
		mgr.GetWebhookServer().Register("/mutate-v1alpha1-servicesplitter",
			&webhook.Admission{Handler: &v1alpha1.ServiceSplitterWebhook{
				Client:       mgr.GetClient(),
				ConsulClient: consulClient,
				Logger:       ctrl.Log.WithName("webhooks").WithName(common.ServiceSplitter),
				ConsulMeta:   consulMeta,
			}})
		mgr.GetWebhookServer().Register("/mutate-v1alpha1-serviceintentions",
			&webhook.Admission{Handler: &v1alpha1.ServiceIntentionsWebhook{
				Client:       mgr.GetClient(),
				ConsulClient: consulClient,
				Logger:       ctrl.Log.WithName("webhooks").WithName(common.ServiceIntentions),
				ConsulMeta:   consulMeta,
			}})
		mgr.GetWebhookServer().Register("/mutate-v1alpha1-ingressgateway",
			&webhook.Admission{Handler: &v1alpha1.IngressGatewayWebhook{
				Client:       mgr.GetClient(),
				ConsulClient: consulClient,
				Logger:       ctrl.Log.WithName("webhooks").WithName(common.IngressGateway),
				ConsulMeta:   consulMeta,
			}})
		mgr.GetWebhookServer().Register("/mutate-v1alpha1-terminatinggateway",
			&webhook.Admission{Handler: &v1alpha1.TerminatingGatewayWebhook{
				Client:       mgr.GetClient(),
				ConsulClient: consulClient,
				Logger:       ctrl.Log.WithName("webhooks").WithName(common.TerminatingGateway),
				ConsulMeta:   consulMeta,
			}})
	}
	// +kubebuilder:scaffold:builder

	if c.flagEnableWebhookCAUpdate {
		err := c.configureCABundleUpdate()
		if err != nil {
			setupLog.Error(err, "problem getting CA Cert")
			return 1
		}
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		return 1
	}
	return 0
}

func (c *Command) configureCABundleUpdate() error {
	// Create a context to be used by the processes started in this command.
	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()
	config, err := rest.InClusterConfig()
	if err != nil {
		return err
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return err
	}

	webhookConfigName := fmt.Sprintf("%s-%s", c.flagResourcePrefix, "controller")
	caPath := fmt.Sprintf("%s/%s", c.flagWebhookTLSCertDir, "serverca.crt")
	caCert, err := ioutil.ReadFile(caPath)
	if err != nil {
		return err
	}
	err = mutatingwebhookconfiguration.UpdateWithCABundle(ctx, clientset, webhookConfigName, caCert)

	return nil
}

func (c *Command) validateFlags() error {
	if len(c.flagSet.Args()) > 0 {
		return errors.New("Invalid arguments: should have no non-flag arguments")
	}
	if c.flagEnableWebhooks && c.flagWebhookTLSCertDir == "" {
		return errors.New("Invalid arguments: -webhook-tls-cert-dir must be set")
	}
	if c.flagDatacenter == "" {
		return errors.New("Invalid arguments: -datacenter must be set")
	}
	if c.httpFlags.ConsulAPITimeout() <= 0 {
		return errors.New("-consul-api-timeout must be set to a value greater than 0")
	}

	return nil
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
Usage: consul-k8s-control-plane controller [options]

  Starts the Consul Kubernetes controller that manages Consul Custom Resource Definitions

`
