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

	shared "github.com/hashicorp/consul-k8s/cli/cmd/shared"
	"github.com/hashicorp/consul-k8s/cli/common"
	"github.com/hashicorp/consul-k8s/cli/common/flag"
	"github.com/hashicorp/consul-k8s/cli/common/terminal"
	"github.com/hashicorp/consul-k8s/cli/helm"
	"github.com/hashicorp/go-multierror"
	"github.com/posener/complete"
	"golang.org/x/sync/errgroup"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	k8errors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	// "context"
	// "errors"
	// "fmt"
	// "strings"
	// "sync"
	// "github.com/posener/complete"
	helmCLI "helm.sh/helm/v3/pkg/cli"
	// "k8s.io/apimachinery/pkg/api/validation"
	// metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	// "k8s.io/client-go/kubernetes"
	// "k8s.io/client-go/rest"
	// "github.com/hashicorp/consul-k8s/cli/common"
	// "github.com/hashicorp/consul-k8s/cli/common/envoy"
	// "github.com/hashicorp/consul-k8s/cli/common/flag"
	// "github.com/hashicorp/consul-k8s/cli/common/terminal"
)

const (
	// debugDuration is the total time that debug runs before being shut down
	debugDuration = 5 * time.Minute

	// debugSince is the time that debug looks back to capture logs from pods
	debugSince = 5 * time.Minute

	// debugDurationGrace is a period of time added to the specified
	// duration to allow log capture within that time
	debugDurationGrace = 2 * time.Second

	// debugMinDuration is the minimum a user can configure the duration
	// to ensure that all information can be collected in time
	debugMinDuration = 10 * time.Second

	// debugArchiveExtension is the extension for archive files
	debugArchiveExtension = ".tar.gz"
)

var notFoundError = errors.New("not found")
var signalInterruptError = errors.New("signal interrupt received")

// debugIndex is used to manage the summary of all data recorded
// during the debug, to be written to json at the end of the run
// and stored at the root. Each attribute corresponds to a file or files.
type debugIndex struct {
	Duration  string   `json:"duration"`
	Since     string   `json:"since"`
	Targets   []string `json:"targets"`
	Timestamp string   `json:"timestamp"`
}

const (
	flagNameKubeConfig  = "kubeconfig"
	flagNameKubeContext = "context"
	flagNameNamespace   = "namespace"
)

// timeDateformat is a modified version of time.RFC3339 which replaces colons with
// hyphens. This is to make it more convenient to untar these files, because
// tar assumes colons indicate the file is on a remote host, unless --force-local
// is used.
const timeDateFormat = "2006-01-02T15-04-05Z0700"

type DebugCommand struct {
	*common.BaseCommand

	kubernetes    kubernetes.Interface
	apiextensions *apiextensionsclient.Clientset // for retrieving k8s CRDs List
	dynamic       dynamic.Interface              // for retrieving k8s CRD resources

	helmEnvSettings   *helmCLI.EnvSettings
	helmActionsRunner helm.HelmActionsRunner

	set *flag.Sets

	flagKubeConfig  string
	flagKubeContext string
	flagNamespace   string

	restConfig *rest.Config

	// flags
	duration time.Duration
	since    time.Duration
	output   string
	archive  bool
	capture  []string

	// validateTiming can be used to skip validation of duration. This
	// is primarily useful for testing
	validateTiming bool
	// timeNow is a shim for testing, it is used to generate the time used in
	// file paths.
	timeNow func() time.Time

	once sync.Once
	help string
}

