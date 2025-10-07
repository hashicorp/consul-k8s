package debug

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"strconv"

	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/consul-k8s/cli/common"
	"github.com/hashicorp/consul-k8s/cli/common/flag"
	"github.com/hashicorp/consul-k8s/cli/common/terminal"
	"github.com/hashicorp/consul-k8s/cli/helm"
	"github.com/hashicorp/go-multierror"
	"github.com/posener/complete"
	"helm.sh/helm/v3/pkg/action"
	helmCLI "helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/release"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	k8errors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/validation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	// debugDuration is the total time that debug runs before being shut down
	debugDuration = 5 * time.Minute

	// debugMinDuration is the minimum a user can configure the duration
	// to ensure that all information can be collected in time
	debugMinDuration = 10 * time.Second
	debugLeastSince  = 10 * time.Second

	debugGraceDuration = 2 * time.Second

	// debugArchiveExtension is the extension for archive files
	debugArchiveExtension = ".tar.gz"
)

// Predefined errors
var (
	notFoundError        = errors.New("not found")
	signalInterruptError = errors.New("signal interrupt received")
	// oneOrMoreErrorOccured is used to indicate that one or more errors occurred
	// during a capture task and they are written successfully to debug bundle,
	// otherwise whole error would be printed on terminal
	oneOrMoreErrorOccured = errors.New(fmt.Sprint("one or more errors occurred during capture for this target",
		"\n\tplease check the respective error file within the debug bundle for details"))
)

// Predefined file and directory permissions
const (
	filePerm = 0644
	dirPerm  = 0755
)

// debugIndex is used to manage the summary of all data recorded
// during the debug, to be written to json at the end of the run
// and stored at the root. Each attribute corresponds to a file or files.
type debugIndex struct {
	Duration  string   `json:"duration"`
	Since     string   `json:"since"`
	Targets   []string `json:"targets"`
	Timestamp string   `json:"timestamp"`
}

// capture Targets: Helm config, CRDs, sidecars, pod logs and Envoy endpoints data.
const (
	targetHelmConfig  = "helm"
	targetCRDs        = "crds"
	targetSidecarPods = "sidecar"
	targetLogs        = "logs"
	targetProxy       = "proxy"
)

// defaultTargets specifies the list of targets that will be captured by default
var defaultTargets = []string{
	targetHelmConfig,
	targetCRDs,
	targetLogs,
	targetSidecarPods,
	targetProxy,
}

// capture Targets Not Found Error Messages
const (
	noHelmReleaseFound = "No helm release found"
	noCRDsFound        = "No consul CRDs found in the cluster"
	noSidecarPodsFound = "No consul injected sidecar pods found in all namespace"
	noProxiesFound     = "No envoy proxy pods found in the cluster"
	noPodsFound        = "No pods found to capture log"
)

// timeDateformat is a modified version of time.RFC3339 which replaces colons with
// hyphens. This is to make it more convenient to untar these files, because
// tar assumes colons indicate the file is on a remote host, unless --force-local
// is used.
const timeDateFormat = "2006-01-02T15-04-05Z0700"

const (
	flagNameKubeConfig  = "kubeconfig"
	flagNameKubeContext = "kubecontext"
	flagNameNamespace   = "namespace"

	flagDuration = "duration"
	flagSince    = "since"
	flagOutput   = "output"
	flagArchive  = "archive"
	flagCapture  = "capture"
)

type captureTask struct {
	name        string
	target      string
	captureFxn  func() error
	notFoundMsg string
}

type DebugCommand struct {
	*common.BaseCommand
	set *flag.Sets

	// Global flags
	flagKubeConfig  string
	flagKubeContext string

	// Command flags
	duration      time.Duration
	since         time.Duration
	output        string
	archive       bool
	capture       []string
	flagNamespace string

	// Internal state
	kubernetes      kubernetes.Interface
	restConfig      *rest.Config
	apiextensions   apiextensionsclient.Interface // for retrieving k8s CRDs
	dynamic         dynamic.Interface             // for retrieving k8s CRD resources
	helmEnvSettings *helmCLI.EnvSettings

	// Dependency Injections for testing
	helmActionsRunner helm.HelmActionsRunner
	proxyCapturer     *EnvoyProxyCapture
	logCapturer       *LogCapture
	once              sync.Once
	help              string
}

