// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package installcni

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hashicorp/consul-k8s/control-plane/cni/config"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/common"
	"github.com/hashicorp/serf/testutil/retry"
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
	t.Cleanup(cancel)
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

	t.Log("File event 1: Copy a base config file that does not contain the consul entry. Should detect and add consul-cni")
	err = copyFile(baseConfigFile, tempDir)
	require.NoError(t, err)
	time.Sleep(50 * time.Millisecond)
	// The golden file contains the consul config.
	expected, err := os.ReadFile(goldenFile)
	require.NoError(t, err)
	// Get the name of the config file in the tempDir and read it.
	tempDestFile := filepath.Join(tempDir, configFile)
	actual, err := os.ReadFile(tempDestFile)
	require.NoError(t, err)
	// Filewatcher should have detected a change and appended to the config file. Make sure
	// files match.
	retry.Run(t, func(r *retry.R) {
		require.Equal(r, string(expected), string(actual))
	})

	t.Log("File event 2: config file changed and consul-cni is not last in the plugin list. Should detect and fix.")
	err = replaceFile(notLastConfigFile, filepath.Join(tempDir, configFile))
	require.NoError(t, err)
	time.Sleep(50 * time.Millisecond)
	// Re-read the config file so we can compare the updated config file.
	actual, err = os.ReadFile(tempDestFile)
	require.NoError(t, err)
	// Filewatcher should have detected change, fixed and appended to the config file. Make sure
	// files match.
	retry.Run(t, func(r *retry.R) {
		require.Equal(r, string(expected), string(actual))
	})

	t.Log("File event 3: consul config was removed from the config file. Should detect and fix.")
	err = replaceFile(baseConfigFile, filepath.Join(tempDir, configFile))
	require.NoError(t, err)
	// Filewatcher should have detected change, fixed and appended to the config file. Make sure
	// files match.
	retry.Run(t, func(r *retry.R) {
		require.Equal(r, string(expected), string(actual))
	})

	// If we exit the test too quickly it can cause a race condition where File event 3 is still running and we
	// delete the config file while the test is doing a write.
	time.Sleep(50 * time.Millisecond)
}

