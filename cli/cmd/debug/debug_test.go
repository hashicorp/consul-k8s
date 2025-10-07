package debug

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/hashicorp/consul-k8s/cli/common"
	"github.com/hashicorp/consul-k8s/cli/common/envoy"
	cmnFlag "github.com/hashicorp/consul-k8s/cli/common/flag"
	"github.com/hashicorp/consul-k8s/cli/common/terminal"
	"github.com/hashicorp/consul-k8s/cli/helm"
	"github.com/hashicorp/go-hclog"
	"github.com/posener/complete"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	helmCLI "helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/release"
	helmRelease "helm.sh/helm/v3/pkg/release"
	helmTime "helm.sh/helm/v3/pkg/time"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsfake "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"

	dynamicFake "k8s.io/client-go/dynamic/fake"
)

func TestFlagParsingFails(t *testing.T) {
	cases := map[string]struct {
		args []string
		out  int
	}{
		"Nonexistent flag passed, -foo bar, should fail": {
			args: []string{"-foo", "bar"},
			out:  1,
		},
		"Invalid argument passed, -namespace YOLO": {
			args: []string{"-namespace", "YOLO"},
			out:  1,
		},
		"Invalid namespace argument passed, -namespace YOLO": {
			args: []string{"-namespace", "YOLO"},
			out:  1,
		},
		"Invalid duration argument passed, -duration invalid": {
			args: []string{"-duration", "invalid"},
			out:  1,
		},
		"Invalid capture target argument passed, -capture foo": {
			args: []string{"-capture", "foo"},
			out:  1,
		},
		"Invalid capture target arguments passed, -capture logs,foo": {
			args: []string{"-capture", "logs,foo"},
			out:  1,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c := initializeDebugCommands(new(bytes.Buffer))
			c.kubernetes = fake.NewSimpleClientset()
			out := c.Run(tc.args)
			require.Equal(t, tc.out, out)
		})
	}
}
func TestPreChecks(t *testing.T) {
	cases := map[string]struct {
		output        string
		setup         func(t *testing.T, testDir string)
		expectedError string
	}{
		"output dir specified, should be created": {
			output: "some-dir",
		},
		"output dir already exists, should error": {
			output: "existing-dir",
			setup: func(t *testing.T, testDir string) {
				err := os.MkdirAll(filepath.Join(testDir, "existing-dir"), 0755)
				require.NoError(t, err)
			},
			expectedError: "output directory already exists",
		},
		"no write permissions for cwd, should error": {
			output: "another-dir",
			setup: func(t *testing.T, testDir string) {
				err := os.Chmod(testDir, 0555)
				require.NoError(t, err)
			},
			expectedError: "could not create output directory, no write permission",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			tempDir := t.TempDir()
			if tc.setup != nil {
				tc.setup(t, tempDir)
			}
			c := initializeDebugCommands(new(bytes.Buffer))
			c.output = filepath.Join(tempDir, tc.output)

			err := c.preChecks()

			if tc.expectedError != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.expectedError)
			} else {
				require.NoError(t, err)
				info, statErr := os.Stat(c.output)
				require.NoError(t, statErr, "output directory should be created")
				require.True(t, info.IsDir(), "output path should be a directory")
			}
		})
	}
}
func TestCreateArchive(t *testing.T) {
	sourceDir := t.TempDir()
	dummyFilePath := filepath.Join(sourceDir, "dummy.txt")
	err := os.WriteFile(dummyFilePath, []byte("hello world"), 0644)
	require.NoError(t, err)

	c := initializeDebugCommands(new(bytes.Buffer))
	c.output = sourceDir
	err = c.createArchive()

	require.NoError(t, err, "createArchive should not return an error")

	archivePath := sourceDir + debugArchiveExtension
	_, err = os.Stat(archivePath)
	require.NoError(t, err, "expected archive file to exist")
	_, err = os.Stat(sourceDir)
	require.True(t, os.IsNotExist(err), "expected source directory to be removed")
}
func TestAutocompleteFlags(t *testing.T) {
	t.Parallel()
	buf := new(bytes.Buffer)
	cmd := initializeDebugCommands(buf)

	predictor := cmd.AutocompleteFlags()

	// Test that we get the expected number of predictions
	args := complete.Args{Last: "-"}
	res := predictor.Predict(args)

	// Grab the list of flags from the Flag object
	flags := make([]string, 0)
	cmd.set.VisitSets(func(name string, set *cmnFlag.Set) {
		set.VisitAll(func(flag *flag.Flag) {
			flags = append(flags, fmt.Sprintf("-%s", flag.Name))
		})
	})

	// Verify that there is a prediction for each flag associated with the command
	assert.Equal(t, len(flags), len(res))
	assert.ElementsMatch(t, flags, res, "flags and predictions didn't match, make sure to add "+
		"new flags to the command AutoCompleteFlags function")
}
func TestAutocompleteArgs(t *testing.T) {
	buf := new(bytes.Buffer)
	cmd := initializeDebugCommands(buf)
	c := cmd.AutocompleteArgs()
	assert.Equal(t, complete.PredictNothing, c)
}
func TestCaptureHelmConfig(t *testing.T) {
	nowTime := helmTime.Now()
	cases := map[string]struct {
		messages          []string
		helmActionsRunner *helm.MockActionRunner
		expectedError     error
	}{
		"empty config": {
			messages: []string{"\n"},
			helmActionsRunner: &helm.MockActionRunner{
				GetStatusFunc: func(status *action.Status, name string) (*helmRelease.Release, error) {
					return &helmRelease.Release{
						Name: "consul", Namespace: "consul",
						Info:   &helmRelease.Info{LastDeployed: nowTime, Status: "READY"},
						Chart:  &chart.Chart{Metadata: &chart.Metadata{Version: "1.0.0"}},
						Config: make(map[string]interface{})}, nil
				},
			},
			expectedError: nil,
		},
		"error": {
			helmActionsRunner: &helm.MockActionRunner{
				GetStatusFunc: func(status *action.Status, name string) (*helmRelease.Release, error) {
					return nil, errors.New("dummy-error")
				},
			},
			expectedError: errors.New("couldn't get the helm release: dummy-error"),
		},
		"some config": {
			messages: []string{"\"global\": \"true\"", "\n", "\"name\": \"consul\""},
			helmActionsRunner: &helm.MockActionRunner{
				GetStatusFunc: func(status *action.Status, name string) (*helmRelease.Release, error) {
					return &helmRelease.Release{
						Name: "consul", Namespace: "consul",
						Info: &helmRelease.Info{LastDeployed: nowTime, Status: "READY"},
						Chart: &chart.Chart{
							Metadata: &chart.Metadata{
								Version: "1.0.0",
							},
						},
						Config: map[string]interface{}{"global": "true"},
					}, nil
				},
			},
			expectedError: nil,
			// expectedOutputBuffer: "Helm config captured",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {

			buf := new(bytes.Buffer)
			c := initializeDebugCommands(buf)
			c.kubernetes = fake.NewSimpleClientset()
			c.helmActionsRunner = tc.helmActionsRunner
			c.helmEnvSettings = helmCLI.New()
			c.output = t.TempDir()
			err := c.captureHelmConfig()

			require.Equal(t, tc.expectedError, err)

			if tc.expectedError != nil {
				return
			}
			expectedFilePath := filepath.Join(c.output, "helm-config.json")
			_, statErr := os.Stat(expectedFilePath)
			require.NoError(t, statErr, "expected helm config file to be created")

			actualConfig, err := os.ReadFile(expectedFilePath)
			require.NoError(t, err)

			for _, msg := range tc.messages {
				require.Contains(t, string(actualConfig), msg)
			}
		})
	}

}
func TestCaptureConsulInjectedSidecarPods(t *testing.T) {
	// Helper to create a fake pod for testing.
	createFakePod := func(name, namespace string, ready, totalContainers, restarts int) *corev1.Pod {
		statuses := make([]corev1.ContainerStatus, totalContainers)
		for i := 0; i < totalContainers; i++ {
			statuses[i] = corev1.ContainerStatus{
				Name:         fmt.Sprintf("container-%d", i),
				RestartCount: int32(restarts),
				Ready:        i < ready,
			}
		}

		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				Labels:    map[string]string{"consul.hashicorp.com/connect-inject-status": "injected"},
			},
			Spec: corev1.PodSpec{
				Containers: make([]corev1.Container, totalContainers),
			},
			Status: corev1.PodStatus{
				Phase:             corev1.PodRunning,
				PodIP:             "192.168.1.100",
				ContainerStatuses: statuses,
			},
		}
	}

	cases := map[string]struct {
		initialPods   []runtime.Object
		expectedError error
		expectFile    bool
		// Change this to check for the specific pod name and its ready status
		expectedPodName  string
		expectedReadyVal string
	}{
		"success with one injected pod": {
			initialPods: []runtime.Object{
				createFakePod("my-app-pod-1", "default", 1, 2, 3),
			},
			expectFile:       true,
			expectedPodName:  "my-app-pod-1",
			expectedReadyVal: "1/2",
		},
		"no injected pods found": {
			initialPods:   []runtime.Object{},
			expectedError: notFoundError,
			expectFile:    false,
		},
		"pod exists but without correct label": {
			initialPods: []runtime.Object{
				&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "unrelated-pod"}},
			},
			expectedError: notFoundError,
			expectFile:    false,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c := initializeDebugCommands(new(bytes.Buffer))
			c.output = t.TempDir()
			c.Ctx = context.Background()
			c.kubernetes = fake.NewSimpleClientset(tc.initialPods...)

			err := c.captureSidecarPods()

			if tc.expectedError != nil {
				require.Error(t, err)
				require.True(t, errors.Is(err, tc.expectedError))
			} else {
				require.NoError(t, err)
			}

			jsonFilePath := filepath.Join(c.output, "sidecarPods.json")
			_, statErr := os.Stat(jsonFilePath)

			if !tc.expectFile {
				require.True(t, os.IsNotExist(statErr), "expected JSON file not to be created")
				return
			}

			require.NoError(t, statErr, "expected JSON file to be created")
			content, readErr := os.ReadFile(jsonFilePath)
			require.NoError(t, readErr)

			// Unmarshal the JSON into a Go map.
			var podsData map[string]map[string]string
			unmarshalErr := json.Unmarshal(content, &podsData)
			require.NoError(t, unmarshalErr, "failed to unmarshal output JSON")

			// Assert that the specific data exists in the map.
			podInfo, ok := podsData[tc.expectedPodName]
			require.True(t, ok, "expected pod not found in JSON output")
			require.Equal(t, tc.expectedReadyVal, podInfo["ready"])
		})
	}
}
func TestListAndCaptureCRDResources(t *testing.T) {
	crd := createFakeCRD()
	cr1 := createFakeCR("my-cr-1", "default")
	cr2 := createFakeCR("my-cr-2", "default")
	cr3 := createFakeCR("my-cr-3", "consul")
	// Define the GVR for the custom resource. This is needed for the fake client setup.
	serviceIntentionsGVR := schema.GroupVersionResource{
		Group:    "consul.hashicorp.com",
		Version:  "v1alpha1",
		Resource: "serviceintentions",
	}
	cases := map[string]struct {
		crdObjects    []runtime.Object
		crObjects     []runtime.Object
		namespace     string
		expectedError error
		assertFunc    func(t *testing.T, crdMap map[string][]unstructured.Unstructured)
	}{
		"success with multiple CRs in default namespace": {
			crdObjects: []runtime.Object{crd},
			crObjects:  []runtime.Object{cr1, cr2, cr3},
			namespace:  "default",
			assertFunc: func(t *testing.T, crdMap map[string][]unstructured.Unstructured) {
				require.Len(t, crdMap, 1, "Expected one CRD type in the map")
				key := fmt.Sprintf("%s/v1alpha1", crd.Name)
				resources, ok := crdMap[key]
				require.True(t, ok, "Expected key for CRD version not found")
				require.Len(t, resources, 2, "Expected two CR instances")
				require.Equal(t, "my-cr-1", resources[0].GetName())
				require.Equal(t, "my-cr-2", resources[1].GetName())
			},
		},
		"success with single CRs in consul namespace": {
			crdObjects: []runtime.Object{crd},
			crObjects:  []runtime.Object{cr1, cr2, cr3},
			namespace:  "consul",
			assertFunc: func(t *testing.T, crdMap map[string][]unstructured.Unstructured) {
				require.Len(t, crdMap, 1, "Expected one CRD type in the map")
				key := fmt.Sprintf("%s/v1alpha1", crd.Name)
				resources, ok := crdMap[key]
				require.True(t, ok, "Expected key for CRD version not found")
				require.Len(t, resources, 1, "Expected one CR instances")
				require.Equal(t, "my-cr-3", resources[0].GetName())
			},
		},
		"no CRDs found": {
			crdObjects:    []runtime.Object{},
			crObjects:     []runtime.Object{},
			namespace:     "default",
			expectedError: notFoundError,
		},
		"crd exists but no resources": {
			crdObjects: []runtime.Object{crd},
			crObjects:  []runtime.Object{}, // No CR instances
			namespace:  "default",
			assertFunc: func(t *testing.T, crdMap map[string][]unstructured.Unstructured) {
				require.Len(t, crdMap, 1, "Expected one CRD type in the map")
				key := fmt.Sprintf("%s/v1alpha1", crd.Name)
				resources, ok := crdMap[key]
				require.True(t, ok, "Expected key for CRD version not found")
				require.Len(t, resources, 0, "Expected zero CR instances")
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c := initializeDebugCommands(new(bytes.Buffer))
			c.flagNamespace = tc.namespace

			c.apiextensions = apiextensionsfake.NewSimpleClientset(tc.crdObjects...)
			listMapping := map[schema.GroupVersionResource]string{
				serviceIntentionsGVR: "ServiceIntentionList",
			}
			dynamicClient := dynamicFake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), listMapping, tc.crObjects...)
			c.dynamic = dynamicClient

			// testListCRDResources
			crdResourcesMap, err := c.listCRDResources()
			if tc.expectedError != nil {
				require.Error(t, err)
				require.True(t, errors.Is(err, tc.expectedError))
			} else {
				require.NoError(t, err)
				tc.assertFunc(t, crdResourcesMap)
			}

			// testCaptureCRDResources
			c.output = t.TempDir()
			err = c.captureCRDResources()
			if name == "no CRDs found" {
				require.Error(t, err)
				require.True(t, errors.Is(err, notFoundError))
				return
			}
			require.NoError(t, err)

			jsonFilePath := filepath.Join(c.output, "CRDsResources.json")
			_, statErr := os.Stat(jsonFilePath)
			require.NoError(t, statErr, "expected JSON file to be created")

			content, readErr := os.ReadFile(jsonFilePath)
			require.NoError(t, readErr)

			// Unmarshal the JSON into a Go map.
			var fileData map[string][]unstructured.Unstructured
			unmarshalErr := json.Unmarshal(content, &fileData)
			require.NoError(t, unmarshalErr, "failed to unmarshal output JSON")

			// Use the same assertion function to validate the file contents
			tc.assertFunc(t, fileData)
		})
	}
}
func TestDebugRun(t *testing.T) {
	// test environment setup
	helmRelease := &release.Release{
		Name: "consul", Namespace: "consul",
		Info:   &release.Info{Status: "deployed"},
		Chart:  &chart.Chart{Metadata: &chart.Metadata{Version: "1.0.0"}},
		Config: make(map[string]interface{}),
	}
	server := startHttpServerForEnvoyStats(envoyDefaultAdminPort, `{"stats": {}}`)
	defer server.Close()
	k8sObjects, crObjects, crdObjects, serviceIntentionsGVR := createTestResource()

	// testcases
	cases := map[string]struct {
		args                 []string
		helmRunner           *helm.MockActionRunner
		fetchLogFunc         func(ns string, podName string, opts *corev1.PodLogOptions) (io.ReadCloser, error)
		fetchEnvoyConfig     func(ctx context.Context, pf common.PortForwarder) (*envoy.EnvoyConfig, error)
		expectedOutputPath   string
		expectedReturnCode   int
		expectedOutputBuffer []string
		expectDebugArchive   bool // whether we expect debug bundle to be an archive or a directory
	}{
		"success case with all targets with duration": {
			args: []string{"-archive=true", "-duration=10s", "-output=tc1"},
			helmRunner: &helm.MockActionRunner{
				GetStatusFunc: func(status *action.Status, name string) (*release.Release, error) {
					return helmRelease, nil
				},
			},
			fetchLogFunc: func(ns string, podName string, opts *corev1.PodLogOptions) (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewBufferString("log line")), nil
			},
			fetchEnvoyConfig: func(ctx context.Context, pf common.PortForwarder) (*envoy.EnvoyConfig, error) {
				return testEnvoyConfig, nil
			},
			expectedOutputPath: "tc1",
			expectedReturnCode: 0,
			expectDebugArchive: true,
			expectedOutputBuffer: []string{"Starting debugger:", "Helm config captured",
				"CRD resources captured", "Consul Sidecar Pods captured", "Envoy Proxy data captured",
				"Capturing pods logs.....", "Pods Logs captured", "Index captured", "Saved debug archive"},
		},
		"success case with all targets with since": {
			args: []string{"-archive=false", "-since=10s", "-output=tc2"}, // Default is all capture targets
			helmRunner: &helm.MockActionRunner{
				GetStatusFunc: func(status *action.Status, name string) (*release.Release, error) {
					return helmRelease, nil
				},
			},
			fetchLogFunc: func(ns string, podName string, opts *corev1.PodLogOptions) (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewBufferString("log line")), nil
			},
			fetchEnvoyConfig: func(ctx context.Context, pf common.PortForwarder) (*envoy.EnvoyConfig, error) {
				return testEnvoyConfig, nil
			},
			expectedOutputPath: "tc2",
			expectedReturnCode: 0,
			expectDebugArchive: false,
			expectedOutputBuffer: []string{"Starting debugger:", "Helm config captured",
				"CRD resources captured", "Consul Sidecar Pods captured", "Envoy Proxy data captured",
				"Capturing pods logs.....", "Pods Logs captured", "Index captured", "Saved debug directory"},
		},
		"helm capture fail": {
			args: []string{"-archive=false", "-duration=10s", "-output=tc3"},
			helmRunner: &helm.MockActionRunner{
				GetStatusFunc: func(status *action.Status, name string) (*release.Release, error) {
					return nil, errors.New("testing helm error")
				},
			},
			fetchLogFunc: func(ns string, podName string, opts *corev1.PodLogOptions) (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewBufferString("log line")), nil
			},
			fetchEnvoyConfig: func(ctx context.Context, pf common.PortForwarder) (*envoy.EnvoyConfig, error) {
				return testEnvoyConfig, nil
			},
			expectedOutputPath: "tc3",
			expectedReturnCode: 1,
			expectDebugArchive: false,
			expectedOutputBuffer: []string{"Starting debugger:", "error capturing Helm config", "testing helm error",
				"CRD resources captured", "Consul Sidecar Pods captured", "Envoy Proxy data captured",
				"Capturing pods logs.....", "Pods Logs captured", "Index captured", "Saved debug directory"},
		},
		"envoy proxy data capture fail": {
			args: []string{"-archive=false", "-duration=10s", "-output=tc4"},
			helmRunner: &helm.MockActionRunner{
				GetStatusFunc: func(status *action.Status, name string) (*release.Release, error) {
					return helmRelease, nil
				},
			},
			fetchLogFunc: func(ns string, podName string, opts *corev1.PodLogOptions) (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewBufferString("log line")), nil
			},
			fetchEnvoyConfig: func(ctx context.Context, pf common.PortForwarder) (*envoy.EnvoyConfig, error) {
				return nil, errors.New("testing envoy config fetch error")
			},
			expectedOutputPath: "tc4",
			expectedReturnCode: 1,
			expectDebugArchive: false,
			expectedOutputBuffer: []string{"Starting debugger:", "Helm config captured", "CRD resources captured",
				"Consul Sidecar Pods captured", "error capturing Envoy Proxy data", oneOrMoreErrorOccured.Error(),
				"Capturing pods logs.....", "Pods Logs captured", "Index captured", "Saved debug directory"},
		},
		"log capture fail": {
			args: []string{"-archive=true", "-duration=10s", "-output=tc5"},
			helmRunner: &helm.MockActionRunner{
				GetStatusFunc: func(status *action.Status, name string) (*release.Release, error) {
					return helmRelease, nil
				},
			},
			fetchLogFunc: func(ns string, podName string, opts *corev1.PodLogOptions) (io.ReadCloser, error) {
				return nil, errors.New("testing log fetch error")
			},
			fetchEnvoyConfig: func(ctx context.Context, pf common.PortForwarder) (*envoy.EnvoyConfig, error) {
				return testEnvoyConfig, nil
			},
			expectedOutputPath: "tc5",
			expectedReturnCode: 1,
			expectDebugArchive: true,
			expectedOutputBuffer: []string{"Starting debugger:", "Helm config captured", "CRD resources captured",
				"Consul Sidecar Pods captured", "Envoy Proxy data captured", "Capturing pods logs.....", "error capturing Pods Logs",
				oneOrMoreErrorOccured.Error(), "Index captured", "Saved debug archive"},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {

			// Setup a temp working directory for the test
			tempDir := t.TempDir()
			originalWD, err := os.Getwd()
			require.NoError(t, err)
			err = os.Chdir(tempDir)
			require.NoError(t, err)
			defer os.Chdir(originalWD)

			tc.expectedOutputPath = filepath.Join(tempDir, tc.expectedOutputPath)

			buf := new(bytes.Buffer)
			c := initializeDebugCommands(buf)

			c.helmActionsRunner = tc.helmRunner
			c.helmEnvSettings = helmCLI.New()
			c.kubernetes = fake.NewSimpleClientset(k8sObjects...)
			c.apiextensions = apiextensionsfake.NewSimpleClientset(crdObjects...)
			listMapping := map[schema.GroupVersionResource]string{serviceIntentionsGVR: "ServiceIntentionList"}
			c.dynamic = dynamicFake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), listMapping, crObjects...)

			c.proxyCapturer = &EnvoyProxyCapture{
				kubernetes: c.kubernetes,
				output:     c.output,
				ctx:        c.Ctx,
			}
			c.logCapturer = &LogCapture{
				BaseCommand: c.BaseCommand,
				kubernetes:  c.kubernetes,
				output:      c.output,
				ctx:         c.Ctx,
			}
			c.proxyCapturer.envoyDefaultAdminPortEndpoint = "localhost:" + strconv.Itoa(envoyDefaultAdminPort)
			c.proxyCapturer.fetchEnvoyConfig = tc.fetchEnvoyConfig
			c.logCapturer.fetchLogsFunc = tc.fetchLogFunc

			returnCode := c.Run(tc.args)

			require.Equal(t, tc.expectedReturnCode, returnCode, "unexpected return code")
			for _, expectedStr := range tc.expectedOutputBuffer {
				require.Contains(t, buf.String(), expectedStr, "unexpected buffer output")
			}

			expectedArchivePath := tc.expectedOutputPath
			if tc.expectDebugArchive {
				expectedArchivePath += debugArchiveExtension
			}
			_, err = os.Stat(expectedArchivePath)
			require.NoError(t, err, "expected archive file to be created")

		})
	}
}