// init sets up flags and help text for the command.
func (c *DebugCommand) init() {
	c.set = flag.NewSets()

	f := c.set.NewSet("Command Options")
	defaultOutputFilename := fmt.Sprintf("consul-debug-%v", time.Now().Format(timeDateFormat))

	f.DurationVar(&flag.DurationVar{
		Name:    flagDuration,
		Target:  &c.duration,
		Default: debugDuration,
		Usage:   "To capture the logs of consul cluster for the a given duration",
		Aliases: []string{"d"},
	})
	f.DurationVar(&flag.DurationVar{
		Name:    flagSince,
		Target:  &c.since,
		Default: 0,
		Usage:   "The time duration since when to capture logs from pods",
		Aliases: []string{"s"},
	})
	f.StringVar(&flag.StringVar{
		Name:    flagOutput,
		Target:  &c.output,
		Default: defaultOutputFilename,
		Usage:   "The filename of the debug output archive.",
		Aliases: []string{"o"},
	})
	f.BoolVar(&flag.BoolVar{
		Name:    flagArchive,
		Target:  &c.archive,
		Default: true,
		Usage:   "Whether to archive the output debug directory to a .tar.gz.",
		Aliases: []string{"a"},
	})
	f.StringSliceVar(&flag.StringSliceVar{
		Name:    flagCapture,
		Target:  &c.capture,
		Default: []string{"all"},
		Usage:   "A list of components to capture. Supported values are: all, helm, crds, sidecar, pods, proxy. (e.g. -capture pods -capture events).",
		Aliases: []string{"c"},
	})
	f.StringVar(&flag.StringVar{
		Name:    flagNameNamespace,
		Target:  &c.flagNamespace,
		Default: "consul",
		Usage:   "The namespace where the target Pod can be found.",
		Aliases: []string{"n"},
	})

	f = c.set.NewSet("Global Options")
	f.StringVar(&flag.StringVar{
		Name:    flagNameKubeConfig,
		Aliases: []string{"kc"},
		Target:  &c.flagKubeConfig,
		Default: "",
		Usage:   "Set the path to kubeconfig file.",
	})
	f.StringVar(&flag.StringVar{
		Name:    flagNameKubeContext,
		Target:  &c.flagKubeContext,
		Default: "",
		Usage:   "Set the Kubernetes context to use.",
	})

	c.help = c.set.Help()
}

// Run executes the list command.
func (c *DebugCommand) Run(args []string) int {
	c.once.Do(c.init)
	defer common.CloseWithError(c.BaseCommand)

	c.Log.ResetNamed("debug")
	defer common.CloseWithError(c.BaseCommand)

	// Parse the command line flags.
	if err := c.set.Parse(args); err != nil {
		c.UI.Output("Error parsing arguments: %v", err.Error(), terminal.WithErrorStyle())
		return 1
	}
	// Validate the command line flags.
	if err := c.validateFlags(); err != nil {
		c.UI.Output("Invalid argument: %v", err.Error(), terminal.WithErrorStyle())
		return 1
	}
	// Checks if cwd have write permissions and
	// if output directory already exists
	if err := c.preChecks(); err != nil {
		c.UI.Output("Pre-checks failed: %v", err.Error(), terminal.WithErrorStyle())
		return 1
	}

	// Testing dependencies
	if c.helmActionsRunner == nil {
		c.helmActionsRunner = &helm.ActionRunner{}
	}

	if c.kubernetes == nil {
		if err := c.initKubernetes(); err != nil {
			c.UI.Output("Error initializing Kubernetes client: %v", err.Error(), terminal.WithErrorStyle())
			return 1
		}
	}
	return c.debugger()
}

func (c *DebugCommand) validateFlags() error {
	if len(c.set.Args()) > 0 {
		return fmt.Errorf("should have no non-flag arguments")
	}
	// Namespace name validation
	if c.flagNamespace != "" {
		if errs := validation.ValidateNamespaceName(c.flagNamespace, false); len(errs) > 0 {
			return fmt.Errorf("invalid namespace name passed for -namespace/-n: %v", strings.Join(errs, "; "))
		}
	}
	// Ensure realistic duration is specified
	if c.duration < debugMinDuration {
		return fmt.Errorf("duration must be longer than %s", debugMinDuration)
	}
	if c.since != 0 && c.since < debugLeastSince {
		return fmt.Errorf("since must be longer than %s", debugLeastSince)
	}

	// If none are specified in capture, we will collect information from all by default
	// otherwise, validate that the specified targets are known/valid
	if len(c.capture) == 0 || (len(c.capture) == 1 && c.capture[0] == "all") {
		c.capture = make([]string, len(defaultTargets))
		copy(c.capture, defaultTargets)
	} else {
		for _, t := range c.capture {
			if !slices.Contains(defaultTargets, t) {
				return fmt.Errorf("invalid capture target agrument: '%s', Valid capture targets are: %s", t, strings.Join(defaultTargets, ", "))
			}
		}
	}
	return nil
}