func TestRun_FileRegenerationAfterRemoval(t *testing.T) {
	tests := []struct {
		name            string
		autorotateToken bool
		fileToRemove    string
		setupFunc       func(tempDir, binDir string, cfg *config.CNIConfig) (string, error)
		verifyFunc      func(t *testing.T, tempDir, binDir string, cfg *config.CNIConfig, removedFile string)
	}{
		{
			name:            "cni-host-token regeneration after removal",
			autorotateToken: true,
			fileToRemove:    "cni-host-token",
			setupFunc: func(tempDir, binDir string, cfg *config.CNIConfig) (string, error) {
				// Create initial host token file
				hostTokenPath := cfg.CNIHostTokenPath
				err := os.WriteFile(hostTokenPath, []byte("initial-token-content"), 0644)
				if err != nil {
					return "", err
				}
				// Create source token file that will be copied
				err = os.WriteFile(cfg.CNITokenPath, []byte("updated-token-content"), 0644)
				if err != nil {
					return "", err
				}
				return hostTokenPath, nil
			},
			verifyFunc: func(t *testing.T, tempDir, binDir string, cfg *config.CNIConfig, removedFile string) {
				// Verify the host token file was regenerated
				retry.Run(t, func(r *retry.R) {
					_, err := os.Stat(cfg.CNIHostTokenPath)
					require.NoError(r, err, "Host token file should be regenerated")
				})
				// Verify content was copied from source token
				content, err := os.ReadFile(cfg.CNIHostTokenPath)
				require.NoError(t, err)
				require.Equal(t, "updated-token-content", string(content))
			},
		},
		{
			name:            "cni binary regeneration after removal",
			autorotateToken: false,
			fileToRemove:    "consul-cni",
			setupFunc: func(tempDir, binDir string, cfg *config.CNIConfig) (string, error) {
				// Create initial CNI binary
				cniBinaryPath := filepath.Join()
				err := os.WriteFile(cniBinaryPath, []byte("initial-binary-content"), 0755)
				if err != nil {
					return "", err
				}
				return cniBinaryPath, nil
			},
			verifyFunc: func(t *testing.T, tempDir, binDir string, cfg *config.CNIConfig, removedFile string) {
				// Verify the CNI binary was regenerated
				cniBinaryPath := filepath.Join(binDir, consulCNIName)
				retry.Run(t, func(r *retry.R) {
					_, err := os.Stat(cniBinaryPath)
					require.NoError(r, err, "CNI binary should be regenerated")
				})
				// Verify content was copied from source
				content, err := os.ReadFile(cniBinaryPath)
				require.NoError(t, err)
				require.Equal(t, "source-binary-content", string(content))
			},
		},
		{
			name:            "kubeconfig file regeneration after removal",
			autorotateToken: false,
			fileToRemove:    "ZZZ-consul-cni-kubeconfig",
			setupFunc: func(tempDir, binDir string, cfg *config.CNIConfig) (string, error) {
				// Create initial kubeconfig file
				kubeconfigPath := filepath.Join(tempDir, cfg.Kubeconfig)
				err := os.WriteFile(kubeconfigPath, []byte("initial-kubeconfig-content"), 0644)
				if err != nil {
					return "", err
				}
				return kubeconfigPath, nil
			},
			verifyFunc: func(t *testing.T, tempDir, binDir string, cfg *config.CNIConfig, removedFile string) {
				// Verify the kubeconfig file was regenerated
				kubeconfigPath := filepath.Join(tempDir, cfg.Kubeconfig)
				retry.Run(t, func(r *retry.R) {
					_, err := os.Stat(kubeconfigPath)
					require.NoError(r, err, "Kubeconfig file should be regenerated")
				})
				// Verify file is not empty (createKubeConfig should have generated content)
				info, err := os.Stat(kubeconfigPath)
				require.NoError(t, err)
				require.Greater(t, info.Size(), int64(0), "Kubeconfig should not be empty")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp directories
			tempDir := t.TempDir()
			binDir := t.TempDir()
			sourceDir := t.TempDir()

			// Create source CNI binary for copying
			sourceBinaryPath := filepath.Join(sourceDir, consulCNIName)
			err := os.WriteFile(sourceBinaryPath, []byte("source-binary-content"), 0755)
			require.NoError(t, err)

			// Create a default configuration
			uid := fmt.Sprintf("%d", time.Now().UnixNano())
			cfg := &config.CNIConfig{
				Name:         config.DefaultPluginName,
				Type:         config.DefaultPluginType,
				CNITokenPath: filepath.Join(tempDir, "token"),
				CNIHostTokenPath: func() string {
					if tt.autorotateToken {
						return filepath.Join(tempDir, config.DefaultCNIHostTokenFilename+"-"+uid)
					}
					return ""
				}(),
				AutorotateToken: tt.autorotateToken,
				CNIBinDir:       binDir,
				CNINetDir:       tempDir,
				Kubeconfig:      "ZZZ-consul-cni-kubeconfig",
				LogLevel:        "info",
				Multus:          false,
			}

			// Setup the Command
			ui := cli.NewMockUi()
			cmd := &Command{
				UI:                  ui,
				flagCNIBinSourceDir: sourceDir,
			}
			cmd.init()
			cmd.logger, err = common.Logger("info", false)
			require.NoError(t, err)

			// Setup initial files
			removedFilePath, err := tt.setupFunc(tempDir, binDir, cfg)
			require.NoError(t, err)

			// Create context and start directory watcher
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)

			go func() {
				err := cmd.directoryWatcher(ctx, cfg, tempDir, "")
				if err != nil && ctx.Err() == nil {
					t.Errorf("directoryWatcher error: %v", err)
				}
			}()

			// Wait for watcher to initialize
			time.Sleep(100 * time.Millisecond)

			// Remove the file to trigger regeneration
			t.Logf("Removing file: %s", removedFilePath)
			err = os.Remove(removedFilePath)
			require.NoError(t, err)

			// Wait for file system event to be processed
			time.Sleep(200 * time.Millisecond)

			// Verify file regeneration
			tt.verifyFunc(t, tempDir, binDir, cfg, removedFilePath)
		})
	}
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
