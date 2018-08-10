package cert

import (
	"context"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// hasOpenSSL is used to determine if the openssl CLI exists for unit tests.
var hasOpenSSL bool

func init() {
	_, err := exec.LookPath("openssl")
	hasOpenSSL = err == nil
}

// Test that valid certificates are generated
func TestGenSource_valid(t *testing.T) {
	if !hasOpenSSL {
		t.Skip("openssl not found")
		return
	}

	require := require.New(t)

	// Generate the bundle
	source := testGenSource()
	bundle, err := source.Certificate(context.Background(), nil)
	require.NoError(err)

	// Create a temporary directory for storing the certs
	td, err := ioutil.TempDir("", "consul")
	require.NoError(err)
	defer os.RemoveAll(td)

	// Write the cert
	require.NoError(ioutil.WriteFile(filepath.Join(td, "ca.pem"), bundle.CACert, 0644))
	require.NoError(ioutil.WriteFile(filepath.Join(td, "leaf.pem"), bundle.Cert, 0644))

	// Use OpenSSL to verify so we have an external, known-working process
	// that can verify this outside of our own implementations.
	cmd := exec.Command(
		"openssl", "verify", "-verbose", "-CAfile", "ca.pem", "leaf.pem")
	cmd.Dir = td
	output, err := cmd.Output()
	t.Log(string(output))
	require.NoError(err)
}

func testGenSource() *GenSource {
	return &GenSource{
		Name:  "Test",
		Hosts: []string{"127.0.0.1", "localhost"},
	}
}
