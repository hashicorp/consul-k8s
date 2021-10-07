package gossipencryptionautogenerate

import (
	"testing"

	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/require"
)

func TestRun_FlagFailures(t *testing.T) {
	t.Parallel()
	cases := []struct {
		flags  []string
		expErr string
	}{
		{
			flags:  []string{},
			expErr: "-namespace must be set",
		},
		{
			flags:  []string{"-namespace", "default"},
			expErr: "-secret-name must be set",
		},
		{
			flags:  []string{"-namespace", "default", "-secret-name", "my-secret", "-log-level", "oak"},
			expErr: "unknown log level",
		},
	}

	for _, c := range cases {
		t.Run(c.expErr, func(t *testing.T) {
			ui := cli.NewMockUi()
			cmd := Command{
				UI: ui,
			}
			code := cmd.Run(c.flags)
			require.Equal(t, 1, code)
			require.Contains(t, ui.ErrorWriter.String(), c.expErr)
		})
	}
}
