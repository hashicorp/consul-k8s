package installcni

import (
	"fmt"
	"io/ioutil"
	"path/filepath"
	"testing"

	"github.com/hashicorp/consul-k8s/control-plane/cni/config"
	"github.com/stretchr/testify/require"
)

// TODO: Add more tests for different types of CNI plugins we may encounter on GKE/AWS/EKS

// TestDefaultCNIConfigFile_NoFiles tests an edge case in defaultCNIConfigFile where it returns "" when there are no
// config files in the directory.
func TestDefaultCNIConfigFile_NoFiles(t *testing.T) {
	cfgFile := ""
	tempDir := t.TempDir()

	actual, err := defaultCNIConfigFile(tempDir)
	require.Equal(t, cfgFile, actual)
	require.Equal(t, nil, err)
}

// TestDefaultCNIConfigFile tests finding the correct config file in the cniNetDir directory.
func TestDefaultCNIConfigFile(t *testing.T) {
	cases := []struct {
		name        string
		cfgFile     string
		dir         func(string) string
		expectedErr error
	}{
		{
			name:    "valid .conflist file found",
			cfgFile: "testdata/10-kindnet.conflist",
			dir: func(cfgFile string) string {
				tempDir := t.TempDir()
				err := copyFile(cfgFile, tempDir)
				if err != nil {
					t.Fatal(err)
				}
				return tempDir
			},
			expectedErr: nil,
		},
		{
			name:    "several files, should choose .conflist file",
			cfgFile: "testdata/10-kindnet.conflist",
			dir: func(cfgFile string) string {
				tempDir := t.TempDir()
				err := copyFile(cfgFile, tempDir)
				if err != nil {
					t.Fatal(err)
				}
				err = copyFile("testdata/10-fake-cni.conf", tempDir)
				if err != nil {
					t.Fatal(err)
				}

				return tempDir
			},
			expectedErr: nil,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tempDir := c.dir(c.cfgFile)
			actual, err := defaultCNIConfigFile(tempDir)

			filename := filepath.Base(c.cfgFile)
			filepath := filepath.Join(tempDir, filename)
			require.Equal(t, filepath, actual)
			require.Equal(t, c.expectedErr, err)
		})
	}
}

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
			consulConfig: config.NewDefaultCNIConfig(),
			cfgFile:      "testdata/10-kindnet.conflist",
			goldenFile:   "testdata/10-kindnet.conflist.golden",
		},
		{
			name:         "invalid kindnet file that already has consul-cni config inserted, should remove entry and append",
			consulConfig: config.NewDefaultCNIConfig(),
			cfgFile:      "testdata/10-kindnet.conflist.alreadyinserted",
			goldenFile:   "testdata/10-kindnet.conflist.golden",
		},
		{
			name: "valid calico file",
			consulConfig: &config.CNIConfig{
				Name:       config.DefaultPluginName,
				Type:       config.DefaultPluginType,
				CNIBinDir:  config.DefaultCNIBinDir,
				CNINetDir:  config.DefaultCNINetDir,
				Kubeconfig: config.DefaultKubeconfig,
				LogLevel:   config.DefaultLogLevel,
				Multus:     config.DefaultMultus,
			},
			cfgFile:    "testdata/10-calico.conflist",
			goldenFile: "testdata/10-calico.conflist.golden",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Copy the config file to a temporary location so that we can append to it.
			tempDir := t.TempDir()
			err := copyFile(c.cfgFile, tempDir)
			require.NoError(t, err)

			// Get the config file name in the tempdir.
			filename := filepath.Base(c.cfgFile)
			tempDestFile := filepath.Join(tempDir, filename)

			err = appendCNIConfig(c.consulConfig, tempDestFile)
			require.NoError(t, err)

			actual, err := ioutil.ReadFile(tempDestFile)
			require.NoError(t, err)

			expected, err := ioutil.ReadFile(c.goldenFile)
			require.NoError(t, err)

			require.Equal(t, string(expected), string(actual))
		})
	}
}

// TestConfigFileToMap test configFileToMap which takes an unstructure JSON config file and converts it into a map.
func TestConfigFileToMap(t *testing.T) {
	cfgFile := "testdata/10-tiny.conflist"

	expectedMap := map[string]interface{}{
		"cniVersion": "0.3.1",
		"name":       "k8s-pod-network",
		"plugins": []interface{}{
			map[string]interface{}{
				"type": "calico",
			},
			map[string]interface{}{
				"type": "bandwidth",
			},
		},
	}

	tempDir := t.TempDir()
	err := copyFile(cfgFile, tempDir)
	require.NoError(t, err)

	filename := filepath.Base(cfgFile)
	tempDestFile := filepath.Join(tempDir, filename)

	actualMap, err := configFileToMap(tempDestFile)
	require.NoError(t, err)
	require.Equal(t, expectedMap, actualMap)
}