func createFakeCRD() *apiextensionsv1.CustomResourceDefinition {
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "serviceintentions.consul.hashicorp.com",
		},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "consul.hashicorp.com",
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{Name: "v1alpha1", Served: true, Storage: true},
			},
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural:   "serviceintentions",
				Singular: "serviceintention",
				Kind:     "ServiceIntention",
			},
		},
	}
}
func createFakeCR(name, namespace string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "consul.hashicorp.com/v1alpha1",
			"kind":       "ServiceIntention",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": namespace,
			},
		},
	}
}

// Helper to convert a slice of concrete pods to a slice of runtime.Object
func convertPodsToRuntimeObjects(pods []corev1.Pod) []runtime.Object {
	objects := make([]runtime.Object, len(pods))
	for i := range pods {
		objects[i] = &pods[i]
	}
	return objects
}

func createTestResource() (k8sObjects, crObjects, crdObjects []runtime.Object, serviceIntentionsGVR schema.GroupVersionResource) {
	crd := createFakeCRD()
	cr := createFakeCR("my-cr-1", "consul")
	serviceIntentionsGVR = schema.GroupVersionResource{
		Group: "consul.hashicorp.com", Version: "v1alpha1", Resource: "serviceintentions",
	}

	sidecarPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "sidecar-pod", Namespace: "default",
			Labels: map[string]string{"consul.hashicorp.com/connect-inject-status": "injected"},
		},
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{{Name: "init-container", Image: "busybox:1.28"}},
			Containers: []corev1.Container{
				{Name: "control-dataplane", Image: "nginx:1.21.6"},
				{Name: "app-container", Image: "nginx:1.21.6"},
			},
		},
	}

	consulServerLabels := map[string]string{"app": "consul", "component": "server"}
	consulStatefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "consul-server", Namespace: "consul",
		},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: consulServerLabels,
			},
		},
	}
	consulServerPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "consul-server-0", Namespace: "consul",
			Labels: consulServerLabels,
		},
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{{Name: "init-container", Image: "busybox:1.28"}},
			Containers:     []corev1.Container{{Name: "consul", Image: "nginx:1.21.6"}},
		},
	}

	k8sObjects = []runtime.Object{sidecarPod, consulStatefulSet, consulServerPod}
	k8sObjects = append(k8sObjects, convertPodsToRuntimeObjects(pods)...)
	crdObjects = []runtime.Object{crd}
	crObjects = []runtime.Object{cr}
	return k8sObjects, crObjects, crdObjects, serviceIntentionsGVR
}

func initializeDebugCommands(buf io.Writer) *DebugCommand {
	// Log at a test level to standard out.
	log := hclog.New(&hclog.LoggerOptions{
		Name:   "test",
		Level:  hclog.Debug,
		Output: os.Stdout,
	})
	cleanupReqAndCompleted := make(chan bool, 1)
	// Setup and initialize the command struct
	command := &DebugCommand{
		BaseCommand: &common.BaseCommand{
			Log:                    log,
			UI:                     terminal.NewUI(context.Background(), buf),
			CleanupReqAndCompleted: cleanupReqAndCompleted,
			Ctx:                    context.Background(),
		},
	}
	command.init()
	return command
}