func (c *DebugCommand) preChecks() error {
	// Ensure the output directory can be created (have write permissions and it does not already exist
	if _, err := os.Stat(c.output); os.IsNotExist(err) {
		err := os.MkdirAll(c.output, 0755)
		if err != nil {
			if os.IsPermission(err) || strings.Contains(err.Error(), "read-only file system") {
				// macOS error: permission denied, linux error: read-only file system
				// current working dir is not writeable
				return fmt.Errorf("could not create output directory, no write permission for current/given path. \n please run consul-k8s debug in a path writable by the user. %s ", err)
			}
			return fmt.Errorf("could not create output directory: %s", err)
		}
	} else {
		return fmt.Errorf("output directory already exists: %s", c.output)
	}
	return nil
}

// AutocompleteFlags returns a mapping of supported flags and autocomplete
// options for this command. The map key for the Flags map should be the
// complete flag such as "-foo" or "--foo".
func (c *DebugCommand) AutocompleteFlags() complete.Flags {
	return complete.Flags{
		fmt.Sprintf("-%s", flagNameNamespace):   complete.PredictNothing,
		fmt.Sprintf("-%s", flagNameKubeConfig):  complete.PredictFiles("*"),
		fmt.Sprintf("-%s", flagNameKubeContext): complete.PredictNothing,
		fmt.Sprintf("-%s", flagDuration):        complete.PredictNothing,
		fmt.Sprintf("-%s", flagSince):           complete.PredictNothing,
		fmt.Sprintf("-%s", flagOutput):          complete.PredictNothing,
		fmt.Sprintf("-%s", flagCapture):         complete.PredictSet(defaultTargets...),
		fmt.Sprintf("-%s", flagArchive):         complete.PredictNothing,
	}
}

// AutocompleteArgs returns the argument predictor for this command.
// Since argument completion is not supported, this will return
// complete.PredictNothing.
func (c *DebugCommand) AutocompleteArgs() complete.Predictor {
	return complete.PredictNothing
}

// initKubernetes initializes the Kubernetes client.
func (c *DebugCommand) initKubernetes() (err error) {
	settings := helmCLI.New()

	if c.flagKubeConfig != "" {
		settings.KubeConfig = c.flagKubeConfig
	}

	if c.flagKubeContext != "" {
		settings.KubeContext = c.flagKubeContext
	}

	if c.restConfig == nil {
		if c.restConfig, err = settings.RESTClientGetter().ToRESTConfig(); err != nil {
			return fmt.Errorf("error creating Kubernetes REST config %v", err)
		}
	}

	if c.kubernetes == nil {
		if c.kubernetes, err = kubernetes.NewForConfig(c.restConfig); err != nil {
			return fmt.Errorf("error creating Kubernetes client %v", err)
		}
	}

	// for retrieving k8s CRDs resources
	//if targetCapture(targetCRDs) is false, skip creating these clients
	if c.captureTarget(targetCRDs) {
		c.dynamic, err = dynamic.NewForConfig(c.restConfig)
		if err != nil {
			return fmt.Errorf("error creating dynamic client: %w", err)
		}
		c.apiextensions, err = apiextensionsclient.NewForConfig(c.restConfig)
		if err != nil {
			return fmt.Errorf("error creating apiextensions client: %w", err)
		}
	}

	if c.flagNamespace == "" {
		c.flagNamespace = settings.Namespace()
	}
	c.helmEnvSettings = settings
	return nil
}

// ===================================================================================================================

