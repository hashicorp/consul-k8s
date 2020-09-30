package controller

import (
	"testing"

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
			expErr: "-webhook-tls-cert-dir must be set",
		},
		{
			flags:  []string{"-datacenter-name", "foo"},
			expErr: "-webhook-tls-cert-dir must be set",
		},
		{
			flags:  []string{"-webhook-tls-cert-dir", "/foo"},
			expErr: "-datacenter-name must be set",
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
