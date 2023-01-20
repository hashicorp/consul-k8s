package loglevel

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/hashicorp/consul-k8s/cli/common"
	"github.com/hashicorp/consul-k8s/cli/common/flag"
	"github.com/hashicorp/consul-k8s/cli/common/terminal"
	"github.com/posener/complete"
	helmCLI "helm.sh/helm/v3/pkg/cli"
	"k8s.io/apimachinery/pkg/api/validation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	defaultAdminPort    int = 19000
	flagNameNamespace       = "namespace"
	flagNameKubeConfig      = "kubeconfig"
	flagNameKubeContext     = "context"
)

type LoggerConfig map[string]string

var ErrMissingPodName = errors.New("Exactly one positional argument is required: <pod-name>")

var levelToColor = map[string]string{
	"trace":    terminal.Green,
	"debug":    terminal.HiWhite,
	"info":     terminal.Blue,
	"warning":  terminal.Yellow,
	"error":    terminal.Red,
	"critical": terminal.Magenta,
	"off":      "",
}

type LogLevelCommand struct {
	*common.BaseCommand

	kubernetes kubernetes.Interface
	set        *flag.Sets

	// Command Flags
	podName     string
	namespace   string
	kubeConfig  string
	kubeContext string

	once            sync.Once
	help            string
	restConfig      *rest.Config
	logLevelFetcher func(context.Context, common.PortForwarder) (LoggerConfig, error)
}

func (l *LogLevelCommand) init() {
	l.set = flag.NewSets()
	f := l.set.NewSet("Command Options")
	f.StringVar(&flag.StringVar{
		Name:    flagNameNamespace,
		Target:  &l.namespace,
		Usage:   "The namespace where the target Pod can be found.",
		Aliases: []string{"n"},
	})

	f = l.set.NewSet("Global Options")
	f.StringVar(&flag.StringVar{
		Name:    flagNameKubeConfig,
		Aliases: []string{"c"},
		Target:  &l.kubeConfig,
		Usage:   "Set the path to kubeconfig file.",
	})
	f.StringVar(&flag.StringVar{
		Name:   flagNameKubeContext,
		Target: &l.kubeContext,
		Usage:  "Set the Kubernetes context to use.",
	})

	l.help = l.set.Help()
}

func (l *LogLevelCommand) Run(args []string) int {
	l.once.Do(l.init)
	l.Log.ResetNamed("loglevel")
	defer common.CloseWithError(l.BaseCommand)

	err := l.parseFlags(args)
	if err != nil {
		l.UI.Output(err.Error(), terminal.WithErrorStyle())
		l.UI.Output(fmt.Sprintf("\n%s", l.Help()))
		return 1
	}

	err = l.validateFlags()
	if err != nil {
		l.UI.Output(err.Error(), terminal.WithErrorStyle())
		l.UI.Output(fmt.Sprintf("\n%s", l.Help()))
		return 1
	}

	if l.logLevelFetcher == nil {
		l.logLevelFetcher = FetchLogLevel
	}

	err = l.initKubernetes()
	if err != nil {
		l.UI.Output(err.Error(), terminal.WithErrorStyle())
		l.UI.Output(fmt.Sprintf("\n%s", l.Help()))
		return 1
	}

	adminPorts, err := l.fetchAdminPorts()
	if err != nil {
		l.UI.Output(err.Error(), terminal.WithErrorStyle())
		l.UI.Output(fmt.Sprintf("\n%s", l.Help()))
		return 1
	}

	logLevels, err := l.fetchLogLevels(adminPorts)
	if err != nil {
		l.UI.Output(err.Error(), terminal.WithErrorStyle())
		l.UI.Output(fmt.Sprintf("\n%s", l.Help()))
		return 1
	}
	l.outputLevels(logLevels)
	return 0
}

func (l *LogLevelCommand) parseFlags(args []string) error {
	positional := []string{}
	// Separate positional args from keyed args
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			break
		}
		positional = append(positional, arg)
	}
	keyed := args[len(positional):]

	if len(positional) != 1 {
		return ErrMissingPodName
	}

	l.podName = positional[0]

	err := l.set.Parse(keyed)
	if err != nil {
		return err
	}
	return nil
}

func (l *LogLevelCommand) validateFlags() error {
	if errs := validation.ValidateNamespaceName(l.namespace, false); l.namespace != "" && len(errs) > 0 {
		return fmt.Errorf("invalid namespace name passed for -namespace/-n: %v", strings.Join(errs, "; "))
	}
	return nil
}

