package synccatalog

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sync"

	"github.com/hashicorp/consul-k8s/catalog"
	"github.com/hashicorp/consul-k8s/helper/controller"
	"github.com/hashicorp/consul-k8s/subcommand"
	k8sflags "github.com/hashicorp/consul-k8s/subcommand/flags"
	"github.com/hashicorp/consul/command/flags"
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

	flags         *flag.FlagSet
	http          *flags.HTTPFlags
	k8s           *k8sflags.K8SFlags
	flagDefault   bool
	flagNamespace string

	once sync.Once
	help string
}

func (c *Command) init() {
	c.flags = flag.NewFlagSet("", flag.ContinueOnError)
	c.flags.BoolVar(&c.flagDefault, "default-sync", true,
		"If true, all valid services are synced by default. If false, "+
			"the service must be annotated properly to sync. In either case "+
			"an annotation can override the default")
	c.flags.StringVar(&c.flagNamespace, "-k8s-namespace", metav1.NamespaceAll,
		"The Kubernetes namespace to watch for service changes and sync. "+
			"If this is not set then it will default to all namespaces.")

	c.http = &flags.HTTPFlags{}
	c.k8s = &k8sflags.K8SFlags{}
	flags.Merge(c.flags, c.http.ClientFlags())
	flags.Merge(c.flags, c.http.ServerFlags())
	flags.Merge(c.flags, c.k8s.Flags())
	c.help = flags.Usage(help, c.flags)
}

func (c *Command) Run(args []string) int {
	c.once.Do(c.init)
	if err := c.flags.Parse(args); err != nil {
		return 1
	}
	if len(c.flags.Args()) > 0 {
		c.UI.Error(fmt.Sprintf("Should have no non-flag arguments."))
		return 1
	}

	config, err := subcommand.K8SConfig(c.k8s.KubeConfig())
	if err != nil {
		c.UI.Error(fmt.Sprintf("Error retrieving Kubernetes auth: %s", err))
		return 1
	}

	// create the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		c.UI.Error(fmt.Sprintf("Error initializing Kubernetes client: %s", err))
		return 1
	}

	// Setup Consul client
	consulClient, err := c.http.APIClient()
	if err != nil {
		c.UI.Error(fmt.Sprintf("Error connecting to Consul agent: %s", err))
		return 1
	}

	// Create the context we'll use to cancel everything
	ctx, cancelF := context.WithCancel(context.Background())

	// Build the Consul sync and start it
	syncer := &catalog.ConsulSyncer{
		Client:    consulClient,
		Log:       hclog.Default().Named("syncer/consul"),
		Namespace: c.flagNamespace,
	}
	go syncer.Run(ctx)

	// Build the controller and start it
	ctl := &controller.Controller{
		Log: hclog.Default().Named("controller/service"),
		Resource: &catalog.ServiceResource{
			Log:            hclog.Default().Named("controller/service"),
			Client:         clientset,
			Syncer:         syncer,
			Namespace:      c.flagNamespace,
			ExplicitEnable: !c.flagDefault,
		},
	}

	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		ctl.Run(ctx.Done())
	}()

	// Wait on an interrupt to exit
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	<-sigCh
	cancelF()
	<-doneCh
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
