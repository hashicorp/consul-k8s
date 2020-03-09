package subcommand

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/freeport"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/require"
)

func TestRun_Defaults(t *testing.T) {
	t.Parallel()
	var cmd Command
	cmd.init()
	require.Equal(t, 10*time.Second, cmd.flagSyncPeriod)
	require.Equal(t, "info", cmd.flagLogLevel)
	require.Equal(t, "consul", cmd.flagConsulBinary)
}

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
				"-consul-binary=",
			},
			ExpErr: "-consul-binary must be set",
		},
		{
			Flags: []string{
				"-service-config=/config.hcl",
				"-consul-binary=consul",
				"-sync-period=0s",
			},
			ExpErr: "-sync-period must be greater than 0",
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

func TestRun_FlagValidation_ServiceConfigFileMissing(t *testing.T) {
	t.Parallel()
	ui := cli.NewMockUi()
	cmd := Command{
		UI: ui,
	}
	responseCode := cmd.Run([]string{"-service-config=/does/not/exist", "-consul-binary=/not/a/valid/path"})
	require.Equal(t, 1, responseCode, ui.ErrorWriter.String())
	require.Contains(t, ui.ErrorWriter.String(), "-service-config file \"/does/not/exist\" not found")
}

func TestRun_FlagValidation_ConsulBinaryMissing(t *testing.T) {
	t.Parallel()

	ui := cli.NewMockUi()
	cmd := Command{
		UI: ui,
	}

	tmpDir, configFile := createServicesTmpFile(t, servicesRegistration)
	defer os.RemoveAll(tmpDir)

	configFlag := "-service-config=" + configFile

	responseCode := cmd.Run([]string{configFlag, "-consul-binary=/not/a/valid/path"})
	require.Equal(t, 1, responseCode, ui.ErrorWriter.String())
	require.Contains(t, ui.ErrorWriter.String(), "-consul-binary \"/not/a/valid/path\" not found")
}

func TestRun_FlagValidation_InvalidLogLevel(t *testing.T) {
	t.Parallel()

	tmpDir, configFile := createServicesTmpFile(t, servicesRegistration)
	defer os.RemoveAll(tmpDir)

	ui := cli.NewMockUi()
	cmd := Command{
		UI: ui,
	}
	responseCode := cmd.Run([]string{"-service-config", configFile, "-consul-binary=consul", "-log-level=foo"})
	require.Equal(t, 1, responseCode, ui.ErrorWriter.String())
	require.Contains(t, ui.ErrorWriter.String(), "unknown log level: foo")
}

// Test that we register the services.
func TestRun_ServicesRegistration(t *testing.T) {
	t.Parallel()

	tmpDir, configFile := createServicesTmpFile(t, servicesRegistration)
	defer os.RemoveAll(tmpDir)

	a, err := testutil.NewTestServerT(t)
	require.NoError(t, err)
	defer a.Stop()

	ui := cli.NewMockUi()
	cmd := Command{
		UI: ui,
	}

	// Run async because we need to kill it when the test is over.
	exitChan := runCommandAsynchronously(&cmd, []string{
		"-http-addr", a.HTTPAddr,
		"-service-config", configFile,
		"-sync-period", "100ms",
	})
	defer stopCommand(t, &cmd, exitChan)

	client, err := api.NewClient(&api.Config{
		Address: a.HTTPAddr,
	})
	require.NoError(t, err)

	timer := &retry.Timer{Timeout: 1 * time.Second, Wait: 100 * time.Millisecond}
	retry.RunWith(timer, t, func(r *retry.R) {
		svc, _, err := client.Agent().Service("service-id", nil)
		require.NoError(r, err)
		require.Equal(r, 80, svc.Port)

		svcProxy, _, err := client.Agent().Service("service-id-sidecar-proxy", nil)
		require.NoError(r, err)
		require.Equal(r, 2000, svcProxy.Port)
	})
}

// Test that we register services when the Consul agent is down at first.
func TestRun_ServicesRegistration_ConsulDown(t *testing.T) {
	t.Parallel()

	tmpDir, configFile := createServicesTmpFile(t, servicesRegistration)
	defer os.RemoveAll(tmpDir)

	ui := cli.NewMockUi()
	cmd := Command{
		UI: ui,
	}
	randomPort := freeport.MustTake(1)[0]
	// Run async because we need to kill it when the test is over.
	exitChan := runCommandAsynchronously(&cmd, []string{
		"-http-addr", fmt.Sprintf("127.0.0.1:%d", randomPort),
		"-service-config", configFile,
		"-sync-period", "100ms",
	})
	defer stopCommand(t, &cmd, exitChan)

	// Start the Consul agent after 500ms.
	time.Sleep(500 * time.Millisecond)
	a, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
		c.Ports = &testutil.TestPortConfig{
			HTTP: randomPort,
		}
	})
	require.NoError(t, err)
	defer a.Stop()

	client, err := api.NewClient(&api.Config{
		Address: a.HTTPAddr,
	})
	require.NoError(t, err)

	// The services should be registered when the Consul agent comes up
	// within 500ms.
	timer := &retry.Timer{Timeout: 500 * time.Millisecond, Wait: 100 * time.Millisecond}
	retry.RunWith(timer, t, func(r *retry.R) {
		svc, _, err := client.Agent().Service("service-id", nil)
		require.NoError(r, err)
		require.Equal(r, 80, svc.Port)

		svcProxy, _, err := client.Agent().Service("service-id-sidecar-proxy", nil)
		require.NoError(r, err)
		require.Equal(r, 2000, svcProxy.Port)
	})
}

