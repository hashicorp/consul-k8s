package connectinject

import (
	"fmt"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/freeport"
	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"
)

func TestRun_FlagValidation(t *testing.T) {
	cases := []struct {
		flags  []string
		expErr string
	}{
		{
			flags:  []string{},
			expErr: "-consul-k8s-image must be set",
		},
		{
			flags:  []string{"-consul-k8s-image", "foo"},
			expErr: "-consul-image must be set",
		},
		{
			flags:  []string{"-consul-k8s-image", "foo", "-consul-image", "foo"},
			expErr: "-envoy-image must be set",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-envoy-image", "envoy:1.16.0",
				"-log-level", "invalid"},
			expErr: "unknown log level: invalid",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-envoy-image", "envoy:1.16.0",
				"-ca-file", "bar"},
			expErr: "Error reading Consul's CA cert file \"bar\"",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-envoy-image", "envoy:1.16.0",
				"-default-sidecar-proxy-cpu-limit=unparseable"},
			expErr: "-default-sidecar-proxy-cpu-limit is invalid",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-envoy-image", "envoy:1.16.0",
				"-default-sidecar-proxy-cpu-request=unparseable"},
			expErr: "-default-sidecar-proxy-cpu-request is invalid",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-envoy-image", "envoy:1.16.0",
				"-default-sidecar-proxy-memory-limit=unparseable"},
			expErr: "-default-sidecar-proxy-memory-limit is invalid",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-envoy-image", "envoy:1.16.0",
				"-default-sidecar-proxy-memory-request=unparseable"},
			expErr: "-default-sidecar-proxy-memory-request is invalid",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-envoy-image", "envoy:1.16.0",
				"-default-sidecar-proxy-memory-request=50Mi",
				"-default-sidecar-proxy-memory-limit=25Mi",
			},
			expErr: "request must be <= limit: -default-sidecar-proxy-memory-request value of \"50Mi\" is greater than the -default-sidecar-proxy-memory-limit value of \"25Mi\"",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-envoy-image", "envoy:1.16.0",
				"-default-sidecar-proxy-cpu-request=50m",
				"-default-sidecar-proxy-cpu-limit=25m",
			},
			expErr: "request must be <= limit: -default-sidecar-proxy-cpu-request value of \"50m\" is greater than the -default-sidecar-proxy-cpu-limit value of \"25m\"",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-envoy-image", "envoy:1.16.0",
				"-init-container-cpu-limit=unparseable"},
			expErr: "-init-container-cpu-limit 'unparseable' is invalid",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-envoy-image", "envoy:1.16.0",
				"-init-container-cpu-request=unparseable"},
			expErr: "-init-container-cpu-request 'unparseable' is invalid",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-envoy-image", "envoy:1.16.0",
				"-init-container-memory-limit=unparseable"},
			expErr: "-init-container-memory-limit 'unparseable' is invalid",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-envoy-image", "envoy:1.16.0",
				"-init-container-memory-request=unparseable"},
			expErr: "-init-container-memory-request 'unparseable' is invalid",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-envoy-image", "envoy:1.16.0",
				"-init-container-memory-request=50Mi",
				"-init-container-memory-limit=25Mi",
			},
			expErr: "request must be <= limit: -init-container-memory-request value of \"50Mi\" is greater than the -init-container-memory-limit value of \"25Mi\"",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-envoy-image", "envoy:1.16.0",
				"-init-container-cpu-request=50m",
				"-init-container-cpu-limit=25m",
			},
			expErr: "request must be <= limit: -init-container-cpu-request value of \"50m\" is greater than the -init-container-cpu-limit value of \"25m\"",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-envoy-image", "envoy:1.16.0",
				"-lifecycle-sidecar-cpu-limit=unparseable"},
			expErr: "-lifecycle-sidecar-cpu-limit 'unparseable' is invalid",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-envoy-image", "envoy:1.16.0",
				"-lifecycle-sidecar-cpu-request=unparseable"},
			expErr: "-lifecycle-sidecar-cpu-request 'unparseable' is invalid",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-envoy-image", "envoy:1.16.0",
				"-lifecycle-sidecar-memory-limit=unparseable"},
			expErr: "-lifecycle-sidecar-memory-limit 'unparseable' is invalid",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-envoy-image", "envoy:1.16.0",
				"-lifecycle-sidecar-memory-request=unparseable"},
			expErr: "-lifecycle-sidecar-memory-request 'unparseable' is invalid",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-envoy-image", "envoy:1.16.0",
				"-lifecycle-sidecar-memory-request=50Mi",
				"-lifecycle-sidecar-memory-limit=25Mi",
			},
			expErr: "request must be <= limit: -lifecycle-sidecar-memory-request value of \"50Mi\" is greater than the -lifecycle-sidecar-memory-limit value of \"25Mi\"",
		},
		{
			flags: []string{"-consul-k8s-image", "foo", "-consul-image", "foo", "-envoy-image", "envoy:1.16.0",
				"-lifecycle-sidecar-cpu-request=50m",
				"-lifecycle-sidecar-cpu-limit=25m",
			},
			expErr: "request must be <= limit: -lifecycle-sidecar-cpu-request value of \"50m\" is greater than the -lifecycle-sidecar-cpu-limit value of \"25m\"",
		},
		{
			flags: []string{"-consul-k8s-image", "hashicorpdev/consul-k8s:latest", "-consul-image", "foo", "-envoy-image", "envoy:1.16.0",
				"-enable-health-checks-controller=true"},
			expErr: "CONSUL_HTTP_ADDR is not specified",
		},
	}

	for _, c := range cases {
		t.Run(c.expErr, func(t *testing.T) {
			k8sClient := fake.NewSimpleClientset()
			ui := cli.NewMockUi()
			cmd := Command{
				UI:        ui,
				clientset: k8sClient,
			}
			code := cmd.Run(c.flags)
			require.Equal(t, 1, code)
			require.Contains(t, ui.ErrorWriter.String(), c.expErr)
		})
	}
}

