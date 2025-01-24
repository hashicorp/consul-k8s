// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package connectinject

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/hashicorp/consul-server-connection-manager/discovery"
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
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	gwv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	authv2beta1 "github.com/hashicorp/consul-k8s/control-plane/api/auth/v2beta1"
	meshv2beta1 "github.com/hashicorp/consul-k8s/control-plane/api/mesh/v2beta1"
	multiclusterv2 "github.com/hashicorp/consul-k8s/control-plane/api/multicluster/v2"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/common"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/flags"
)

const (
	WebhookCAFilename = "ca.crt"
)

type Command struct {
	UI cli.Ui

	flagListen                string
	flagCertDir               string // Directory with TLS certs for listening (PEM)
	flagDefaultInject         bool   // True to inject by default
	flagConfigFile            string // Path to a config file in JSON format
	flagConsulImage           string // Docker image for Consul
	flagConsulDataplaneImage  string // Docker image for Envoy
	flagConsulK8sImage        string // Docker image for consul-k8s
	flagGlobalImagePullPolicy string // Pull policy for all Consul images (consul, consul-dataplane, consul-k8s)
	flagACLAuthMethod         string // Auth Method to use for ACLs, if enabled
	flagEnvoyExtraArgs        string // Extra envoy args when starting envoy
	flagEnableWebhookCAUpdate bool
	flagLogLevel              string
	flagLogJSON               bool
	flagResourceAPIs          bool // Use V2 APIs
	flagV2Tenancy             bool // Use V2 partitions (ent only) and namespaces instead of V1 counterparts

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
	flagDefaultEnvoyProxyConcurrency     int

	// Proxy lifecycle settings.
	flagDefaultEnableSidecarProxyLifecycle                       bool
	flagDefaultEnableSidecarProxyLifecycleShutdownDrainListeners bool
	flagDefaultSidecarProxyLifecycleShutdownGracePeriodSeconds   int
	flagDefaultSidecarProxyLifecycleStartupGracePeriodSeconds    int
	flagDefaultSidecarProxyLifecycleGracefulPort                 string
	flagDefaultSidecarProxyLifecycleGracefulShutdownPath         string
	flagDefaultSidecarProxyLifecycleGracefulStartupPath          string

	flagDefaultSidecarProxyStartupFailureSeconds  int
	flagDefaultSidecarProxyLivenessFailureSeconds int

	// Metrics settings.
	flagDefaultEnableMetrics        bool
	flagEnableGatewayMetrics        bool
	flagDefaultEnableMetricsMerging bool
	flagDefaultMergedMetricsPort    string
	flagDefaultPrometheusScrapePort string
	flagDefaultPrometheusScrapePath string

	// Init container resource settings.
	flagInitContainerCPULimit      string
	flagInitContainerCPURequest    string
	flagInitContainerMemoryLimit   string
	flagInitContainerMemoryRequest string

	// Transparent proxy flags.
	flagDefaultEnableTransparentProxy          bool
	flagTransparentProxyDefaultOverwriteProbes bool

	// CNI flag.
	flagEnableCNI bool

	// Additional metadata to get applied to nodes.
	flagNodeMeta map[string]string

	// Peering flags.
	flagEnablePeering bool

	// WAN Federation flags.
	flagEnableFederation bool

	flagEnableAutoEncrypt bool

	// Consul telemetry collector
	flagEnableTelemetryCollector bool

	// Consul DNS flags.
	flagEnableConsulDNS bool
	flagResourcePrefix  string

	flagEnableOpenShift bool

	flagSet *flag.FlagSet
	consul  *flags.ConsulFlags

	clientset kubernetes.Interface

	// sidecarProxy* are resource limits that are parsed and validated from other flags
	// these are individual members because there are override annotations
	sidecarProxyCPULimit      resource.Quantity
	sidecarProxyCPURequest    resource.Quantity
	sidecarProxyMemoryLimit   resource.Quantity
	sidecarProxyMemoryRequest resource.Quantity

	// static resources requirements for connect-init
	initContainerResources corev1.ResourceRequirements

	caCertPem []byte

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
	utilruntime.Must(gwv1beta1.AddToScheme(scheme))
	utilruntime.Must(gwv1alpha2.AddToScheme(scheme))

	// V2 resources
	utilruntime.Must(authv2beta1.AddAuthToScheme(scheme))
	utilruntime.Must(meshv2beta1.AddMeshToScheme(scheme))
	utilruntime.Must(multiclusterv2.AddMultiClusterToScheme(scheme))

	// +kubebuilder:scaffold:scheme
}