// Test that we parse all flags and pass them down to the underlying Consul command.
func TestRun_ConsulCommandFlags(t *testing.T) {
	t.Parallel()
	tmpDir, configFile := createServicesTmpFile(t, servicesRegistration)
	defer os.RemoveAll(tmpDir)

	a, err := testutil.NewTestServerT(t)
	require.NoError(t, err)
	defer a.Stop()

	ui := cli.NewMockUi()
	cmd := Command{
		UI: ui,
	}

	// Run async because we need to kill it when the test is over.
	exitChan := runCommandAsynchronously(&cmd, []string{
		"-http-addr", a.HTTPAddr,
		"-service-config", configFile,
		"-sync-period", "1s",
		"-consul-binary", "consul",
		"-token=abc",
		"-token-file=/token/file",
		"-ca-file=/ca/file",
		"-ca-path=/ca/path",
		"-client-cert=/client/cert",
		"-client-key=/client/key",
		"-tls-server-name=consul.foo.com",
	})
	defer stopCommand(t, &cmd, exitChan)

	expectedCommand := []string{
		"services",
		"register",
		"-http-addr=" + a.HTTPAddr,
		"-token=abc",
		"-token-file=/token/file",
		"-ca-file=/ca/file",
		"-ca-path=/ca/path",
		"-client-cert=/client/cert",
		"-client-key=/client/key",
		"-tls-server-name=consul.foo.com",
		configFile,
	}
	timer := &retry.Timer{Timeout: 1000 * time.Millisecond, Wait: 100 * time.Millisecond}
	retry.RunWith(timer, t, func(r *retry.R) {
		require.ElementsMatch(r, expectedCommand, cmd.consulCommand)
	})
}

// This function starts the command asynchronously and returns a non-blocking chan.
// When finished, the command will send its exit code to the channel.
// Note that it's the responsibility of the caller to terminate the command by calling stopCommand,
// otherwise it can run forever.
func runCommandAsynchronously(cmd *Command, args []string) chan int {
	// We have to run cmd.init() to ensure that the channel the command is
	// using to watch for os interrupts is initialized. If we don't do this,
	// then if stopCommand is called immediately, it will block forever
	// because it calls interrupt() which will attempt to send on a nil channel.
	cmd.init()
	exitChan := make(chan int, 1)
	go func() {
		exitChan <- cmd.Run(args)
	}()
	return exitChan
}

func stopCommand(t *testing.T, cmd *Command, exitChan chan int) {
	if len(exitChan) == 0 {
		cmd.interrupt()
	}
	select {
	case c := <-exitChan:
		require.Equal(t, 0, c, string(cmd.UI.(*cli.MockUi).ErrorWriter.Bytes()))
	}
}

// createServicesTmpFile creates a temp directory
// and writes servicesRegistration as an HCL file there.
func createServicesTmpFile(t *testing.T, serviceHCL string) (string, string) {
	tmpDir, err := ioutil.TempDir("", "")
	require.NoError(t, err)

	configFile := filepath.Join(tmpDir, "svc.hcl")
	err = ioutil.WriteFile(configFile, []byte(serviceHCL), 0600)
	require.NoError(t, err)

	return tmpDir, configFile
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
