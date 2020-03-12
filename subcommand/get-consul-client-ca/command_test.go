package getconsulclientca

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"testing"
	"time"

	"github.com/hashicorp/consul-k8s/helper/cert"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/freeport"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/hashicorp/go-discover"
	"github.com/hashicorp/go-hclog"
	"github.com/mitchellh/cli"
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

	ui := cli.NewMockUi()
	cmd := Command{
		UI: ui,
	}

	// start the test server
	a, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
		c.Connect = map[string]interface{}{
			"enabled": true,
		}
	})
	require.NoError(t, err)
	defer a.Stop()

	// run the command
	exitCode := cmd.Run([]string{
		"-server-addr", a.HTTPAddr,
		"-output-file", outputFile.Name(),
	})
	require.Equal(t, 0, exitCode, ui.ErrorWriter.String())

	client, err := api.NewClient(&api.Config{
		Address: a.HTTPAddr,
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

	ui := cli.NewMockUi()
	cmd := Command{
		UI: ui,
	}

	randomPorts := freeport.MustTake(6)

	// Start the consul agent asynchronously
	var a *testutil.TestServer
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
		})
		require.NoError(t, err)
	}()

	exitCode := cmd.Run([]string{
		"-server-addr", fmt.Sprintf("http://127.0.0.1:%d", randomPorts[1]),
		"-output-file", outputFile.Name(),
	})
	require.Equal(t, 0, exitCode)

	// make sure a has been initialized by the time we call Stop()
	retry.Run(t, func(r *retry.R) {
		require.NotNil(r, a)
	})
	defer a.Stop()

	client, err := api.NewClient(&api.Config{
		Address: a.HTTPAddr,
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

	ui := cli.NewMockUi()
	cmd := Command{
		UI: ui,
	}

	// start test server
	a, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
		c.Connect = map[string]interface{}{
			"enabled": true,
		}
	})
	require.NoError(t, err)
	defer a.Stop()

	client, err := api.NewClient(&api.Config{
		Address: a.HTTPAddr,
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
		"-server-addr", a.HTTPAddr,
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

	// create a fake provider
	// that always returns the server address
	// provided through the cloud-auto join string
	provider := &fakeProvider{}
	cmd := Command{
		UI:        ui,
		providers: map[string]discover.Provider{"fake": provider},
	}

	caFile, certFile, keyFile, cleanup := generateServerCerts(t)
	defer cleanup()

	randomPorts := freeport.MustTake(5)
	// start the test server
	a, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
		c.Connect = map[string]interface{}{
			"enabled": true,
		}
		c.CAFile = caFile
		c.CertFile = certFile
		c.KeyFile = keyFile
		c.Ports = &testutil.TestPortConfig{
			DNS:     randomPorts[0],
			HTTP:    randomPorts[1],
			HTTPS:   8501,
			SerfLan: randomPorts[2],
			SerfWan: randomPorts[3],
			Server:  randomPorts[4],
		}
	})
	require.NoError(t, err)
	defer a.Stop()

	// run the command
	exitCode := cmd.Run([]string{
		"-server-addr", "provider=fake address=127.0.0.1",
		"-tls-server-name", "localhost",
		"-output-file", outputFile.Name(),
		"-ca-file", caFile,
	})
	require.Equal(t, 0, exitCode, ui.ErrorWriter.String())

	// check that the provider has been called
	require.Equal(t, 1, provider.addrsNumCalls, "provider's Addrs method was not called")

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

