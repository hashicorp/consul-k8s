package connectinject

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/deckarep/golang-set"
	"github.com/hashicorp/consul-k8s/connect-inject"
	"github.com/hashicorp/consul-k8s/helper/cert"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/command/flags"
	"github.com/hashicorp/go-hclog"
	"github.com/mitchellh/cli"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type arrayFlags []string

func (i *arrayFlags) Set(value string) error {
	*i = append(*i, value)
	return nil
}

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

	flagResources       bool   // True to enable resources for init and sidecar pods
	flagCPULimit        string // CPU Limits
	flagMemoryLimit     string // Memory Limits
	flagCPURequest      string // CPU Requests
	flagMemoryRequest   string // Memory Limits

	// Flags to support namespaces
	flagEnableNamespaces           bool     // Use namespacing on all components
	flagConsulDestinationNamespace string   // Consul namespace to register everything if not mirroring
	flagAllowK8sNamespacesList     []string // K8s namespaces to explicitly inject
	flagDenyK8sNamespacesList      []string // K8s namespaces to deny injection (has precedence)
	flagEnableK8SNSMirroring       bool     // Enables mirroring of k8s namespaces into Consul
	flagK8SNSMirroringPrefix       string   // Prefix added to Consul namespaces created when mirroring
	flagCrossNamespaceACLPolicy    string   // The name of the ACL policy to add to every created namespace if ACLs are enabled

	flagSet *flag.FlagSet
	http    *flags.HTTPFlags

	consulClient *api.Client
	clientset    kubernetes.Interface

	once sync.Once
	help string
	cert atomic.Value
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
	c.flagSet.StringVar(&c.flagConsulImage, "consul-image", connectinject.DefaultConsulImage,
		"Docker image for Consul. Defaults to consul:1.7.1.")
	c.flagSet.StringVar(&c.flagEnvoyImage, "envoy-image", connectinject.DefaultEnvoyImage,
		"Docker image for Envoy. Defaults to envoyproxy/envoy-alpine:v1.13.0.")
	c.flagSet.StringVar(&c.flagConsulK8sImage, "consul-k8s-image", "",
		"Docker image for consul-k8s. Used for the connect sidecar.")
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
	c.flagSet.BoolVar(&c.flagEnableNamespaces, "enable-namespaces", false,
		"[Enterprise Only] Enables namespaces, in either a single Consul namespace or mirrored")
	c.flagSet.StringVar(&c.flagConsulDestinationNamespace, "consul-destination-namespace", "default",
		"[Enterprise Only] Defines which Consul namespace to register all injected services into. If '-enable-namespace-mirroring' "+
			"is true, this is not used.")
	c.flagSet.BoolVar(&c.flagEnableK8SNSMirroring, "enable-k8s-namespace-mirroring", false, "[Enterprise Only] Enables "+
		"k8s namespace mirroring")
	c.flagSet.StringVar(&c.flagK8SNSMirroringPrefix, "k8s-namespace-mirroring-prefix", "",
		"[Enterprise Only] Prefix that will be added to all k8s namespaces mirrored into Consul if mirroring is enabled.")
	c.flagSet.StringVar(&c.flagCrossNamespaceACLPolicy, "consul-cross-namespace-acl-policy", "",
		"[Enterprise Only] Name of the ACL policy to attach to all created Consul namespaces to allow service "+
			"discovery across Consul namespaces. Only necessary if ACLs are enabled.")

	c.http = &flags.HTTPFlags{}
	flags.Merge(c.flagSet, c.http.ClientFlags())
	flags.Merge(c.flagSet, c.http.ServerFlags())

	c.flagSet.BoolVar(&c.flagResources, "enable-resources", false,
		"Enable to set custom resources for init and sidecar pods")
	c.flagSet.StringVar(&c.flagCPULimit, "cpu-limit", "", "CPU Limits for init and sidecar pods")
	c.flagSet.StringVar(&c.flagMemoryLimit, "memory-limit", "", "Memory Limits for init and sidecar pods")
	c.flagSet.StringVar(&c.flagCPURequest, "cpu-request", "", "CPU Requests for init and sidecar pods")
	c.flagSet.StringVar(&c.flagMemoryRequest, "memory-request", "", "Memory Requests for init and sidecar pods")
	c.help = flags.Usage(help, c.flagSet)
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
		c.consulClient, err = api.NewClient(cfg)
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
	certCh := make(chan cert.Bundle)
	certNotify := &cert.Notify{Ch: certCh, Source: certSource}
	defer certNotify.Stop()
	go certNotify.Start(context.Background())
	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()
	go c.certWatcher(ctx, certCh, c.clientset)

	// Convert allow/deny lists to sets
	allowSet := mapset.NewSet()
	denySet := mapset.NewSet()
	for _, allow := range c.flagAllowK8sNamespacesList {
		allowSet.Add(allow)
	}
	for _, deny := range c.flagDenyK8sNamespacesList {
		denySet.Add(deny)
	}

	// Build the HTTP handler and server
	injector := connectinject.Handler{
		ConsulClient:               c.consulClient,
		ImageConsul:                c.flagConsulImage,
		ImageEnvoy:                 c.flagEnvoyImage,
		ImageConsulK8S:             c.flagConsulK8sImage,
		RequireAnnotation:          !c.flagDefaultInject,
		AuthMethod:                 c.flagACLAuthMethod,
		WriteServiceDefaults:       c.flagWriteServiceDefaults,
		DefaultProtocol:            c.flagDefaultProtocol,
		ConsulCACert:               string(consulCACert),
		CPULimit:          c.flagCPULimit,
		MemoryLimit:       c.flagMemoryLimit,
		CPURequest:        c.flagCPURequest,
		MemoryRequest:     c.flagMemoryRequest,
		EnableNamespaces:           c.flagEnableNamespaces,
		AllowK8sNamespacesSet:      allowSet,
		DenyK8sNamespacesSet:       denySet,
		ConsulDestinationNamespace: c.flagConsulDestinationNamespace,
		EnableK8SNSMirroring:       c.flagEnableK8SNSMirroring,
		K8SNSMirroringPrefix:       c.flagK8SNSMirroringPrefix,
		CrossNamespaceACLPolicy:    c.flagCrossNamespaceACLPolicy,
		Log:                        hclog.Default().Named("handler"),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/mutate", injector.Handle)
	mux.HandleFunc("/health/ready", c.handleReady)
	var handler http.Handler = mux
	server := &http.Server{
		Addr:      c.flagListen,
		Handler:   handler,
		TLSConfig: &tls.Config{GetCertificate: c.getCertificate},
	}

	c.UI.Info(fmt.Sprintf("Listening on %q...", c.flagListen))
	if err := server.ListenAndServeTLS("", ""); err != nil {
		c.UI.Error(fmt.Sprintf("Error listening: %s", err))
		return 1
	}

	return 0
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

func (c *Command) certWatcher(ctx context.Context, ch <-chan cert.Bundle, clientset kubernetes.Interface) {
	var bundle cert.Bundle
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
				Patch(c.flagAutoName, types.JSONPatchType, []byte(fmt.Sprintf(
					`[{
						"op": "add",
						"path": "/webhooks/0/clientConfig/caBundle",
						"value": %q
					}]`, value)))
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
