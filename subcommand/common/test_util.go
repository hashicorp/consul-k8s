package common

import (
	"io/ioutil"
	"os"
	"testing"
	"time"

	"github.com/hashicorp/consul-k8s/helper/cert"
	"github.com/stretchr/testify/require"
)

// GenerateServerCerts generates Consul CA
// and a server certificate and saves them to temp files.
// It returns file names in this order:
// CA certificate, server certificate, and server key.
func GenerateServerCerts(t *testing.T) (string, string, string) {
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

	t.Cleanup(func() {
		os.Remove(caFile.Name())
		os.Remove(certFile.Name())
		os.Remove(certKeyFile.Name())
	})
	return caFile.Name(), certFile.Name(), certKeyFile.Name()
}

// WriteTempFile writes contents to a temporary file and returns the file
// name. It will remove the file once the test completes.
func WriteTempFile(t *testing.T, contents string) string {
	t.Helper()
	file, err := ioutil.TempFile("", "testName")
	require.NoError(t, err)
	_, err = file.WriteString(contents)
	require.NoError(t, err)

	t.Cleanup(func() {
		os.Remove(file.Name())
	})
	return file.Name()
}
