package loglevel

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/posener/complete"
	helmCLI "helm.sh/helm/v3/pkg/cli"
	"k8s.io/apimachinery/pkg/api/validation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/hashicorp/consul-k8s/cli/common"
	"github.com/hashicorp/consul-k8s/cli/common/envoy"
	"github.com/hashicorp/consul-k8s/cli/common/flag"
	"github.com/hashicorp/consul-k8s/cli/common/terminal"
)

const (
	defaultAdminPort    = 19000
	flagNameNamespace   = "namespace"
	flagNameUpdateLevel = "update-level"
	flagNameKubeConfig  = "kubeconfig"
	flagNameKubeContext = "context"
)

var ErrIncorrectArgFormat = errors.New("Exactly one positional argument is required: <pod-name>")

type LoggerConfig map[string]string

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
	level       string
	kubeConfig  string
	kubeContext string

	once               sync.Once
	help               string
	restConfig         *rest.Config
	envoyLoggingCaller func(context.Context, common.PortForwarder, *envoy.LoggerParams) (map[string]string, error)
}

func (l *LogLevelCommand) init() {
	l.Log.ResetNamed("loglevel")
	l.set = flag.NewSets()
	f := l.set.NewSet("Command Options")
	f.StringVar(&flag.StringVar{
		Name:    flagNameNamespace,
		Target:  &l.namespace,
		Usage:   "The namespace where the target Pod can be found.",
		Aliases: []string{"n"},
	})

	f.StringVar(&flag.StringVar{
		Name:    flagNameUpdateLevel,
		Target:  &l.level,
		Usage:   "Update the level for the logger. Can be either `-update-level warning` to change all loggers to warning, or a comma delineated list of loggers with level can be passed like `-update-level grpc:warning,http:info` to only modify specific loggers.",
		Aliases: []string{"u"},
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
	defer common.CloseWithError(l.BaseCommand)

	err := l.parseFlags(args)
	if err != nil {
		return l.logOutputAndDie(err)
	}
	err = l.validateFlags()
	if err != nil {
		return l.logOutputAndDie(err)
	}

	if l.envoyLoggingCaller == nil {
		l.envoyLoggingCaller = envoy.CallLoggingEndpoint
	}

	err = l.initKubernetes()
	if err != nil {
		return l.logOutputAndDie(err)
	}

	adminPorts, err := l.fetchAdminPorts()
	if err != nil {
		return l.logOutputAndDie(err)
	}

	err = l.fetchOrSetLogLevels(adminPorts)
	if err != nil {
		return l.logOutputAndDie(err)
	}

	return 0
}

func (l *LogLevelCommand) parseFlags(args []string) error {
	if len(args) == 0 {
		return ErrIncorrectArgFormat
	}

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
		return ErrIncorrectArgFormat
	}

	l.podName = positional[0]

	err := l.set.Parse(keyed)
	if err != nil {
		return err
	}

	return nil
}

func (l *LogLevelCommand) validateFlags() error {
	if l.namespace == "" {
		return nil
	}

	errs := validation.ValidateNamespaceName(l.namespace, false)
	if len(errs) > 0 {
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

func (l *LogLevelCommand) fetchOrSetLogLevels(adminPorts map[string]int) error {
	loggers := make(map[string]LoggerConfig, 0)

	for name, port := range adminPorts {
		pf := common.PortForward{
			Namespace:  l.namespace,
			PodName:    l.podName,
			RemotePort: port,
			KubeClient: l.kubernetes,
			RestConfig: l.restConfig,
		}
		params, err := parseParams(l.level)
		if err != nil {
			return err
		}
		logLevels, err := l.envoyLoggingCaller(l.Ctx, &pf, params)
		if err != nil {
			return err
		}
		loggers[name] = logLevels
	}

	l.outputLevels(loggers)
	return nil
}

func parseParams(params string) (*envoy.LoggerParams, error) {
	loggerParams := envoy.NewLoggerParams()
	if len(params) == 0 {
		return loggerParams, nil
	}

	// contains global log level change
	if !strings.Contains(params, ":") {
		err := loggerParams.SetGlobalLoggerLevel(params)
		if err != nil {
			return nil, err
		}
		return loggerParams, nil
	}

	// contains changes to at least 1 specific log level
	loggerChanges := strings.Split(params, ",")

	for _, logger := range loggerChanges {
		levelValues := strings.Split(logger, ":")
		err := loggerParams.SetLoggerLevel(levelValues[0], levelValues[1])
		if err != nil {
			return nil, err
		}
	}
	return loggerParams, nil
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

func (l *LogLevelCommand) logOutputAndDie(err error) int {
	l.UI.Output(err.Error(), terminal.WithErrorStyle())
	l.UI.Output(fmt.Sprintf("\n%s", l.Help()))
	return 1
}