func (l *LogLevelCommand) initKubernetes() error {
	settings := helmCLI.New()
	var err error

	if l.kubeConfig != "" {
		settings.KubeConfig = l.kubeConfig
	}

	if l.kubeContext != "" {
		settings.KubeContext = l.kubeContext
	}

	if l.restConfig == nil {
		l.restConfig, err = settings.RESTClientGetter().ToRESTConfig()
		if err != nil {
			return fmt.Errorf("error creating Kubernetes REST config %v", err)
		}

	}

	if l.kubernetes == nil {
		l.kubernetes, err = kubernetes.NewForConfig(l.restConfig)
		if err != nil {
			return fmt.Errorf("error creating Kubernetes client %v", err)
		}
	}
	if l.namespace == "" {
		l.namespace = settings.Namespace()
	}

	return nil
}

// fetchAdminPorts retrieves all admin ports for Envoy Proxies running in a pod given namespace.
func (l *LogLevelCommand) fetchAdminPorts() (map[string]int, error) {
	adminPorts := make(map[string]int, 0)
	pod, err := l.kubernetes.CoreV1().Pods(l.namespace).Get(l.Ctx, l.podName, metav1.GetOptions{})
	if err != nil {
		return adminPorts, err
	}

	connectService, isMultiport := pod.Annotations["consul.hashicorp.com/connect-service"]

	if !isMultiport {
		// Return the default port configuration.
		adminPorts[l.podName] = defaultAdminPort
		return adminPorts, nil
	}

	for idx, svc := range strings.Split(connectService, ",") {
		adminPorts[svc] = defaultAdminPort + idx
	}

	return adminPorts, nil
}

func (l *LogLevelCommand) fetchLogLevels(adminPorts map[string]int) (map[string]LoggerConfig, error) {
	loggers := make(map[string]LoggerConfig, 0)

	for name, port := range adminPorts {
		pf := common.PortForward{
			Namespace:  l.namespace,
			PodName:    l.podName,
			RemotePort: port,
			KubeClient: l.kubernetes,
			RestConfig: l.restConfig,
		}

		logLevels, err := l.logLevelFetcher(l.Ctx, &pf)
		if err != nil {
			return loggers, err
		}
		loggers[name] = logLevels
	}
	return loggers, nil
}

// FetchLogLevel requests the logging endpoint from Envoy Admin Interface for a given port
// more can be read about that endpoint https://www.envoyproxy.io/docs/envoy/latest/operations/admin#post--logging
func FetchLogLevel(ctx context.Context, portForward common.PortForwarder) (LoggerConfig, error) {
	endpoint, err := portForward.Open(ctx)
	if err != nil {
		return nil, err
	}

	defer portForward.Close()

	// this endpoint does not support returning json, so we've gotta parse the plain text
	response, err := http.Post(fmt.Sprintf("http://%s/logging", endpoint), "text/plain", bytes.NewBuffer([]byte{}))
	if err != nil {
		return nil, err
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to reach envoy: %v", err)
	}
	loggers := strings.Split(string(body), "\n")
	logLevels := make(map[string]string)
	var name string
	var level string

	// the first line here is just a header
	for _, logger := range loggers[1:] {
		if len(logger) == 0 {
			continue
		}
		fmt.Sscanf(logger, "%s %s", &name, &level)
		name = strings.TrimRight(name, ":")
		logLevels[name] = level
	}
	return logLevels, nil
}

func (l *LogLevelCommand) outputLevels(logLevels map[string]LoggerConfig) {
	l.UI.Output(fmt.Sprintf("Envoy log configuration for %s in namespace default:", l.podName))
	for n, levels := range logLevels {
		l.UI.Output(fmt.Sprintf("Log Levels for %s", n), terminal.WithHeaderStyle())
		table := terminal.NewTable("Name", "Level")
		for name, level := range levels {
			table.AddRow([]string{name, level}, []string{"", levelToColor[level]})
		}
		l.UI.Table(table)
		l.UI.Output("")
	}
}

func (l *LogLevelCommand) Help() string {
	l.once.Do(l.init)
	return fmt.Sprintf("%s\n\nUsage: consul-k8s proxy log <pod-name> [flags]\n\n%s", l.Synopsis(), l.help)
}

func (l *LogLevelCommand) Synopsis() string {
	return "Inspect and Modify the Envoy Log configuration for a given Pod."
}

// AutocompleteFlags returns a mapping of supported flags and autocomplete
// options for this command. The map key for the Flags map should be the
// complete flag such as "-foo" or "--foo".
func (l *LogLevelCommand) AutocompleteFlags() complete.Flags {
	return complete.Flags{
		fmt.Sprintf("-%s", flagNameNamespace):   complete.PredictNothing,
		fmt.Sprintf("-%s", flagNameKubeConfig):  complete.PredictFiles("*"),
		fmt.Sprintf("-%s", flagNameKubeContext): complete.PredictNothing,
	}
}

// AutocompleteArgs returns the argument predictor for this command.
// Since argument completion is not supported, this will return
// complete.PredictNothing.
func (l *LogLevelCommand) AutocompleteArgs() complete.Predictor {
	return complete.PredictNothing
}