func TestConsulServerAddr(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		cmd          *Command
		expectedAddr string
	}{
		{
			"cloud auto-join string",
			&Command{
				flagServerAddr: "provider=fake address=external-server-address",
				providers:      map[string]discover.Provider{"fake": &fakeProvider{}},
			},
			"https://external-server-address:8501",
		},
		{
			"DNS address without a port and scheme",
			&Command{
				flagServerAddr: "server-address",
			},
			"https://server-address:8501",
		},
		{
			"DNS address without a port and with HTTP scheme",
			&Command{
				flagServerAddr: "http://server-address",
			},
			"http://server-address:8500",
		},
		{
			"DNS address without a port and with HTTPS scheme",
			&Command{
				flagServerAddr: "https://server-address",
			},
			"https://server-address:8501",
		},
		{
			"DNS address with a port and HTTP scheme",
			&Command{
				flagServerAddr: "http://server-address:8700",
			},
			"http://server-address:8700",
		},
		{
			"DNS address with a port but without a scheme",
			&Command{
				flagServerAddr: "server-address:8500",
			},
			"server-address:8500",
		},
		{
			"IP address without a port and scheme",
			&Command{
				flagServerAddr: "1.1.1.1",
			},
			"https://1.1.1.1:8501",
		},
		{
			"IP address without a port and with HTTP scheme",
			&Command{
				flagServerAddr: "http://1.1.1.1",
			},
			"http://1.1.1.1:8500",
		},
		{
			"IP address without a port and with HTTPS scheme",
			&Command{
				flagServerAddr: "https://1.1.1.1",
			},
			"https://1.1.1.1:8501",
		},
		{
			"IP address with a port and HTTP scheme",
			&Command{
				flagServerAddr: "http://1.1.1.1:8700",
			},
			"http://1.1.1.1:8700",
		},
		{
			"IP address with a port but without a scheme",
			&Command{
				flagServerAddr: "1.1.1.1:8500",
			},
			"1.1.1.1:8500",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			addr, err := c.cmd.consulServerAddr(hclog.New(&hclog.LoggerOptions{
				Level:  3,
				Output: os.Stderr,
			}))
			require.NoError(t, err)
			require.Equal(t, c.expectedAddr, addr)
		})
	}
}

// generateCA generates Consul CA
// and returns cert and key as pem strings.
func generateCA(t *testing.T) (caPem, keyPem string) {
	require := require.New(t)

	_, keyPem, caPem, _, err := cert.GenerateCA("Consul Agent CA - Test")
	require.NoError(err)

	return
}

// generateServerCerts generates Consul CA
// and a server certificate and saves them to temp files.
// It returns file names in this order:
// CA certificate, server certificate, and server key.
// Note that it's the responsibility of the caller to
// remove the temporary files created by this function.
func generateServerCerts(t *testing.T) (string, string, string, func()) {
	require := require.New(t)

	caFile, err := ioutil.TempFile("", "ca")
	require.NoError(err)

	certFile, err := ioutil.TempFile("", "cert")
	require.NoError(err)

	certKeyFile, err := ioutil.TempFile("", "key")
	require.NoError(err)

	// Generate CA
	signer, _, caCertPem, caCertTemplate, err := cert.GenerateCA("Consul Agent CA - Test")
	require.NoError(err)

	// Generate Server Cert
	name := "server.dc1.consul"
	hosts := []string{name, "localhost", "127.0.0.1"}
	certPem, keyPem, err := cert.GenerateCert(name, 1*time.Hour, caCertTemplate, signer, hosts)
	require.NoError(err)

	// Write certs and key to files
	_, err = caFile.WriteString(caCertPem)
	require.NoError(err)
	_, err = certFile.WriteString(certPem)
	require.NoError(err)
	_, err = certKeyFile.WriteString(keyPem)
	require.NoError(err)

	cleanupFunc := func() {
		os.Remove(caFile.Name())
		os.Remove(certFile.Name())
		os.Remove(certKeyFile.Name())
	}
	return caFile.Name(), certFile.Name(), certKeyFile.Name(), cleanupFunc
}

type fakeProvider struct {
	addrsNumCalls int
}

func (p *fakeProvider) Addrs(args map[string]string, l *log.Logger) ([]string, error) {
	p.addrsNumCalls++
	return []string{args["address"]}, nil
}

func (p *fakeProvider) Help() string {
	return "fake-provider help"
}
