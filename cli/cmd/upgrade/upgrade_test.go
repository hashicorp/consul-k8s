package upgrade

import (
	"flag"
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/consul-k8s/cli/common"
	cmnFlag "github.com/hashicorp/consul-k8s/cli/common/flag"
	"github.com/hashicorp/consul-k8s/cli/preset"
	"github.com/hashicorp/go-hclog"
	"github.com/posener/complete"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidateFlags tests the validate flags function.
func TestValidateFlags(t *testing.T) {
	// The following cases should all error, if they fail to this test fails.
	testCases := []struct {
		description string
		input       []string
	}{
		{
			"Should disallow non-flag arguments.",
			[]string{"foo", "-auto-approve"},
		},
		{
			"Should disallow specifying both values file AND presets.",
			[]string{"-f='f.txt'", "-preset=demo"},
		},
		{
			"Should error on invalid presets.",
			[]string{"-preset=foo"},
		},
		{
			"Should error on invalid timeout.",
			[]string{"-timeout=invalid-timeout"},
		},
		{
			"Should have errored on a non-existant file.",
			[]string{"-f=\"does_not_exist.txt\""},
		},
	}

	for _, testCase := range testCases {
		c := getInitializedCommand(t)
		t.Run(testCase.description, func(t *testing.T) {
			if err := c.validateFlags(testCase.input); err == nil {
				t.Errorf("Test case should have failed.")
			}
		})
	}
}

// getInitializedCommand sets up a command struct for tests.
func getInitializedCommand(t *testing.T) *Command {
	t.Helper()
	log := hclog.New(&hclog.LoggerOptions{
		Name:   "cli",
		Level:  hclog.Info,
		Output: os.Stdout,
	})

	baseCommand := &common.BaseCommand{
		Log: log,
	}

	c := &Command{
		BaseCommand: baseCommand,
	}
	c.init()
	return c
}

func TestTaskCreateCommand_AutocompleteFlags(t *testing.T) {
	t.Parallel()
	cmd := getInitializedCommand(t)

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

func TestTaskCreateCommand_AutocompleteArgs(t *testing.T) {
	cmd := getInitializedCommand(t)
	c := cmd.AutocompleteArgs()
	assert.Equal(t, complete.PredictNothing, c)
}

func TestGetPreset(t *testing.T) {
	testCases := []struct {
		description string
		presetName  string
	}{
		{
			"'cloud' should return a CloudPreset'.",
			preset.PresetCloud,
		},
		{
			"'quickstart' should return a QuickstartPreset'.",
			preset.PresetQuickstart,
		},
		{
			"'secure' should return a SecurePreset'.",
			preset.PresetSecure,
		},
	}

	for _, tc := range testCases {
		c := getInitializedCommand(t)
		t.Run(tc.description, func(t *testing.T) {
			p, err := c.getPreset(tc.presetName, "consul")
			require.NoError(t, err)
			switch p.(type) {
			case *preset.CloudPreset:
				require.Equal(t, preset.PresetCloud, tc.presetName)
			case *preset.QuickstartPreset:
				require.Equal(t, preset.PresetQuickstart, tc.presetName)
			case *preset.SecurePreset:
				require.Equal(t, preset.PresetSecure, tc.presetName)
			}
		})
	}
}

// TestValidateCloudPresets tests the validate flags function when passed the cloud preset.
func TestValidateCloudPresets(t *testing.T) {
	testCases := []struct {
		description        string
		input              []string
		preProcessingFunc  func()
		postProcessingFunc func()
		expectError        bool
	}{
		{
			"Should not error on cloud preset when HCP_CLIENT_ID and HCP_CLIENT_SECRET envvars are present and hcp-resource-id parameter is provided.",
			[]string{"-preset=cloud", "-hcp-resource-id=foobar"},
			func() {
				os.Setenv("HCP_CLIENT_ID", "foo")
				os.Setenv("HCP_CLIENT_SECRET", "bar")
			},
			func() {
				os.Setenv("HCP_CLIENT_ID", "")
				os.Setenv("HCP_CLIENT_SECRET", "")
			},
			false,
		},
		{
			"Should error on cloud preset when HCP_CLIENT_ID is not provided.",
			[]string{"-preset=cloud", "-hcp-resource-id=foobar"},
			func() {
				os.Setenv("HCP_CLIENT_ID", "")
				os.Setenv("HCP_CLIENT_SECRET", "bar")
			},
			func() {
				os.Setenv("HCP_CLIENT_ID", "")
				os.Setenv("HCP_CLIENT_SECRET", "")
			},
			true,
		},
		{
			"Should error on cloud preset when HCP_CLIENT_SECRET is not provided.",
			[]string{"-preset=cloud", "-hcp-resource-id=foobar"},
			func() {
				os.Setenv("HCP_CLIENT_ID", "foo")
				os.Setenv("HCP_CLIENT_SECRET", "")
			},
			func() {
				os.Setenv("HCP_CLIENT_ID", "")
				os.Setenv("HCP_CLIENT_SECRET", "")
			},
			true,
		},
		{
			"Should error on cloud preset when -hcp-resource-id flag is not provided.",
			[]string{"-preset=cloud"},
			func() {
				os.Setenv("HCP_CLIENT_ID", "foo")
				os.Setenv("HCP_CLIENT_SECRET", "bar")
			},
			func() {
				os.Setenv("HCP_CLIENT_ID", "")
				os.Setenv("HCP_CLIENT_SECRET", "")
			},
			true,
		},
		{
			"Should error when -hcp-resource-id flag is provided but cloud preset is not specified.",
			[]string{"-hcp-resource-id=foobar"},
			func() {
				os.Setenv("HCP_CLIENT_ID", "foo")
				os.Setenv("HCP_CLIENT_SECRET", "bar")
			},
			func() {
				os.Setenv("HCP_CLIENT_ID", "")
				os.Setenv("HCP_CLIENT_SECRET", "")
			},
			true,
		},
	}

	for _, testCase := range testCases {
		testCase.preProcessingFunc()
		c := getInitializedCommand(t)
		t.Run(testCase.description, func(t *testing.T) {
			err := c.validateFlags(testCase.input)
			if testCase.expectError && err == nil {
				t.Errorf("Test case should have failed.")
			} else if !testCase.expectError && err != nil {
				t.Errorf("Test case should not have failed.")
			}
		})
		testCase.postProcessingFunc()
	}
}
