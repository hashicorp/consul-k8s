// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package installcni

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
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
	err = copyFile(baseConfigFile, tempDir, "")
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

func TestRun_TokenFileWatcher(t *testing.T) {
	tests := []struct {
		name        string
		description string
		testFunc    func(t *testing.T, cmd *Command, sourceTokenPath, hostTokenPath string, ctx context.Context)
	}{
		{
			name:        "source token file deletion and recreation",
			description: "Test that when source token file is deleted and recreated with new content, host file reflects updated content",
			testFunc: func(t *testing.T, cmd *Command, sourceTokenPath, hostTokenPath string, ctx context.Context) {
				// Create initial source token file
				initialContent := "initial-token-content-12345"
				err := os.WriteFile(sourceTokenPath, []byte(initialContent), 0644)
				require.NoError(t, err)

				// Start token file watcher
				go func() {
					err := cmd.tokenFileWatcher(ctx, sourceTokenPath, hostTokenPath)
					if err != nil && ctx.Err() == nil {
						t.Errorf("tokenFileWatcher error: %v", err)
					}
				}()

				// Wait for initial copy
				time.Sleep(200 * time.Millisecond)

				// Verify initial copy worked
				retry.Run(t, func(r *retry.R) {
					content, err := os.ReadFile(hostTokenPath)
					require.NoError(r, err, "Host token file should exist")
					require.Equal(r, initialContent, string(content), "Initial content should match")
				})

				t.Log("Step 1: Delete source token file")
				err = os.Remove(sourceTokenPath)
				require.NoError(t, err)

				// Wait for file system event processing
				time.Sleep(100 * time.Millisecond)

				t.Log("Step 2: Recreate source token file with new content")
				updatedContent := "updated-token-content-67890"
				err = os.WriteFile(sourceTokenPath, []byte(updatedContent), 0644)
				require.NoError(t, err)

				// Wait for token watcher to detect and copy new content
				time.Sleep(300 * time.Millisecond)

				t.Log("Step 3: Verify host token file has updated content")
				retry.Run(t, func(r *retry.R) {
					content, err := os.ReadFile(hostTokenPath)
					require.NoError(r, err, "Host token file should still exist")
					require.Equal(r, updatedContent, string(content), "Host token should have updated content")
				})
			},
		},
		{
			name:        "multiple source token updates",
			description: "Test multiple consecutive updates to source token file are reflected in host file",
			testFunc: func(t *testing.T, cmd *Command, sourceTokenPath, hostTokenPath string, ctx context.Context) {
				// Create initial source token file
				initialContent := "token-v1"
				err := os.WriteFile(sourceTokenPath, []byte(initialContent), 0644)
				require.NoError(t, err)

				// Start token file watcher
				go func() {
					err := cmd.tokenFileWatcher(ctx, sourceTokenPath, hostTokenPath)
					if err != nil && ctx.Err() == nil {
						t.Errorf("tokenFileWatcher error: %v", err)
					}
				}()

				// Wait for initial copy
				time.Sleep(200 * time.Millisecond)

				// Test multiple updates
				updates := []string{"token-v2", "token-v3", "token-v4"}
				for i, content := range updates {
					t.Logf("Step %d: Update token to %s", i+1, content)

					// Remove and recreate with new content
					err = os.Remove(sourceTokenPath)
					require.NoError(t, err)
					time.Sleep(50 * time.Millisecond)

					err = os.WriteFile(sourceTokenPath, []byte(content), 0644)
					require.NoError(t, err)
					time.Sleep(200 * time.Millisecond)

					// Verify host file has the latest content
					retry.Run(t, func(r *retry.R) {
						hostContent, err := os.ReadFile(hostTokenPath)
						require.NoError(r, err, "Host token file should exist")
						require.Equal(r, content, string(hostContent), "Host token should have latest content")
					})
				}
			},
		},
		{
			name:        "source token chmod event handling",
			description: "Test that chmod events on source token trigger re-copy to host",
			testFunc: func(t *testing.T, cmd *Command, sourceTokenPath, hostTokenPath string, ctx context.Context) {
				// Create initial source token file
				initialContent := "chmod-test-token-abc123"
				err := os.WriteFile(sourceTokenPath, []byte(initialContent), 0644)
				require.NoError(t, err)

				// Start token file watcher
				go func() {
					err := cmd.tokenFileWatcher(ctx, sourceTokenPath, hostTokenPath)
					if err != nil && ctx.Err() == nil {
						t.Errorf("tokenFileWatcher error: %v", err)
					}
				}()

				// Wait for initial copy
				time.Sleep(200 * time.Millisecond)

				// Verify initial copy worked
				retry.Run(t, func(r *retry.R) {
					content, err := os.ReadFile(hostTokenPath)
					require.NoError(r, err, "Host token file should exist")
					require.Equal(r, initialContent, string(content), "Initial content should match")
				})

				t.Log("Step 1: Update source token content and trigger chmod")
				updatedContent := "chmod-updated-token-xyz789"
				err = os.WriteFile(sourceTokenPath, []byte(updatedContent), 0600)
				require.NoError(t, err)

				// Trigger chmod event which should cause re-copy
				err = os.Chmod(sourceTokenPath, 0644)
				require.NoError(t, err)

				// Wait for token watcher to detect chmod and copy new content
				time.Sleep(300 * time.Millisecond)

				t.Log("Step 2: Verify host token file has updated content after chmod")
				retry.Run(t, func(r *retry.R) {
					content, err := os.ReadFile(hostTokenPath)
					require.NoError(r, err, "Host token file should still exist")
					require.Equal(r, updatedContent, string(content), "Host token should have updated content after chmod")
				})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp directories
			tempDir := t.TempDir()

			// Setup paths
			sourceTokenPath := filepath.Join(tempDir, "source-token")
			hostTokenPath := filepath.Join(tempDir, "host-token")

			// Setup the Command
			ui := cli.NewMockUi()
			cmd := &Command{
				UI: ui,
			}
			cmd.init()
			var err error
			cmd.logger, err = common.Logger("info", false)
			require.NoError(t, err)

			// Create context
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)

			// Run the specific test
			tt.testFunc(t, cmd, sourceTokenPath, hostTokenPath, ctx)
		})
	}
}