func (c *Command) init() {
	c.flagSet = flag.NewFlagSet("", flag.ContinueOnError)
	c.flagSet.StringVar(&c.flagListen, "listen", ":8080", "Address to bind listener to.")
	c.flagSet.StringVar(&c.flagConfigFile, "config-file", "", "Path to a JSON config file.")
	c.flagSet.Var((*flags.FlagMapValue)(&c.flagNodeMeta), "node-meta",
		"Metadata to set on the node, formatted as key=value. This flag may be specified multiple times to set multiple meta fields.")
	c.flagSet.BoolVar(&c.flagDefaultInject, "default-inject", true, "Inject by default.")
	c.flagSet.StringVar(&c.flagCertDir, "tls-cert-dir", "",
		"Directory with PEM-encoded TLS certificate and key to serve.")
	c.flagSet.StringVar(&c.flagConsulImage, "consul-image", "",
		"Docker image for Consul.")
	c.flagSet.StringVar(&c.flagConsulDataplaneImage, "consul-dataplane-image", "",
		"Docker image for Consul Dataplane.")
	c.flagSet.StringVar(&c.flagConsulK8sImage, "consul-k8s-image", "",
		"Docker image for consul-k8s. Used for the connect sidecar.")
	c.flagSet.StringVar(&c.flagGlobalImagePullPolicy, "global-image-pull-policy", "",
		"ImagePullPolicy for all images used by Consul (consul, consul-dataplane, consul-k8s).")
	c.flagSet.BoolVar(&c.flagEnablePeering, "enable-peering", false, "Enable cluster peering controllers.")
	c.flagSet.BoolVar(&c.flagEnableFederation, "enable-federation", false, "Enable Consul WAN Federation.")
	c.flagSet.StringVar(&c.flagEnvoyExtraArgs, "envoy-extra-args", "",
		"Extra envoy command line args to be set when starting envoy (e.g \"--log-level debug --disable-hot-restart\").")
	c.flagSet.StringVar(&c.flagACLAuthMethod, "acl-auth-method", "",
		"The name of the Kubernetes Auth Method to use for connectInjection if ACLs are enabled.")
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
	c.flagSet.BoolVar(&c.flagEnableCNI, "enable-cni", false,
		"Enable CNI traffic redirection for all Consul service mesh applications.")
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
	c.flagSet.BoolVar(&c.flagEnableAutoEncrypt, "enable-auto-encrypt", false,
		"Indicates whether TLS with auto-encrypt should be used when talking to Consul clients.")
	c.flagSet.BoolVar(&c.flagEnableTelemetryCollector, "enable-telemetry-collector", false,
		"Indicates whether proxies should be registered with configuration to enable forwarding metrics to consul-telemetry-collector")
	c.flagSet.StringVar(&c.flagLogLevel, "log-level", zapcore.InfoLevel.String(),
		fmt.Sprintf("Log verbosity level. Supported values (in order of detail) are "+
			"%q, %q, %q, and %q.", zapcore.DebugLevel.String(), zapcore.InfoLevel.String(), zapcore.WarnLevel.String(), zapcore.ErrorLevel.String()))
	c.flagSet.BoolVar(&c.flagLogJSON, "log-json", false,
		"Enable or disable JSON output format for logging.")
	c.flagSet.BoolVar(&c.flagResourceAPIs, "enable-resource-apis", false,
		"Enable or disable Consul V2 Resource APIs.")
	c.flagSet.BoolVar(&c.flagV2Tenancy, "enable-v2tenancy", false,
		"Enable or disable Consul V2 tenancy.")

	// Proxy sidecar resource setting flags.
	c.flagSet.StringVar(&c.flagDefaultSidecarProxyCPURequest, "default-sidecar-proxy-cpu-request", "", "Default sidecar proxy CPU request.")
	c.flagSet.StringVar(&c.flagDefaultSidecarProxyCPULimit, "default-sidecar-proxy-cpu-limit", "", "Default sidecar proxy CPU limit.")
	c.flagSet.StringVar(&c.flagDefaultSidecarProxyMemoryRequest, "default-sidecar-proxy-memory-request", "", "Default sidecar proxy memory request.")
	c.flagSet.StringVar(&c.flagDefaultSidecarProxyMemoryLimit, "default-sidecar-proxy-memory-limit", "", "Default sidecar proxy memory limit.")

	// Proxy lifecycle setting flags.
	c.flagSet.BoolVar(&c.flagDefaultEnableSidecarProxyLifecycle, "default-enable-sidecar-proxy-lifecycle", false, "Default for enabling sidecar proxy lifecycle management.")
	c.flagSet.BoolVar(&c.flagDefaultEnableSidecarProxyLifecycleShutdownDrainListeners, "default-enable-sidecar-proxy-lifecycle-shutdown-drain-listeners", false, "Default for enabling sidecar proxy listener draining of inbound connections during shutdown.")
	c.flagSet.IntVar(&c.flagDefaultSidecarProxyLifecycleShutdownGracePeriodSeconds, "default-sidecar-proxy-lifecycle-shutdown-grace-period-seconds", 0, "Default sidecar proxy shutdown grace period in seconds.")
	c.flagSet.IntVar(&c.flagDefaultSidecarProxyLifecycleStartupGracePeriodSeconds, "default-sidecar-proxy-lifecycle-startup-grace-period-seconds", 0, "Default sidecar proxy startup grace period in seconds.")
	c.flagSet.StringVar(&c.flagDefaultSidecarProxyLifecycleGracefulPort, "default-sidecar-proxy-lifecycle-graceful-port", strconv.Itoa(constants.DefaultGracefulPort), "Default port for sidecar proxy lifecycle management HTTP endpoints.")
	c.flagSet.StringVar(&c.flagDefaultSidecarProxyLifecycleGracefulShutdownPath, "default-sidecar-proxy-lifecycle-graceful-shutdown-path", "/graceful_shutdown", "Default sidecar proxy lifecycle management graceful shutdown path.")
	c.flagSet.StringVar(&c.flagDefaultSidecarProxyLifecycleGracefulStartupPath, "default-sidecar-proxy-lifecycle-graceful-startup-path", "/graceful_startup", "Default sidecar proxy lifecycle management graceful startup path.")

	c.flagSet.IntVar(&c.flagDefaultSidecarProxyStartupFailureSeconds, "default-sidecar-proxy-startup-failure-seconds", 0, "Default number of seconds for the k8s startup probe to fail before the proxy container is restarted. Zero disables the probe.")
	c.flagSet.IntVar(&c.flagDefaultSidecarProxyLivenessFailureSeconds, "default-sidecar-proxy-liveness-failure-seconds", 0, "Default number of seconds for the k8s liveness probe to fail before the proxy container is restarted. Zero disables the probe.")

	// Metrics setting flags.
	c.flagSet.BoolVar(&c.flagDefaultEnableMetrics, "default-enable-metrics", false, "Default for enabling connect service metrics.")
	c.flagSet.BoolVar(&c.flagEnableGatewayMetrics, "enable-gateway-metrics", false, "Allows enabling Consul gateway metrics.")
	c.flagSet.BoolVar(&c.flagDefaultEnableMetricsMerging, "default-enable-metrics-merging", false, "Default for enabling merging of connect service metrics and envoy proxy metrics.")
	c.flagSet.StringVar(&c.flagDefaultMergedMetricsPort, "default-merged-metrics-port", "20100", "Default port for merged metrics endpoint on the consul-sidecar.")
	c.flagSet.StringVar(&c.flagDefaultPrometheusScrapePort, "default-prometheus-scrape-port", "20200", "Default port where Prometheus scrapes connect metrics from.")
	c.flagSet.StringVar(&c.flagDefaultPrometheusScrapePath, "default-prometheus-scrape-path", "/metrics", "Default path where Prometheus scrapes connect metrics from.")

	// Init container resource setting flags.
	c.flagSet.StringVar(&c.flagInitContainerCPURequest, "init-container-cpu-request", "50m", "Init container CPU request.")
	c.flagSet.StringVar(&c.flagInitContainerCPULimit, "init-container-cpu-limit", "50m", "Init container CPU limit.")
	c.flagSet.StringVar(&c.flagInitContainerMemoryRequest, "init-container-memory-request", "25Mi", "Init container memory request.")
	c.flagSet.StringVar(&c.flagInitContainerMemoryLimit, "init-container-memory-limit", "150Mi", "Init container memory limit.")

	c.flagSet.IntVar(&c.flagDefaultEnvoyProxyConcurrency, "default-envoy-proxy-concurrency", 2, "Default Envoy proxy concurrency.")

	c.consul = &flags.ConsulFlags{}

	flags.Merge(c.flagSet, c.consul.Flags())
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

	if err := c.parseAndValidateSidecarProxyFlags(); err != nil {
		c.UI.Error(err.Error())
		return 1
	}

	// Validate resource request/limit flags and parse into corev1.ResourceRequirements
	if err := c.parseAndValidateResourceFlags(); err != nil {
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

	zapLogger, err := common.ZapLogger(c.flagLogLevel, c.flagLogJSON)
	if err != nil {
		c.UI.Error(fmt.Sprintf("Error setting up logging: %s", err.Error()))
		return 1
	}
	ctrl.SetLogger(zapLogger)
	klog.SetLogger(zapLogger)

	// TODO (agentless): find a way to integrate zap logger (via having a generic logger interface in connection manager).
	hcLog, err := common.NamedLogger(c.flagLogLevel, c.flagLogJSON, "consul-server-connection-manager")
	if err != nil {
		c.UI.Error(fmt.Sprintf("Error setting up logging: %s", err.Error()))
		return 1
	}

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

	if c.consul.CACertFile != "" {
		var err error
		c.caCertPem, err = os.ReadFile(c.consul.CACertFile)
		if err != nil {
			c.UI.Error(fmt.Sprintf("error reading Consul's CA cert file %q", c.consul.CACertFile))
			return 1
		}
	}

	// Create a context to be used by the processes started in this command.
	ctx, cancelFunc := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancelFunc()

	// Start Consul server Connection manager.
	serverConnMgrCfg, err := c.consul.ConsulServerConnMgrConfig()
	if err != nil {
		c.UI.Error(fmt.Sprintf("unable to create config for consul-server-connection-manager: %s", err))
		return 1
	}

	watcher, err := discovery.NewWatcher(ctx, serverConnMgrCfg, hcLog.Named("consul-server-connection-manager"))
	if err != nil {
		c.UI.Error(fmt.Sprintf("unable to create Consul server watcher: %s", err))
		return 1
	}
	defer watcher.Stop()
	go watcher.Run()

	// This is a blocking command that is run in order to ensure we only start the
	// connect-inject controllers only after we have access to the Consul server.
	_, err = watcher.State()
	if err != nil {
		c.UI.Error(fmt.Sprintf("unable to start Consul server watcher: %s", err))
		return 1
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:           scheme,
		LeaderElection:   true,
		LeaderElectionID: "consul-controller-lock",
		Logger:           zapLogger,
		Metrics: metricsserver.Options{
			BindAddress: "0.0.0.0:9444",
		},
		HealthProbeBindAddress: "0.0.0.0:9445",
		WebhookServer: webhook.NewServer(webhook.Options{
			CertDir: c.flagCertDir,
			Host:    listenSplits[0],
			Port:    port,
		}),
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		return 1
	}

	// Right now we exclusively start controllers for V1 or V2.
	// In the future we might add a flag to pick and choose from both.
	if c.flagResourceAPIs {
		err = c.configureV2Controllers(ctx, mgr, watcher)
	} else {
		err = c.configureV1Controllers(ctx, mgr, watcher)
	}
	if err != nil {
		setupLog.Error(err, fmt.Sprintf("could not configure controllers: %s", err.Error()))
		return 1
	}

	if err = mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		return 1
	}
	c.UI.Info("shutting down")
	return 0
}

