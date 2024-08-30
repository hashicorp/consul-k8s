// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package synccatalog

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/armon/go-metrics/prometheus"
	"github.com/cenkalti/backoff"
	mapset "github.com/deckarep/golang-set"
	"github.com/hashicorp/consul-server-connection-manager/discovery"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/go-hclog"
	"github.com/mitchellh/cli"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	"github.com/hashicorp/consul-k8s/control-plane/catalog/metrics"
	catalogtoconsul "github.com/hashicorp/consul-k8s/control-plane/catalog/to-consul"
	catalogtok8s "github.com/hashicorp/consul-k8s/control-plane/catalog/to-k8s"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/helper/controller"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/common"
	metricsutil "github.com/hashicorp/consul-k8s/control-plane/subcommand/common"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/flags"
)

// Command is the command for syncing the K8S and Consul service
// catalogs (one or both directions).
type Command struct {
	UI cli.Ui

	flags                        *flag.FlagSet
	consul                       *flags.ConsulFlags
	k8s                          *flags.K8SFlags
	flagListen                   string
	flagToConsul                 bool
	flagToK8S                    bool
	flagConsulDomain             string
	flagConsulK8STag             string
	flagConsulNodeName           string
	flagK8SDefault               bool
	flagK8SServicePrefix         string
	flagConsulServicePrefix      string
	flagK8SSourceNamespace       string
	flagK8SWriteNamespace        string
	flagConsulWritePeriod        time.Duration
	flagSyncClusterIPServices    bool
	flagSyncLBEndpoints          bool
	flagNodePortSyncType         string
	flagAddK8SNamespaceSuffix    bool
	flagLogLevel                 string
	flagLogJSON                  bool
	flagPurgeK8SServicesFromNode string
	flagFilter                   string

	// Flags to support namespaces
	flagEnableNamespaces           bool     // Use namespacing on all components
	flagConsulDestinationNamespace string   // Consul namespace to register everything if not mirroring
	flagAllowK8sNamespacesList     []string // K8s namespaces to explicitly inject
	flagDenyK8sNamespacesList      []string // K8s namespaces to deny injection (has precedence)
	flagEnableK8SNSMirroring       bool     // Enables mirroring of k8s namespaces into Consul
	flagK8SNSMirroringPrefix       string   // Prefix added to Consul namespaces created when mirroring
	flagCrossNamespaceACLPolicy    string   // The name of the ACL policy to add to every created namespace if ACLs are enabled

	// Metrics settings.
	flagEnableMetrics        bool
	flagMetricsPort          string
	flagMetricsPath          string
	flagMetricsRetentionTime string

	// Flags to support Kubernetes Ingress resources
	flagEnableIngress   bool // Register services using the hostname from an ingress resource
	flagLoadBalancerIPs bool // Use the load balancer IP of an ingress resource instead of the hostname

	clientset kubernetes.Interface

	// ready indicates whether this controller is ready to sync services. This will be changed to true once the
	// consul-server-connection-manager has finished initial initialization.
	ready bool

	once           sync.Once
	sigCh          chan os.Signal
	help           string
	logger         hclog.Logger
	connMgr        consul.ServerConnectionManager
	prometheusSink *prometheus.PrometheusSink
}