// init sets up flags and help text for the command.
func (c *DebugCommand) init() {
	c.set = flag.NewSets()

	f := c.set.NewSet("Command Options")
	defaultOutputFilename := fmt.Sprintf("consul-debug-%v", time.Now().Format(timeDateFormat))

	f.DurationVar(&flag.DurationVar{
		Name:    "duration",
		Target:  &c.duration,
		Default: debugDuration,
		Usage:   "To capture the logs of consul cluster for the a given duration",
		Aliases: []string{"d"},
	})
	f.DurationVar(&flag.DurationVar{
		Name:    "since",
		Target:  &c.since,
		Default: 0,
		Usage:   "The time duration since when to capture logs from pods",
		Aliases: []string{"s"},
	})
	f.StringVar(&flag.StringVar{
		Name:    "output",
		Target:  &c.output,
		Default: defaultOutputFilename,
		Usage:   "The filename of the debug output archive",
		Aliases: []string{"o"},
	})
	f.BoolVar(&flag.BoolVar{
		Name:    "archive",
		Target:  &c.archive,
		Default: true,
		Usage:   "Whether to archive the output debug directory to a .tar.gz",
	})
	f.StringSliceVar(&flag.StringSliceVar{
		Name:    "capture",
		Target:  &c.capture,
		Default: []string{"all"},
		Usage:   "A list of components to capture. Supported values are: all, pods, events, nodes, services, endpoints, configmaps, daemonsets, statefulsets, deployments, replicasets. (e.g. -capture pods -capture events)",
	})

	f = c.set.NewSet("Global Options")
	f.StringVar(&flag.StringVar{
		Name:    flagNameKubeConfig,
		Aliases: []string{"c"},
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
	f.StringVar(&flag.StringVar{
		Name:    flagNameNamespace,
		Target:  &c.flagNamespace,
		Default: "consul",
		Usage:   "The namespace where the target Pod can be found.",
		Aliases: []string{"n"},
	})

	c.validateTiming = true
	c.timeNow = func() time.Time {
		return time.Now().UTC()
	}
	c.help = c.set.Help()
}

// Run executes the list command.
func (c *DebugCommand) Run(args []string) int {
	c.once.Do(c.init)
	defer common.CloseWithError(c.BaseCommand)

	if c.helmActionsRunner == nil {
		c.helmActionsRunner = &helm.ActionRunner{}
	}

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
	if err := c.preValidations(); err != nil {
		c.UI.Output("Pre-validation failed: %v", err.Error(), terminal.WithErrorStyle())
		return 1
	}
	if c.kubernetes == nil {
		if err := c.initKubernetes(); err != nil {
			c.UI.Output("Error initializing Kubernetes client: %v", err.Error(), terminal.WithErrorStyle())
			return 1
		}
	}

	archiveName := c.output
	if c.archive {
		archiveName = archiveName + debugArchiveExtension
	}

	c.UI.Output("\nStarting debugger: ")

	// Output metadata about debug run
	c.UI.Output(fmt.Sprintf(" - Output:           %s", archiveName))
	c.UI.Output(fmt.Sprintf(" - Capture Targets:  %s", strings.Join(c.capture, ", ")))

	c.duration = c.duration + debugDurationGrace

	select {
	case <-c.CleanupReq:
	default:
	}
	c.CleanupReq <- true
	defer func() { c.CleanupConfirmation <- 1 }()

	// capture helm config, CRDs and its resources, consul injected sidecar pods, proxy data, if asked
	if err := c.captureStaticInfo(); err != nil {
		c.UI.Output("Error capturing static info: %v", err.Error(), terminal.WithErrorStyle())
		// return 1 // error(s) already printed in captureStaticInfo, primarily useful for testing
	}

	// capture pod logs & events, if asked
	if c.CaptureTarget(targetLogs) {
		g := new(errgroup.Group)
		g.Go(func() error {
			return c.capturePodLogsAndEvents()
		})
		err := g.Wait()
		if err != nil {
			if errors.Is(err, signalInterruptError) {
				c.UI.Output("Debug run interrupted, cleaning up partial capture...", terminal.WithErrorStyle())
				err := os.RemoveAll(c.output)
				if err != nil {
					c.UI.Output(fmt.Sprintf("error cleaning up partial capture: %v", err), terminal.WithErrorStyle())
					return 1
				}
				c.UI.Output(" - Cleanup completed")
				return 1
			}
			c.UI.Output(fmt.Sprintf("error capturing consul pods logs: %v", err), terminal.WithErrorStyle())
			c.UI.Output("Partial logs might be captured, please verify saved debug archive", terminal.WithWarningStyle())
			// return 1
		}
	}

	// Capture metadata about debug run at the root of the debug archive
	index := &debugIndex{
		Duration:  c.duration.String(),
		Since:     c.since.String(),
		Targets:   c.capture,
		Timestamp: time.Now().Format(time.RFC3339),
	}
	if err := writeJSONFile(filepath.Join(c.output, "index.json"), index); err != nil {
		c.UI.Output(fmt.Sprintf("error writing index.json: %v", err))
		// return 1
	}
	// Archive the data if configured to
	if c.archive {
		err := c.createArchive()
		if err != nil {
			c.UI.Output(fmt.Sprintf("error archiving debug output: %v", err), terminal.WithErrorStyle())
			return 1
		}
	}
	c.UI.Output(fmt.Sprintf("Saved debug archive: %s", archiveName))

	return 0
}

func (c *DebugCommand) validateFlags() error {
	if len(c.set.Args()) > 0 {
		return fmt.Errorf("should have no non-flag arguments")
	}

	// Ensure realistic duration is specified
	if c.validateTiming {
		if c.duration < debugMinDuration {
			return fmt.Errorf("duration must be longer than %s", debugMinDuration)
		}
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
func (c *DebugCommand) preValidations() error {
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

// TODO: check if AutocompleteFlags & AutocompleteArgs are required

// AutocompleteFlags returns a mapping of supported flags and autocomplete
// options for this command. The map key for the Flags map should be the
// complete flag such as "-foo" or "--foo".
func (c *DebugCommand) AutocompleteFlags() complete.Flags {
	return complete.Flags{
		fmt.Sprintf("-%s", flagNameKubeConfig):  complete.PredictFiles("*"),
		fmt.Sprintf("-%s", flagNameKubeContext): complete.PredictNothing,
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
	if c.CaptureTarget(targetCRDs) {
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

// captureStaticInfo - captures Helm config, CRDs and its resources, consul injected sidecar pods, proxy data, if asked
func (c *DebugCommand) captureStaticInfo() error {
	c.UI.Output("\nCapturing static info......")
	var errs error
	if c.CaptureTarget(targetHelmConfig) {
		err := c.captureHelmConfig()
		if err != nil {
			c.UI.Output(fmt.Sprintf("error capturing Helm config: %v", err), terminal.WithErrorStyle())
			errs = multierror.Append(errs, err)
		} else {
			c.UI.Output("Helm config captured", terminal.WithSuccessStyle())
		}
	}
	if c.CaptureTarget(targetCRDs) {
		err := c.captureCRDResources()
		if err != nil {
			if errors.Is(err, notFoundError) {
				c.UI.Output("No Consul CRDs found in Kubernetes cluster", terminal.WithWarningStyle())
			} else {
				c.UI.Output(fmt.Sprintf("error capturing CRD resources: %v", err), terminal.WithErrorStyle())
				errs = multierror.Append(errs, err)
			}
		} else {
			c.UI.Output("CRDs resources captured", terminal.WithSuccessStyle())
		}
	}
	if c.CaptureTarget(targetSidecarPods) {
		err := c.captureConsulInjectedSidecarPods()
		if err != nil {
			if errors.Is(err, notFoundError) {
				c.UI.Output("No Consul injected sidecar pods found in Kubernetes cluster.", terminal.WithWarningStyle())
			} else {
				c.UI.Output(fmt.Sprintf("error capturing Consul Injected Sidecar Pods: %v", err), terminal.WithErrorStyle())
				errs = multierror.Append(errs, err)
			}
		} else {
			c.UI.Output("Consul Injected Sidecar Pods captured", terminal.WithSuccessStyle())
		}
	}
	if c.CaptureTarget(targetEnvoy) {
		err := c.captureEnvoyProxyData()
		if err != nil {
			if errors.Is(err, notFoundError) {
				c.UI.Output("No Envoy Proxy pods found in Kubernetes cluster in all namespaces.", terminal.WithWarningStyle())
			} else {
				c.UI.Output(fmt.Sprintf("error capturing Consul Envoy Proxy data: \n%v", err), terminal.WithErrorStyle())
				errs = multierror.Append(errs, err)
			}
		} else {
			c.UI.Output("Envoy Proxy data captured", terminal.WithSuccessStyle())
		}
	}
	return errs
}

// captureHelmConfig - captures consul-k8s Helm configuration and write it to helm-config.json file within debug archive
func (c *DebugCommand) captureHelmConfig() error {
	helmRelease, _, _, err := shared.GetHelmRelease(c.helmEnvSettings, c.helmActionsRunner)
	if err != nil {
		return fmt.Errorf("couldn't retrieve Helm release: %v", err)
	}
	err = writeJSONFile(filepath.Join(c.output, "helm-config.json"), helmRelease)
	if err != nil {
		return fmt.Errorf("couldn't write Helm config to json file: %v", err)
	}
	return nil
}

// captureCRDResources - captures consul-k8s CRDs and their instances and write it to CRDsResources.json file within debug archive
func (c *DebugCommand) captureCRDResources() error {
	var errs error
	crdResources, err := c.listCRDResources()
	if err != nil {
		if errors.Is(err, notFoundError) {
			return err
		}
		errs = multierror.Append(errs, err)
	}
	if len(crdResources) != 0 {
		err := writeJSONFile(filepath.Join(c.output, "CRDsResources.json"), crdResources)
		if err != nil {
			errs = multierror.Append(errs, fmt.Errorf("couldn't write CRDs Resources to json file: %v", err))
		}
	}
	return errs
}

// listCRDResources - captures all Consul-related CRDs and lists their applied resources for ALL VERSION of CRDs respectively in a map.
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
				errs = multierror.Append(errs, fmt.Errorf("couldn't retrieve resources for CRD %s and version %s: %w", crd.Name, version.Name, err))
				continue
			}
			crdResourcesMap[key] = unstructuredList.Items
		}
	}
	return crdResourcesMap, errs
}

// captureConsulInjectedSidecarPods - captures & list all pods across all namespaces and their status (number of pods ready/desired)
// that have been injected with Consul sidecars using the label consul.hashicorp.com/connect-inject-status=injected.
// and write it to sidecarPods.json within debug archive
func (c *DebugCommand) captureConsulInjectedSidecarPods() error {
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

// ===================================================================================================================

// captureTarget returns true if the target capture type is enabled. (Otherwords, is the target given in capture flag in command line)
func (c *DebugCommand) CaptureTarget(target string) bool {
	if c.capture == nil || slices.Contains(c.capture, "all") || slices.Contains(c.capture, target) {
		return true
	}
	return false
}

func writeJSONFile(filename string, content interface{}) error {
	marshaled, err := json.MarshalIndent(content, "", "\t")
	if err != nil {
		return err
	}
	return os.WriteFile(filename, marshaled, 0644)
}

// ===================================================================================================================

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

// ===================================================================================================================

// Helm config, pod logs, CRDs, proxy stats, and Envoy endpoints.
const (
	targetHelmConfig  = "helm"    // captures helm config 														// for whole k8s cluster
	targetCRDs        = "crds"    // captures crds and theit applied resources(k8s objects) 					// for whole k8s cluster
	targetSidecarPods = "sidecar" // captures consul injected sidecar pods metadata								// for whole k8s cluster
	targetLogs        = "logs"    // capture logs & events 														// for ALL pods in the k8s cluster related to consul
	targetEnvoy       = "envoy"   // capture envoy endpoint {/stats, /endpoints, /clusters, /config_dumps},  	// for ALL proxy pod in the k8s cluster related to consul
)

// defaultTargets specifies the list of targets that will be captured by default
var defaultTargets = []string{
	targetHelmConfig,
	targetCRDs,
	targetLogs,
	targetSidecarPods,
	targetEnvoy,
}

// Help returns a description of the command and how it is used.
func (c *DebugCommand) Help() string {
	c.once.Do(c.init)
	return c.Synopsis() + "\n\nUsage: consul-k8s debug [flags]\n\n" + c.help
}

// Synopsis returns a one-line command summary.
func (c *DebugCommand) Synopsis() string {
	return "debug consul on Kubernetes."
}
