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
	helmCLI "helm.sh/helm/v3/pkg/cli"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const defaultAdminPort int = 19000

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

type LogCommand struct {
	*common.BaseCommand

	kubernetes kubernetes.Interface
	set        *flag.Sets

	// Command Flags
	podName string

	once            sync.Once
	help            string
	restConfig      *rest.Config
	logLevelFetcher func(context.Context, common.PortForwarder) (LoggerConfig, error)
}

func (l *LogCommand) init() {
	l.set = flag.NewSets()
	l.help = l.set.Help()
}

func (l *LogCommand) Run(args []string) int {
	l.once.Do(l.init)
	l.Log.ResetNamed("loglevel")
	defer common.CloseWithError(l.BaseCommand)

	err := l.parseFlags(args)
	if err != nil {
		fmt.Println(err)
		return 1
	}

	if l.logLevelFetcher == nil {
		l.logLevelFetcher = FetchLogLevel
	}

	err = l.initKubernetes()
	if err != nil {
		fmt.Println(err)
		return 1
	}

	adminPorts, err := l.fetchAdminPorts()
	if err != nil {
		fmt.Println(err)
		return 1
	}

	logLevels, err := l.fetchLogLevels(adminPorts)
	if err != nil {
		fmt.Println(err)
		return 1
	}
	l.outputLevels(logLevels)
	return 0
}

func (l *LogCommand) parseFlags(args []string) error {
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

func (l *LogCommand) initKubernetes() error {
	settings := helmCLI.New()
	var err error

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
	return nil
}

func (l *LogCommand) fetchAdminPorts() (map[string]int, error) {
	adminPorts := make(map[string]int, 0)
	// TODO: support different namespaces
	pod, err := l.kubernetes.CoreV1().Pods("default").Get(l.Ctx, l.podName, metav1.GetOptions{})
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

func (l *LogCommand) fetchLogLevels(adminPorts map[string]int) (map[string]LoggerConfig, error) {
	loggers := make(map[string]LoggerConfig, 0)

	for name, port := range adminPorts {
		pf := common.PortForward{
			Namespace:  "default", // TODO: change this to use the configurable namespace
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

func FetchLogLevel(ctx context.Context, portForward common.PortForwarder) (LoggerConfig, error) {
	endpoint, err := portForward.Open(ctx)
	if err != nil {
		return nil, err
	}

	defer portForward.Close()

	response, err := http.Post(fmt.Sprintf("http://%s/logging", endpoint), "application/json", bytes.NewBuffer([]byte{}))
	if err != nil {
		return nil, err
	}
	body, err := io.ReadAll(response.Body)
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

func (l *LogCommand) Help() string {
	l.once.Do(l.init)
	return fmt.Sprintf("%s\n\nUsage: consul-k8s proxy log <pod-name> [flags]\n\n%s", l.Synopsis(), l.help)
}

func (l *LogCommand) Synopsis() string {
	return "Inspect and Modify the Envoy Log configuration for a given Pod."
}

func (l *LogCommand) outputLevels(logLevels map[string]LoggerConfig) {
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