func (c *Command) init() {
	c.flags = flag.NewFlagSet("", flag.ContinueOnError)
	c.flags.StringVar(&c.flagListen, "listen", ":8080", "Address to bind listener to.")
	c.flags.BoolVar(&c.flagToConsul, "to-consul", true,
		"If true, K8S services will be synced to Consul.")
	c.flags.BoolVar(&c.flagToK8S, "to-k8s", true,
		"If true, Consul services will be synced to Kubernetes.")
	c.flags.BoolVar(&c.flagK8SDefault, "k8s-default-sync", true,
		"If true, all valid services in K8S are synced by default. If false, "+
			"the service must be annotated properly to sync. In either case "+
			"an annotation can override the default")
	c.flags.StringVar(&c.flagK8SServicePrefix, "k8s-service-prefix", "",
		"A prefix to prepend to all services written to Kubernetes from Consul. "+
			"If this is not set then services will have no prefix.")
	c.flags.StringVar(&c.flagConsulServicePrefix, "consul-service-prefix", "",
		"A prefix to prepend to all services written to Consul from Kubernetes. "+
			"If this is not set then services will have no prefix.")
	c.flags.StringVar(&c.flagK8SSourceNamespace, "k8s-source-namespace", metav1.NamespaceAll,
		"The Kubernetes namespace to watch for service changes and sync to Consul. "+
			"If this is not set then it will default to all namespaces.")
	c.flags.StringVar(&c.flagK8SWriteNamespace, "k8s-write-namespace", metav1.NamespaceDefault,
		"The Kubernetes namespace to write to for services from Consul. "+
			"If this is not set then it will default to the default namespace.")
	c.flags.StringVar(&c.flagConsulDomain, "consul-domain", "consul",
		"The domain for Consul services to use when writing services to "+
			"Kubernetes. Defaults to consul.")
	c.flags.StringVar(&c.flagConsulK8STag, "consul-k8s-tag", "k8s",
		"Tag value for K8S services registered in Consul")
	c.flags.StringVar(&c.flagConsulNodeName, "consul-node-name", "k8s-sync",
		"The Consul node name to register for catalog sync. Defaults to k8s-sync. To be discoverable "+
			"via DNS, the name should only contain alpha-numerics and dashes.")
	c.flags.DurationVar(&c.flagConsulWritePeriod, "consul-write-interval", 30*time.Second,
		"The interval to perform syncing operations creating Consul services, formatted "+
			"as a time.Duration. All changes are merged and write calls are only made "+
			"on this interval. Defaults to 30 seconds (30s).")
	c.flags.BoolVar(&c.flagSyncClusterIPServices, "sync-clusterip-services", true,
		"If true, all valid ClusterIP services in K8S are synced by default. If false, "+
			"ClusterIP services are not synced to Consul.")
	c.flags.BoolVar(&c.flagSyncLBEndpoints, "sync-lb-services-endpoints", false,
		"If true, LoadBalancer service endpoints instead of ingress addresses will be synced to Consul. If false, "+
			"LoadBalancer endpoints are not synced to Consul.")
	c.flags.StringVar(&c.flagNodePortSyncType, "node-port-sync-type", "ExternalOnly",
		"Defines the type of sync for NodePort services. Valid options are ExternalOnly, "+
			"InternalOnly and ExternalFirst.")
	c.flags.BoolVar(&c.flagAddK8SNamespaceSuffix, "add-k8s-namespace-suffix", false,
		"If true, Kubernetes namespace will be appended to service names synced to Consul separated by a dash. "+
			"If false, no suffix will be appended to the service names in Consul. "+
			"If the service name annotation is provided, the suffix is not appended.")
	c.flags.StringVar(&c.flagLogLevel, "log-level", "info",
		"Log verbosity level. Supported values (in order of detail) are \"trace\", "+
			"\"debug\", \"info\", \"warn\", and \"error\".")
	c.flags.BoolVar(&c.flagLogJSON, "log-json", false,
		"Enable or disable JSON output format for logging.")
	c.flags.StringVar(&c.flagPurgeK8SServicesFromNode, "purge-k8s-services-from-node", "",
		"Specifies the name of the Consul node for which to deregister synced Kubernetes services.")
	c.flags.StringVar(&c.flagFilter, "filter", "",
		"Specifies the expression used to filter the services on the Consul node that will be deregistered, "+
			"the syntax here is the same as the syntax used in the List Services for Node API in the Consul catalog.")

	c.flags.Var((*flags.AppendSliceValue)(&c.flagAllowK8sNamespacesList), "allow-k8s-namespace",
		"K8s namespaces to explicitly allow. May be specified multiple times.")
	c.flags.Var((*flags.AppendSliceValue)(&c.flagDenyK8sNamespacesList), "deny-k8s-namespace",
		"K8s namespaces to explicitly deny. Takes precedence over allow. May be specified multiple times.")
	c.flags.BoolVar(&c.flagEnableNamespaces, "enable-namespaces", false,
		"[Enterprise Only] Enables namespaces, in either a single Consul namespace or mirrored.")
	c.flags.StringVar(&c.flagConsulDestinationNamespace, "consul-destination-namespace", "default",
		"[Enterprise Only] Defines which Consul namespace to register all synced services into. If '-enable-k8s-namespace-mirroring' "+
			"is true, this is not used.")
	c.flags.BoolVar(&c.flagEnableK8SNSMirroring, "enable-k8s-namespace-mirroring", false, "[Enterprise Only] Enables "+
		"namespace mirroring.")
	c.flags.StringVar(&c.flagK8SNSMirroringPrefix, "k8s-namespace-mirroring-prefix", "",
		"[Enterprise Only] Prefix that will be added to all k8s namespaces mirrored into Consul if mirroring is enabled.")
	c.flags.StringVar(&c.flagCrossNamespaceACLPolicy, "consul-cross-namespace-acl-policy", "",
		"[Enterprise Only] Name of the ACL policy to attach to all created Consul namespaces to allow service "+
			"discovery across Consul namespaces. Only necessary if ACLs are enabled.")

	c.flags.BoolVar(&c.flagEnableMetrics, "enable-metrics", false, "set this flag to enable metrics collection")
	c.flags.StringVar(&c.flagMetricsPath, "metrics-path", "/metrics", "specify to set the path used for metrics scraping")
	c.flags.StringVar(&c.flagMetricsPort, "metrics-port", "20300", "specify to set the port used for metrics scraping")
	c.flags.StringVar(&c.flagMetricsRetentionTime, "prometheus-retention-time", "1m", "configures the retention time for metrics in the Prometheus sink")

	c.flags.BoolVar(&c.flagEnableIngress, "enable-ingress", false,
		"[Enterprise Only] Enables namespaces, in either a single Consul namespace or mirrored.")
	c.flags.BoolVar(&c.flagLoadBalancerIPs, "loadBalancer-ips", false,
		"[Enterprise Only] Enables namespaces, in either a single Consul namespace or mirrored.")

	c.consul = &flags.ConsulFlags{}
	c.k8s = &flags.K8SFlags{}
	flags.Merge(c.flags, c.consul.Flags())
	flags.Merge(c.flags, c.k8s.Flags())

	c.help = flags.Usage(help, c.flags)

	// Wait on an interrupt or terminate to exit. This channel must be initialized before
	// Run() is called so that there are no race conditions where the channel
	// is not defined.
	if c.sigCh == nil {
		c.sigCh = make(chan os.Signal, 1)
		signal.Notify(c.sigCh, syscall.SIGINT, syscall.SIGTERM)
	}
}

