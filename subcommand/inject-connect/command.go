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
	flagConsulCACert         string // Path to CA Certificate to use when communicating with Consul clients

	// Flags to support namespaces
	flagEnableNamespaces       bool     // Use namespacing on all components
	flagConsulNamespace        string   // Consul namespace to register everything if not mirroring
	flagAllowK8sNamespacesList []string // K8s namespaces to explicitly inject
	flagDenyK8sNamespacesList  []string // K8s namespaces to deny injection (has precedence)
	flagEnableNSMirroring      bool     // Enables mirroring of k8s namespaces into Consul
	flagMirroringPrefix        string   // Prefix added to Consul namespaces created when mirroring

	flagSet *flag.FlagSet
	http    *flags.HTTPFlags

	consulClient *api.Client
	clientset    *kubernetes.Clientset

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
		"Docker image for Consul. Defaults to Consul 1.7.0.")
	c.flagSet.StringVar(&c.flagEnvoyImage, "envoy-image", connectinject.DefaultEnvoyImage,
		"Docker image for Envoy. Defaults to Envoy 1.9.1.")
	c.flagSet.StringVar(&c.flagConsulK8sImage, "consul-k8s-image", "",
		"Docker image for consul-k8s. Used for the connect sidecar.")
	c.flagSet.StringVar(&c.flagACLAuthMethod, "acl-auth-method", "",
		"The name of the Kubernetes Auth Method to use for connectInjection if ACLs are enabled.")
	c.flagSet.BoolVar(&c.flagWriteServiceDefaults, "enable-central-config", false,
		"Write a service-defaults config for every Connect service using protocol from -default-protocol or Pod annotation.")
	c.flagSet.StringVar(&c.flagDefaultProtocol, "default-protocol", "",
		"The default protocol to use in central config registrations.")
	c.flagSet.StringVar(&c.flagConsulCACert, "consul-ca-cert", "",
		"Path to CA certificate to use if communicating with Consul clients over HTTPS.")
	c.flagSet.BoolVar(&c.flagEnableNamespaces, "enable-namespaces", false,
		"Enables namespaces, in either a single Consul namespace or mirrored [Enterprise only feature]")
	c.flagSet.StringVar(&c.flagConsulNamespace, "consul-namespace", "default",
		"Defines which Consul namespace to register all injected services into. If `enable-namespace-mirroring` "+
			"is true, this is not used.")
	c.flagSet.Var((*flags.AppendSliceValue)(&c.flagAllowK8sNamespacesList), "allow-namespace",
		"K8s namespaces to explicitly allow. May be specified multiple times.")
	c.flagSet.Var((*flags.AppendSliceValue)(&c.flagDenyK8sNamespacesList), "deny-namespace",
		"K8s namespaces to explicitly deny. Takes precedence over allow. May be specified multiple times.")
	c.flagSet.BoolVar(&c.flagEnableNSMirroring, "enable-namespace-mirroring", false, "Enables namespace mirroring")
	c.flagSet.StringVar(&c.flagMirroringPrefix, "mirroring-prefix", "",
		"Prefix that will be added to all k8s namespaces mirrored into Consul if mirroring is enabled.")

	c.http = &flags.HTTPFlags{}
	flags.Merge(c.flagSet, c.http.ClientFlags())
	flags.Merge(c.flagSet, c.http.ServerFlags())

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

	// Set up Consul client
	if c.consulClient == nil {
		var err error
		c.consulClient, err = c.http.APIClient()
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

	var consulCACert []byte
	if c.flagConsulCACert != "" {
		var err error
		consulCACert, err = ioutil.ReadFile(c.flagConsulCACert)
		if err != nil {
			c.UI.Error(fmt.Sprintf("Error reading Consul's CA cert file %s: %s", c.flagConsulCACert, err))
			return 1
		}
	}

	// Build the HTTP handler and server
	injector := connectinject.Handler{
		ConsulClient:          c.consulClient,
		ImageConsul:           c.flagConsulImage,
		ImageEnvoy:            c.flagEnvoyImage,
		ImageConsulK8S:        c.flagConsulK8sImage,
		RequireAnnotation:     !c.flagDefaultInject,
		AuthMethod:            c.flagACLAuthMethod,
		WriteServiceDefaults:  c.flagWriteServiceDefaults,
		DefaultProtocol:       c.flagDefaultProtocol,
		ConsulCACert:          string(consulCACert),
		EnableNamespaces:      c.flagEnableNamespaces,
		AllowK8sNamespacesSet: allowSet,
		DenyK8sNamespacesSet:  denySet,
		ConsulNamespaceName:   c.flagConsulNamespace,
		EnableNSMirroring:     c.flagEnableNSMirroring,
		Log:                   hclog.Default().Named("handler"),
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

func (c *Command) certWatcher(ctx context.Context, ch <-chan cert.Bundle, clientset *kubernetes.Clientset) {
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

			_, err := clientset.Admissionregistration().
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
