package getconsulclientca

import (
	"crypto"
	"crypto/x509"
	"fmt"
	"github.com/hashicorp/go-discover"
	"io/ioutil"
	"log"
	"net"
	"os"
	"testing"
	"time"

	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/freeport"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/hashicorp/consul/tlsutil"
	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/require"
)

func TestRun_FlagsValidation(t *testing.T) {
	t.Parallel()
	ui := cli.NewMockUi()
	cmd := Command{
		UI: ui,
	}

	exitCode := cmd.Run([]string{
		"-output-file", "",
	})
	require.Equal(t, 1, exitCode)
	require.Contains(t, ui.ErrorWriter.String(), "-output-file must be set")
}

// Test that in the happy case scenario
// we retrieve the CA from Consul and
// write it to a file
func TestRun(t *testing.T) {
	t.Parallel()
	outputFile, err := ioutil.TempFile("", "ca")
	require.NoError(t, err)

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
	require.Equal(t, 0, exitCode)

	client, err := api.NewClient(&api.Config{
		Address: a.HTTPAddr,
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

// Test that if the Consul server is not available at first,
// we continue to poll it until it comes up.
func TestRun_ConsulServerAvailableLater(t *testing.T) {
	t.Parallel()
	outputFile, err := ioutil.TempFile("", "ca")
	require.NoError(t, err)

	ui := cli.NewMockUi()
	cmd := Command{
		UI: ui,
	}

	randomPorts := freeport.MustTake(6)

	// Start the command asynchronously
	exitCode := -1
	go func() {
		exitCode = cmd.Run([]string{
			"-server-addr", fmt.Sprintf("http://127.0.0.1:%d", randomPorts[1]),
			"-output-file", outputFile.Name(),
		})
		require.Equal(t, 0, exitCode)
	}()

	// start the test server
	time.Sleep(500 * time.Millisecond)
	a, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
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
	defer a.Stop()

	// wait for command to exit
	retry.Run(t, func(r *retry.R) {
		require.Equal(r, 0, exitCode)
	})

	client, err := api.NewClient(&api.Config{
		Address: a.HTTPAddr,
	})
	require.NoError(t, err)

	// get the actual ca cert from consul
	var expectedCARoot string
	timer := &retry.Timer{Timeout: 500 * time.Millisecond, Wait: 100 * time.Millisecond}
	retry.RunWith(timer, t, func(r *retry.R) {
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

	// set it as an active CA in Consul
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

	ui := cli.NewMockUi()
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
		"-server-addr", "provider=fake",
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

// generateCA generates Consul CA
// and returns cert and key as pem strings.
func generateCA(t *testing.T) (caPem, keyPem string) {
	require := require.New(t)

	sn, err := tlsutil.GenerateSerialNumber()
	require.NoError(err)

	var signer crypto.Signer
	signer, keyPem, err = tlsutil.GeneratePrivateKey()
	require.NoError(err)

	constraints := []string{"consul", "localhost"}
	caPem, err = tlsutil.GenerateCA(signer, sn, 1, constraints)
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
	sn, err := tlsutil.GenerateSerialNumber()
	require.NoError(err)

	s, _, err := tlsutil.GeneratePrivateKey()
	require.NoError(err)

	constraints := []string{"consul", "localhost"}
	ca, err := tlsutil.GenerateCA(s, sn, 1, constraints)
	require.NoError(err)

	// Generate Server Cert
	name := fmt.Sprintf("server.%s.%s", "dc1", "consul")
	DNSNames := []string{name, "localhost"}
	IPAddresses := []net.IP{net.ParseIP("127.0.0.1")}
	extKeyUsage := []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}

	sn, err = tlsutil.GenerateSerialNumber()
	require.NoError(err)

	pub, priv, err := tlsutil.GenerateCert(s, ca, sn, name, 1, DNSNames, IPAddresses, extKeyUsage)
	require.NoError(err)

	// Write certs and key to files
	_, err = caFile.WriteString(ca)
	require.NoError(err)
	_, err = certFile.WriteString(pub)
	require.NoError(err)
	_, err = certKeyFile.WriteString(priv)
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
	return []string{"127.0.0.1"}, nil
}

func (p *fakeProvider) Help() string {
	return "fake-provider help"
}
