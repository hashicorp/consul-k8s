package installcni

import (
	"io/ioutil"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestKubeConfigYaml generates a kubeconfig yaml file and compares it against a golden file
// Note: This test can fail if the version of client-go/kubernetes changes. The kubectl Config struct sometimes
// inserts a `as-user-extra: null` into the yaml it generates depending on the version. When this happen, the golden
// file needs to be updated. v0.22.2 does not have as-user-extra, while v0.24.2 does.
func TestKubeConfigYaml(t *testing.T) {
	cases := []struct {
		name                     string
		server                   string
		token                    string
		certificateAuthorityData []byte
		goldenFile               string // Golden file that our output should look like.
	}{
		{
			name:                     "valid kubeconfig file",
			server:                   "https://[172.30.0.1]:443",
			token:                    "eyJhbGciOiJSUzI1NiIsImtp",
			certificateAuthorityData: []byte("LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0"),
			goldenFile:               "ZZZ-consul-cni-kubeconfig.golden",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			actual, err := kubeConfigYaml(c.server, c.token, c.certificateAuthorityData)
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
