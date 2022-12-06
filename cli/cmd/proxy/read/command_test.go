package read

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/hashicorp/consul-k8s/cli/common"
	cmnFlag "github.com/hashicorp/consul-k8s/cli/common/flag"
	"github.com/hashicorp/consul-k8s/cli/common/terminal"
	"github.com/hashicorp/go-hclog"
	"github.com/posener/complete"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestFlagParsing(t *testing.T) {
	cases := map[string]struct {
		args []string
		out  int
	}{
		"No args": {
			args: []string{},
			out:  1,
		},
		"Multiple podnames passed": {
			args: []string{"podname", "podname2"},
			out:  1,
		},
		"Nonexistent flag passed, -foo bar": {
			args: []string{"podName", "-foo", "bar"},
			out:  1,
		},
		"Invalid argument passed, -namespace YOLO": {
			args: []string{"podName", "-namespace", "YOLO"},
			out:  1,
		},
		"User passed incorrect output": {
			args: []string{"podName", "-output", "image"},
			out:  1,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c := setupCommand(new(bytes.Buffer))
			c.kubernetes = fake.NewSimpleClientset()

			out := c.Run(tc.args)
			require.Equal(t, tc.out, out)
		})
	}
}

func TestReadCommandOutput(t *testing.T) {
	podName := "fakePod"

	// These regular expressions must be present in the output.
	expectedHeader := fmt.Sprintf("Envoy configuration for %s in namespace default:", podName)
	expected := map[string][]string{
		"-clusters": {"==> Clusters \\(5\\)",
			"Name.*FQDN.*Endpoints.*Type.*Last Updated",
			"local_agent.*192\\.168\\.79\\.187:8502.*STATIC.*2022-05-13T04:22:39\\.553Z",
			"local_app.*127\\.0\\.0\\.1:8080.*STATIC.*2022-05-13T04:22:39\\.655Z",
			"client.*client\\.default\\.dc1\\.internal\\.bc3815c2-1a0f-f3ff-a2e9-20d791f08d00\\.consul.*EDS",
			"frontend.*frontend\\.default\\.dc1\\.internal\\.bc3815c2-1a0f-f3ff-a2e9-20d791f08d00\\.consul",
			"original-destination.*ORIGINAL_DST"},

		"-endpoints": {"==> Endpoints \\(6\\)",
			"Address:Port.*Cluster.*Weight.*Status",
			"192.168.79.187:8502.*local_agent.*1.00.*HEALTHY",
			"127.0.0.1:8080.*local_app.*1.00.*HEALTHY",
			"192.168.18.110:20000.*client.*1.00.*HEALTHY",
			"192.168.52.101:20000.*client.*1.00.*HEALTHY",
			"192.168.65.131:20000.*client.*1.00.*HEALTHY",
			"192.168.63.120:20000.*frontend.*1.00.*HEALTHY"},

		"-listeners": {"==> Listeners \\(2\\)",
			"Name.*Address:Port.*Direction.*Filter Chain Match.*Filters.*Last Updated",
			"public_listener.*192\\.168\\.69\\.179:20000.*INBOUND.*Any.*\\* -> local_app/",
			"outbound_listener.*127.0.0.1:15001.*OUTBOUND.*10\\.100\\.134\\.173/32, 240\\.0\\.0\\.3/32.*TCP: -> client",
			"10\\.100\\.31\\.2/32, 240\\.0\\.0\\.5/32.*TCP: -> frontend",
			"Any.*TCP: -> original-destination"},

		"-routes": {"==> Routes \\(1\\)",
			"Name.*Destination Cluster.*Last Updated",
			"public_listener.*local_app/"},

		"-secrets": {"==> Secrets \\(2\\)",
			"Name.*Type.*Last Updated",
			"default.*Dynamic Active",
			"ROOTCA.*Dynamic Warming"},
	}

	cases := map[string][]string{
		"No filters":             {},
		"Clusters":               {"-clusters"},
		"Endpoints":              {"-endpoints"},
		"Listeners":              {"-listeners"},
		"Routes":                 {"-routes"},
		"Secrets":                {"-secrets"},
		"Clusters and routes":    {"-clusters", "-routes"},
		"Secrets then listeners": {"-secrets", "-listeners"},
	}

	fakePod := v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: "default",
		},
	}

	buf := new(bytes.Buffer)
	c := setupCommand(buf)
	c.kubernetes = fake.NewSimpleClientset(&v1.PodList{Items: []v1.Pod{fakePod}})

	// A fetchConfig function that just returns the test Envoy config.
	c.fetchConfig = func(context.Context, common.PortForwarder) (*EnvoyConfig, error) {
		return testEnvoyConfig, nil
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			args := append([]string{podName}, tc...)
			out := c.Run(args)
			require.Equal(t, 0, out)

			actual := buf.String()

			require.Regexp(t, expectedHeader, actual)
			for _, table := range tc {
				for _, expression := range expected[table] {
					require.Regexp(t, expression, actual)
				}
			}
		})
	}
}