func (c *Command) validateFlags() error {
	if c.flagConsulK8sImage == "" {
		return errors.New("-consul-k8s-image must be set")
	}
	if c.flagConsulImage == "" {
		return errors.New("-consul-image must be set")
	}
	if c.flagConsulDataplaneImage == "" {
		return errors.New("-consul-dataplane-image must be set")
	}

	switch corev1.PullPolicy(c.flagGlobalImagePullPolicy) {
	case corev1.PullAlways:
	case corev1.PullNever:
	case corev1.PullIfNotPresent:
	case "":
		break
	default:
		return errors.New("-global-image-pull-policy must be `IfNotPresent`, `Always`, `Never`, or `` ")
	}

	// In Consul 1.17, multiport beta shipped with v2 catalog + mesh resources backed by v1 tenancy
	// and acls (experiments=[resource-apis]).
	//
	// With Consul 1.18, we built out v2 tenancy with no support for acls, hence need to be explicit
	// about which combination of v1 + v2 features are enabled.
	//
	// To summarize:
	// - experiments=[resource-apis] => v2 catalog and mesh + v1 tenancy and acls
	// - experiments=[resource-apis, v2tenancy] => v2 catalog and mesh + v2 tenancy + acls disabled
	if c.flagV2Tenancy && !c.flagResourceAPIs {
		return errors.New("-enable-resource-apis must be set to 'true' if -enable-v2tenancy is set")
	}

	if c.flagEnablePartitions && c.consul.Partition == "" {
		return errors.New("-partition must set if -enable-partitions is set to 'true'")
	}

	if c.consul.Partition != "" && !c.flagEnablePartitions {
		return errors.New("-enable-partitions must be set to 'true' if -partition is set")
	}

	if c.flagDefaultEnvoyProxyConcurrency < 0 {
		return errors.New("-default-envoy-proxy-concurrency must be >= 0 if set")
	}

	// Validate ports in metrics flags.
	err := common.ValidateUnprivilegedPort("-default-merged-metrics-port", c.flagDefaultMergedMetricsPort)
	if err != nil {
		return err
	}
	err = common.ValidateUnprivilegedPort("-default-prometheus-scrape-port", c.flagDefaultPrometheusScrapePort)
	if err != nil {
		return err
	}

	return nil
}

