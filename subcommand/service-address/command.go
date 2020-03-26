package serviceaddress

import (
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"sync"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/hashicorp/consul-k8s/subcommand"
	k8sflags "github.com/hashicorp/consul-k8s/subcommand/flags"
	"github.com/hashicorp/consul/command/flags"
	"github.com/hashicorp/go-hclog"
	"github.com/mitchellh/cli"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type Command struct {
	UI cli.Ui

	flags    *flag.FlagSet
	k8sFlags *k8sflags.K8SFlags

	flagNamespace   string
	flagServiceName string
	flagOutputFile  string

	retryDuration time.Duration
	k8sClient     kubernetes.Interface
	once          sync.Once
	help          string
}

func (c *Command) init() {
	c.flags = flag.NewFlagSet("", flag.ContinueOnError)
	c.flags.StringVar(&c.flagNamespace, "k8s-namespace", "",
		"Kubernetes namespace where service is created")
	c.flags.StringVar(&c.flagServiceName, "name", "",
		"Name of the service")
	c.flags.StringVar(&c.flagOutputFile, "output-file", "",
		"Path to file to write load balancer address")

	c.k8sFlags = &k8sflags.K8SFlags{}
	flags.Merge(c.flags, c.k8sFlags.Flags())
	c.help = flags.Usage(help, c.flags)
}

// Run waits until a Kubernetes service has an ingress address and then writes
// it to an output file.
func (c *Command) Run(args []string) int {
	c.once.Do(c.init)
	if err := c.validateFlags(args); err != nil {
		c.UI.Error(err.Error())
		return 1
	}

	if c.k8sClient == nil {
		config, err := subcommand.K8SConfig(c.k8sFlags.KubeConfig())
		if err != nil {
			c.UI.Error(fmt.Sprintf("Error retrieving Kubernetes auth: %s", err))
			return 1
		}
		c.k8sClient, err = kubernetes.NewForConfig(config)
		if err != nil {
			c.UI.Error(fmt.Sprintf("Error initializing Kubernetes client: %s", err))
			return 1
		}
	}
	if c.retryDuration == 0 {
		c.retryDuration = 1 * time.Second
	}
	log := hclog.New(&hclog.LoggerOptions{
		Level:  hclog.Info,
		Output: os.Stderr,
	})

	// Run until we get an address from the service.
	var address string
	var unretryableErr error
	backoff.Retry(withErrLogger(log, func() error {
		svc, err := c.k8sClient.CoreV1().Services(c.flagNamespace).Get(c.flagServiceName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("getting service %s: %s", c.flagServiceName, err)
		}
		switch svc.Spec.Type {
		case v1.ServiceTypeClusterIP:
			address = svc.Spec.ClusterIP
			return nil
		case v1.ServiceTypeNodePort:
			unretryableErr = errors.New("services of type NodePort are not supported")
			return nil
		case v1.ServiceTypeExternalName:
			unretryableErr = errors.New("services of type ExternalName are not supported")
			return nil
		case v1.ServiceTypeLoadBalancer:
			for _, ingr := range svc.Status.LoadBalancer.Ingress {
				if ingr.IP != "" {
					address = ingr.IP
					return nil
				} else if ingr.Hostname != "" {
					address = ingr.Hostname
					return nil
				}
			}
			return fmt.Errorf("service %s has no ingress IP or hostname", c.flagServiceName)
		default:
			unretryableErr = fmt.Errorf("unknown service type %q", svc.Spec.Type)
			return nil
		}
	}), backoff.NewConstantBackOff(c.retryDuration))

	if unretryableErr != nil {
		c.UI.Error(fmt.Sprintf("Unable to get service address: %s", unretryableErr.Error()))
		return 1
	}

	// Write the address to file.
	err := ioutil.WriteFile(c.flagOutputFile, []byte(address), 0600)
	if err != nil {
		c.UI.Error(fmt.Sprintf("Unable to write address to file: %s", err))
		return 1
	}

	c.UI.Info(fmt.Sprintf("Address %q written to %s successfully", address, c.flagOutputFile))
	return 0
}

func (c *Command) validateFlags(args []string) error {
	if err := c.flags.Parse(args); err != nil {
		return err
	}
	if len(c.flags.Args()) > 0 {
		return errors.New("should have no non-flag arguments")
	}
	if c.flagNamespace == "" {
		return errors.New("-k8s-namespace must be set")
	}
	if c.flagServiceName == "" {
		return errors.New("-name must be set")
	}
	if c.flagOutputFile == "" {
		return errors.New("-output-file must be set")
	}
	return nil
}

// withErrLogger runs op and logs if op returns an error.
// It returns the result of op.
func withErrLogger(log hclog.Logger, op func() error) func() error {
	return func() error {
		err := op()
		if err != nil {
			log.Error(err.Error())
		}
		return err
	}
}

func (c *Command) Synopsis() string { return synopsis }
func (c *Command) Help() string {
	c.once.Do(c.init)
	return c.help
}

const synopsis = "Output Kubernetes Service address to file"
const help = `
Usage: consul-k8s service-address [options]

  Waits until the Kubernetes service specified by -name in namespace
  -k8s-namespace is created, then writes its address to -output-file.
  The address written depends on the service type:
    ClusterIP - Cluster IP
    NodePort - Not supported
    LoadBalancer - Load balancer's IP or hostname
    ExternalName - Not Supported
`
