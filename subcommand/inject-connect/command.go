package connectinject

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	connectinject "github.com/hashicorp/consul-k8s/connect-inject"
	"github.com/hashicorp/consul-k8s/consul"
	"github.com/hashicorp/consul-k8s/helper/cert"
	"github.com/hashicorp/consul-k8s/helper/controller"
	"github.com/hashicorp/consul-k8s/subcommand/common"
	"github.com/hashicorp/consul-k8s/subcommand/flags"
	"github.com/hashicorp/consul/api"
	"github.com/mitchellh/cli"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type Command struct {
	UI cli.Ui

	flagListen               string
	flagAutoName             string // MutatingWebhookConfiguration for updating
	flagAutoHosts            string // SANs for the auto-generated TLS cert.
	flagCertFile             string // TLS cert for listening (PEM)
	flagKeyFile              string // TLS cert private key (PEM)
	flagDefaultInject        bool   // True to inject by default
	flagConsulImage          string // Docker image for Consul
	flagEnvoyImage           string // Docker image for Envoy
	flagConsulK8sImage       string // Docker image for consul-k8s
	flagACLAuthMethod        string // Auth Method to use for ACLs, if enabled
	flagWriteServiceDefaults bool   // True to enable central config injection
	flagDefaultProtocol      string // Default protocol for use with central config
	flagConsulCACert         string // [Deprecated] Path to CA Certificate to use when communicating with Consul clients
	flagEnvoyExtraArgs       string // Extra envoy args when starting envoy
	flagLogLevel             string

	// Flags to support namespaces
	flagEnableNamespaces           bool     // Use namespacing on all components
	flagConsulDestinationNamespace string   // Consul namespace to register everything if not mirroring
	flagAllowK8sNamespacesList     []string // K8s namespaces to explicitly inject
	flagDenyK8sNamespacesList      []string // K8s namespaces to deny injection (has precedence)
	flagEnableK8SNSMirroring       bool     // Enables mirroring of k8s namespaces into Consul
	flagK8SNSMirroringPrefix       string   // Prefix added to Consul namespaces created when mirroring
	flagCrossNamespaceACLPolicy    string   // The name of the ACL policy to add to every created namespace if ACLs are enabled

	// Flags to enable connect-inject health checks.
	flagEnableHealthChecks          bool          // Start the health check controller.
	flagHealthChecksReconcilePeriod time.Duration // Period for health check reconcile.

	// Flags for cleanup controller.
	flagEnableCleanupController          bool          // Start the cleanup controller.
	flagCleanupControllerReconcilePeriod time.Duration // Period for cleanup controller reconcile.

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
	flagConsulSidecarCPULimit      string
	flagConsulSidecarCPURequest    string
	flagConsulSidecarMemoryLimit   string
	flagConsulSidecarMemoryRequest string

	// Init container resource settings.
	flagInitContainerCPULimit      string
	flagInitContainerCPURequest    string
	flagInitContainerMemoryLimit   string
	flagInitContainerMemoryRequest string

	flagSet *flag.FlagSet
	http    *flags.HTTPFlags

	consulClient *api.Client
	clientset    kubernetes.Interface

	sigCh chan os.Signal
	once  sync.Once
	help  string
	cert  atomic.Value
}

