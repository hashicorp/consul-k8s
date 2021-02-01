package consulInit

import (
	"flag"
	"fmt"
	"github.com/hashicorp/consul-k8s/subcommand/flags"
	"github.com/hashicorp/consul/api"
	"github.com/mitchellh/cli"
	"io/ioutil"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type Command struct {
	UI cli.Ui

	flagAutoHosts       string // SANs for the auto-generated TLS cert.
	flagCertFile        string // TLS cert for listening (PEM)
	flagKeyFile         string // TLS cert private key (PEM)
	flagEnvoyImage      string // Docker image for Envoy
	flagACLAuthMethod   string // Auth Method to use for ACLs, if enabled
	flagDefaultProtocol string // Default protocol for use with central config
	flagConsulCACert    string // [Deprecated] Path to CA Certificate to use when communicating with Consul clients
	flagEnvoyExtraArgs  string // Extra envoy args when starting envoy
	flagLogLevel        string

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
	c.flagSet.StringVar(&c.flagAutoHosts, "tls-auto-hosts", "",
		"Comma-separated hosts for auto-generated TLS cert. If specified, will auto generate cert bundle.")
	c.flagSet.StringVar(&c.flagCertFile, "tls-cert-file", "",
		"PEM-encoded TLS certificate to serve. If blank, will generate random cert.")
	c.flagSet.StringVar(&c.flagKeyFile, "tls-key-file", "",
		"PEM-encoded TLS private key to serve. If blank, will generate random cert.")
	c.flagSet.StringVar(&c.flagEnvoyImage, "envoy-image", "",
		"Docker image for Envoy.")
	c.flagSet.StringVar(&c.flagEnvoyExtraArgs, "envoy-extra-args", "",
		"Extra envoy command line args to be set when starting envoy (e.g \"--log-level debug --disable-hot-restart\").")
	c.flagSet.StringVar(&c.flagACLAuthMethod, "acl-auth-method", "",
		"The name of the Kubernetes Auth Method to use for connectInjection if ACLs are enabled.")
	c.flagSet.StringVar(&c.flagDefaultProtocol, "default-protocol", "",
		"The default protocol to use in central config registrations.")
	c.flagSet.StringVar(&c.flagConsulCACert, "consul-ca-cert", "",
		"[Deprecated] Please use '-ca-file' flag instead. Path to CA certificate to use if communicating with Consul clients over HTTPS.")
	c.flagSet.StringVar(&c.flagLogLevel, "log-level", "info",
		"Log verbosity level. Supported values (in order of detail) are \"trace\", "+
			"\"debug\", \"info\", \"warn\", and \"error\".")

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
	localConfig := api.DefaultConfig()
	c.consulClient, _ = api.NewClient(localConfig)

	// Setup a health check.
	mux := http.NewServeMux()
	mux.HandleFunc("/health/ready", c.handleReady)

	podName := os.Getenv("POD_NAME")
	podNamespace := os.Getenv("POD_NAMESPACE")
	if podName == "" || podNamespace == "" {
		c.UI.Error(fmt.Sprintf("unable to get pod name/namespace: %s/%s", podName, podNamespace))
		return 1
	}
	//	Wait for the service to exist which matches this pod
	for {
		// continuously poll consul agent for a service registered with this pod name
		time.Sleep(1 * time.Second)
		// TODO: label filtering now that iryna fixed it!
		serviceList, err := c.consulClient.Agent().Services()
		if err != nil {
			c.UI.Error(fmt.Sprintf("unable to get agent services: %s", err))
			continue
		}
		for _, y := range serviceList {
			if y.Meta["pod-name"] == podName {
				c.UI.Info(fmt.Sprintf("Registered pod has been detected: %s", y.Meta["pod-name"]))
				// write the proxyid to a file in the emptydir vol
				data := fmt.Sprintf("%s-%s-%s", podName, y.ID, "sidecar-proxy")
				err = ioutil.WriteFile("/consul/connect-inject/proxyid", []byte(data), 0777)
				if err != nil {
					c.UI.Error(fmt.Sprintf("unable to write proxyid out: %s", err))
					return 1
				}
				return 0
			}
		}
	}
	return 1
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

func (c *Command) Synopsis() string { return synopsis }
func (c *Command) Help() string {
	c.once.Do(c.init)
	return c.help
}

const synopsis = "inject init container"
const help = `
Usage: consul-k8s consul-init [options]

	Continuously polls for registered services before passing along
    the requisite information to the init_container which bootstraps
    envoy.
`
