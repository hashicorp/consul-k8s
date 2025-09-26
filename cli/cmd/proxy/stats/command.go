// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package stats

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/hashicorp/consul-k8s/cli/common"
	"github.com/hashicorp/consul-k8s/cli/common/flag"
	"github.com/hashicorp/consul-k8s/cli/common/terminal"
	"github.com/hashicorp/consul-k8s/cli/helm"
	"github.com/posener/complete"
	helmCLI "helm.sh/helm/v3/pkg/cli"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const envoyAdminPort = 19000
const (
	filePerm = 0644
	dirPerm  = 0755
)

const (
	flagNameNamespace    = "namespace"
	flagNameKubeConfig   = "kubeconfig"
	flagNameKubeContext  = "context"
	flagNameOutputFormat = "output"
)

type StatsCommand struct {
	*common.BaseCommand

	helmActionsRunner helm.HelmActionsRunner

	kubernetes kubernetes.Interface

	restConfig *rest.Config

	set *flag.Sets

	flagKubeConfig   string
	flagKubeContext  string
	flagNamespace    string
	flagPod          string
	flagOutputFormat string

	once sync.Once
	help string
}

func (c *StatsCommand) init() {
	c.set = flag.NewSets()
	if c.helmActionsRunner == nil {
		c.helmActionsRunner = &helm.ActionRunner{}
	}

	f := c.set.NewSet("Command Options")
	f.StringVar(&flag.StringVar{
		Name:    flagNameNamespace,
		Target:  &c.flagNamespace,
		Usage:   "The namespace where the target Pod can be found.",
		Aliases: []string{"n"},
	})
	f.StringVar(&flag.StringVar{
		Name:    flagNameOutputFormat,
		Target:  &c.flagOutputFormat,
		Usage:   "To write the stats of a given proxy pod in a json file.",
		Default: "",
		Aliases: []string{"o"},
	})

	f = c.set.NewSet("Global Options")
	f.StringVar(&flag.StringVar{
		Name:    flagNameKubeConfig,
		Aliases: []string{"c"},
		Target:  &c.flagKubeConfig,
		Default: "",
		Usage:   "Path to kubeconfig file.",
	})
	f.StringVar(&flag.StringVar{
		Name:    flagNameKubeContext,
		Target:  &c.flagKubeContext,
		Default: "",
		Usage:   "Kubernetes context to use.",
	})

	c.help = c.set.Help()
}

// validateFlags checks the command line flags and values for errors.
func (c *StatsCommand) validateFlags() error {
	if len(c.set.Args()) > 0 {
		return errors.New("should have no non-flag arguments")
	}
	if c.flagOutputFormat != "" && c.flagOutputFormat != "archive" {
		return fmt.Errorf("invalid output format: '%s'. Only supported format for now is 'archive'", c.flagOutputFormat)
	}
	return nil
}

func (c *StatsCommand) Run(args []string) int {
	c.once.Do(c.init)

	if err := c.parseFlags(args); err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		c.UI.Output("\n" + c.Help())
		return 1
	}

	if err := c.validateFlags(); err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		c.UI.Output("\n" + c.Help())
		return 1
	}

	if c.flagPod == "" {
		c.UI.Output("pod name is required")
		return 1
	}

	// helmCLI.New() will create a settings object which is used by the Helm Go SDK calls.
	settings := helmCLI.New()
	if c.flagKubeConfig != "" {
		settings.KubeConfig = c.flagKubeConfig
	}
	if c.flagKubeContext != "" {
		settings.KubeContext = c.flagKubeContext
	}

	if c.flagNamespace == "" {
		c.flagNamespace = settings.Namespace()
	}

	if err := c.setupKubeClient(settings); err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}

	if c.restConfig == nil {
		var err error
		if c.restConfig, err = settings.RESTClientGetter().ToRESTConfig(); err != nil {
			c.UI.Output("error setting rest config")
			return 1
		}
	}

	pf := common.PortForward{
		Namespace:  c.flagNamespace,
		PodName:    c.flagPod,
		RemotePort: envoyAdminPort,
		KubeClient: c.kubernetes,
		RestConfig: c.restConfig,
	}

	if c.flagOutputFormat == "archive" {
		fileName := fmt.Sprintf("proxy-stats-%s.json", c.flagPod)
		proxyStatsFilePath := filepath.Join("proxy", fileName)
		err := c.captureEnvoyStats(&pf, proxyStatsFilePath)
		if err != nil {
			c.UI.Output("error capturing envoy stats: %v", err, terminal.WithErrorStyle())
			return 1
		}
		c.UI.Output("proxy stats '%s' output saved to '%s'", c.flagPod, proxyStatsFilePath, terminal.WithSuccessStyle())
		return 0
	}

	stats, err := c.getEnvoyStats(&pf)
	if err != nil {
		c.UI.Output("error fetching envoy stats %v", err, terminal.WithErrorStyle())
		return 1
	}
	c.UI.Output(stats)
	return 0
}