// TestPluginsFromMap tests pluginsFromMap which takes an unmarshalled config JSON map, return the plugin list asserted
// as a []interface{}.
func TestPluginsFromMap(t *testing.T) {
	cfgMap := map[string]interface{}{
		"cniVersion": "0.3.1",
		"name":       "k8s-pod-network",
		"plugins": []interface{}{
			map[string]interface{}{
				"type": "calico",
			},
			map[string]interface{}{
				"type": "bandwidth",
			},
		},
	}

	expectedPlugins := []interface{}{
		map[string]interface{}{
			"type": "calico",
		},
		map[string]interface{}{
			"type": "bandwidth",
		},
	}

	actualPlugins, err := pluginsFromMap(cfgMap)
	require.NoError(t, err)
	require.Equal(t, expectedPlugins, actualPlugins)
}

func TestConsulMapFromConfig(t *testing.T) {
	consulConfig := &config.CNIConfig{
		Name:       config.DefaultPluginName,
		Type:       config.DefaultPluginType,
		CNIBinDir:  config.DefaultCNIBinDir,
		CNINetDir:  config.DefaultCNINetDir,
		Kubeconfig: config.DefaultKubeconfig,
		LogLevel:   config.DefaultLogLevel,
		Multus:     config.DefaultMultus,
	}

	expectedMap := map[string]interface{}{
		"cni_bin_dir": "/opt/cni/bin",
		"cni_net_dir": "/etc/cni/net.d",
		"kubeconfig":  "ZZZ-consul-cni-kubeconfig",
		"log_level":   "info",
		"multus":      false,
		"name":        "consul-cni",
		"type":        "consul-cni",
	}

	actualMap, err := consulMapFromConfig(consulConfig)
	require.NoError(t, err)
	require.Equal(t, expectedMap, actualMap)
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

// TestValidConfig tests validating the config file.
func TestValidConfig(t *testing.T) {
	cases := []struct {
		name         string
		cfgFile      string
		consulConfig *config.CNIConfig
		expectedErr  error
	}{
		{
			name:         "config is missing from file",
			cfgFile:      "testdata/10-kindnet.conflist",
			consulConfig: &config.CNIConfig{},
			expectedErr:  fmt.Errorf("consul-cni config missing from config file"),
		},
		{
			name:         "config passed to installer does not match config in config file",
			cfgFile:      "testdata/10-kindnet.conflist.golden",
			consulConfig: &config.CNIConfig{},
			expectedErr:  fmt.Errorf("consul-cni config has changed"),
		},
		{
			name:    "config passed to installer does not match config in config file",
			cfgFile: "testdata/10-kindnet.conflist.golden",
			consulConfig: &config.CNIConfig{
				CNIBinDir: "foo",
				CNINetDir: "bar",
			},
			expectedErr: fmt.Errorf("consul-cni config has changed"),
		},
		{
			name:    "config passed matches config in config file",
			cfgFile: "testdata/10-kindnet.conflist.golden",
			consulConfig: &config.CNIConfig{
				CNIBinDir:  "/opt/cni/bin",
				CNINetDir:  "/etc/cni/net.d",
				Kubeconfig: "ZZZ-consul-cni-kubeconfig",
				LogLevel:   "info",
				Multus:     false,
				Name:       "consul-cni",
				Type:       "consul-cni",
			},
			expectedErr: nil,
		},
		{
			name:    "config is corrupted and consul-cni is not last in chain",
			cfgFile: "testdata/10-kindnet.conflist.notlast",
			consulConfig: &config.CNIConfig{
				CNIBinDir:  "/opt/cni/bin",
				CNINetDir:  "/etc/cni/net.d",
				Kubeconfig: "ZZZ-consul-cni-kubeconfig",
				LogLevel:   "info",
				Multus:     false,
				Name:       "consul-cni",
				Type:       "consul-cni",
			},
			expectedErr: fmt.Errorf("consul-cni config is not the last plugin in plugin chain"),
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			actualErr := validConfig(c.consulConfig, c.cfgFile)
			require.Equal(t, c.expectedErr, actualErr)
		})
	}
}
