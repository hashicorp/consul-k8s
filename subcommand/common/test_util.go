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
// Note that it's the responsibility of the caller to
// remove the temporary files created by this function.
func GenerateServerCerts(t *testing.T) (string, string, string, func()) {
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