func TestRun_SignalCleanup(t *testing.T) {
	tests := []struct {
		name            string
		signal          os.Signal
		autorotateToken bool
		description     string
	}{
		{
			name:            "SIGTERM cleanup with autorotate token",
			signal:          syscall.SIGTERM,
			autorotateToken: true,
			description:     "Test that SIGTERM signal triggers cleanup of all files including host token",
		},
		{
			name:            "SIGINT cleanup with autorotate token",
			signal:          os.Interrupt,
			autorotateToken: true,
			description:     "Test that SIGINT signal triggers cleanup of all files including host token",
		},
		{
			name:            "SIGTERM cleanup without autorotate token",
			signal:          syscall.SIGTERM,
			autorotateToken: false,
			description:     "Test that SIGTERM signal triggers cleanup of CNI binary and kubeconfig (no host token)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp directories
			tempDir := t.TempDir()
			binDir := t.TempDir()
			sourceDir := t.TempDir()

			// Create configuration
			uid := fmt.Sprintf("%d", time.Now().UnixNano())
			cfg := &config.CNIConfig{
				Name:         config.DefaultPluginName,
				Type:         config.DefaultPluginType,
				CNITokenPath: filepath.Join(sourceDir, "token"),
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

			// Setup the Command with custom signal channel
			ui := cli.NewMockUi()
			cmd := &Command{
				UI:                  ui,
				flagCNIBinSourceDir: sourceDir,
				sigCh:               make(chan os.Signal, 1), // Custom signal channel for testing
				flagInstallationID:  uid,
			}

			var err error
			cmd.logger, err = common.Logger("info", false)
			require.NoError(t, err)

			// Create all the files and sourceFiles required for test
			t.Log("Creating files for test setup...")

			// Create source CNI binary for copying
			err = os.WriteFile(filepath.Join(sourceDir, config.DefaultPluginType), []byte("test-binary-content"), 0755)
			require.NoError(t, err)

			// Create a CNI config file to test cleanup
			configFile := filepath.Join(cfg.CNINetDir, "10-test.conflist")
			configContent := `{
				"cniVersion": "0.3.1",
				"name": "test-network",
				"plugins": [
					{
						"type": "bridge",
						"bridge": "cni0"
					}
				]
			}`
			err = os.WriteFile(configFile, []byte(configContent), 0644)
			require.NoError(t, err)

			// 2. Kubeconfig file
			kubeconfigPath := filepath.Join(cfg.CNINetDir, cfg.Kubeconfig+"-"+uid)
			configContentByte, err := os.ReadFile("testdata/ZZZ-consul-cni-kubeconfig-tokenautorotate.golden")
			require.NoError(t, err)
			err = os.WriteFile(kubeconfigPath, configContentByte, 0644)
			require.NoError(t, err)

			// 3. Source token file (if autorotate is enabled)
			if tt.autorotateToken {
				// Also create source token
				err = os.WriteFile(cfg.CNITokenPath, []byte("test-source-token"), 0644)
				require.NoError(t, err)
			}

			// Start the command in a goroutine
			var runResult int
			var runErr error
			done := make(chan struct{})

			go func() {
				defer close(done)
				// Create args that would normally be passed to Run
				args := []string{
					"-cni-bin-dir", binDir,
					"-cni-net-dir", tempDir,
					"-bin-source-dir", sourceDir,
					"-cni-token-path", sourceDir,
					"-kubeconfig", cfg.Kubeconfig,
					"-installation-id", uid,
					"-log-level", "info",
				}
				if tt.autorotateToken {
					args = append(args, "-autorotate-token")
				}

				runResult = cmd.Run(args)

			}()

			time.Sleep(2000 * time.Millisecond)

			// Verify all files exist before cleanup
			t.Log("Verifying files exist before signal...")
			cniBinaryPath := filepath.Join(cfg.CNIBinDir, cfg.Type)
			_, err = os.Stat(cniBinaryPath)
			require.NoError(t, err, "CNI binary should exist before cleanup")

			// Verify the content of the CNI binary file
			content, err := os.ReadFile(cniBinaryPath)
			require.NoError(t, err, "Should be able to read CNI binary file")
			require.Equal(t, "test-binary-content", string(content), "CNI binary content should match")

			_, err = os.Stat(kubeconfigPath)
			require.NoError(t, err, "Kubeconfig should exist before cleanup")
			if tt.autorotateToken {
				hostTokenPath := filepath.Join(tempDir, config.DefaultCNIHostTokenFilename+"-"+uid)
				_, err = os.Stat(hostTokenPath)
				require.NoError(t, err, "Host token should exist before cleanup")

				// Verify the content of the host token file
				content, err = os.ReadFile(hostTokenPath)
				require.NoError(t, err, "Should be able to read host token file")
				require.Equal(t, "test-source-token", string(content), "Host token content should match")
			}

			t.Logf("Sending %v signal to trigger cleanup...", tt.signal)
			// Send the signal to trigger cleanup
			cmd.sigCh <- tt.signal

			// Wait for the command to complete
			select {
			case <-done:
				t.Log("Command completed successfully")
			case <-time.After(10 * time.Second):
				t.Fatal("Command did not complete within timeout")
			}

			// Verify the command returned successfully (0)
			require.Equal(t, 0, runResult, "Command should return 0 on successful cleanup")
			require.NoError(t, runErr, "Command should not return an error")

			// Wait a bit for cleanup to complete
			time.Sleep(200 * time.Millisecond)

			// Verify all files have been cleaned up
			t.Log("Verifying files are cleaned up after signal...")

			// Check CNI binary is removed
			_, err = os.Stat(cniBinaryPath)
			require.True(t, os.IsNotExist(err), "CNI binary should be removed after cleanup")

			// Check kubeconfig is removed
			_, err = os.Stat(kubeconfigPath)
			require.True(t, os.IsNotExist(err), "Kubeconfig should be removed after cleanup")

			// Check host token is removed (if it was created)
			if tt.autorotateToken {
				hostTokenPath := filepath.Join(tempDir, config.DefaultCNIHostTokenFilename+"-"+uid)
				_, err = os.Stat(hostTokenPath)
				require.True(t, os.IsNotExist(err), "Host token should be removed after cleanup")
			}

			t.Log("All files successfully cleaned up after signal")
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