func (c *Command) Run(args []string) int {
	c.once.Do(c.init)
	if err := c.flags.Parse(args); err != nil {
		return 1
	}
	if len(c.flags.Args()) > 0 {
		c.UI.Error("Should have no non-flag arguments.")
		return 1
	}

	// Validate flags
	if err := c.validateFlags(); err != nil {
		c.UI.Error(err.Error())
		return 1
	}

	// Create the k8s clientset
	if c.clientset == nil {
		config, err := subcommand.K8SConfig(c.k8s.KubeConfig())
		if err != nil {
			c.UI.Error(fmt.Sprintf("Error retrieving Kubernetes auth: %s", err))
			return 1
		}

		c.clientset, err = kubernetes.NewForConfig(config)
		if err != nil {
			c.UI.Error(fmt.Sprintf("Error initializing Kubernetes client: %s", err))
			return 1
		}
	}

	// Set up logging
	if c.logger == nil {
		var err error
		c.logger, err = common.Logger(c.flagLogLevel, c.flagLogJSON)
		if err != nil {
			c.UI.Error(err.Error())
			return 1
		}
	}

	// Create Consul API config object.
	consulConfig := c.consul.ConsulClientConfig()

	// Create a context to be used by the processes started in this command.
	ctx, cancelFunc := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancelFunc()

	if c.connMgr == nil {
		// Start Consul server Connection manager.
		serverConnMgrCfg, err := c.consul.ConsulServerConnMgrConfig()
		if err != nil {
			c.UI.Error(fmt.Sprintf("unable to create config for consul-server-connection-manager: %s", err))
			return 1
		}
		c.connMgr, err = discovery.NewWatcher(ctx, serverConnMgrCfg, c.logger.Named("consul-server-connection-manager"))
		if err != nil {
			c.UI.Error(fmt.Sprintf("unable to create Consul server watcher: %s", err))
			return 1
		}

		go c.connMgr.Run()
		defer c.connMgr.Stop()
	}

	// This is a blocking command that is run in order to ensure we only start the
	// sync-catalog controllers only after we have access to the Consul server.
	_, err := c.connMgr.State()
	if err != nil {
		c.UI.Error(fmt.Sprintf("unable to start Consul server watcher: %s", err))
		return 1
	}
	c.ready = true

	if c.flagPurgeK8SServicesFromNode != "" {
		consulClient, err := consul.NewClientFromConnMgr(consulConfig, c.connMgr)
		if err != nil {
			c.UI.Error(fmt.Sprintf("unable to instantiate consul client: %s", err))
			return 1
		}
		if err := c.removeAllK8SServicesFromConsulNode(consulClient); err != nil {
			c.UI.Error(fmt.Sprintf("unable to remove all K8S services: %s", err))
			return 1
		}
		return 0
	}

	// Convert allow/deny lists to sets
	allowSet := flags.ToSet(c.flagAllowK8sNamespacesList)
	denySet := flags.ToSet(c.flagDenyK8sNamespacesList)
	if c.flagK8SSourceNamespace != "" {
		// For backwards compatibility, if `flagK8SSourceNamespace` is set,
		// it will be the only allowed namespace
		allowSet = mapset.NewSet(c.flagK8SSourceNamespace)
	}

	metricsConfig := metrics.SyncCatalogMetricsConfig(c.flagEnableMetrics, c.flagMetricsPort, c.flagMetricsPath)
	metricsConfig.PrometheusMetricsRetentionTime = c.flagMetricsRetentionTime

	// Create the metrics sink
	sink, err := c.recordMetrics()
	if err != nil {
		c.logger.Error("Prometheus sink not initialized, metrics cannot be displayed", "error", err)
	}
	c.prometheusSink = sink

	c.logger.Info("K8s namespace syncing configuration", "k8s namespaces allowed to be synced", allowSet,
		"k8s namespaces denied from syncing", denySet)

	// Create the context we'll use to cancel everything
	ctx, cancelF := context.WithCancel(context.Background())

	// Start the K8S-to-Consul syncer
	var toConsulCh chan struct{}
	if c.flagToConsul {
		// Build the Consul sync and start it
		syncer := &catalogtoconsul.ConsulSyncer{
			ConsulClientConfig:      consulConfig,
			ConsulServerConnMgr:     c.connMgr,
			Log:                     c.logger.Named("to-consul/sink"),
			EnableNamespaces:        c.flagEnableNamespaces,
			CrossNamespaceACLPolicy: c.flagCrossNamespaceACLPolicy,
			SyncPeriod:              c.flagConsulWritePeriod,
			ServicePollPeriod:       c.flagConsulWritePeriod * 2,
			ConsulK8STag:            c.flagConsulK8STag,
			ConsulNodeName:          c.flagConsulNodeName,
			PrometheusSink:          c.prometheusSink,
		}
		go syncer.Run(ctx)

		// Build the controller and start it
		ctl := &controller.Controller{
			Log: c.logger.Named("to-consul/controller"),
			Resource: &catalogtoconsul.ServiceResource{
				Log:                        c.logger.Named("to-consul/source"),
				Client:                     c.clientset,
				Syncer:                     syncer,
				Ctx:                        ctx,
				AllowK8sNamespacesSet:      allowSet,
				DenyK8sNamespacesSet:       denySet,
				ExplicitEnable:             !c.flagK8SDefault,
				ClusterIPSync:              c.flagSyncClusterIPServices,
				LoadBalancerEndpointsSync:  c.flagSyncLBEndpoints,
				NodePortSync:               catalogtoconsul.NodePortSyncType(c.flagNodePortSyncType),
				ConsulK8STag:               c.flagConsulK8STag,
				ConsulServicePrefix:        c.flagConsulServicePrefix,
				AddK8SNamespaceSuffix:      c.flagAddK8SNamespaceSuffix,
				EnableNamespaces:           c.flagEnableNamespaces,
				ConsulDestinationNamespace: c.flagConsulDestinationNamespace,
				EnableK8SNSMirroring:       c.flagEnableK8SNSMirroring,
				K8SNSMirroringPrefix:       c.flagK8SNSMirroringPrefix,
				ConsulNodeName:             c.flagConsulNodeName,
				EnableIngress:              c.flagEnableIngress,
				SyncLoadBalancerIPs:        c.flagLoadBalancerIPs,
				MetricsConfig:              metricsConfig,
			},
		}

		toConsulCh = make(chan struct{})
		go func() {
			defer close(toConsulCh)
			ctl.Run(ctx.Done())
		}()
	}

	// Start Consul-to-K8S sync
	var toK8SCh chan struct{}
	if c.flagToK8S {
		sink := &catalogtok8s.K8SSink{
			Client:         c.clientset,
			Namespace:      c.flagK8SWriteNamespace,
			Log:            c.logger.Named("to-k8s/sink"),
			Ctx:            ctx,
			PrometheusSink: c.prometheusSink,
		}

		source := &catalogtok8s.Source{
			ConsulClientConfig:  consulConfig,
			ConsulServerConnMgr: c.connMgr,
			Domain:              c.flagConsulDomain,
			Sink:                sink,
			Prefix:              c.flagK8SServicePrefix,
			Log:                 c.logger.Named("to-k8s/source"),
			ConsulK8STag:        c.flagConsulK8STag,
		}
		go source.Run(ctx)

		// Build the controller and start it
		ctl := &controller.Controller{
			Log:      c.logger.Named("to-k8s/controller"),
			Resource: sink,
		}

		toK8SCh = make(chan struct{})
		go func() {
			defer close(toK8SCh)
			ctl.Run(ctx.Done())
		}()
	}

	// Start healthcheck handler
	go func() {
		mux := http.NewServeMux()
		mux.HandleFunc("/health/ready", c.handleReady)
		var handler http.Handler = mux

		c.UI.Info(fmt.Sprintf("Listening on %q...", c.flagListen))
		if err := http.ListenAndServe(c.flagListen, handler); err != nil {
			c.UI.Error(fmt.Sprintf("Error listening: %s", err))
		}
	}()

	// Start metrics handler
	go func() {
		mux := http.NewServeMux()
		mux.Handle(c.flagMetricsPath, c.authorizeMiddleware()(promhttp.Handler()))
		var handler http.Handler = mux

		c.UI.Info(fmt.Sprintf("Listening on %q...", c.flagMetricsPort))
		if err := http.ListenAndServe(fmt.Sprintf(":%s", c.flagMetricsPort), handler); err != nil {
			c.UI.Error(fmt.Sprintf("Error listening: %s", err))
		}
	}()

	select {
	// Unexpected exit
	case <-toConsulCh:
		cancelF()
		if toK8SCh != nil {
			<-toK8SCh
		}
		return 1

	// Unexpected exit
	case <-toK8SCh:
		cancelF()
		if toConsulCh != nil {
			<-toConsulCh
		}
		return 1

	// Interrupted/terminated, gracefully exit
	case sig := <-c.sigCh:
		c.logger.Info(fmt.Sprintf("%s received, shutting down", sig))
		cancelF()
		if toConsulCh != nil {
			<-toConsulCh
		}
		if toK8SCh != nil {
			<-toK8SCh
		}
		return 0
	}
}