// debugger is the main function that orchestrate the debug process
func (c *DebugCommand) debugger() int {
	archiveName := c.output
	if c.archive {
		archiveName = archiveName + debugArchiveExtension
	}

	// Set up signal handling to ensure we can clean up properly
	// Once we read from this buffered channel, channel will be empty and main will wait cleanup.
	// We will again send true to this channel once cleanup is completed.
	select {
	case <-c.CleanupReqAndCompleted:
	default:
	}

	c.UI.Output("\nStarting debugger: ")
	// Output metadata about debug run
	c.UI.Output(fmt.Sprintf(" - Output:           %s", archiveName))
	c.UI.Output(fmt.Sprintf(" - Capture Targets:  %s", strings.Join(c.capture, ", ")))

	if c.proxyCapturer == nil {
		c.proxyCapturer = &EnvoyProxyCapture{
			kubernetes: c.kubernetes,
			restConfig: c.restConfig,
			output:     c.output,
			ctx:        c.Ctx,
		}
	}
	if c.logCapturer == nil {
		c.logCapturer = &LogCapture{
			BaseCommand: c.BaseCommand,
			kubernetes:  c.kubernetes,
			namespace:   c.flagNamespace,
			ctx:         c.Ctx,
			output:      c.output,
			since:       c.since,
			duration:    c.duration,
		}
	}
	tasks := []captureTask{
		{name: "Helm config", target: targetHelmConfig, captureFxn: c.captureHelmConfig, notFoundMsg: noHelmReleaseFound},
		{name: "CRD resources", target: targetCRDs, captureFxn: c.captureCRDResources, notFoundMsg: noCRDsFound},
		{name: "Consul Sidecar Pods", target: targetSidecarPods, captureFxn: c.captureSidecarPods, notFoundMsg: noSidecarPodsFound},
		{name: "Envoy Proxy data", target: targetProxy, captureFxn: c.proxyCapturer.captureEnvoyProxyData, notFoundMsg: noProxiesFound},
		{name: "Pods Logs", target: targetLogs, captureFxn: c.logCapturer.captureLogs, notFoundMsg: noPodsFound},
		{name: "Index", target: "index", captureFxn: c.captureIndex, notFoundMsg: ""},
	}

	errorsOccuredDuringCapture := false
	for _, task := range tasks {
		if c.Ctx.Err() != nil {
			return 1
		}
		c.runCapture(task, &errorsOccuredDuringCapture)
	}
	return c.archiveDebugBundleAndReturn(archiveName, errorsOccuredDuringCapture)
}

// archiveDebugBundleAndReturn - creates archive if requested and
// returns appropriate exit code based on capture status
func (c *DebugCommand) archiveDebugBundleAndReturn(archiveName string, errorsOccuredDuringCapture bool) int {
	if !c.archive {
		c.UI.Output(fmt.Sprintf("Saved debug directory: %s", archiveName))
		if errorsOccuredDuringCapture {
			return 1
		}
		return 0
	} else {
		var archiveErr error
		archiveErr = c.createArchive()
		if archiveErr != nil {
			c.UI.Output(fmt.Sprintf("error creating archive: %v", archiveErr), terminal.WithErrorStyle())
			c.UI.Output(fmt.Sprintf("Saved debug directory: %s", c.output))
		} else {
			c.UI.Output(fmt.Sprintf("Saved debug archive: %s", archiveName))
		}
		if errorsOccuredDuringCapture {
			return 1
		}
		return 0
	}
}

// cleanupAndReturn - cleans up partial debug capture and returns 1
func (c *DebugCommand) cleanupAndReturn() int {
	defer func() { c.CleanupReqAndCompleted <- true }()
	c.UI.Output("\nDebug run interrupted (received signal interrupt)", terminal.WithErrorStyle())

	// if signal interrupt is before archive creation,
	// even if archive flag is true, only dir will be present to cleanup.
	bundles := []string{c.output, c.output + debugArchiveExtension}

	for _, bundle := range bundles {
		if _, err := os.Stat(bundle); err == nil {
			// found the bundle to cleanup
			c.UI.Output(" - Cleaning up partial capture...")
			err := os.RemoveAll(bundle)
			if err != nil {
				c.UI.Output(fmt.Sprintf("error cleaning up partial capture: %v", err), terminal.WithErrorStyle())
				c.UI.Output(fmt.Sprintf("Partial debug capture, saved debug dir: %s", bundle), terminal.WithWarningStyle())
				c.UI.Output(fmt.Sprint("Please delete it and re-run the debug command for completed capture"), terminal.WithWarningStyle())
				return 1
			}
			c.UI.Output(" - Cleanup completed")
			return 1
		}
	}
	return 1
}

