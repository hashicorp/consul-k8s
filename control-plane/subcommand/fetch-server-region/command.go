// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package fetchserverregion

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sync"

	"github.com/hashicorp/go-hclog"
	"github.com/mitchellh/cli"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/hashicorp/consul-k8s/control-plane/subcommand/common"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/flags"
)

// The consul-logout command issues a Consul logout API request to delete an ACL token.
type Command struct {
	UI cli.Ui

	flagLogLevel   string
	flagLogJSON    bool
	flagNodeName   string
	flagOutputFile string

	flagSet *flag.FlagSet
	k8s     *flags.K8SFlags

	once   sync.Once
	help   string
	logger hclog.Logger

	// for testing
	clientset kubernetes.Interface
}

type Locality struct {
	Region string `json:"region"`
}

type Config struct {
	Locality Locality `json:"locality"`
}

func (c *Command) init() {
	c.flagSet = flag.NewFlagSet("", flag.ContinueOnError)
	c.flagSet.StringVar(&c.flagLogLevel, "log-level", "info",
		"Log verbosity level. Supported values (in order of detail) are \"trace\", "+
			"\"debug\", \"info\", \"warn\", and \"error\".")
	c.flagSet.BoolVar(&c.flagLogJSON, "log-json", false,
		"Enable or disable JSON output format for logging.")
	c.flagSet.StringVar(&c.flagNodeName, "node-name", "",
		"Specifies the node name that will be used.")
	c.flagSet.StringVar(&c.flagOutputFile, "output-file", "",
		"The file path for writing the locality portion of a Consul agent configuration to.")

	c.k8s = &flags.K8SFlags{}
	flags.Merge(c.flagSet, c.k8s.Flags())

	c.help = flags.Usage(help, c.flagSet)

}

func (c *Command) Run(args []string) int {
	var err error
	c.once.Do(c.init)

	if err := c.flagSet.Parse(args); err != nil {
		return 1
	}

	if c.logger == nil {
		c.logger, err = common.Logger(c.flagLogLevel, c.flagLogJSON)
		if err != nil {
			c.UI.Error(err.Error())
			return 1
		}
	}

	if c.flagNodeName == "" {
		c.UI.Error("-node-name is required")
		return 1
	}

	if c.flagOutputFile == "" {
		c.UI.Error("-output-file is required")
		return 1
	}

	if c.clientset == nil {
		config, err := rest.InClusterConfig()
		if err != nil {
			// This just allows us to test it locally.
			kubeconfig := clientcmd.RecommendedHomeFile
			config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
			if err != nil {
				c.UI.Error(err.Error())
				return 1
			}
		}

		c.clientset, err = kubernetes.NewForConfig(config)
		if err != nil {
			c.UI.Error(err.Error())
			return 1
		}
	}

	config := c.fetchLocalityConfig()

	jsonData, err := json.Marshal(config)
	if err != nil {
		c.UI.Error(err.Error())
		return 1
	}

	err = os.WriteFile(c.flagOutputFile, jsonData, 0644)
	if err != nil {
		c.UI.Error(fmt.Sprintf("Error writing locality file: %s", err))
		return 1
	}

	return 0
}

func (c *Command) fetchLocalityConfig() Config {
	var cfg Config
	node, err := c.clientset.CoreV1().Nodes().Get(context.Background(), c.flagNodeName, metav1.GetOptions{})
	if err != nil {
		return cfg
	}

	cfg.Locality.Region = node.Labels[corev1.LabelTopologyRegion]

	return cfg
}

func (c *Command) Synopsis() string { return synopsis }
func (c *Command) Help() string {
	c.once.Do(c.init)
	return c.help
}

const synopsis = "Fetch the cloud region for a Consul server from the Kubernetes node's region label."
const help = `
Usage: consul-k8s-control-plane fetch-server-region [options]

  Fetch the region for a Consul server.
  Not intended for stand-alone use.
`
