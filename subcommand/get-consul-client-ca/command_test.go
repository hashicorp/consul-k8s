package getconsulclientca

import (
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/consul-k8s/helper/cert"
	"github.com/hashicorp/consul-k8s/helper/go-discover/mocks"
	"github.com/hashicorp/consul-k8s/subcommand/common"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/freeport"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/hashicorp/go-discover"
	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestRun_FlagsValidation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		flags  []string
		expErr string
	}{
		{
			flags:  []string{},
			expErr: "-output-file must be set",
		},
		{
			flags: []string{
				"-output-file=output.pem",
			},
			expErr: "-server-addr must be set",
		},
		{
			flags: []string{
				"-output-file=output.pem",
				"-server-addr=foo.com",
				"-log-level=invalid-log-level",
			},
			expErr: "Unknown log level: invalid-log-level",
		},
	}

	for _, c := range cases {
		t.Run(c.expErr, func(t *testing.T) {
			ui := cli.NewMockUi()
			cmd := Command{
				UI: ui,
			}

			exitCode := cmd.Run(c.flags)
			require.Equal(t, 1, exitCode, ui.ErrorWriter.String())
			require.Contains(t, ui.ErrorWriter.String(), c.expErr)
		})
	}
}

// Test that in the happy case scenario
// we retrieve the CA from Consul and
// write it to a file
func TestRun(t *testing.T) {
	t.Parallel()
	outputFile, err := ioutil.TempFile("", "ca")
	require.NoError(t, err)
	defer os.Remove(outputFile.Name())

	caFile, certFile, keyFile, cleanup := common.GenerateServerCerts(t)
	defer cleanup()

	ui := cli.NewMockUi()
	cmd := Command{
		UI: ui,
	}

	// start the test server
	a, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
		c.Connect = map[string]interface{}{
			"enabled": true,
		}
		c.CAFile = caFile
		c.CertFile = certFile
		c.KeyFile = keyFile
	})
	require.NoError(t, err)
	defer a.Stop()

	// run the command
	exitCode := cmd.Run([]string{
		"-server-addr", strings.Split(a.HTTPSAddr, ":")[0],
		"-server-port", strings.Split(a.HTTPSAddr, ":")[1],
		"-ca-file", caFile,
		"-output-file", outputFile.Name(),
	})
	require.Equal(t, 0, exitCode, ui.ErrorWriter.String())

	client, err := api.NewClient(&api.Config{
		Address: a.HTTPSAddr,
		Scheme:  "https",
		TLSConfig: api.TLSConfig{
			CAFile: caFile,
		},
	})
	require.NoError(t, err)

	// get the actual root ca cert from consul so we can compare that
	// with the command output
	roots, _, err := client.Agent().ConnectCARoots(nil)
	require.NoError(t, err)
	require.NotNil(t, roots)
	require.NotNil(t, roots.Roots)
	require.Len(t, roots.Roots, 1)
	require.True(t, roots.Roots[0].Active)
	expectedCARoot := roots.Roots[0].RootCertPEM

	// read the file contents
	actualCARoot, err := ioutil.ReadFile(outputFile.Name())
	require.NoError(t, err)
	require.Equal(t, expectedCARoot, string(actualCARoot))
}

// Test that if the Consul server is not available at first,
// we continue to poll it until it comes up.
func TestRun_ConsulServerAvailableLater(t *testing.T) {
	t.Parallel()
	outputFile, err := ioutil.TempFile("", "ca")
	require.NoError(t, err)
	defer os.Remove(outputFile.Name())

	caFile, certFile, keyFile, cleanup := common.GenerateServerCerts(t)
	defer cleanup()

	ui := cli.NewMockUi()
	cmd := Command{
		UI: ui,
	}

	randomPorts := freeport.MustTake(6)

	// Start the consul agent asynchronously
	var a *testutil.TestServer
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		// start the test server after 100ms
		time.Sleep(100 * time.Millisecond)
		a, err = testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
			c.Ports = &testutil.TestPortConfig{
				DNS:     randomPorts[0],
				HTTP:    randomPorts[1],
				HTTPS:   randomPorts[2],
				SerfLan: randomPorts[3],
				SerfWan: randomPorts[4],
				Server:  randomPorts[5],
			}
			c.Connect = map[string]interface{}{
				"enabled": true,
			}
			c.CAFile = caFile
			c.CertFile = certFile
			c.KeyFile = keyFile
		})
		require.NoError(t, err)
		wg.Done()
	}()
	defer func() {
		if a != nil {
			a.Stop()
		}
	}()

	exitCode := cmd.Run([]string{
		"-server-addr", "localhost",
		"-server-port", fmt.Sprintf("%d", randomPorts[2]),
		"-ca-file", caFile,
		"-output-file", outputFile.Name(),
	})
	require.Equal(t, 0, exitCode, ui.ErrorWriter)

	wg.Wait()
	client, err := api.NewClient(&api.Config{
		Address: a.HTTPSAddr,
		Scheme:  "https",
		TLSConfig: api.TLSConfig{
			CAFile: caFile,
		},
	})
	require.NoError(t, err)

	// get the actual ca cert from consul
	var expectedCARoot string
	retry.Run(t, func(r *retry.R) {
		roots, _, err := client.Agent().ConnectCARoots(nil)
		require.NoError(r, err)
		require.NotNil(r, roots)
		require.NotNil(r, roots.Roots)
		require.Len(r, roots.Roots, 1)
		require.True(r, roots.Roots[0].Active)
		expectedCARoot = roots.Roots[0].RootCertPEM
	})

	// check that the file contents match the actual CA
	actualCARoot, err := ioutil.ReadFile(outputFile.Name())
	require.NoError(t, err)
	require.Equal(t, expectedCARoot, string(actualCARoot))
}

