package installcni

import (
	"io/ioutil"
	"path/filepath"
	"testing"

	"github.com/hashicorp/consul-k8s/control-plane/cni/config"
	"github.com/stretchr/testify/require"
)

// TODO: Add more tests for different types of CNI plugins we may encounter on GKE/AWS/EKS
// TODO: Remove kindnet tests and replace with Calico as kindnet does not work with other CNI plugins as it is not a real chained plugin. Kindnet does not

// TestCreateCNIConfigFile tests the writing of the config file.
func TestAppendCNIConfig(t *testing.T) {
	cases := []struct {
		name         string
		consulConfig *config.CNIConfig
		// source config file that we would expect to see in /opt/cni/net.d
		cfgFile string
		// golden file that our output should look like
		goldenFile string
	}{
		{
			name:         "valid kindnet file",
			consulConfig: &config.CNIConfig{},
			cfgFile:      "testdata/10-kindnet.conflist",
			goldenFile:   "testdata/10-kindnet.conflist.golden",
		},
		{
			name:         "invalid kindnet file that already has consul-cni config inserted, should remove entry and append",
			consulConfig: &config.CNIConfig{},
			cfgFile:      "testdata/10-kindnet.conflist.alreadyinserted",
			goldenFile:   "testdata/10-kindnet.conflist.golden",
		},
	}
	// Create a default config
	cfg := &config.CNIConfig{
		Name:       pluginName,
		Type:       pluginType,
		CNIBinDir:  defaultCNIBinDir,
		CNINetDir:  defaultCNINetDir,
		DNSPrefix:  "",
		Kubeconfig: defaultKubeconfig,
		LogLevel:   defaultLogLevel,
		Multus:     defaultMultus,
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// copy the config file to a temporary location so that we can append to it
			tempDir := t.TempDir()
			err := copyFile(c.cfgFile, tempDir)
			if err != nil {
				t.Fatal(err)
			}

			// get the config file name in the tempdir
			filename := filepath.Base(c.cfgFile)
			tempDestFile := filepath.Join(tempDir, filename)

			err = appendCNIConfig(cfg, tempDestFile)
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

// TestRemoveCNIConfig tests the writing of the config file.
// Doing the opposite of the TestAppendCNIConfig test. We start with a proper golden file and should
// end up with an empty cfg file.
func TestRemoveCNIConfig(t *testing.T) {
	cases := []struct {
		name       string
		cfgFile    string
		goldenFile string
	}{
		{
			name:       "remove cni config from populated kindnet file",
			cfgFile:    "testdata/10-kindnet.conflist",
			goldenFile: "testdata/10-kindnet.conflist.golden",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// copy the config file to a temporary location so that we can append to it
			tempDir := t.TempDir()
			err := copyFile(c.goldenFile, tempDir)
			if err != nil {
				t.Fatal(err)
			}

			// get the config file name in the tempdir
			filename := filepath.Base(c.goldenFile)
			tempDestFile := filepath.Join(tempDir, filename)

			err = removeCNIConfig(tempDestFile)
			if err != nil {
				t.Fatal(err)
			}

			actual, err := ioutil.ReadFile(tempDestFile)
			require.NoError(t, err)

			expected, err := ioutil.ReadFile(c.cfgFile)
			require.NoError(t, err)

			require.Equal(t, string(expected), string(actual))
		})
	}

}
