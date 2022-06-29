package installcni

import (
	"io/ioutil"
	"path/filepath"
	"testing"

	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/require"
)

// TestWriteKubeConfig tests the generated kubeconfig file.
func TestWriteKubeConfig(t *testing.T) {
	logger := hclog.New(nil)
	cases := []struct {
		name       string
		fields     *KubeConfigFields
		destFile   string // destination file that we write (sometimes the name changes from .conf -> .conflist)
		goldenFile string // golden file that our output should look like
	}{
		{
			name: "valid kubeconfig file",
			fields: &KubeConfigFields{
				KubernetesServiceProtocol: "https",
				KubernetesServiceHost:     "172.30.0.1",
				KubernetesServicePort:     "443",
				TLSConfig:                 "certificate-authority-data: LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0",
				ServiceAccountToken:       "eyJhbGciOiJSUzI1NiIsImtp",
			},
			destFile:   "ZZZ-consul-cni-kubeconfig",
			goldenFile: "ZZZ-consul-cni-kubeconfig.golden",
		},
	}

	// TODO: set context so that the command will timeout

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tempDir := t.TempDir()
			tempDestFile := filepath.Join(tempDir, c.destFile)

			err := writeKubeConfig(c.fields, tempDestFile, logger)
			if err != nil {
				t.Fatal(err)
			}

			actual, err := ioutil.ReadFile(tempDestFile)
			require.NoError(t, err)

			golden := filepath.Join("testdata", c.goldenFile)
			expected, err := ioutil.ReadFile(golden)
			require.NoError(t, err)

			require.Equal(t, string(expected), string(actual))
		})
	}
}