// remove all k8s services from Consul.
func (c *Command) removeAllK8SServicesFromConsulNode(consulClient *api.Client) error {
	node, _, err := consulClient.Catalog().NodeServiceList(c.flagPurgeK8SServicesFromNode, &api.QueryOptions{Filter: c.flagFilter})
	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	services := node.Services
	errChan := make(chan error, 1)
	batchSize := 300
	maxRetries := 2
	retryDelay := 200 * time.Millisecond

	// Ask for user confirmation before purging services
	for {
		c.UI.Info(fmt.Sprintf("Are you sure you want to delete %v K8S services from %v? (y/n): ", len(services), c.flagPurgeK8SServicesFromNode))
		var input string
		fmt.Scanln(&input)
		if input = strings.ToLower(input); input == "y" {
			break
		} else if input == "n" {
			return nil
		} else {
			c.UI.Info("Invalid input. Please enter 'y' or 'n'.")
		}
	}

	for i := 0; i < len(services); i += batchSize {
		end := i + batchSize
		if end > len(services) {
			end = len(services)
		}

		wg.Add(1)
		go func(batch []*api.AgentService) {
			defer wg.Done()

			for _, service := range batch {
				var b backoff.BackOff = backoff.NewConstantBackOff(retryDelay)
				b = backoff.WithMaxRetries(b, uint64(maxRetries))
				err := backoff.Retry(func() error {
					_, err := consulClient.Catalog().Deregister(&api.CatalogDeregistration{
						Node:      c.flagPurgeK8SServicesFromNode,
						ServiceID: service.ID,
					}, nil)
					return err
				}, b)
				if err != nil {
					if len(errChan) == 0 {
						errChan <- err
					}
				}
			}
			c.UI.Info(fmt.Sprintf("Processed %v K8S services from %v", len(batch), c.flagPurgeK8SServicesFromNode))
		}(services[i:end])
		wg.Wait()
	}

	close(errChan)
	if err = <-errChan; err != nil {
		return err
	}
	c.UI.Info("All K8S services were deregistered from Consul")
	return nil
}

