package connectinject

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	connectinject "github.com/hashicorp/consul-k8s/control-plane/connect-inject"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	mutatingwebhookconfiguration "github.com/hashicorp/consul-k8s/control-plane/helper/mutating-webhook-configuration"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/common"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/flags"
	"github.com/hashicorp/consul/api"
	"github.com/mitchellh/cli"
	"go.uber.org/zap/zapcore"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

const WebhookCAFilename = "ca.crt"

type Command struct {
	UI cli.Ui

	flagListen                string
	flagCertDir               string // Directory with TLS certs for listening (PEM)
	flagDefaultInject         bool   // True to inject by default
	flagConsulImage           string // Docker image for Consul
	flagEnvoyImage            string // Docker image for Envoy
	flagConsulK8sImage        string // Docker image for consul-k8s
	flagACLAuthMethod         string // Auth Method to use for ACLs, if enabled
	flagWriteServiceDefaults  bool   // True to enable central config injection
	flagDefaultProtocol       string // Default protocol for use with central config
	flagConsulCACert          string // [Deprecated] Path to CA Certificate to use when communicating with Consul clients
	flagEnvoyExtraArgs        string // Extra envoy args when starting envoy
	flagEnableWebhookCAUpdate bool
	flagLogLevel              string
	flagLogJSON               bool

	flagAllowK8sNamespacesList []string // K8s namespaces to explicitly inject
	flagDenyK8sNamespacesList  []string // K8s namespaces to deny injection (has precedence)

	flagEnablePartitions bool // Use Admin Partitions on all components

	// Flags to support Consul namespaces
	flagEnableNamespaces           bool   // Use namespacing on all components
	flagConsulDestinationNamespace string // Consul namespace to register everything if not mirroring
	flagEnableK8SNSMirroring       bool   // Enables mirroring of k8s namespaces into Consul
	flagK8SNSMirroringPrefix       string // Prefix added to Consul namespaces created when mirroring
	flagCrossNamespaceACLPolicy    string // The name of the ACL policy to add to every created namespace if ACLs are enabled

	// Flags for endpoints controller.
	flagReleaseName      string
	flagReleaseNamespace string

	// Proxy resource settings.
	flagDefaultSidecarProxyCPULimit      string
	flagDefaultSidecarProxyCPURequest    string
	flagDefaultSidecarProxyMemoryLimit   string
	flagDefaultSidecarProxyMemoryRequest string

	// Metrics settings.
	flagDefaultEnableMetrics        bool
	flagDefaultEnableMetricsMerging bool
	flagDefaultMergedMetricsPort    string
	flagDefaultPrometheusScrapePort string
	flagDefaultPrometheusScrapePath string

	// Consul sidecar resource settings.
	flagDefaultConsulSidecarCPULimit      string
	flagDefaultConsulSidecarCPURequest    string
	flagDefaultConsulSidecarMemoryLimit   string
	flagDefaultConsulSidecarMemoryRequest string

	// Init container resource settings.
	flagInitContainerCPULimit      string
	flagInitContainerCPURequest    string
	flagInitContainerMemoryLimit   string
	flagInitContainerMemoryRequest string

	// Transparent proxy flags.
	flagDefaultEnableTransparentProxy          bool
	flagTransparentProxyDefaultOverwriteProbes bool

	// Consul DNS flags.
	flagEnableConsulDNS bool
	flagResourcePrefix  string

	flagEnableOpenShift bool

	flagSet *flag.FlagSet
	http    *flags.HTTPFlags

	consulClient *api.Client
	clientset    kubernetes.Interface

	once sync.Once
	help string
}

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	// We need v1alpha1 here to add the peering api to the scheme
	utilruntime.Must(v1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func (c *Command) init() {
	c.flagSet = flag.NewFlagSet("", flag.ContinueOnError)
	c.flagSet.StringVar(&c.flagListen, "listen", ":8080", "Address to bind listener to.")
	c.flagSet.BoolVar(&c.flagDefaultInject, "default-inject", true, "Inject by default.")
	c.flagSet.StringVar(&c.flagCertDir, "tls-cert-dir", "",
		"Directory with PEM-encoded TLS certificate and key to serve.")
	c.flagSet.StringVar(&c.flagConsulImage, "consul-image", "",
		"Docker image for Consul.")
	c.flagSet.StringVar(&c.flagEnvoyImage, "envoy-image", "",
		"Docker image for Envoy.")
	c.flagSet.StringVar(&c.flagConsulK8sImage, "consul-k8s-image", "",
		"Docker image for consul-k8s. Used for the connect sidecar.")
	c.flagSet.StringVar(&c.flagEnvoyExtraArgs, "envoy-extra-args", "",
		"Extra envoy command line args to be set when starting envoy (e.g \"--log-level debug --disable-hot-restart\").")
	c.flagSet.StringVar(&c.flagACLAuthMethod, "acl-auth-method", "",
		"The name of the Kubernetes Auth Method to use for connectInjection if ACLs are enabled.")
	c.flagSet.BoolVar(&c.flagWriteServiceDefaults, "enable-central-config", false,
		"Write a service-defaults config for every Connect service using protocol from -default-protocol or Pod annotation.")
	c.flagSet.StringVar(&c.flagDefaultProtocol, "default-protocol", "",
		"The default protocol to use in central config registrations.")
	c.flagSet.StringVar(&c.flagConsulCACert, "consul-ca-cert", "",
		"[Deprecated] Please use '-ca-file' flag instead. Path to CA certificate to use if communicating with Consul clients over HTTPS.")
	c.flagSet.Var((*flags.AppendSliceValue)(&c.flagAllowK8sNamespacesList), "allow-k8s-namespace",
		"K8s namespaces to explicitly allow. May be specified multiple times.")
	c.flagSet.Var((*flags.AppendSliceValue)(&c.flagDenyK8sNamespacesList), "deny-k8s-namespace",
		"K8s namespaces to explicitly deny. Takes precedence over allow. May be specified multiple times.")
	c.flagSet.StringVar(&c.flagReleaseName, "release-name", "consul", "The Consul Helm installation release name, e.g 'helm install <RELEASE-NAME>'")
	c.flagSet.StringVar(&c.flagReleaseNamespace, "release-namespace", "default", "The Consul Helm installation namespace, e.g 'helm install <RELEASE-NAME> --namespace <RELEASE-NAMESPACE>'")
	c.flagSet.BoolVar(&c.flagEnablePartitions, "enable-partitions", false,
		"[Enterprise Only] Enables Admin Partitions.")
	c.flagSet.BoolVar(&c.flagEnableNamespaces, "enable-namespaces", false,
		"[Enterprise Only] Enables namespaces, in either a single Consul namespace or mirrored.")
	c.flagSet.StringVar(&c.flagConsulDestinationNamespace, "consul-destination-namespace", "default",
		"[Enterprise Only] Defines which Consul namespace to register all injected services into. If '-enable-k8s-namespace-mirroring' "+
			"is true, this is not used.")
	c.flagSet.BoolVar(&c.flagEnableK8SNSMirroring, "enable-k8s-namespace-mirroring", false, "[Enterprise Only] Enables "+
		"k8s namespace mirroring.")
	c.flagSet.StringVar(&c.flagK8SNSMirroringPrefix, "k8s-namespace-mirroring-prefix", "",
		"[Enterprise Only] Prefix that will be added to all k8s namespaces mirrored into Consul if mirroring is enabled.")
	c.flagSet.StringVar(&c.flagCrossNamespaceACLPolicy, "consul-cross-namespace-acl-policy", "",
		"[Enterprise Only] Name of the ACL policy to attach to all created Consul namespaces to allow service "+
			"discovery across Consul namespaces. Only necessary if ACLs are enabled.")
	c.flagSet.BoolVar(&c.flagDefaultEnableTransparentProxy, "default-enable-transparent-proxy", true,
		"Enable transparent proxy mode for all Consul service mesh applications by default.")
	c.flagSet.BoolVar(&c.flagTransparentProxyDefaultOverwriteProbes, "transparent-proxy-default-overwrite-probes", true,
		"Overwrite Kubernetes probes to point to Envoy by default when in Transparent Proxy mode.")
	c.flagSet.BoolVar(&c.flagEnableConsulDNS, "enable-consul-dns", false,
		"Enables Consul DNS lookup for services in the mesh.")
	c.flagSet.StringVar(&c.flagResourcePrefix, "resource-prefix", "",
		"Release prefix of the Consul installation used to determine Consul DNS Service name.")
	c.flagSet.BoolVar(&c.flagEnableOpenShift, "enable-openshift", false,
		"Indicates that the command runs in an OpenShift cluster.")
	c.flagSet.BoolVar(&c.flagEnableWebhookCAUpdate, "enable-webhook-ca-update", false,
		"Enables updating the CABundle on the webhook within this controller rather than using the web cert manager.")
	c.flagSet.StringVar(&c.flagLogLevel, "log-level", zapcore.InfoLevel.String(),
		fmt.Sprintf("Log verbosity level. Supported values (in order of detail) are "+
			"%q, %q, %q, and %q.", zapcore.DebugLevel.String(), zapcore.InfoLevel.String(), zapcore.WarnLevel.String(), zapcore.ErrorLevel.String()))
	c.flagSet.BoolVar(&c.flagLogJSON, "log-json", false,
		"Enable or disable JSON output format for logging.")

	// Proxy sidecar resource setting flags.
	c.flagSet.StringVar(&c.flagDefaultSidecarProxyCPURequest, "default-sidecar-proxy-cpu-request", "", "Default sidecar proxy CPU request.")
	c.flagSet.StringVar(&c.flagDefaultSidecarProxyCPULimit, "default-sidecar-proxy-cpu-limit", "", "Default sidecar proxy CPU limit.")
	c.flagSet.StringVar(&c.flagDefaultSidecarProxyMemoryRequest, "default-sidecar-proxy-memory-request", "", "Default sidecar proxy memory request.")
	c.flagSet.StringVar(&c.flagDefaultSidecarProxyMemoryLimit, "default-sidecar-proxy-memory-limit", "", "Default sidecar proxy memory limit.")

	// Metrics setting flags.
	c.flagSet.BoolVar(&c.flagDefaultEnableMetrics, "default-enable-metrics", false, "Default for enabling connect service metrics.")
	c.flagSet.BoolVar(&c.flagDefaultEnableMetricsMerging, "default-enable-metrics-merging", false, "Default for enabling merging of connect service metrics and envoy proxy metrics.")
	c.flagSet.StringVar(&c.flagDefaultMergedMetricsPort, "default-merged-metrics-port", "20100", "Default port for merged metrics endpoint on the consul-sidecar.")
	c.flagSet.StringVar(&c.flagDefaultPrometheusScrapePort, "default-prometheus-scrape-port", "20200", "Default port where Prometheus scrapes connect metrics from.")
	c.flagSet.StringVar(&c.flagDefaultPrometheusScrapePath, "default-prometheus-scrape-path", "/metrics", "Default path where Prometheus scrapes connect metrics from.")

	// Init container resource setting flags.
	c.flagSet.StringVar(&c.flagInitContainerCPURequest, "init-container-cpu-request", "50m", "Init container CPU request.")
	c.flagSet.StringVar(&c.flagInitContainerCPULimit, "init-container-cpu-limit", "50m", "Init container CPU limit.")
	c.flagSet.StringVar(&c.flagInitContainerMemoryRequest, "init-container-memory-request", "25Mi", "Init container memory request.")
	c.flagSet.StringVar(&c.flagInitContainerMemoryLimit, "init-container-memory-limit", "150Mi", "Init container memory limit.")

	// Consul sidecar resource setting flags.
	c.flagSet.StringVar(&c.flagDefaultConsulSidecarCPURequest, "default-consul-sidecar-cpu-request", "20m", "Default consul sidecar CPU request.")
	c.flagSet.StringVar(&c.flagDefaultConsulSidecarCPULimit, "default-consul-sidecar-cpu-limit", "20m", "Default consul sidecar CPU limit.")
	c.flagSet.StringVar(&c.flagDefaultConsulSidecarMemoryRequest, "default-consul-sidecar-memory-request", "25Mi", "Default consul sidecar memory request.")
	c.flagSet.StringVar(&c.flagDefaultConsulSidecarMemoryLimit, "default-consul-sidecar-memory-limit", "50Mi", "Default consul sidecar memory limit.")

	c.http = &flags.HTTPFlags{}

	flags.Merge(c.flagSet, c.http.Flags())
	// flag.CommandLine is a package level variable representing the default flagSet. The init() function in
	// "sigs.k8s.io/controller-runtime/pkg/client/config", which is imported by ctrl, registers the flag --kubeconfig to
	// the default flagSet. That's why we need to merge it to have access with our flagSet.
	flags.Merge(c.flagSet, flag.CommandLine)
	c.help = flags.Usage(help, c.flagSet)
}

func (c *Command) Run(args []string) int {
	c.once.Do(c.init)
	if err := c.flagSet.Parse(args); err != nil {
		return 1
	}

	// Validate flags
	if err := c.validateFlags(); err != nil {
		c.UI.Error(err.Error())
		return 1
	}

	// Proxy resources.
	var sidecarProxyCPULimit, sidecarProxyCPURequest, sidecarProxyMemoryLimit, sidecarProxyMemoryRequest resource.Quantity
	var err error
	if c.flagDefaultSidecarProxyCPURequest != "" {
		sidecarProxyCPURequest, err = resource.ParseQuantity(c.flagDefaultSidecarProxyCPURequest)
		if err != nil {
			c.UI.Error(fmt.Sprintf("-default-sidecar-proxy-cpu-request is invalid: %s", err))
			return 1
		}
	}
	if c.flagDefaultSidecarProxyCPULimit != "" {
		sidecarProxyCPULimit, err = resource.ParseQuantity(c.flagDefaultSidecarProxyCPULimit)
		if err != nil {
			c.UI.Error(fmt.Sprintf("-default-sidecar-proxy-cpu-limit is invalid: %s", err))
			return 1
		}
	}
	if sidecarProxyCPULimit.Value() != 0 && sidecarProxyCPURequest.Cmp(sidecarProxyCPULimit) > 0 {
		c.UI.Error(fmt.Sprintf(
			"request must be <= limit: -default-sidecar-proxy-cpu-request value of %q is greater than the -default-sidecar-proxy-cpu-limit value of %q",
			c.flagDefaultSidecarProxyCPURequest, c.flagDefaultSidecarProxyCPULimit))
		return 1
	}

	if c.flagDefaultSidecarProxyMemoryRequest != "" {
		sidecarProxyMemoryRequest, err = resource.ParseQuantity(c.flagDefaultSidecarProxyMemoryRequest)
		if err != nil {
			c.UI.Error(fmt.Sprintf("-default-sidecar-proxy-memory-request is invalid: %s", err))
			return 1
		}
	}
	if c.flagDefaultSidecarProxyMemoryLimit != "" {
		sidecarProxyMemoryLimit, err = resource.ParseQuantity(c.flagDefaultSidecarProxyMemoryLimit)
		if err != nil {
			c.UI.Error(fmt.Sprintf("-default-sidecar-proxy-memory-limit is invalid: %s", err))
			return 1
		}
	}
	if sidecarProxyMemoryLimit.Value() != 0 && sidecarProxyMemoryRequest.Cmp(sidecarProxyMemoryLimit) > 0 {
		c.UI.Error(fmt.Sprintf(
			"request must be <= limit: -default-sidecar-proxy-memory-request value of %q is greater than the -default-sidecar-proxy-memory-limit value of %q",
			c.flagDefaultSidecarProxyMemoryRequest, c.flagDefaultSidecarProxyMemoryLimit))
		return 1
	}

	// Validate ports in metrics flags.
	err = common.ValidateUnprivilegedPort("-default-merged-metrics-port", c.flagDefaultMergedMetricsPort)
	if err != nil {
		c.UI.Error(err.Error())
		return 1
	}
	err = common.ValidateUnprivilegedPort("-default-prometheus-scrape-port", c.flagDefaultPrometheusScrapePort)
	if err != nil {
		c.UI.Error(err.Error())
		return 1
	}

	// Validate resource request/limit flags and parse into corev1.ResourceRequirements
	initResources, consulSidecarResources, err := c.parseAndValidateResourceFlags()
	if err != nil {
		c.UI.Error(err.Error())
		return 1
	}

	// We must have an in-cluster K8S client.
	if c.clientset == nil {
		config, err := rest.InClusterConfig()
		if err != nil {
			c.UI.Error(fmt.Sprintf("error loading in-cluster K8S config: %s", err))
			return 1
		}
		c.clientset, err = kubernetes.NewForConfig(config)
		if err != nil {
			c.UI.Error(fmt.Sprintf("error creating K8S client: %s", err))
			return 1
		}
	}

	// Create Consul API config object.
	cfg := api.DefaultConfig()
	c.http.MergeOntoConfig(cfg)
	if cfg.TLSConfig.CAFile == "" && c.flagConsulCACert != "" {
		cfg.TLSConfig.CAFile = c.flagConsulCACert
	}
	consulURLRaw := cfg.Address
	// cfg.Address may or may not be prefixed with scheme.
	if !strings.Contains(cfg.Address, "://") {
		consulURLRaw = fmt.Sprintf("%s://%s", cfg.Scheme, cfg.Address)
	}
	consulURL, err := url.Parse(consulURLRaw)
	if err != nil {
		c.UI.Error(fmt.Sprintf("error parsing consul address %q: %s", consulURLRaw, err))
		return 1
	}

	// Load CA file contents.
	var consulCACert []byte
	if cfg.TLSConfig.CAFile != "" {
		var err error
		consulCACert, err = ioutil.ReadFile(cfg.TLSConfig.CAFile)
		if err != nil {
			c.UI.Error(fmt.Sprintf("error reading Consul's CA cert file %q: %s", cfg.TLSConfig.CAFile, err))
			return 1
		}
	}

	// Set up Consul client.
	if c.consulClient == nil {
		var err error
		c.consulClient, err = consul.NewClient(cfg, c.http.ConsulAPITimeout())
		if err != nil {
			c.UI.Error(fmt.Sprintf("error connecting to Consul agent: %s", err))
			return 1
		}
	}

	// Create a context to be used by the processes started in this command.
	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()

	// Convert allow/deny lists to sets.
	allowK8sNamespaces := flags.ToSet(c.flagAllowK8sNamespacesList)
	denyK8sNamespaces := flags.ToSet(c.flagDenyK8sNamespacesList)

	zapLogger, err := common.ZapLogger(c.flagLogLevel, c.flagLogJSON)
	if err != nil {
		c.UI.Error(fmt.Sprintf("Error setting up logging: %s", err.Error()))
		return 1
	}
	ctrl.SetLogger(zapLogger)
	klog.SetLogger(zapLogger)

	listenSplits := strings.SplitN(c.flagListen, ":", 2)
	if len(listenSplits) < 2 {
		c.UI.Error(fmt.Sprintf("missing port in address: %s", c.flagListen))
		return 1
	}
	port, err := strconv.Atoi(listenSplits[1])
	if err != nil {
		c.UI.Error(fmt.Sprintf("unable to parse port string: %s", err))
		return 1
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		LeaderElection:         true,
		LeaderElectionID:       "consul-controller-lock",
		Host:                   listenSplits[0],
		Port:                   port,
		Logger:                 zapLogger,
		MetricsBindAddress:     "0.0.0.0:9444",
		HealthProbeBindAddress: "0.0.0.0:9445",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		return 1
	}

	metricsConfig := connectinject.MetricsConfig{
		DefaultEnableMetrics:        c.flagDefaultEnableMetrics,
		DefaultEnableMetricsMerging: c.flagDefaultEnableMetricsMerging,
		DefaultMergedMetricsPort:    c.flagDefaultMergedMetricsPort,
		DefaultPrometheusScrapePort: c.flagDefaultPrometheusScrapePort,
		DefaultPrometheusScrapePath: c.flagDefaultPrometheusScrapePath,
	}

	if err = (&connectinject.EndpointsController{
		Client:                     mgr.GetClient(),
		ConsulClient:               c.consulClient,
		ConsulScheme:               consulURL.Scheme,
		ConsulPort:                 consulURL.Port(),
		AllowK8sNamespacesSet:      allowK8sNamespaces,
		DenyK8sNamespacesSet:       denyK8sNamespaces,
		MetricsConfig:              metricsConfig,
		ConsulClientCfg:            cfg,
		EnableConsulPartitions:     c.flagEnablePartitions,
		EnableConsulNamespaces:     c.flagEnableNamespaces,
		ConsulDestinationNamespace: c.flagConsulDestinationNamespace,
		EnableNSMirroring:          c.flagEnableK8SNSMirroring,
		NSMirroringPrefix:          c.flagK8SNSMirroringPrefix,
		CrossNSACLPolicy:           c.flagCrossNamespaceACLPolicy,
		EnableTransparentProxy:     c.flagDefaultEnableTransparentProxy,
		TProxyOverwriteProbes:      c.flagTransparentProxyDefaultOverwriteProbes,
		AuthMethod:                 c.flagACLAuthMethod,
		Log:                        ctrl.Log.WithName("controller").WithName("endpoints"),
		Scheme:                     mgr.GetScheme(),
		ReleaseName:                c.flagReleaseName,
		ReleaseNamespace:           c.flagReleaseNamespace,
		Context:                    ctx,
		ConsulAPITimeout:           c.http.ConsulAPITimeout(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", connectinject.EndpointsController{})
		return 1
	}

	if err = mgr.AddReadyzCheck("ready", connectinject.ReadinessCheck{CertDir: c.flagCertDir}.Ready); err != nil {
		setupLog.Error(err, "unable to create readiness check", "controller", connectinject.EndpointsController{})
		return 1
	}

	if err = (&connectinject.PeeringAcceptorController{
		Client:       mgr.GetClient(),
		ConsulClient: c.consulClient,
		Log:          ctrl.Log.WithName("controller").WithName("peering-acceptor"),
		Scheme:       mgr.GetScheme(),
		Context:      ctx,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "peering-acceptor")
		return 1
	}
	if err = (&connectinject.PeeringDialerController{
		Client:       mgr.GetClient(),
		ConsulClient: c.consulClient,
		Log:          ctrl.Log.WithName("controller").WithName("peering-dialer"),
		Scheme:       mgr.GetScheme(),
		Context:      ctx,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "peering-dialer")
		return 1
	}
	mgr.GetWebhookServer().CertDir = c.flagCertDir

	mgr.GetWebhookServer().Register("/mutate",
		&webhook.Admission{Handler: &connectinject.Handler{
			Clientset:                     c.clientset,
			ConsulClient:                  c.consulClient,
			ImageConsul:                   c.flagConsulImage,
			ImageEnvoy:                    c.flagEnvoyImage,
			EnvoyExtraArgs:                c.flagEnvoyExtraArgs,
			ImageConsulK8S:                c.flagConsulK8sImage,
			RequireAnnotation:             !c.flagDefaultInject,
			AuthMethod:                    c.flagACLAuthMethod,
			ConsulCACert:                  string(consulCACert),
			DefaultProxyCPURequest:        sidecarProxyCPURequest,
			DefaultProxyCPULimit:          sidecarProxyCPULimit,
			DefaultProxyMemoryRequest:     sidecarProxyMemoryRequest,
			DefaultProxyMemoryLimit:       sidecarProxyMemoryLimit,
			MetricsConfig:                 metricsConfig,
			InitContainerResources:        initResources,
			DefaultConsulSidecarResources: consulSidecarResources,
			ConsulPartition:               c.http.Partition(),
			AllowK8sNamespacesSet:         allowK8sNamespaces,
			DenyK8sNamespacesSet:          denyK8sNamespaces,
			EnableNamespaces:              c.flagEnableNamespaces,
			ConsulDestinationNamespace:    c.flagConsulDestinationNamespace,
			EnableK8SNSMirroring:          c.flagEnableK8SNSMirroring,
			K8SNSMirroringPrefix:          c.flagK8SNSMirroringPrefix,
			CrossNamespaceACLPolicy:       c.flagCrossNamespaceACLPolicy,
			EnableTransparentProxy:        c.flagDefaultEnableTransparentProxy,
			TProxyOverwriteProbes:         c.flagTransparentProxyDefaultOverwriteProbes,
			EnableConsulDNS:               c.flagEnableConsulDNS,
			ResourcePrefix:                c.flagResourcePrefix,
			EnableOpenShift:               c.flagEnableOpenShift,
			Log:                           ctrl.Log.WithName("handler").WithName("connect"),
			LogLevel:                      c.flagLogLevel,
			LogJSON:                       c.flagLogJSON,
			ConsulAPITimeout:              c.http.ConsulAPITimeout(),
		}})

	if c.flagEnableWebhookCAUpdate {
		err := c.updateWebhookCABundle(ctx)
		if err != nil {
			setupLog.Error(err, "problem getting CA Cert")
			return 1
		}
	}

	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		return 1
	}
	c.UI.Info("shutting down")
	return 0
}

func (c *Command) updateWebhookCABundle(ctx context.Context) error {
	webhookConfigName := fmt.Sprintf("%s-connect-injector", c.flagResourcePrefix)
	caPath := fmt.Sprintf("%s/%s", c.flagCertDir, WebhookCAFilename)
	caCert, err := ioutil.ReadFile(caPath)
	if err != nil {
		return err
	}
	err = mutatingwebhookconfiguration.UpdateWithCABundle(ctx, c.clientset, webhookConfigName, caCert)
	if err != nil {
		return err
	}
	return nil
}
func (c *Command) validateFlags() error {
	if c.flagConsulK8sImage == "" {
		return errors.New("-consul-k8s-image must be set")

	}
	if c.flagConsulImage == "" {
		return errors.New("-consul-image must be set")
	}
	if c.flagEnvoyImage == "" {
		return errors.New("-envoy-image must be set")
	}
	if c.flagWriteServiceDefaults {
		return errors.New("-enable-central-config is no longer supported")
	}
	if c.flagDefaultProtocol != "" {
		return errors.New("-default-protocol is no longer supported")
	}

	if c.flagEnablePartitions && c.http.Partition() == "" {
		return errors.New("-partition-name must set if -enable-partitions is set to 'true'")
	}

	if c.http.Partition() != "" && !c.flagEnablePartitions {
		return errors.New("-enable-partitions must be set to 'true' if -partition-name is set")
	}

	if c.http.ConsulAPITimeout() <= 0 {
		return errors.New("-consul-api-timeout must be set to a value greater than 0")
	}
	return nil
}
func (c *Command) parseAndValidateResourceFlags() (corev1.ResourceRequirements, corev1.ResourceRequirements, error) {
	// Init container
	var initContainerCPULimit, initContainerCPURequest, initContainerMemoryLimit, initContainerMemoryRequest resource.Quantity

	// Parse and validate the initContainer resources.
	initContainerCPURequest, err := resource.ParseQuantity(c.flagInitContainerCPURequest)
	if err != nil {
		return corev1.ResourceRequirements{}, corev1.ResourceRequirements{},
			fmt.Errorf("-init-container-cpu-request '%s' is invalid: %s", c.flagInitContainerCPURequest, err)
	}
	initContainerCPULimit, err = resource.ParseQuantity(c.flagInitContainerCPULimit)
	if err != nil {
		return corev1.ResourceRequirements{}, corev1.ResourceRequirements{},
			fmt.Errorf("-init-container-cpu-limit '%s' is invalid: %s", c.flagInitContainerCPULimit, err)
	}
	if initContainerCPULimit.Value() != 0 && initContainerCPURequest.Cmp(initContainerCPULimit) > 0 {
		return corev1.ResourceRequirements{}, corev1.ResourceRequirements{}, fmt.Errorf(
			"request must be <= limit: -init-container-cpu-request value of %q is greater than the -init-container-cpu-limit value of %q",
			c.flagInitContainerCPURequest, c.flagInitContainerCPULimit)
	}

	initContainerMemoryRequest, err = resource.ParseQuantity(c.flagInitContainerMemoryRequest)
	if err != nil {
		return corev1.ResourceRequirements{}, corev1.ResourceRequirements{},
			fmt.Errorf("-init-container-memory-request '%s' is invalid: %s", c.flagInitContainerMemoryRequest, err)
	}
	initContainerMemoryLimit, err = resource.ParseQuantity(c.flagInitContainerMemoryLimit)
	if err != nil {
		return corev1.ResourceRequirements{}, corev1.ResourceRequirements{},
			fmt.Errorf("-init-container-memory-limit '%s' is invalid: %s", c.flagInitContainerMemoryLimit, err)
	}
	if initContainerMemoryLimit.Value() != 0 && initContainerMemoryRequest.Cmp(initContainerMemoryLimit) > 0 {
		return corev1.ResourceRequirements{}, corev1.ResourceRequirements{}, fmt.Errorf(
			"request must be <= limit: -init-container-memory-request value of %q is greater than the -init-container-memory-limit value of %q",
			c.flagInitContainerMemoryRequest, c.flagInitContainerMemoryLimit)
	}

	// Put into corev1.ResourceRequirements form
	initResources := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    initContainerCPURequest,
			corev1.ResourceMemory: initContainerMemoryRequest,
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    initContainerCPULimit,
			corev1.ResourceMemory: initContainerMemoryLimit,
		},
	}

	// Consul sidecar
	var consulSidecarCPULimit, consulSidecarCPURequest, consulSidecarMemoryLimit, consulSidecarMemoryRequest resource.Quantity

	// Parse and validate the Consul sidecar resources
	consulSidecarCPURequest, err = resource.ParseQuantity(c.flagDefaultConsulSidecarCPURequest)
	if err != nil {
		return corev1.ResourceRequirements{}, corev1.ResourceRequirements{},
			fmt.Errorf("-default-consul-sidecar-cpu-request '%s' is invalid: %s", c.flagDefaultConsulSidecarCPURequest, err)
	}
	consulSidecarCPULimit, err = resource.ParseQuantity(c.flagDefaultConsulSidecarCPULimit)
	if err != nil {
		return corev1.ResourceRequirements{}, corev1.ResourceRequirements{},
			fmt.Errorf("-default-consul-sidecar-cpu-limit '%s' is invalid: %s", c.flagDefaultConsulSidecarCPULimit, err)
	}
	if consulSidecarCPULimit.Value() != 0 && consulSidecarCPURequest.Cmp(consulSidecarCPULimit) > 0 {
		return corev1.ResourceRequirements{}, corev1.ResourceRequirements{}, fmt.Errorf(
			"request must be <= limit: -default-consul-sidecar-cpu-request value of %q is greater than the -default-consul-sidecar-cpu-limit value of %q",
			c.flagDefaultConsulSidecarCPURequest, c.flagDefaultConsulSidecarCPULimit)
	}

	consulSidecarMemoryRequest, err = resource.ParseQuantity(c.flagDefaultConsulSidecarMemoryRequest)
	if err != nil {
		return corev1.ResourceRequirements{}, corev1.ResourceRequirements{},
			fmt.Errorf("-default-consul-sidecar-memory-request '%s' is invalid: %s", c.flagDefaultConsulSidecarMemoryRequest, err)
	}
	consulSidecarMemoryLimit, err = resource.ParseQuantity(c.flagDefaultConsulSidecarMemoryLimit)
	if err != nil {
		return corev1.ResourceRequirements{}, corev1.ResourceRequirements{},
			fmt.Errorf("-default-consul-sidecar-memory-limit '%s' is invalid: %s", c.flagDefaultConsulSidecarMemoryLimit, err)
	}
	if consulSidecarMemoryLimit.Value() != 0 && consulSidecarMemoryRequest.Cmp(consulSidecarMemoryLimit) > 0 {
		return corev1.ResourceRequirements{}, corev1.ResourceRequirements{}, fmt.Errorf(
			"request must be <= limit: -default-consul-sidecar-memory-request value of %q is greater than the -default-consul-sidecar-memory-limit value of %q",
			c.flagDefaultConsulSidecarMemoryRequest, c.flagDefaultConsulSidecarMemoryLimit)
	}

	// Put into corev1.ResourceRequirements form
	consulSidecarResources := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    consulSidecarCPURequest,
			corev1.ResourceMemory: consulSidecarMemoryRequest,
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    consulSidecarCPULimit,
			corev1.ResourceMemory: consulSidecarMemoryLimit,
		},
	}

	return initResources, consulSidecarResources, nil
}

func (c *Command) Synopsis() string { return synopsis }
func (c *Command) Help() string {
	c.once.Do(c.init)
	return c.help
}

const synopsis = "Inject Connect proxy sidecar."
const help = `
Usage: consul-k8s-control-plane inject-connect [options]

  Run the admission webhook server for injecting the Consul Connect
  proxy sidecar. The sidecar uses Envoy by default.

`
