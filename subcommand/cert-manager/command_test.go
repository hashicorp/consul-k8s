package certmanager

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
			expErr: "secret-name must be set",
		},
		{
			flags:  []string{"-secret-name=tls-secret", "-tls-cert-file=/tmp/tls/cert.pem"},
			expErr: "both tls-cert-file and tls-key-file must be provided",
		},
		{
			flags:  []string{"-secret-name=tls-secret", "-tls-key-file=/tmp/tls/key.pem"},
			expErr: "both tls-cert-file and tls-key-file must be provided",
		},
		{
			flags:  []string{"-secret-name=tls-secret", "-secret-namespace=consul-ns"},
			expErr: "either webhook-name or tls-cert-file and tls-key-file must be provided",
		},
	}

	for _, c := range cases {
		t.Run(c.expErr, func(tt *testing.T) {
			ui := cli.NewMockUi()
			cmd := Command{
				UI: ui,
			}
			exitCode := cmd.Run(c.flags)
			require.Equal(tt, 1, exitCode, ui.ErrorWriter.String())
			require.Contains(tt, ui.ErrorWriter.String(), c.expErr)
		})
	}
}
