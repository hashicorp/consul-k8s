package subcommand

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/require"
)

func TestRun_FlagValidation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		Flags  []string
		ExpErr string
	}{
		{
			Flags:  []string{""},
			ExpErr: "-service-config must be set",
		},
		{
			Flags: []string{
				"-service-config=/config.hcl",
				"-consul-location=",
			},
			ExpErr: "-consul-location must be set",
		},
		{
			Flags: []string{
				"-service-config=/config.hcl",
				"-consul-location=/consul",
				"-sync-period=notparseable",
			},
			ExpErr: "-sync-period is invalid: time: invalid duration notparseable",
		},
	}

	for _, c := range cases {
		t.Run(c.ExpErr, func(t *testing.T) {
			ui := cli.NewMockUi()
			cmd := Command{
				UI: ui,
			}
			responseCode := cmd.Run(c.Flags)
			require.Equal(t, 1, responseCode, ui.ErrorWriter.String())
			require.Contains(t, ui.ErrorWriter.String(), c.ExpErr)
		})
	}
}

func TestRun_ServiceConfigFileMissing(t *testing.T) {
	t.Parallel()
	ui := cli.NewMockUi()
	cmd := Command{
		UI: ui,
	}
	responseCode := cmd.Run([]string{"-service-config=/does/not/exist", "-consul-location=/not/a/valid/path"})
	require.Equal(t, 1, responseCode, ui.ErrorWriter.String())
	require.Contains(t, ui.ErrorWriter.String(), "-service-config file \"/does/not/exist\" not found")
}

func TestRun_ConsulLocationMissing(t *testing.T) {
	t.Parallel()
	ui := cli.NewMockUi()
	cmd := Command{
		UI: ui,
	}

	// Create a temporary service registration file
	tmpDir, err := ioutil.TempDir("", "")
	require.NoError(t, err)
	defer func() { os.RemoveAll(tmpDir) }()

	configFile := filepath.Join(tmpDir, "svc.hcl")
	err = ioutil.WriteFile(configFile, []byte(servicesRegistration), 0600)
	require.NoError(t, err)

	configFlag := "-service-config=" + configFile

	responseCode := cmd.Run([]string{configFlag, "-consul-location=/not/a/valid/path"})
	require.Equal(t, 1, responseCode, ui.ErrorWriter.String())
	require.Contains(t, ui.ErrorWriter.String(), "-consul-location binary \"/not/a/valid/path\" not found")
}

const servicesRegistration = `
services {
	id   = "service-id"
	name = "service"
	port = 80
}
services {
	id   = "service-id-sidecar-proxy"
	name = "service-sidecar-proxy"
	port = 2000
	kind = "connect-proxy"
	proxy {
	  destination_service_name = "service"
	  destination_service_id = "service-id"
	  local_service_port = 80
	}
}`
