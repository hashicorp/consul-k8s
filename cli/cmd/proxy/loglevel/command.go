// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package loglevel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/posener/complete"
	"golang.org/x/sync/errgroup"
	helmCLI "helm.sh/helm/v3/pkg/cli"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/validation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/hashicorp/consul-k8s/cli/common"
	"github.com/hashicorp/consul-k8s/cli/common/envoy"
	"github.com/hashicorp/consul-k8s/cli/common/flag"
	"github.com/hashicorp/consul-k8s/cli/common/terminal"
	"github.com/hashicorp/go-multierror"
)

const (
	defaultAdminPort    = 19000
	flagNameNamespace   = "namespace"
	flagNameUpdateLevel = "update-level"
	flagNameReset       = "reset"
	flagNameKubeConfig  = "kubeconfig"
	flagNameKubeContext = "context"
	flagNameCapture     = "capture"
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
	reset       bool
	capture     string
	kubeConfig  string
	kubeContext string

	once               sync.Once
	help               string
	restConfig         *rest.Config
	envoyLoggingCaller func(context.Context, common.PortForwarder, *envoy.LoggerParams) (map[string]string, error)
	getLogFunc         func(context.Context, *corev1.Pod, *corev1.PodLogOptions) ([]byte, error)
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
	f.StringVar(&flag.StringVar{
		Name:    flagNameCapture,
		Target:  &l.capture,
		Default: "",
		Usage:   "Captures pod log for the given duration according to existing/new update-level. It can be used with -update-level <any> flag to capture logs at that level or with -reset flag to capture logs at default info level",
	})

	f.BoolVar(&flag.BoolVar{
		Name:    flagNameReset,
		Target:  &l.reset,
		Usage:   "Reset the log level for all loggers in a pod to the Envoy default (info).",
		Aliases: []string{"r"},
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

	// if we're resetting the default log level for envoy is info: https://www.envoyproxy.io/docs/envoy/latest/start/quick-start/run-envoy#debugging-envoy
	if l.reset {
		l.level = "info"
	}

	if l.envoyLoggingCaller == nil {
		l.envoyLoggingCaller = envoy.CallLoggingEndpoint
	}
	if l.getLogFunc == nil {
		l.getLogFunc = l.getLogs
	}

	err = l.initKubernetes()
	if err != nil {
		return l.logOutputAndDie(err)
	}

	adminPorts, err := l.fetchAdminPorts()
	if err != nil {
		return l.logOutputAndDie(err)
	}

	if l.capture == "" {
		newLogger, err := l.fetchOrSetLogLevels(adminPorts)
		if err != nil {
			return l.logOutputAndDie(err)
		}
		l.outputLevels(newLogger)
		return 0
	}

	if err := l.captureLogsAndResetLogLevels(adminPorts); err != nil {
		return 1
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
	if l.level != "" && l.reset {
		return fmt.Errorf("cannot set log level to %q and reset to 'info' at the same time", l.level)
	}
	if l.namespace != "" {
		errs := validation.ValidateNamespaceName(l.namespace, false)
		if len(errs) > 0 {
			return fmt.Errorf("invalid namespace name passed for -namespace/-n: %v", strings.Join(errs, "; "))
		}
	}
	if l.capture != "" {
		if _, err := time.ParseDuration(l.capture); err != nil {
			return fmt.Errorf("invalid duration passed for -capture: %v", err)
		}
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

func (l *LogLevelCommand) fetchOrSetLogLevels(adminPorts map[string]int) (map[string]LoggerConfig, error) {
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
			return nil, err
		}
		logLevels, err := l.envoyLoggingCaller(l.Ctx, &pf, params)
		if err != nil {
			return nil, err
		}
		loggers[name] = logLevels
	}
	return loggers, nil
}

func (l *LogLevelCommand) captureLogsAndResetLogLevels(adminPorts map[string]int) error {
	// If no new level is provided, just capture logs.
	if l.level == "" {
		return l.captureLogs()
	}

	// NEW LOG LEVELS provided via -update-level flag,
	// 1. Fetch existing log levels before setting NEW log levels (for reset after log capture)
	// 2. Set NEW log levels
	// 3. Capture logs at NEW log levels for the given duration
	// 4. Reset back to existing log levels

	existingLogger, err := l.fetchExistingLogLevels(adminPorts)
	if err != nil {
		return err
	}

	// cleanup is required to ensure that if new log level set,
	// should be reset back to existing log level after log capture
	// even if user interrupts the command during log capture.
	select {
	case <-l.CleanupReq:
	default:
	}
	l.CleanupReq <- true
	defer func() {
		l.CleanupConfirmation <- 1
	}()
	defer func() {
		if err := l.resetLogLevels(existingLogger, adminPorts); err != nil {
			l.UI.Output(fmt.Sprintf("error resetting log levels: %v", err), terminal.WithErrorStyle())
		} else {
			l.UI.Output("Reset completed successfully!")
		}
	}()

	l.UI.Output(fmt.Sprintf("Setting new log levels..."))
	currentLogger, err := l.fetchOrSetLogLevels(adminPorts)
	if err != nil {
		return fmt.Errorf("error setting new log levels: %w", err)
	}
	l.outputLevels(currentLogger)

	err = l.captureLogs()
	if err != nil {
		l.UI.Output(err.Error(), terminal.WithErrorStyle())
	}
	return err
}

func (l *LogLevelCommand) fetchExistingLogLevels(adminPorts map[string]int) (map[string]LoggerConfig, error) {
	l.UI.Output(fmt.Sprintf("Fetching existing log levels..."))
	existingLogger := make(map[string]LoggerConfig, 0)
	newLogLevels := l.level
	l.level = ""
	existingLogger, err := l.fetchOrSetLogLevels(adminPorts)
	l.level = newLogLevels
	if err != nil {
		return nil, fmt.Errorf("error fetching existing log levels: %w", err)
	}
	return existingLogger, nil
}

func (l *LogLevelCommand) resetLogLevels(existingLogger map[string]LoggerConfig, adminPorts map[string]int) error {
	l.UI.Output("Resetting log levels back to previous configuration...")
	var errs error
	// Use a fresh context for resetting log levels as
	// l.Ctx might be cancelled during log capture DUE TO user interrupt
	originalCtx := l.Ctx
	l.Ctx = context.Background()
	defer func() {
		l.Ctx = originalCtx
	}()
	for loggerName, levels := range existingLogger {
		var levelParams []string
		for k, v := range levels {
			// EnvoyLoggers is a map of valid logger for consul and
			// fetchOrSetLogLevels return ALL the envoy logger (not the one specific of consul)
			// so below check is needed to filter out unspecified loggers.
			// It can be removed once the above is fixed.
			if _, ok := envoy.EnvoyLoggers[k]; ok {
				levelParams = append(levelParams, fmt.Sprintf("%s:%s", k, v))
			}
		}
		if len(levelParams) > 0 {
			l.level = strings.Join(levelParams, ",")
		} else {
			l.level = "info"
		}
		_, err := l.fetchOrSetLogLevels(map[string]int{loggerName: adminPorts[loggerName]})
		if err != nil {
			errs = multierror.Append(errs, fmt.Errorf("error resetting log level for %s: %w", loggerName, err))
		}
	}
	return errs
}

func (l *LogLevelCommand) captureLogs() error {
	l.UI.Output("Starting log capture...")
	g := new(errgroup.Group)
	g.Go(func() error {
		return l.fetchPodLogs(l.Ctx)
	})
	err := g.Wait()
	if err != nil {
		return fmt.Errorf("error capturing logs: %w", err)
	}
	return nil
}

// fetchPodLogs - captures the logs from the given pod for the given duration and writes it to a file
func (l *LogLevelCommand) fetchPodLogs(ctx context.Context) error {
	duration, err := time.ParseDuration(l.capture)
	if err != nil {
		return fmt.Errorf("error parsing capture's duration")
	}
	durationChn := time.After(duration)

	sinceSeconds := int64(duration.Seconds())
	pod, err := l.kubernetes.CoreV1().Pods(l.namespace).Get(ctx, l.podName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("error getting pod object from k8s: %w", err)
	}

	var podLogOptions *corev1.PodLogOptions
	for _, container := range pod.Spec.Containers {
		if container.Name == "consul-dataplane" {
			podLogOptions = &corev1.PodLogOptions{
				Container:    container.Name,
				SinceSeconds: &sinceSeconds,
				Timestamps:   true,
			}
		}
	}
	proxyLogFilePath := filepath.Join("proxy", fmt.Sprintf("proxy-log-%s.log", l.podName))

	// metadata of log capture
	l.UI.Output("Pod Name:             %s", pod.Name)
	l.UI.Output("Namespace:            %s", pod.Namespace)
	l.UI.Output("Log Capture Duration: %s", l.capture)
	l.UI.Output("Log File Path:        %s", proxyLogFilePath)

	select {
	case <-durationChn:
		logs, err := l.logger(ctx, pod, podLogOptions)
		if err != nil {
			return err
		}
		// Create file path and directory for storing logs
		// NOTE: currently it is writing log file in cwd /proxy only. Also, log file contents will be overwritten if
		// the command is run multiple times for the same pod name or if file already exists.
		if err := os.MkdirAll(filepath.Dir(proxyLogFilePath), 0755); err != nil {
			return fmt.Errorf("error creating directory for log file: %w", err)
		}
		if err := os.WriteFile(proxyLogFilePath, logs, 0644); err != nil {
			return fmt.Errorf("error writing log to file: %v", err)
		}
		l.UI.Output("Logs saved to '%s'", proxyLogFilePath, terminal.WithSuccessStyle())
		return nil
	case <-ctx.Done():
		return fmt.Errorf("stopping collection due to shutdown signal recieved")
	}
}
func (l *LogLevelCommand) logger(ctx context.Context, pod *corev1.Pod, podLogOptions *corev1.PodLogOptions) ([]byte, error) {
	return l.getLogFunc(ctx, pod, podLogOptions)
}
func (l *LogLevelCommand) getLogs(ctx context.Context, pod *corev1.Pod, podLogOptions *corev1.PodLogOptions) ([]byte, error) {
	podLogRequest := l.kubernetes.CoreV1().Pods(l.namespace).GetLogs(pod.Name, podLogOptions)
	podLogStream, err := podLogRequest.Stream(ctx)
	if err != nil {
		return nil, fmt.Errorf("error getting logs: %v\n", err)
	}
	defer podLogStream.Close()

	logs, err := io.ReadAll(podLogStream)
	if err != nil {
		return nil, fmt.Errorf("error reading log streams: %w", err)
	}
	return logs, nil
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
		fmt.Sprintf("-%s", flagNameCapture):     complete.PredictAnything,
		fmt.Sprintf("-%s", flagNameUpdateLevel): complete.PredictAnything,
		fmt.Sprintf("-%s", flagNameReset):       complete.PredictNothing,
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
