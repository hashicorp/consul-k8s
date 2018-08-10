package cert

import (
	"context"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

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
	t.Parallel()

	if !hasOpenSSL {
		t.Skip("openssl not found")
		return
	}

	// Generate the bundle
	source := testGenSource()
	bundle, err := source.Certificate(context.Background(), nil)
	require.NoError(t, err)
	testBundle(t, &bundle)
}

// Test that certs are regenerated near expiry
func TestGenSource_expiry(t *testing.T) {
	t.Parallel()

	if !hasOpenSSL {
		t.Skip("openssl not found")
		return
	}

	// Generate the bundle
	source := testGenSource()
	source.Expiry = 5 * time.Second
	source.ExpiryWithin = 2 * time.Second

	// First bundle
	bundle, err := source.Certificate(context.Background(), nil)
	require.NoError(t, err)
	testBundle(t, &bundle)

	// Generate again
	start := time.Now()
	next, err := source.Certificate(context.Background(), &bundle)
	dur := time.Now().Sub(start)
	require.NoError(t, err)
	require.False(t, bundle.Equal(&next))
	require.True(t, dur > time.Second)
	testBundle(t, &bundle)
}

func testGenSource() *GenSource {
	return &GenSource{
		Name:  "Test",
		Hosts: []string{"127.0.0.1", "localhost"},
	}
}

func testBundle(t *testing.T, bundle *Bundle) {
	require := require.New(t)

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
