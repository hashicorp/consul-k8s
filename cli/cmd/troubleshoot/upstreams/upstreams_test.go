package upstreams

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
	"k8s.io/client-go/kubernetes/fake"
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

func TestFormatIPs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		actual   []string
		expected string
	}{
		{
			name:     "single IPs",
			actual:   []string{"1.1.1.1"},
			expected: "1.1.1.1",
		},

		{
			name:     "several IPs",
			actual:   []string{"1.1.1.1", "2.2.2.2"},
			expected: "1.1.1.1, 2.2.2.2",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := formatIPs(c.actual)
			if c.expected != got {
				t.Errorf("expected %v, got %v", c.expected, got)
			}
		})
	}
}

func TestFormatClusterNames(t *testing.T) {
	cases := []struct {
		name     string
		actual   map[string]struct{}
		expected string
	}{
		{
			name: "single cluster",
			actual: map[string]struct{}{
				"cluster1": {},
			},
			expected: "cluster1",
		},
		{
			name: "several clusters",
			actual: map[string]struct{}{
				"cluster1": {},
				"cluster2": {},
				"cluster3": {},
			},
			expected: "cluster1, cluster2, cluster3",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := formatClusterNames(c.actual)
			if c.expected != got {
				t.Errorf("expected %v, got %v", c.expected, got)
			}
		})
	}
}

func setupCommand(buf io.Writer) *UpstreamsCommand {
	// Log at a test level to standard out.
	log := hclog.New(&hclog.LoggerOptions{
		Name:   "test",
		Level:  hclog.Debug,
		Output: os.Stdout,
	})

	// Setup and initialize the command struct
	command := &UpstreamsCommand{
		BaseCommand: &common.BaseCommand{
			Log: log,
			UI:  terminal.NewUI(context.Background(), buf),
		},
	}
	command.init()

	return command
}
