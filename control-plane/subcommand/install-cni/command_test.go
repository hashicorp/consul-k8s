package installcni

import (
	"io/ioutil"
	"path/filepath"
	"testing"

	"github.com/hashicorp/consul-k8s/control-plane/cni/config"
	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/require"
)

// TODO: Test scenario where a goes from .conf to .conflist.
// TODO: Test multus plugin.
// TODO: Add more tests for different types of CNI plugins we may encounter on GKE/AWS/EKS
// TODO: Remove kindnet tests and replace with Calico as kindnet does not work with other CNI plugins as it is not a real chained plugin. Kindnet does not
//       pass through the previous result which causes the consul-cni plugin to fail (It took a while to figure that one out...)
// TODO: Find a way to make these file based test faster. Maybe Afero?

// TestCreateCNIConfigFile tests the writing of the config file.
func TestCreateCNIConfigFile(t *testing.T) {
	logger := hclog.New(nil)

	cases := []struct {
		name         string
		consulConfig *config.CNIConfig
		// source config file that we would expect to see in /opt/cni/net.d
		srcFile string
		// destination file that we write (sometimes the name changes from .conf -> .conflist)
		destFile string
		// golden file that our output should look like
		goldenFile string
	}{
		{
			name:         "valid kindnet file",
			consulConfig: &config.CNIConfig{},
			srcFile:      "testdata/10-kindnet.conflist",
			destFile:     "10-kindnet.conflist",
			goldenFile:   "testdata/10-kindnet.conflist.golden",
		},
		{
			name:         "invalid kindnet file that already has consul-cni config inserted, should remove entry and append",
			consulConfig: &config.CNIConfig{},
			srcFile:      "testdata/10-kindnet.conflist.alreadyinserted",
			destFile:     "10-kindnet.conflist",
			goldenFile:   "testdata/10-kindnet.conflist.golden",
		},
	}

	// set context so that the command will timeout

	// Create a default config
	cfg := &config.CNIConfig{
		Name:       pluginName,
		Type:       pluginType,
		CNIBinDir:  defaultCNIBinDir,
		CNINetDir:  defaultCNINetDir,
		Multus:     defaultMultus,
		Kubeconfig: defaultKubeconfig,
		LogLevel:   defaultLogLevel,
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tempDir := t.TempDir()
			tempDestFile := filepath.Join(tempDir, c.destFile)

			err := appendCNIConfig(cfg, c.srcFile, tempDestFile, logger)
			if err != nil {
				t.Fatal(err)
			}

			actual, err := ioutil.ReadFile(tempDestFile)
			require.NoError(t, err)

			expected, err := ioutil.ReadFile(c.goldenFile)
			require.NoError(t, err)

			require.Equal(t, string(expected), string(actual))
		})
	}
}
