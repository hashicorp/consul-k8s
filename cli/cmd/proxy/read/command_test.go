package read

import (
	"os"
	"testing"

	"github.com/hashicorp/consul-k8s/cli/common"
	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"
)

func TestArgumentParsing(t *testing.T) {
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
			c := getInitializedCommand(t)
			c.kubernetes = fake.NewSimpleClientset()
			out := c.Run(tc.args)
			require.Equal(t, tc.out, out)
		})
	}
}

// getInitializedCommand sets up a command struct for tests.
func getInitializedCommand(t *testing.T) *ReadCommand {
	t.Helper()
	log := hclog.New(&hclog.LoggerOptions{
		Name:   "test",
		Level:  hclog.Debug,
		Output: os.Stdout,
	})

	baseCommand := &common.BaseCommand{
		Log: log,
	}

	c := &ReadCommand{
		BaseCommand: baseCommand,
	}
	c.init()
	return c
}