// TestFilterWarnings ensures that a warning is printed if the user applies a
// field filter (e.g. -fqdn default) and a table filter (e.g. -secrets) where
// the former does not affect the output of the latter.
func TestFilterWarnings(t *testing.T) {
	podName := "fakePod"
	cases := map[string]struct {
		input    []string
		warnings []string
	}{
		"fully qualified domain name doesn't apply to listeners": {
			input:    []string{"-fqdn", "default", "-listeners"},
			warnings: []string{"The filter `-fqdn default` does not apply to the tables displayed."},
		},
		"fully qualified domain name doesn't apply to routes": {
			input:    []string{"-fqdn", "default", "-routes"},
			warnings: []string{"The filter `-fqdn default` does not apply to the tables displayed."},
		},
		"fully qualified domain name doesn't apply to endpoints": {
			input:    []string{"-fqdn", "default", "-endpoints"},
			warnings: []string{"The filter `-fqdn default` does not apply to the tables displayed."},
		},
		"fully qualified domain name doesn't apply to secrets": {
			input:    []string{"-fqdn", "default", "-secrets"},
			warnings: []string{"The filter `-fqdn default` does not apply to the tables displayed."},
		},
		"fully qualified domain name doesn't apply to endpoints or listeners": {
			input:    []string{"-fqdn", "default", "-endpoints", "-listeners"},
			warnings: []string{"The filter `-fqdn default` does not apply to the tables displayed."},
		},
		"fully qualified domain name doesn't apply to listeners, routes, endpoints, or secrets": {
			input:    []string{"-fqdn", "default", "-listeners", "-routes", "-endpoints", "-secrets"},
			warnings: []string{"The filter `-fqdn default` does not apply to the tables displayed."},
		},
		"port doesn't apply to routes": {
			input:    []string{"-port", "8080", "-routes"},
			warnings: []string{"The filter `-port 8080` does not apply to the tables displayed."},
		},
		"port doesn't apply to secrets": {
			input:    []string{"-port", "8080", "-secrets"},
			warnings: []string{"The filter `-port 8080` does not apply to the tables displayed."},
		},
		"port doesn't apply to secrets or routes": {
			input:    []string{"-port", "8080", "-secrets", "-routes"},
			warnings: []string{"The filter `-port 8080` does not apply to the tables displayed."},
		},
		"address does not apply to routes": {
			input:    []string{"-address", "127.0.0.1", "-routes"},
			warnings: []string{"The filter `-address 127.0.0.1` does not apply to the tables displayed."},
		},
		"address does not apply to secrets": {
			input:    []string{"-address", "127.0.0.1", "-secrets"},
			warnings: []string{"The filter `-address 127.0.0.1` does not apply to the tables displayed."},
		},
		"warn address and port": {
			input: []string{"-address", "127.0.0.1", "-port", "8080", "-secrets"},
			warnings: []string{
				"The filter `-address 127.0.0.1` does not apply to the tables displayed.",
				"The filter `-port 8080` does not apply to the tables displayed.",
			},
		},
		"warn fqdn, address, and port": {
			input: []string{"-fqdn", "default", "-address", "127.0.0.1", "-port", "8080", "-secrets"},
			warnings: []string{
				"The filter `-fqdn default` does not apply to the tables displayed.",
				"The filter `-address 127.0.0.1` does not apply to the tables displayed.",
				"The filter `-port 8080` does not apply to the tables displayed.",
			},
		},
		"no warning produced (happy case)": {
			input:    []string{"-fqdn", "default", "-clusters"},
			warnings: []string{},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			fakePod := v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      podName,
					Namespace: "default",
				},
			}

			buf := new(bytes.Buffer)
			c := setupCommand(buf)
			c.kubernetes = fake.NewSimpleClientset(&v1.PodList{Items: []v1.Pod{fakePod}})
			c.fetchConfig = func(context.Context, common.PortForwarder) (*EnvoyConfig, error) {
				return testEnvoyConfig, nil
			}

			exitCode := c.Run(append([]string{podName}, tc.input...))
			require.Equal(t, 0, exitCode) // This shouldn't error out, just warn the user.

			for _, warning := range tc.warnings {
				require.Contains(t, buf.String(), warning)
			}
		})
	}
}

func setupCommand(buf io.Writer) *ReadCommand {
	// Log at a test level to standard out.
	log := hclog.New(&hclog.LoggerOptions{
		Name:   "test",
		Level:  hclog.Debug,
		Output: os.Stdout,
	})

	// Setup and initialize the command struct
	command := &ReadCommand{
		BaseCommand: &common.BaseCommand{
			Log: log,
			UI:  terminal.NewUI(context.Background(), buf),
		},
	}
	command.init()

	return command
}

func TestTaskCreateCommand_AutocompleteFlags(t *testing.T) {
	t.Parallel()
	buf := new(bytes.Buffer)
	cmd := setupCommand(buf)

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
	buf := new(bytes.Buffer)
	cmd := setupCommand(buf)
	c := cmd.AutocompleteArgs()
	assert.Equal(t, complete.PredictNothing, c)
}