func (c *Command) init() {
	c.flagSet = flag.NewFlagSet("", flag.ContinueOnError)
	c.flagSet.StringVar(&c.flagListen, "listen", ":8080", "Address to bind listener to.")
	c.flagSet.BoolVar(&c.flagDefaultInject, "default-inject", true, "Inject by default.")
	c.flagSet.StringVar(&c.flagAutoName, "tls-auto", "",
		"MutatingWebhookConfiguration name. If specified, will auto generate cert bundle.")
	c.flagSet.StringVar(&c.flagAutoHosts, "tls-auto-hosts", "",
		"Comma-separated hosts for auto-generated TLS cert. If specified, will auto generate cert bundle.")
	c.flagSet.StringVar(&c.flagCertFile, "tls-cert-file", "",
		"PEM-encoded TLS certificate to serve. If blank, will generate random cert.")
	c.flagSet.StringVar(&c.flagKeyFile, "tls-key-file", "",
		"PEM-encoded TLS private key to serve. If blank, will generate random cert.")
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
	c.flagSet.BoolVar(&c.flagEnableHealthChecks, "enable-health-checks-controller", false,
		"Enables health checks controller.")
	c.flagSet.DurationVar(&c.flagHealthChecksReconcilePeriod, "health-checks-reconcile-period", 1*time.Minute, "Reconcile period for health checks controller.")
	c.flagSet.BoolVar(&c.flagEnableCleanupController, "enable-cleanup-controller", true,
		"Enables cleanup controller that cleans up stale Consul service instances.")
	c.flagSet.DurationVar(&c.flagCleanupControllerReconcilePeriod, "cleanup-controller-reconcile-period", 5*time.Minute, "Reconcile period for cleanup controller.")
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
	c.flagSet.StringVar(&c.flagLogLevel, "log-level", "info",
		"Log verbosity level. Supported values (in order of detail) are \"trace\", "+
			"\"debug\", \"info\", \"warn\", and \"error\".")

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
	c.flagSet.StringVar(&c.flagConsulSidecarCPURequest, "consul-sidecar-cpu-request", "20m", "Consul sidecar CPU request.")
	c.flagSet.StringVar(&c.flagConsulSidecarCPULimit, "consul-sidecar-cpu-limit", "20m", "Consul sidecar CPU limit.")
	c.flagSet.StringVar(&c.flagConsulSidecarMemoryRequest, "consul-sidecar-memory-request", "25Mi", "Consul sidecar memory request.")
	c.flagSet.StringVar(&c.flagConsulSidecarMemoryLimit, "consul-sidecar-memory-limit", "50Mi", "Consul sidecar memory limit.")

	c.http = &flags.HTTPFlags{}

	flags.Merge(c.flagSet, c.http.Flags())
	c.help = flags.Usage(help, c.flagSet)

	// Wait on an interrupt or terminate for exit, be sure to init it before running
	// the controller so that we don't receive an interrupt before it's ready.
	if c.sigCh == nil {
		c.sigCh = make(chan os.Signal, 1)
		signal.Notify(c.sigCh, syscall.SIGINT, syscall.SIGTERM)
	}
}