// runCapture - runs a capture function if the target is specified in the capture list.
// Hanldles errors and output messages.
func (c *DebugCommand) runCapture(task captureTask, errorsOccured *bool) {
	target := task.target
	captureName := task.name
	captureFunction := task.captureFxn
	notFoundMsg := task.notFoundMsg

	// Skip if target not specified in capture list
	if !c.captureTarget(target) {
		return
	}
	err := captureFunction()
	if err != nil {
		switch {
		case errors.Is(err, signalInterruptError):
			c.cleanupAndReturn()
		case errors.Is(err, notFoundError):
			c.UI.Output(notFoundMsg, terminal.WithWarningStyle())
		default:
			*errorsOccured = true
			c.UI.Output(fmt.Sprintf("error capturing %s: %v", captureName, err), terminal.WithErrorStyle())
		}
	} else {
		c.UI.Output(fmt.Sprintf("%s captured", captureName), terminal.WithSuccessStyle())
	}
}

// ===================================================================================================================

// captureHelmConfig - captures consul-k8s Helm configuration and
// write it to helm-config.json file within debug archive
func (c *DebugCommand) captureHelmConfig() error {
	// Setup logger to stream Helm library logs.
	var uiLogger = func(s string, args ...interface{}) {
		logMsg := fmt.Sprintf(s, args...)
		c.UI.Output(logMsg, terminal.WithLibraryStyle())
	}
	_, releaseName, namespace, err := c.helmActionsRunner.CheckForInstallations(&helm.CheckForInstallationsOptions{
		Settings:    c.helmEnvSettings,
		ReleaseName: common.DefaultReleaseName,
		DebugLog:    uiLogger,
	})
	if err != nil {
		return fmt.Errorf("couldn't find the helm releases: %w", err)
	}
	helmRelease, err := c.checkHelmInstallation(c.helmEnvSettings, uiLogger, releaseName, namespace)
	if err != nil {
		return err
	}
	helmFilePath := filepath.Join(c.output, "helm-config.json")
	err = writeJSONFile(helmFilePath, helmRelease)
	if err != nil {
		return fmt.Errorf("couldn't write Helm config to json file: %v", err)
	}
	return nil
}

// checkHelmInstallation uses the helm Go SDK to depict the status of a named release.
// This function then prints the version of the release, it's status (unknown, deployed, uninstalled, ...), and the overwritten values.
func (c *DebugCommand) checkHelmInstallation(settings *helmCLI.EnvSettings, uiLogger action.DebugLog, releaseName, namespace string) (*release.Release, error) {
	// Need a specific action config to call helm status, where namespace comes from the previous call to list.
	statusConfig := new(action.Configuration)
	statusConfig, err := helm.InitActionConfig(statusConfig, namespace, settings, uiLogger)
	if err != nil {
		return nil, fmt.Errorf("couldn't intialise helm go SDK action configuration: %s", err)
	}
	statuser := action.NewStatus(statusConfig)
	rel, err := c.helmActionsRunner.GetStatus(statuser, releaseName)
	if err != nil {
		return nil, fmt.Errorf("couldn't get the helm release: %s", err)
	}
	return rel, nil
}

// captureCRDResources - captures consul-k8s CRDs and their instances
// and write it to CRDsResources.json file within debug archive
func (c *DebugCommand) captureCRDResources() error {
	crdResources, err := c.listCRDResources()
	if err != nil {
		if errors.Is(err, notFoundError) || strings.Contains(err.Error(), "couldn't retrive CRDs") {
			return err
		}
	}

	var writeErrors error
	if len(crdResources) != 0 {
		crdsFilePath := filepath.Join(c.output, "CRDsResources.json")
		err = writeJSONFile(crdsFilePath, crdResources)
		if err != nil {
			writeErrors = multierror.Append(writeErrors, fmt.Errorf("couldn't write CRD resources to json file: %v", err))
		}
	}

	if err != nil {
		errorFilePath := filepath.Join(c.output, "CRDsResourcesErrors.txt")
		err = fileWriter(errorFilePath, []byte(err.Error()))
		if err != nil {
			writeErrors = multierror.Append(writeErrors, fmt.Errorf("couldn't write CRD resources errors to text file: %v", err))
		}
	}
	if writeErrors != nil {
		return multierror.Append(err, writeErrors)
	}
	if err != nil {
		return oneOrMoreErrorOccured
	}
	return nil
}