func (c *Command) parseAndValidateSidecarProxyFlags() error {
	var err error

	if c.flagDefaultSidecarProxyCPURequest != "" {
		c.sidecarProxyCPURequest, err = resource.ParseQuantity(c.flagDefaultSidecarProxyCPURequest)
		if err != nil {
			return fmt.Errorf("-default-sidecar-proxy-cpu-request is invalid: %w", err)
		}
	}

	if c.flagDefaultSidecarProxyCPULimit != "" {
		c.sidecarProxyCPULimit, err = resource.ParseQuantity(c.flagDefaultSidecarProxyCPULimit)
		if err != nil {
			return fmt.Errorf("-default-sidecar-proxy-cpu-limit is invalid: %w", err)
		}
	}
	if c.sidecarProxyCPULimit.Value() != 0 && c.sidecarProxyCPURequest.Cmp(c.sidecarProxyCPULimit) > 0 {
		return fmt.Errorf("request must be <= limit: -default-sidecar-proxy-cpu-request value of %q is greater than the -default-sidecar-proxy-cpu-limit value of %q",
			c.flagDefaultSidecarProxyCPURequest, c.flagDefaultSidecarProxyCPULimit)
	}

	if c.flagDefaultSidecarProxyMemoryRequest != "" {
		c.sidecarProxyMemoryRequest, err = resource.ParseQuantity(c.flagDefaultSidecarProxyMemoryRequest)
		if err != nil {
			return fmt.Errorf("-default-sidecar-proxy-memory-request is invalid: %w", err)
		}
	}
	if c.flagDefaultSidecarProxyMemoryLimit != "" {
		c.sidecarProxyMemoryLimit, err = resource.ParseQuantity(c.flagDefaultSidecarProxyMemoryLimit)
		if err != nil {
			return fmt.Errorf("-default-sidecar-proxy-memory-limit is invalid: %w", err)
		}
	}
	if c.sidecarProxyMemoryLimit.Value() != 0 && c.sidecarProxyMemoryRequest.Cmp(c.sidecarProxyMemoryLimit) > 0 {
		return fmt.Errorf("request must be <= limit: -default-sidecar-proxy-memory-request value of %q is greater than the -default-sidecar-proxy-memory-limit value of %q",
			c.flagDefaultSidecarProxyMemoryRequest, c.flagDefaultSidecarProxyMemoryLimit)
	}

	return nil
}