func (c *StatsCommand) getEnvoyStats(pf common.PortForwarder) (string, error) {
	_, err := pf.Open(c.Ctx)
	if err != nil {
		return "", fmt.Errorf("error port forwarding %s", err)
	}
	defer pf.Close()

	resp, err := http.Get(fmt.Sprintf("http://localhost:%s/stats", strconv.Itoa(pf.GetLocalPort())))
	if err != nil {
		return "", fmt.Errorf("error hitting stats endpoint of envoy %s", err)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("error reading body of http response %s", err)
	}

	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	return string(bodyBytes), nil
}

// captureEnvoyStats - fetches proxy stats for a pod (using portforwarder) in json format and saves it to give filepath	(proxyStatsFilePath)
func (c *StatsCommand) captureEnvoyStats(pf common.PortForwarder, proxyStatsFilePath string) error {
	endpoint, err := pf.Open(c.Ctx)
	if err != nil {
		return fmt.Errorf("error port forwarding %w", err)
	}
	defer pf.Close()
	resp, err := http.Get(fmt.Sprintf("http://%s/stats?format=json", endpoint))
	if err != nil {
		return fmt.Errorf("error hitting stats endpoint of envoy %w", err)
	}
	stats, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("error reading body of http response %w", err)
	}
	defer resp.Body.Close()

	// Create file path and directory for storing stats
	// NOTE: currently it is writing log file in cwd /proxy dir only. Also, file contents will be overwritten if
	// the command is run multiple times for the same pod name or if file already exists.
	if err := os.MkdirAll(filepath.Dir(proxyStatsFilePath), dirPerm); err != nil {
		return fmt.Errorf("error creating proxy stats output directory: %w", err)
	}

	var statsJson interface{}
	err = json.Unmarshal(stats, &statsJson)
	if err != nil {
		return fmt.Errorf("error unmarshalling JSON proxy stats output: %w", err)
	}
	marshaled, err := json.MarshalIndent(statsJson, "", "\t")
	if err != nil {
		return fmt.Errorf("error converting proxy stats output to json: %w", err)
	}

	if err := os.WriteFile(proxyStatsFilePath, marshaled, filePerm); err != nil {
		// Note: Please do not delete the directory created above even if writing file fails.
		// This (proxy) directory is used by all proxy read, log, list, stats command, for storing their outputs as archive.
		return fmt.Errorf("error writing proxy stats output to json file '%s': %w", proxyStatsFilePath, err)
	}
	return nil
}

// setupKubeClient to use for non Helm SDK calls to the Kubernetes API The Helm SDK will use
// settings.RESTClientGetter for its calls as well, so this will use a consistent method to
// target the right cluster for both Helm SDK and non Helm SDK calls.
func (c *StatsCommand) setupKubeClient(settings *helmCLI.EnvSettings) error {
	if c.kubernetes == nil {
		restConfig, err := settings.RESTClientGetter().ToRESTConfig()
		if err != nil {
			c.UI.Output("Error retrieving Kubernetes authentication: %v", err, terminal.WithErrorStyle())
			return err
		}
		c.kubernetes, err = kubernetes.NewForConfig(restConfig)
		if err != nil {
			c.UI.Output("Error initializing Kubernetes client: %v", err, terminal.WithErrorStyle())
			return err
		}
	}

	return nil
}

func (c *StatsCommand) parseFlags(args []string) error {
	// Separate positional arguments from keyed arguments.
	var positional []string
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			break
		}
		positional = append(positional, arg)
	}
	keyed := args[len(positional):]

	if len(positional) != 1 {
		return fmt.Errorf("exactly one positional argument is required: <pod-name>")
	}
	c.flagPod = positional[0]

	if err := c.set.Parse(keyed); err != nil {
		return err
	}

	return nil
}

// AutocompleteFlags returns a mapping of supported flags and autocomplete
// options for this command. The map key for the Flags map should be the
// complete flag such as "-foo" or "--foo".
func (c *StatsCommand) AutocompleteFlags() complete.Flags {
	return complete.Flags{
		fmt.Sprintf("-%s", flagNameNamespace):    complete.PredictNothing,
		fmt.Sprintf("-%s", flagNameOutputFormat): complete.PredictNothing,
		fmt.Sprintf("-%s", flagNameKubeConfig):   complete.PredictFiles("*"),
		fmt.Sprintf("-%s", flagNameKubeContext):  complete.PredictNothing,
	}
}

// AutocompleteArgs returns the argument predictor for this command.
// Since argument completion is not supported, this will return
// complete.PredictNothing.
func (c *StatsCommand) AutocompleteArgs() complete.Predictor {
	return complete.PredictNothing
}

// Help returns a description of the command and how it is used.
func (c *StatsCommand) Help() string {
	c.once.Do(c.init)
	return c.Synopsis() + "\n\nUsage: consul-k8s proxy stats pod-name -n namespace [flags]\n\n" + c.help
}

// Synopsis returns a one-line command summary.
func (c *StatsCommand) Synopsis() string {
	return "Display Envoy stats for a proxy"
}