// listCRDResources - captures all Consul-related CRDs
// and lists their applied resources for ALL VERSION of CRDs respectively in a map.
func (c *DebugCommand) listCRDResources() (map[string][]unstructured.Unstructured, error) {
	namespace := c.flagNamespace

	// List all CRDs in the cluster
	crdList, err := c.apiextensions.ApiextensionsV1().CustomResourceDefinitions().List(c.Ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("couldn't retrive CRDs: %w", err)
	}
	crdResourcesMap := make(map[string][]unstructured.Unstructured)
	if len(crdList.Items) == 0 {
		return nil, notFoundError
	}

	var errs error
	// Loop through each CRD and list its applied resources for ALL versions and collect errors for any CRD (if any)
	for _, crd := range crdList.Items {

		// Iterate through each version of the CRD
		for _, version := range crd.Spec.Versions {
			// Check if the version is served and is not deprecated
			if !version.Served || version.Deprecated {
				continue
			}

			// Construct the GroupVersionResource for the dynamic client.
			gvr := schema.GroupVersionResource{
				Group:    crd.Spec.Group,
				Version:  version.Name,
				Resource: crd.Spec.Names.Plural,
			}

			// Use the dynamic client to list all resources for this CRD and version.
			unstructuredList, err := c.dynamic.Resource(gvr).Namespace(namespace).List(c.Ctx, metav1.ListOptions{})

			// Add a version-specific key to the map to store resources for each version of a CRD
			key := fmt.Sprintf("%s/%s", crd.Name, version.Name)
			if err != nil {
				if k8errors.IsNotFound(err) {
					// The resource might not exist for this CRD & version, so adding empty value against this key
					crdResourcesMap[key] = []unstructured.Unstructured{}
					continue
				}
				errs = multierror.Append(errs, fmt.Errorf("CRD: %s [%s] - \tcouldn't retrieve applied resources: \t%w", crd.Name, version.Name, err))
				continue
			}
			crdResourcesMap[key] = unstructuredList.Items
		}
	}
	return crdResourcesMap, errs
}

// captureSidecarPods - captures all pods across all namespaces
// and their status (number of pods ready/desired) that have been injected by Consul
//
//	using the label consul.hashicorp.com/connect-inject-status=injected.
func (c *DebugCommand) captureSidecarPods() error {
	pods, err := c.kubernetes.CoreV1().Pods("").List(c.Ctx, metav1.ListOptions{
		LabelSelector: "consul.hashicorp.com/connect-inject-status=injected",
	})
	if err != nil {
		return fmt.Errorf("couldn't list Consul injected sidecar pods: %s", err)
	}

	if len(pods.Items) == 0 {
		return notFoundError
	} else {
		podsData := make(map[string]map[string]string)
		for _, pod := range pods.Items {
			age := time.Since(pod.CreationTimestamp.Time).Round(time.Minute)
			var readyCount int
			for _, status := range pod.Status.ContainerStatuses {
				if status.Ready {
					readyCount++
				}
			}
			readyStatus := fmt.Sprintf("%d/%d", readyCount, len(pod.Spec.Containers))

			// restartCount - shows how many times the container(s) within each pod have restarted.
			var totalRestartCount int32
			for _, status := range pod.Status.ContainerStatuses {
				totalRestartCount += status.RestartCount
			}

			ip := pod.Status.PodIP

			data := map[string]string{
				"ready":     readyStatus,
				"status":    string(pod.Status.Phase),
				"restart":   strconv.Itoa(int(totalRestartCount)),
				"age":       age.String(),
				"namespace": pod.Namespace,
				"ip":        ip,
			}
			podsData[pod.Name] = data
		}
		err = writeJSONFile(filepath.Join(c.output, "sidecarPods.json"), podsData)
		if err != nil {
			return fmt.Errorf("couldn't write Consul injected sidecar pods to json file: %v", err)
		}
		return nil
	}
}

// captureIndex - captures debug run metadata and writes to file at the root of the debug archive
func (c *DebugCommand) captureIndex() error {
	var index debugIndex
	if c.captureTarget(targetLogs) {
		index = debugIndex{
			Duration: c.duration.String(),
			Since:    c.since.String(),
		}
	}
	index = debugIndex{
		Targets:   c.capture,
		Timestamp: time.Now().Format(time.RFC3339),
	}
	err := writeJSONFile(filepath.Join(c.output, "index.json"), index)
	if err != nil {
		return fmt.Errorf("error writing index.json: %s", err)
	}
	return nil
}

// ===================================================================================================================