func (c *Command) parseAndValidateResourceFlags() error {
	// Init container
	var initContainerCPULimit, initContainerCPURequest, initContainerMemoryLimit, initContainerMemoryRequest resource.Quantity

	// Parse and validate the initContainer resources.
	initContainerCPURequest, err := resource.ParseQuantity(c.flagInitContainerCPURequest)
	if err != nil {
		return fmt.Errorf("-init-container-cpu-request '%s' is invalid: %s", c.flagInitContainerCPURequest, err)
	}
	initContainerCPULimit, err = resource.ParseQuantity(c.flagInitContainerCPULimit)
	if err != nil {
		return fmt.Errorf("-init-container-cpu-limit '%s' is invalid: %s", c.flagInitContainerCPULimit, err)
	}
	if initContainerCPULimit.Value() != 0 && initContainerCPURequest.Cmp(initContainerCPULimit) > 0 {
		return fmt.Errorf(
			"request must be <= limit: -init-container-cpu-request value of %q is greater than the -init-container-cpu-limit value of %q",
			c.flagInitContainerCPURequest, c.flagInitContainerCPULimit)
	}

	initContainerMemoryRequest, err = resource.ParseQuantity(c.flagInitContainerMemoryRequest)
	if err != nil {
		return fmt.Errorf("-init-container-memory-request '%s' is invalid: %s", c.flagInitContainerMemoryRequest, err)
	}
	initContainerMemoryLimit, err = resource.ParseQuantity(c.flagInitContainerMemoryLimit)
	if err != nil {
		return fmt.Errorf("-init-container-memory-limit '%s' is invalid: %s", c.flagInitContainerMemoryLimit, err)
	}
	if initContainerMemoryLimit.Value() != 0 && initContainerMemoryRequest.Cmp(initContainerMemoryLimit) > 0 {
		return fmt.Errorf(
			"request must be <= limit: -init-container-memory-request value of %q is greater than the -init-container-memory-limit value of %q",
			c.flagInitContainerMemoryRequest, c.flagInitContainerMemoryLimit)
	}

	// Put into corev1.ResourceRequirements form
	c.initContainerResources = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    initContainerCPURequest,
			corev1.ResourceMemory: initContainerMemoryRequest,
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    initContainerCPULimit,
			corev1.ResourceMemory: initContainerMemoryLimit,
		},
	}

	return nil
}

func (c *Command) Synopsis() string { return synopsis }
func (c *Command) Help() string {
	c.once.Do(c.init)
	return c.help
}

const (
	synopsis = "Inject the proxy sidecar, run endpoints controller and peering controllers."
	help     = `
Usage: consul-k8s-control-plane inject-connect [options]

  Run the admission webhook server for injecting the sidecar proxy,
  the endpoints controller, and the peering controllers.
`
)
