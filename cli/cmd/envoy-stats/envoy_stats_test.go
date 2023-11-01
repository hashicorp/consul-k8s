package envoy_stats

import (
	"bytes"
	"context"
	"github.com/hashicorp/consul-k8s/cli/common"
	"github.com/hashicorp/consul-k8s/cli/common/terminal"
	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/require"
	"io"
	"k8s.io/client-go/kubernetes/fake"
	"os"
	"testing"
)

func TestFlagParsing(t *testing.T) {
	cases := map[string]struct {
		args []string
		out  int
	}{
		"No args, should fail": {
			args: []string{},
			out:  1,
		},
		"Nonexistent flag passed, -foo bar, should fail": {
			args: []string{"-foo", "bar"},
			out:  1,
		},
		"Invalid argument passed, -namespace notaname, should fail": {
			args: []string{"-namespace", "notaname"},
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
func setupCommand(buf io.Writer) *Command {
	// Log at a test level to standard out.
	log := hclog.New(&hclog.LoggerOptions{
		Name:   "test",
		Level:  hclog.Debug,
		Output: os.Stdout,
	})

	// Setup and initialize the command struct
	command := &Command{
		BaseCommand: &common.BaseCommand{
			Log: log,
			UI:  terminal.NewUI(context.Background(), buf),
		},
	}
	command.init()

	return command
}
