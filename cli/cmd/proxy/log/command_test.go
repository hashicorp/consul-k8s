package log

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"

	"github.com/hashicorp/consul-k8s/cli/common"
	"github.com/hashicorp/consul-k8s/cli/common/terminal"
	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/require"
)

func TestFlagParsing(t *testing.T) {
	testCases := map[string]struct {
		args []string
		out  int
	}{
		"No args": {
			args: []string{},
			out:  1,
		},
		"With pod name": {
			args: []string{"now-this-is-pod-racing"},
			out:  0,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			c := setupCommand(bytes.NewBuffer([]byte{}))
			out := c.Run(tc.args)
			require.Equal(t, tc.out, out)
		})
	}
}

func setupCommand(buf io.Writer) *LogCommand {
	log := hclog.New(&hclog.LoggerOptions{
		Name:   "test",
		Level:  hclog.Debug,
		Output: os.Stdout,
	})

	command := &LogCommand{
		BaseCommand: &common.BaseCommand{
			Log: log,
			UI:  terminal.NewUI(context.Background(), buf),
		},
	}
	command.init()
	return command
}
