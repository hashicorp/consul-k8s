package synccatalog

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"sync"
	"syscall"
	"time"

	mapset "github.com/deckarep/golang-set"
	catalogtoconsul "github.com/hashicorp/consul-k8s/control-plane/catalog/to-consul"
	catalogtok8s "github.com/hashicorp/consul-k8s/control-plane/catalog/to-k8s"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/helper/controller"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/common"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/flags"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/go-hclog"
	"github.com/mitchellh/cli"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

// Command is the command for syncing the K8S and Consul service
// catalogs (one or both directions).
type Command struct {
	UI cli.Ui

	flags                     *flag.FlagSet
	http                      *flags.HTTPFlags
	k8s                       *flags.K8SFlags
	flagListen                string
	flagToConsul              bool
	flagToK8S                 bool
	flagConsulDomain          string
	flagConsulK8STag          string
	flagConsulNodeName        string
	flagK8SDefault            bool
	flagK8SServicePrefix      string
	flagConsulServicePrefix   string
	flagK8SSourceNamespace    string
	flagK8SWriteNamespace     string
	flagConsulWritePeriod     time.Duration
	flagSyncClusterIPServices bool
	flagSyncLBEndpoints       bool
	flagNodePortSyncType      string
	flagAddK8SNamespaceSuffix bool
	flagLogLevel              string
	flagLogJSON               bool

	// Flags to support namespaces
	flagEnableNamespaces           bool     // Use namespacing on all components
	flagConsulDestinationNamespace string   // Consul namespace to register everything if not mirroring
	flagAllowK8sNamespacesList     []string // K8s namespaces to explicitly inject
	flagDenyK8sNamespacesList      []string // K8s namespaces to deny injection (has precedence)
	flagEnableK8SNSMirroring       bool     // Enables mirroring of k8s namespaces into Consul
	flagK8SNSMirroringPrefix       string   // Prefix added to Consul namespaces created when mirroring
	flagCrossNamespaceACLPolicy    string   // The name of the ACL policy to add to every created namespace if ACLs are enabled

	consulClient *api.Client
	clientset    kubernetes.Interface

	once   sync.Once
	sigCh  chan os.Signal
	help   string
	logger hclog.Logger
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

	c.http = &flags.HTTPFlags{}
	c.k8s = &flags.K8SFlags{}
	flags.Merge(c.flags, c.http.Flags())
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

	// Setup Consul client
	if c.consulClient == nil {
		var err error
		cfg := api.DefaultConfig()
		c.http.MergeOntoConfig(cfg)
		c.consulClient, err = consul.NewClient(cfg)
		if err != nil {
			c.UI.Error(fmt.Sprintf("Error connecting to Consul agent: %s", err))
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

	// Convert allow/deny lists to sets
	allowSet := flags.ToSet(c.flagAllowK8sNamespacesList)
	denySet := flags.ToSet(c.flagDenyK8sNamespacesList)
	if c.flagK8SSourceNamespace != "" {
		// For backwards compatibility, if `flagK8SSourceNamespace` is set,
		// it will be the only allowed namespace
		allowSet = mapset.NewSet(c.flagK8SSourceNamespace)
	}
	c.logger.Info("K8s namespace syncing configuration", "k8s namespaces allowed to be synced", allowSet,
		"k8s namespaces denied from syncing", denySet)

	// Create the context we'll use to cancel everything
	ctx, cancelF := context.WithCancel(context.Background())

	// Start the K8S-to-Consul syncer
	var toConsulCh chan struct{}
	if c.flagToConsul {
		// If namespaces are enabled we need to use a new Consul API endpoint
		// to list node services. This endpoint is only available in Consul
		// 1.7+. To preserve backwards compatibility, when namespaces are not
		// enabled we use a client that queries the older API endpoint.
		var svcsClient catalogtoconsul.ConsulNodeServicesClient
		if c.flagEnableNamespaces {
			svcsClient = &catalogtoconsul.NamespacesNodeServicesClient{
				Client: c.consulClient,
			}
		} else {
			svcsClient = &catalogtoconsul.PreNamespacesNodeServicesClient{
				Client: c.consulClient,
			}
		}
		// Build the Consul sync and start it
		syncer := &catalogtoconsul.ConsulSyncer{
			Client:                   c.consulClient,
			Log:                      c.logger.Named("to-consul/sink"),
			EnableNamespaces:         c.flagEnableNamespaces,
			CrossNamespaceACLPolicy:  c.flagCrossNamespaceACLPolicy,
			SyncPeriod:               c.flagConsulWritePeriod,
			ServicePollPeriod:        c.flagConsulWritePeriod * 2,
			ConsulK8STag:             c.flagConsulK8STag,
			ConsulNodeName:           c.flagConsulNodeName,
			ConsulNodeServicesClient: svcsClient,
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
			Client:    c.clientset,
			Namespace: c.flagK8SWriteNamespace,
			Log:       c.logger.Named("to-k8s/sink"),
			Ctx:       ctx,
		}

		source := &catalogtok8s.Source{
			Client:       c.consulClient,
			Domain:       c.flagConsulDomain,
			Sink:         sink,
			Prefix:       c.flagK8SServicePrefix,
			Log:          c.logger.Named("to-k8s/source"),
			ConsulK8STag: c.flagConsulK8STag,
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

func (c *Command) handleReady(rw http.ResponseWriter, req *http.Request) {
	// The main readiness check is whether sync can talk to
	// the consul cluster, in this case querying for the leader
	_, err := c.consulClient.Status().Leader()
	if err != nil {
		c.UI.Error(fmt.Sprintf("[GET /health/ready] Error getting leader status: %s", err))
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
// so it can exit gracefully. This function is needed for tests
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

	return nil
}

const synopsis = "Sync Kubernetes services and Consul services."
const help = `
Usage: consul-k8s-control-plane sync-catalog [options]

  Sync K8S pods, services, and more with the Consul service catalog.
  This enables K8S services to discover and communicate with external
  services, and allows external services to discover and communicate with
  K8S services.

`
