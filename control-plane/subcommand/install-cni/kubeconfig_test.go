package installcni

import (
	"io/ioutil"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestWriteKubeConfig tests the generated kubeconfig file.
func TestWriteKubeConfig(t *testing.T) {
	cases := []struct {
		name                 string
		server               string
		token                string
		certificateAuthority string
		goldenFile           string // golden file that our output should look like
	}{
		{
			name:                 "valid kubeconfig file",
			server:               "https://[172.30.0.1]:443",
			token:                "eyJhbGciOiJSUzI1NiIsImtp",
			certificateAuthority: "LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0",
			goldenFile:           "ZZZ-consul-cni-kubeconfig.golden",
		},
	}

	// TODO: set context so that the command will timeout

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			actual, err := kubeConfigYaml(c.server, c.token, c.certificateAuthority)
			if err != nil {
				t.Fatal(err)
			}

			require.NoError(t, err)

			golden := filepath.Join("testdata", c.goldenFile)
			expected, err := ioutil.ReadFile(golden)
			require.NoError(t, err)

			require.Equal(t, string(expected), string(actual))
		})
	}
}
