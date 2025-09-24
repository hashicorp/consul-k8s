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
	"testing"

	"github.com/hashicorp/consul-k8s/cli/common"
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
	helmRelease "helm.sh/helm/v3/pkg/release"
	helmTime "helm.sh/helm/v3/pkg/time"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
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

func initializeDebugCommands(buf io.Writer) *DebugCommand {
	// Log at a test level to standard out.
	log := hclog.New(&hclog.LoggerOptions{
		Name:   "test",
		Level:  hclog.Debug,
		Output: os.Stdout,
	})
	cleanupReq := make(chan bool, 1)
	cleanupConfirmation := make(chan int, 1)
	// Setup and initialize the command struct
	command := &DebugCommand{
		BaseCommand: &common.BaseCommand{
			Log:                 log,
			UI:                  terminal.NewUI(context.Background(), buf),
			CleanupReq:          cleanupReq,
			CleanupConfirmation: cleanupConfirmation,
		},
	}
	command.init()
	return command
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

			err := c.captureConsulInjectedSidecarPods()

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

			fmt.Println(string(content))
			fmt.Println(podsData)

			// Assert that the specific data exists in the map.
			podInfo, ok := podsData[tc.expectedPodName]
			require.True(t, ok, "expected pod not found in JSON output")
			require.Equal(t, tc.expectedReadyVal, podInfo["ready"])
		})
	}
}