// captureTarget returns true if the target capture type is enabled.
// (Otherwords, is the target given in capture flag in command line)
func (c *DebugCommand) captureTarget(target string) bool {
	if c.capture == nil || slices.Contains(c.capture, "all") || slices.Contains(c.capture, target) || target == "index" {
		return true
	}
	return false
}

func writeJSONFile(filename string, content interface{}) error {
	marshaled, err := json.MarshalIndent(content, "", "\t")
	if err != nil {
		return err
	}
	err = os.MkdirAll(filepath.Dir(filename), dirPerm)
	if err != nil {
		return fmt.Errorf("failed to create directory, %w", err)
	}
	err = os.WriteFile(filename, marshaled, filePerm)
	if err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}
	return nil
}

func fileWriter(filename string, content []byte) error {
	err := os.MkdirAll(filepath.Dir(filename), dirPerm)
	if err != nil {
		return fmt.Errorf("failed to create directory, %w", err)
	}
	err = os.WriteFile(filename, content, filePerm)
	if err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}
	return nil
}

// createArchive walks the files in the temporary directory
// and creates a tar file that is gzipped with the contents
func (c *DebugCommand) createArchive() error {
	path := c.output + debugArchiveExtension

	tempName, err := c.createArchiveTemp(path)
	if err != nil {
		return err
	}

	if err := os.Rename(tempName, path); err != nil {
		return err
	}
	// fsync the dir to make the rename stick
	if err := syncParentDir(path); err != nil {
		return err
	}

	// Remove directory that has been archived
	if err := os.RemoveAll(c.output); err != nil {
		return fmt.Errorf("failed to remove archived directory: %s", err)
	}

	return nil
}

func syncParentDir(name string) error {
	f, err := os.Open(filepath.Dir(name))
	if err != nil {
		return err
	}
	defer f.Close()

	return f.Sync()
}

func (c *DebugCommand) createArchiveTemp(path string) (tempName string, err error) {
	dir := filepath.Dir(path)
	name := filepath.Base(path)

	f, err := os.CreateTemp(dir, name+".tmp")
	if err != nil {
		return "", fmt.Errorf("failed to create compressed temp archive: %s", err)
	}

	g := gzip.NewWriter(f)
	t := tar.NewWriter(g)

	tempName = f.Name()

	cleanup := func(err error) (string, error) {
		_ = t.Close()
		_ = g.Close()
		_ = f.Close()
		_ = os.Remove(tempName)
		return "", err
	}

	err = filepath.Walk(c.output, func(file string, fi os.FileInfo, err error) error {
		if err != nil {
			return fmt.Errorf("failed to walk filepath for archive: %s", err)
		}

		header, err := tar.FileInfoHeader(fi, fi.Name())
		if err != nil {
			return fmt.Errorf("failed to create compressed archive header: %s", err)
		}

		header.Name = filepath.Join(filepath.Base(c.output), strings.TrimPrefix(file, c.output))

		if err := t.WriteHeader(header); err != nil {
			return fmt.Errorf("failed to write compressed archive header: %s", err)
		}

		// Only copy files
		if !fi.Mode().IsRegular() {
			return nil
		}

		f, err := os.Open(file)
		if err != nil {
			return fmt.Errorf("failed to open target files for archive: %s", err)
		}

		if _, err := io.Copy(t, f); err != nil {
			return fmt.Errorf("failed to copy files for archive: %s", err)
		}

		return f.Close()
	})

	if err != nil {
		return cleanup(fmt.Errorf("failed to walk output path for archive: %s", err))
	}

	// Explicitly close things in the correct order (tar then gzip) so we
	// know if they worked.
	if err := t.Close(); err != nil {
		return cleanup(err)
	}
	if err := g.Close(); err != nil {
		return cleanup(err)
	}

	// Guarantee that the contents of the temp file are flushed to disk.
	if err := f.Sync(); err != nil {
		return cleanup(err)
	}

	// Close the temp file and go back to the wrapper function for the rest.
	if err := f.Close(); err != nil {
		return cleanup(err)
	}

	return tempName, nil
}

// Help returns a description of the command and how it is used.
func (c *DebugCommand) Help() string {
	c.once.Do(c.init)
	return c.Synopsis() + "\n\nUsage: Consul-k8s debug [flags]\n\n" + c.help
}

// Synopsis returns a one-line command summary.
func (c *DebugCommand) Synopsis() string {
	return "Capture debugging information from a Consul deployment on Kubernetes."
}