func (c *Command) Run(args []string) int {
	c.once.Do(c.init)
	if err := c.flagSet.Parse(args); err != nil {
		return 1
	}

	// Validate flags.
	if c.flagConsulK8sImage == "" {
		c.UI.Error("-consul-k8s-image must be set")
		return 1
	}
	if c.flagConsulImage == "" {
		c.UI.Error("-consul-image must be set")
		return 1
	}
	if c.flagEnvoyImage == "" {
		c.UI.Error("-envoy-image must be set")
		return 1
	}
	if c.flagWriteServiceDefaults {
		c.UI.Error("-enable-central-config is no longer supported")
		return 1
	}
	if c.flagDefaultProtocol != "" {
		c.UI.Error("-default-protocol is no longer supported")
		return 1
	}

	logger, err := common.Logger(c.flagLogLevel)
	if err != nil {
		c.UI.Error(err.Error())
		return 1
	}

	// Proxy resources
	var sidecarProxyCPULimit, sidecarProxyCPURequest, sidecarProxyMemoryLimit, sidecarProxyMemoryRequest resource.Quantity
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

	// Validate ports in metrics flags
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

	// We must have an in-cluster K8S client
	if c.clientset == nil {
		config, err := rest.InClusterConfig()
		if err != nil {
			c.UI.Error(fmt.Sprintf("Error loading in-cluster K8S config: %s", err))
			return 1
		}
		c.clientset, err = kubernetes.NewForConfig(config)
		if err != nil {
			c.UI.Error(fmt.Sprintf("Error creating K8S client: %s", err))
			return 1
		}
	}

	// create Consul API config object
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
		c.UI.Error(fmt.Sprintf("Error parsing consul address %q: %s", consulURLRaw, err))
		return 1
	}

	// load CA file contents
	var consulCACert []byte
	if cfg.TLSConfig.CAFile != "" {
		var err error
		consulCACert, err = ioutil.ReadFile(cfg.TLSConfig.CAFile)
		if err != nil {
			c.UI.Error(fmt.Sprintf("Error reading Consul's CA cert file %q: %s", cfg.TLSConfig.CAFile, err))
			return 1
		}
	}

	// Set up Consul client
	if c.consulClient == nil {
		var err error
		c.consulClient, err = consul.NewClient(cfg)
		if err != nil {
			c.UI.Error(fmt.Sprintf("Error connecting to Consul agent: %s", err))
			return 1
		}
	}

	// Determine where to source the certificates from
	var certSource cert.Source = &cert.GenSource{
		Name:  "Connect Inject",
		Hosts: strings.Split(c.flagAutoHosts, ","),
	}
	if c.flagCertFile != "" {
		certSource = &cert.DiskSource{
			CertPath: c.flagCertFile,
			KeyPath:  c.flagKeyFile,
		}
	}

	// Create the certificate notifier so we can update for certificates,
	// then start all the background routines for updating certificates.
	certCh := make(chan cert.MetaBundle)
	certNotify := &cert.Notify{Ch: certCh, Source: certSource}
	defer certNotify.Stop()
	go certNotify.Start(context.Background())
	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()
	go c.certWatcher(ctx, certCh, c.clientset)

	// Convert allow/deny lists to sets
	allowK8sNamespaces := flags.ToSet(c.flagAllowK8sNamespacesList)
	denyK8sNamespaces := flags.ToSet(c.flagDenyK8sNamespacesList)

	// Build the HTTP handler and server
	injector := connectinject.Handler{
		ConsulClient:                c.consulClient,
		ImageConsul:                 c.flagConsulImage,
		ImageEnvoy:                  c.flagEnvoyImage,
		EnvoyExtraArgs:              c.flagEnvoyExtraArgs,
		ImageConsulK8S:              c.flagConsulK8sImage,
		RequireAnnotation:           !c.flagDefaultInject,
		AuthMethod:                  c.flagACLAuthMethod,
		ConsulCACert:                string(consulCACert),
		DefaultProxyCPURequest:      sidecarProxyCPURequest,
		DefaultProxyCPULimit:        sidecarProxyCPULimit,
		DefaultProxyMemoryRequest:   sidecarProxyMemoryRequest,
		DefaultProxyMemoryLimit:     sidecarProxyMemoryLimit,
		DefaultEnableMetrics:        c.flagDefaultEnableMetrics,
		DefaultEnableMetricsMerging: c.flagDefaultEnableMetricsMerging,
		DefaultMergedMetricsPort:    c.flagDefaultMergedMetricsPort,
		DefaultPrometheusScrapePort: c.flagDefaultPrometheusScrapePort,
		DefaultPrometheusScrapePath: c.flagDefaultPrometheusScrapePath,
		InitContainerResources:      initResources,
		ConsulSidecarResources:      consulSidecarResources,
		EnableNamespaces:            c.flagEnableNamespaces,
		AllowK8sNamespacesSet:       allowK8sNamespaces,
		DenyK8sNamespacesSet:        denyK8sNamespaces,
		ConsulDestinationNamespace:  c.flagConsulDestinationNamespace,
		EnableK8SNSMirroring:        c.flagEnableK8SNSMirroring,
		K8SNSMirroringPrefix:        c.flagK8SNSMirroringPrefix,
		CrossNamespaceACLPolicy:     c.flagCrossNamespaceACLPolicy,
		Log:                         logger.Named("handler"),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/mutate", injector.Handle)
	mux.HandleFunc("/health/ready", c.handleReady)
	var handler http.Handler = mux
	serverErrors := make(chan error)
	server := &http.Server{
		Addr:      c.flagListen,
		Handler:   handler,
		TLSConfig: &tls.Config{GetCertificate: c.getCertificate},
	}

	// Start the mutating webhook server.
	go func() {
		c.UI.Info(fmt.Sprintf("Listening on %q...", c.flagListen))
		if err := server.ListenAndServeTLS("", ""); err != nil {
			c.UI.Error(fmt.Sprintf("Error listening: %s", err))
			serverErrors <- err
		}
	}()

	// Start the cleanup controller that cleans up Consul service instances
	// still registered after the pod has been deleted (usually due to a force delete).
	ctrlExitCh := make(chan error)

	if c.flagEnableCleanupController {
		cleanupResource := connectinject.CleanupResource{
			Log:                    logger.Named("cleanupResource"),
			KubernetesClient:       c.clientset,
			Ctx:                    ctx,
			ReconcilePeriod:        c.flagCleanupControllerReconcilePeriod,
			ConsulClient:           c.consulClient,
			ConsulScheme:           consulURL.Scheme,
			ConsulPort:             consulURL.Port(),
			EnableConsulNamespaces: c.flagEnableNamespaces,
		}
		cleanupCtrl := &controller.Controller{
			Log:      logger.Named("cleanupController"),
			Resource: &cleanupResource,
		}
		go func() {
			cleanupCtrl.Run(ctx.Done())
			if ctx.Err() == nil {
				ctrlExitCh <- fmt.Errorf("cleanup controller exited unexpectedly")
			}
		}()
	}

	if c.flagEnableHealthChecks {
		healthResource := connectinject.HealthCheckResource{
			Log:                 logger.Named("healthCheckResource"),
			KubernetesClientset: c.clientset,
			ConsulScheme:        consulURL.Scheme,
			ConsulPort:          consulURL.Port(),
			Ctx:                 ctx,
			ReconcilePeriod:     c.flagHealthChecksReconcilePeriod,
		}

		healthChecksCtrl := &controller.Controller{
			Log:      logger.Named("healthCheckController"),
			Resource: &healthResource,
		}

		// Start the health check controller, reconcile is started at the same time
		// and new events will queue in the informer.
		go func() {
			healthChecksCtrl.Run(ctx.Done())
			// If ctl.Run() exits before ctx is cancelled, then our health checks
			// controller isn't running. In that case we need to shutdown since
			// this is unrecoverable.
			if ctx.Err() == nil {
				ctrlExitCh <- fmt.Errorf("health checks controller exited unexpectedly")
			}
		}()
	}

	// Block until we get a signal or something errors.
	select {
	case sig := <-c.sigCh:
		c.UI.Info(fmt.Sprintf("%s received, shutting down", sig))
		if err := server.Close(); err != nil {
			c.UI.Error(fmt.Sprintf("shutting down server: %v", err))
			return 1
		}
		return 0

	case <-serverErrors:
		return 1

	case err := <-ctrlExitCh:
		c.UI.Error(fmt.Sprintf("controller error: %v", err))
		return 1
	}
}

