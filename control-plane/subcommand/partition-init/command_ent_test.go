//go:build enterprise
// +build enterprise

package partition_init

import (
	"strings"
	"testing"

	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/require"
)

func TestRun_FlagValidation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		flags  []string
		expErr string
	}{
		{
			flags:  nil,
			expErr: "-server-address must be set at least once",
		},
		{
			flags:  []string{"-server-address", "foo"},
			expErr: "-partition-name must be set",
		},
		{
			flags:  []string{"-server-address", "foo", "-partition-name", "bar", "-log-level", "invalid"},
			expErr: "unknown log level: invalid",
		},
	}

	for _, c := range cases {
		t.Run(c.expErr, func(tt *testing.T) {
			ui := cli.NewMockUi()
			cmd := Command{UI: ui}
			exitCode := cmd.Run(c.flags)
			require.Equal(tt, 1, exitCode, ui.ErrorWriter.String())
			require.Contains(tt, ui.ErrorWriter.String(), c.expErr)
		})
	}
}

func TestRun_PartitionCreate(t *testing.T) {
	partitionName := "test-partition"

	a, err := testutil.NewTestServerConfigT(t, nil)
	require.NoError(t, err)
	a.WaitForLeader(t)
	defer func() {
		err := a.Stop()
		require.NoError(t, err)
	}()

	ui := cli.NewMockUi()
	cmd := Command{
		UI: ui,
	}
	cmd.init()
	args := []string{
		"-server-address=" + strings.Split(a.HTTPAddr, ":")[0],
		"-server-port=" + strings.Split(a.HTTPAddr, ":")[1],
		"-partition-name", partitionName,
	}

	responseCode := cmd.Run(args)

	require.Equal(t, 0, responseCode)
}

// TODO: Write tests with ACLs enabled
