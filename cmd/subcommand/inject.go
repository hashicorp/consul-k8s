package subcommand

import (
	"crypto/tls"
	"flag"
	"fmt"
	"net/http"
	"sync"

	"github.com/hashicorp/consul-k8s/tools/connect-inject"
	"github.com/hashicorp/consul/command/flags"
	"github.com/mitchellh/cli"
)

type Inject struct {
	UI cli.Ui

	flagCertFile string // TLS cert for listening (PEM)
	flagKeyFile  string // TLS cert private key (PEM)
	flagSet      *flag.FlagSet

	once sync.Once
	help string
}

func (c *Inject) init() {
	c.flagSet = flag.NewFlagSet("", flag.ContinueOnError)
	c.flagSet.StringVar(&c.flagCertFile, "tls-cert-file", "", "PEM-encoded TLS certificate to serve")
	c.flagSet.StringVar(&c.flagKeyFile, "tls-key-file", "", "PEM-encoded TLS private key to serve")
	c.help = flags.Usage(help, c.flagSet)
}

func (c *Inject) Run(args []string) int {
	c.once.Do(c.init)
	if err := c.flagSet.Parse(args); err != nil {
		return 1
	}

	cert, err := tls.LoadX509KeyPair(c.flagCertFile, c.flagKeyFile)
	if err != nil {
		c.UI.Error(fmt.Sprintf("Error loading TLS keypair: %s", err))
		return 1
	}

	var h connectinject.Handler
	mux := http.NewServeMux()
	mux.HandleFunc("/mutate", h.Handle)
	server := &http.Server{
		Addr:      ":8080",
		Handler:   mux,
		TLSConfig: &tls.Config{Certificates: []tls.Certificate{cert}},
	}

	if err := server.ListenAndServeTLS("", ""); err != nil {
		c.UI.Error(fmt.Sprintf("Error listening: %s", err))
		return 1
	}

	return 0
}

func (c *Inject) Synopsis() string { return synopsis }
func (c *Inject) Help() string {
	c.once.Do(c.init)
	return c.help
}

const synopsis = "Inject Connect proxy sidecar."
const help = `
Usage: consul-k8s inject [options]

  Run the admission webhook server for injecting the Consul Connect
  proxy sidecar.

`
