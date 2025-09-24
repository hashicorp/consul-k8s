package debug

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/hashicorp/consul-k8s/cli/common"
	cmnFlag "github.com/hashicorp/consul-k8s/cli/common/flag"
	"github.com/hashicorp/consul-k8s/cli/common/terminal"
	"github.com/hashicorp/go-hclog"
	"github.com/posener/complete"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
