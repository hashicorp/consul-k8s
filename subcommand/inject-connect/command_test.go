package subcommand

import (
	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestRun_FlagValidation(t *testing.T) {
	cases := []struct {
		Flags  []string
		ExpErr string
	}{
		{
			Flags:  []string{},
			ExpErr: "-consul-k8s-image must be set",
		},
	}

	for _, c := range cases {
		t.Run(c.ExpErr, func(t *testing.T) {
			ui := cli.NewMockUi()
			cmd := Command{
				UI: ui,
			}
			code := cmd.Run([]string{})
			require.Equal(t, 1, code)
			require.Contains(t, ui.ErrorWriter.String(), c.ExpErr)
		})
	}
}
