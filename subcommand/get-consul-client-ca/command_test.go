package getconsulclientca

import (
	"crypto"
	"fmt"
	"io/ioutil"
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
		"-http-addr", a.HTTPAddr,
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
			"-http-addr", fmt.Sprintf("http://127.0.0.1:%d", randomPorts[1]),
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
		"-http-addr", a.HTTPAddr,
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
