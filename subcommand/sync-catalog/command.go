package synccatalog

import (
	"flag"
	"sync"

	"github.com/hashicorp/consul-k8s/catalog"
	"github.com/hashicorp/consul-k8s/helper/controller"
	"github.com/hashicorp/consul/command/flags"
	"github.com/hashicorp/go-hclog"
	"github.com/mitchellh/cli"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/client-go/tools/clientcmd"
)

// Command is the command for syncing the K8S and Consul service
// catalogs (one or both directions).
type Command struct {
	UI cli.Ui

	flagSet *flag.FlagSet

	once sync.Once
	help string
}

func (c *Command) init() {
	c.flagSet = flag.NewFlagSet("", flag.ContinueOnError)
	c.help = flags.Usage(help, c.flagSet)
}

func (c *Command) Run(args []string) int {
	// use the current context in kubeconfig
	config, err := clientcmd.BuildConfigFromFlags("", "/Users/mitchellh/.kube/config")
	if err != nil {
		panic(err.Error())
	}

	// create the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	ctl := &controller.Controller{
		Log: hclog.Default().Named("controller"),
		Resource: &catalog.ServiceResource{
			Log:    hclog.Default().Named("service"),
			Client: clientset,
		},
	}

	ctl.Run(make(chan struct{}))
	return 0
}

func (c *Command) Synopsis() string { return synopsis }
func (c *Command) Help() string {
	c.once.Do(c.init)
	return c.help
}

const synopsis = "Sync Kubernetes services and Consul services."
const help = `
Usage: consul-k8s sync-catalog [options]

  Sync K8S pods, services, and more with the Consul service catalog.
  This enables K8S services to discover and communicate with external
  services, and allows external services to discover and communicate with
  K8S services.

`
