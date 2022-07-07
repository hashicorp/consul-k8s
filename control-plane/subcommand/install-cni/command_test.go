package installcni

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hashicorp/consul-k8s/control-plane/cni/config"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/common"
	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/require"
)

func TestRun_FlagDefaults(t *testing.T) {
	cmd := Command{}
	cmd.init()

	require.Equal(t, cmd.flagCNIBinDir, config.DefaultCNIBinDir)
	require.Equal(t, cmd.flagCNINetDir, config.DefaultCNINetDir)
	require.Equal(t, cmd.flagCNIBinSourceDir, defaultCNIBinSourceDir)
	require.Equal(t, cmd.flagKubeconfig, config.DefaultKubeconfig)
	require.Equal(t, cmd.flagLogLevel, config.DefaultLogLevel)
	require.Equal(t, cmd.flagLogJSON, defaultLogJSON)
	require.Equal(t, cmd.flagMultus, config.DefaultMultus)
}

func TestRun_DirectoryWatcher(t *testing.T) {
	// Create a default configuration that matches golden file.

	consulConfig := config.NewDefaultCNIConfig()
	configFile := "10-kindnet.conflist"
	baseConfigFile := "testdata/10-kindnet.conflist"
	goldenFile := "testdata/10-kindnet.conflist.golden"
	notLastConfigFile := "testdata/10-kindnet.conflist.notlast"

	// Create a Command and context.
	var err error
	ctx, cancel := context.WithCancel(context.Background())
	tempDir := t.TempDir()

	// Setup the Command.
	ui := cli.NewMockUi()
	cmd := &Command{
		UI: ui,
	}
	cmd.init()
	cmd.logger, err = common.Logger("info", false)
	require.NoError(t, err)

	// Create the file watcher.
	go func() {
		err := cmd.directoryWatcher(ctx, consulConfig, tempDir, "")
		require.NoError(t, err)
	}()
	time.Sleep(50 * time.Millisecond)

	// Copy a base config file that does not contain the consul entry.
	err = copyFile(baseConfigFile, tempDir)
	require.NoError(t, err)
	time.Sleep(50 * time.Millisecond)
	// The golden file contains the consul config.
	expected, err := ioutil.ReadFile(goldenFile)
	require.NoError(t, err)
	// Get the name of the config file in the tempDir and read it.
	tempDestFile := filepath.Join(tempDir, configFile)
	actual, err := ioutil.ReadFile(tempDestFile)
	require.NoError(t, err)
	// Filewatcher should have detected a change and appended to the config file. Make sure
	// files match.
	require.Equal(t, string(expected), string(actual))

	// Event 2: config file changed where consul is not last in the plugin list.
	err = replaceFile(notLastConfigFile, filepath.Join(tempDir, configFile))
	require.NoError(t, err)
	time.Sleep(50 * time.Millisecond)
	// Re-read the config file so we can compare the updated config file.
	actual, err = ioutil.ReadFile(tempDestFile)
	require.NoError(t, err)
	// Filewatcher should have detected change, fixed and appended to the config file. Make sure
	// files match.
	require.Equal(t, string(expected), string(actual))

	// Event 3: consul config was removed from the config file. Should detect and fix.
	err = replaceFile(baseConfigFile, filepath.Join(tempDir, configFile))
	require.NoError(t, err)
	time.Sleep(50 * time.Millisecond)
	// Filewatcher should have detected change, fixed and appended to the config file. Make sure
	// files match.
	require.Equal(t, string(expected), string(actual))
	cancel()
}

func replaceFile(srcFile, destFile string) error {
	if _, err := os.Stat(srcFile); os.IsNotExist(err) {
		return fmt.Errorf("source %s file does not exist: %v", srcFile, err)
	}

	filename := filepath.Base(destFile)
	destDir := filepath.Dir(destFile)

	info, err := os.Stat(destDir)
	if os.IsNotExist(err) {
		return fmt.Errorf("destination directory %s does not exist: %v", destDir, err)
	}

	if !info.IsDir() {
		return fmt.Errorf("destination directory %s is not a directory: %v", destDir, err)
	}

	// Check if the user bit is enabled in file permission.
	if info.Mode().Perm()&(1<<(uint(7))) == 0 {
		return fmt.Errorf("cannot write to destination directory %s: %v", destDir, err)
	}

	srcBytes, err := os.ReadFile(srcFile)
	if err != nil {
		return fmt.Errorf("could not read %s file: %v", srcFile, err)
	}

	err = os.WriteFile(filepath.Join(destDir, filename), srcBytes, info.Mode())
	if err != nil {
		return fmt.Errorf("error copying %s file to %s: %v", filename, destDir, err)
	}
	return nil
}
