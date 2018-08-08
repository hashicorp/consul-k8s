package subcommand

import (
	"flag"
	"fmt"
	"net/http"

	"github.com/hashicorp/consul-k8s/tools/connect-inject"
	"github.com/mitchellh/cli"
)

type Inject struct {
	UI cli.Ui
}

func (c *Inject) Run(args []string) int {
	flags := flag.NewFlagSet("", flag.ContinueOnError)
	if err := flags.Parse(args); err != nil {
		return 1
	}

	var h connectinject.Handler
	mux := http.NewServeMux()
	mux.HandleFunc("/mutate", h.Handle)
	server := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	if err := server.ListenAndServe(); err != nil {
		c.UI.Error(fmt.Sprintf("Error listening: %s", err))
		return 1
	}

	return 0
}

func (c *Inject) Synopsis() string { return synopsis }
func (c *Inject) Help() string     { return help }

const synopsis = "Inject Connect proxy sidecar."
const help = `
Usage: consul-k8s inject [options]

  Run the admission webhook server for injecting the Consul Connect
  proxy sidecar.

`