// Test that the command checks for the active root CA
// and only writes the active one to the output file, ignoring
// the inactive one.
func TestRun_GetsOnlyActiveRoot(t *testing.T) {
	t.Parallel()
	outputFile, err := ioutil.TempFile("", "ca")
	require.NoError(t, err)
	defer os.Remove(outputFile.Name())

	caFile, certFile, keyFile, cleanup := common.GenerateServerCerts(t)
	defer cleanup()

	ui := cli.NewMockUi()
	cmd := Command{
		UI: ui,
	}

	// start test server
	a, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
		c.Connect = map[string]interface{}{
			"enabled": true,
		}
		c.CAFile = caFile
		c.CertFile = certFile
		c.KeyFile = keyFile
	})
	require.NoError(t, err)
	defer a.Stop()

	client, err := api.NewClient(&api.Config{
		Address: a.HTTPSAddr,
		Scheme:  "https",
		TLSConfig: api.TLSConfig{
			CAFile: caFile,
		},
	})
	require.NoError(t, err)

	// generate a new CA
	ca, key := generateCA(t)

	// set it as an active CA in Consul,
	// which will make Consul return both CAs -
	// this CA as the active and the original CA as inactive.
	retry.Run(t, func(r *retry.R) {
		_, err = client.Connect().CASetConfig(&api.CAConfig{
			Provider: "consul",
			Config: map[string]interface{}{
				"RootCert":   ca,
				"PrivateKey": key,
			},
		}, nil)
		require.NoError(r, err)
	})

	exitCode := cmd.Run([]string{
		"-server-addr", strings.Split(a.HTTPSAddr, ":")[0],
		"-server-port", strings.Split(a.HTTPSAddr, ":")[1],
		"-ca-file", caFile,
		"-output-file", outputFile.Name(),
	})
	require.Equal(t, 0, exitCode)

	// get the actual ca cert from consul
	var expectedCARoot string
	retry.Run(t, func(r *retry.R) {
		roots, _, err := client.Agent().ConnectCARoots(nil)
		require.NoError(r, err)
		require.NotNil(r, roots)
		require.NotNil(r, roots.Roots)
		require.Len(r, roots.Roots, 2)
		if roots.Roots[0].Active {
			expectedCARoot = roots.Roots[0].RootCertPEM
		} else {
			expectedCARoot = roots.Roots[1].RootCertPEM
		}
	})

	// read the file contents
	actualCARoot, err := ioutil.ReadFile(outputFile.Name())
	require.NoError(t, err)
	require.Equal(t, expectedCARoot, string(actualCARoot))
}

// Test that when using cloud auto-join
// it uses the provider to get the address of the server
func TestRun_WithProvider(t *testing.T) {
	t.Parallel()
	outputFile, err := ioutil.TempFile("", "ca")
	require.NoError(t, err)
	defer os.Remove(outputFile.Name())

	ui := cli.NewMockUi()

	// create a mock provider
	// that always returns the server address
	// provided through the cloud-auto join string
	provider := new(mocks.MockProvider)
	// create stubs for our MockProvider so that it returns
	// the address of the test agent
	provider.On("Addrs", mock.Anything, mock.Anything).Return([]string{"127.0.0.1"}, nil)

	cmd := Command{
		UI:        ui,
		providers: map[string]discover.Provider{"mock": provider},
	}

	caFile, certFile, keyFile, cleanup := common.GenerateServerCerts(t)
	defer cleanup()

	// start the test server
	a, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
		c.Connect = map[string]interface{}{
			"enabled": true,
		}
		c.CAFile = caFile
		c.CertFile = certFile
		c.KeyFile = keyFile
	})
	require.NoError(t, err)
	defer a.Stop()

	// run the command
	exitCode := cmd.Run([]string{
		"-server-addr", "provider=mock",
		"-server-port", strings.Split(a.HTTPSAddr, ":")[1],
		"-output-file", outputFile.Name(),
		"-ca-file", caFile,
	})
	require.Equal(t, 0, exitCode, ui.ErrorWriter.String())

	// check that the provider has been called
	provider.AssertNumberOfCalls(t, "Addrs", 1)

	client, err := api.NewClient(&api.Config{
		Address: a.HTTPSAddr,
		Scheme:  "https",
		TLSConfig: api.TLSConfig{
			CAFile: caFile,
		},
	})
	require.NoError(t, err)

	// get the actual root ca cert from consul
	roots, _, err := client.Agent().ConnectCARoots(nil)
	require.NoError(t, err)
	require.NotNil(t, roots)
	require.NotNil(t, roots.Roots)
	require.Len(t, roots.Roots, 1)
	require.True(t, roots.Roots[0].Active)
	expectedCARoot := roots.Roots[0].RootCertPEM

	// read the file contents
	actualCARoot, err := ioutil.ReadFile(outputFile.Name())
	require.NoError(t, err)
	require.Equal(t, expectedCARoot, string(actualCARoot))
}

// generateCA generates Consul CA
// and returns cert and key as pem strings.
func generateCA(t *testing.T) (caPem, keyPem string) {
	require := require.New(t)

	_, keyPem, caPem, _, err := cert.GenerateCA("Consul Agent CA - Test")
	require.NoError(err)

	return
}