func (c *Command) handleReady(rw http.ResponseWriter, _ *http.Request) {
	if !c.ready {
		c.UI.Error("[GET /health/ready] sync catalog controller is not yet ready")
		rw.WriteHeader(500)
		return
	}
	rw.WriteHeader(204)
}

func (c *Command) Synopsis() string { return synopsis }
func (c *Command) Help() string {
	c.once.Do(c.init)
	return c.help
}

// interrupt sends os.Interrupt signal to the command
// so it can exit gracefully. This function is needed for tests.
func (c *Command) interrupt() {
	c.sendSignal(syscall.SIGINT)
}

func (c *Command) sendSignal(sig os.Signal) {
	c.sigCh <- sig
}

func (c *Command) validateFlags() error {
	// For the Consul node name to be discoverable via DNS, it must contain only
	// dashes and alphanumeric characters. Length is also constrained.
	// These restrictions match those defined in Consul's agent definition.
	var invalidDnsRe = regexp.MustCompile(`[^A-Za-z0-9\\-]+`)
	const maxDNSLabelLength = 63

	if invalidDnsRe.MatchString(c.flagConsulNodeName) {
		return fmt.Errorf("-consul-node-name=%s is invalid: node name will not be discoverable "+
			"via DNS due to invalid characters. Valid characters include all alpha-numerics and dashes",
			c.flagConsulNodeName,
		)
	}
	if len(c.flagConsulNodeName) > maxDNSLabelLength {
		return fmt.Errorf("-consul-node-name=%s is invalid: node name will not be discoverable "+
			"via DNS due to it being too long. Valid lengths are between 1 and 63 bytes",
			c.flagConsulNodeName,
		)
	}

	if c.flagMetricsPort != "" {
		if _, valid := metricsutil.ParseScrapePort(c.flagMetricsPort); !valid {
			return errors.New("-metrics-port must be a valid unprivileged port number")
		}
	}

	return nil
}