func (c *Command) interrupt() {
	c.sendSignal(syscall.SIGINT)
}

func (c *Command) sendSignal(sig os.Signal) {
	c.sigCh <- sig
}

func (c *Command) handleReady(rw http.ResponseWriter, req *http.Request) {
	// Always ready at this point. The main readiness check is whether
	// there is a TLS certificate. If we reached this point it means we
	// served a TLS certificate.
	rw.WriteHeader(204)
}

func (c *Command) getCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	certRaw := c.cert.Load()
	if certRaw == nil {
		return nil, fmt.Errorf("No certificate available.")
	}

	return certRaw.(*tls.Certificate), nil
}

func (c *Command) certWatcher(ctx context.Context, ch <-chan cert.MetaBundle, clientset kubernetes.Interface) {
	var bundle cert.MetaBundle
	for {
		select {
		case bundle = <-ch:
			c.UI.Output("Updated certificate bundle received. Updating certs...")
			// Bundle is updated, set it up

		case <-time.After(1 * time.Second):
			// This forces the mutating webhook config to remain updated
			// fairly quickly. This is a jank way to do this and we should
			// look to improve it in the future. Since we use Patch requests
			// it is pretty cheap to do, though.

		case <-ctx.Done():
			// Quit
			return
		}

		cert, err := tls.X509KeyPair(bundle.Cert, bundle.Key)
		if err != nil {
			c.UI.Error(fmt.Sprintf("Error loading TLS keypair: %s", err))
			continue
		}

		// If there is a MWC name set, then update the CA bundle.
		if c.flagAutoName != "" && len(bundle.CACert) > 0 {
			// The CA Bundle value must be base64 encoded
			value := base64.StdEncoding.EncodeToString(bundle.CACert)

			_, err := clientset.AdmissionregistrationV1beta1().
				MutatingWebhookConfigurations().
				Patch(context.TODO(), c.flagAutoName, types.JSONPatchType, []byte(fmt.Sprintf(
					`[{
						"op": "add",
						"path": "/webhooks/0/clientConfig/caBundle",
						"value": %q
					}]`, value)), metav1.PatchOptions{})
			if err != nil {
				c.UI.Error(fmt.Sprintf(
					"Error updating MutatingWebhookConfiguration: %s",
					err))
				continue
			}
		}

		// Update the certificate
		c.cert.Store(&cert)
	}
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
	consulSidecarCPURequest, err = resource.ParseQuantity(c.flagConsulSidecarCPURequest)
	if err != nil {
		return corev1.ResourceRequirements{}, corev1.ResourceRequirements{},
			fmt.Errorf("-consul-sidecar-cpu-request '%s' is invalid: %s", c.flagConsulSidecarCPURequest, err)
	}
	consulSidecarCPULimit, err = resource.ParseQuantity(c.flagConsulSidecarCPULimit)
	if err != nil {
		return corev1.ResourceRequirements{}, corev1.ResourceRequirements{},
			fmt.Errorf("-consul-sidecar-cpu-limit '%s' is invalid: %s", c.flagConsulSidecarCPULimit, err)
	}
	if consulSidecarCPULimit.Value() != 0 && consulSidecarCPURequest.Cmp(consulSidecarCPULimit) > 0 {
		return corev1.ResourceRequirements{}, corev1.ResourceRequirements{}, fmt.Errorf(
			"request must be <= limit: -consul-sidecar-cpu-request value of %q is greater than the -consul-sidecar-cpu-limit value of %q",
			c.flagConsulSidecarCPURequest, c.flagConsulSidecarCPULimit)
	}

	consulSidecarMemoryRequest, err = resource.ParseQuantity(c.flagConsulSidecarMemoryRequest)
	if err != nil {
		return corev1.ResourceRequirements{}, corev1.ResourceRequirements{},
			fmt.Errorf("-consul-sidecar-memory-request '%s' is invalid: %s", c.flagConsulSidecarMemoryRequest, err)
	}
	consulSidecarMemoryLimit, err = resource.ParseQuantity(c.flagConsulSidecarMemoryLimit)
	if err != nil {
		return corev1.ResourceRequirements{}, corev1.ResourceRequirements{},
			fmt.Errorf("-consul-sidecar-memory-limit '%s' is invalid: %s", c.flagConsulSidecarMemoryLimit, err)
	}
	if consulSidecarMemoryLimit.Value() != 0 && consulSidecarMemoryRequest.Cmp(consulSidecarMemoryLimit) > 0 {
		return corev1.ResourceRequirements{}, corev1.ResourceRequirements{}, fmt.Errorf(
			"request must be <= limit: -consul-sidecar-memory-request value of %q is greater than the -consul-sidecar-memory-limit value of %q",
			c.flagConsulSidecarMemoryRequest, c.flagConsulSidecarMemoryLimit)
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
Usage: consul-k8s inject-connect [options]

  Run the admission webhook server for injecting the Consul Connect
  proxy sidecar. The sidecar uses Envoy by default.

`