func TestRun_ResourceLimitDefaults(t *testing.T) {
	cmd := Command{}
	cmd.init()

	// Init container defaults
	require.Equal(t, cmd.flagInitContainerCPURequest, "50m")
	require.Equal(t, cmd.flagInitContainerCPULimit, "50m")
	require.Equal(t, cmd.flagInitContainerMemoryRequest, "25Mi")
	require.Equal(t, cmd.flagInitContainerMemoryLimit, "150Mi")

	// Lifecycle sidecar container defaults
	require.Equal(t, cmd.flagLifecycleSidecarCPURequest, "20m")
	require.Equal(t, cmd.flagLifecycleSidecarCPULimit, "20m")
	require.Equal(t, cmd.flagLifecycleSidecarMemoryRequest, "25Mi")
	require.Equal(t, cmd.flagLifecycleSidecarMemoryLimit, "50Mi")
}

func TestRun_ValidationHealthCheckEnv(t *testing.T) {
	cases := []struct {
		name    string
		envVars []string
		flags   []string
		expErr  string
	}{
		{
			envVars: []string{api.HTTPAddrEnvName, "0.0.0.0:999999"},
			flags: []string{"-consul-k8s-image", "hashicorp/consul-k8s", "-consul-image", "foo", "-envoy-image", "envoy:1.16.0",
				"-enable-health-checks-controller=true"},
			expErr: "Error parsing CONSUL_HTTP_ADDR: parse \"0.0.0.0:999999\": first path segment in URL cannot contain colon",
		},
	}
	for _, c := range cases {
		t.Run(c.expErr, func(t *testing.T) {
			k8sClient := fake.NewSimpleClientset()
			ui := cli.NewMockUi()
			cmd := Command{
				UI:        ui,
				clientset: k8sClient,
			}
			os.Setenv(c.envVars[0], c.envVars[1])
			code := cmd.Run(c.flags)
			os.Unsetenv(c.envVars[0])
			require.Equal(t, 1, code)
			require.Contains(t, ui.ErrorWriter.String(), c.expErr)
		})
	}
}

// Test that with health checks enabled, if the listener fails to bind that
// everything shuts down gracefully and the command exits.
func TestRun_CommandFailsWithInvalidListener(t *testing.T) {
	k8sClient := fake.NewSimpleClientset()
	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		clientset: k8sClient,
	}
	os.Setenv(api.HTTPAddrEnvName, "http://0.0.0.0:9999")
	code := cmd.Run([]string{
		"-consul-k8s-image", "hashicorp/consul-k8s", "-consul-image", "foo", "-envoy-image", "envoy:1.16.0",
		"-enable-health-checks-controller=true",
		"-listen", "999999",
	})
	os.Unsetenv(api.HTTPAddrEnvName)
	require.Equal(t, 1, code)
	require.Contains(t, ui.ErrorWriter.String(), "Error listening: listen tcp: address 999999: missing port in address")
}

// Test that when healthchecks are enabled that SIGINT/SIGTERM exits the
// command cleanly.
func TestRun_CommandExitsCleanlyAfterSignal(t *testing.T) {

	t.Run("SIGINT", testSignalHandling(syscall.SIGINT))
	t.Run("SIGTERM", testSignalHandling(syscall.SIGTERM))
}

func testSignalHandling(sig os.Signal) func(*testing.T) {
	return func(t *testing.T) {
		k8sClient := fake.NewSimpleClientset()
		ui := cli.NewMockUi()
		cmd := Command{
			UI:        ui,
			clientset: k8sClient,
		}
		ports := freeport.MustTake(1)

		// NOTE: This url doesn't matter because Consul is never called.
		os.Setenv(api.HTTPAddrEnvName, "http://0.0.0.0:9999")
		defer os.Unsetenv(api.HTTPAddrEnvName)

		// Start the command asynchronously and then we'll send an interrupt.
		exitChan := runCommandAsynchronously(&cmd, []string{
			"-consul-k8s-image", "hashicorp/consul-k8s", "-consul-image", "foo", "-envoy-image", "envoy:1.16.0",
			"-enable-health-checks-controller=true",
			"-listen", fmt.Sprintf(":%d", ports[0]),
		})

		// Send the signal
		cmd.sendSignal(sig)

		// Assert that it exits cleanly or timeout.
		select {
		case exitCode := <-exitChan:
			require.Equal(t, 0, exitCode, ui.ErrorWriter.String())
		case <-time.After(time.Second * 1):
			// Fail if the stopCh was not caught.
			require.Fail(t, "timeout waiting for command to exit")
		}
	}
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