func (c *Command) recordMetrics() (*prometheus.PrometheusSink, error) {
	var err error

	duration, err := time.ParseDuration(c.flagMetricsRetentionTime)
	if err != nil {
		return &prometheus.PrometheusSink{}, err
	}

	var counters = [][]prometheus.CounterDefinition{
		catalogtoconsul.SyncToConsulCounters,
		catalogtok8s.SyncToK8sCounters,
	}

	var counterDefs []prometheus.CounterDefinition

	for _, counter := range counters {
		counterDefs = append(counterDefs, counter...)
	}

	opts := prometheus.PrometheusOpts{
		Expiration:         duration,
		CounterDefinitions: counterDefs,
		GaugeDefinitions:   catalogtoconsul.SyncCatalogGauge,
	}

	sink, err := prometheus.NewPrometheusSinkFrom(opts)
	if err != nil {
		return &prometheus.PrometheusSink{}, err
	}

	return sink, nil
}

// authorizeMiddleware validates the token and returns http handler.
func (c *Command) authorizeMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// TO-DO: Validate the token and proceed to the next handler
			next.ServeHTTP(w, r)
		})
	}
}

const synopsis = "Sync Kubernetes services and Consul services."
const help = `
Usage: consul-k8s-control-plane sync-catalog [options]

  Sync K8S pods, services, and more with the Consul service catalog.
  This enables K8S services to discover and communicate with external
  services, and allows external services to discover and communicate with
  K8S services.

`
