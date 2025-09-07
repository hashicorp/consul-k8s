// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package installcni

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/hashicorp/consul-k8s/control-plane/cni/config"
	"github.com/hashicorp/go-hclog"
	"github.com/mitchellh/cli"

	"github.com/hashicorp/consul-k8s/control-plane/subcommand/common"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/flags"
)

const (
	defaultCNIBinSourceDir  = "/bin"
	consulCNIName           = "consul-cni" // Name of the plugin and binary. They must be the same as per the CNI spec.
	defaultLogJSON          = false
	maxInitializeRetryCount = 3
)

// Command flags and structure.
type Command struct {
	UI cli.Ui

	// flagCNIBinDir is the location on the host of the consul-cni binary.
	flagCNIBinDir string
	// flagCNINetDir is the location on the host of cni configuration.
	flagCNINetDir string
	// flagCNIBinSourceDir is the location of consul-cni binary inside the installer container (/bin).
	flagCNIBinSourceDir string
	// flageKubeconfig is the filename of the generated kubeconfig that the plugin will use to communicate with
	// the kubernetes api.
	flagKubeconfig string
	// flagK8sAutorotateToken is a boolean flag for token autorotate feature.
	flagK8sAutorotateToken bool
	// flagLogLevel is the logging level.
	flagLogLevel string
	// flagLogJson is a boolean flag for json logging  format.
	flagLogJSON bool
	// flagMultus is a boolean flag for multus support.
	flagMultus bool
	// flagInstallationID is a unique identifier for this installation instance.
	flagInstallationID string
	// flagCNITokenPath is the path to the CNI token file for testing purposes.
	flagCNITokenPath string

	flagSet *flag.FlagSet

	once   sync.Once
	help   string
	logger hclog.Logger
	sigCh  chan os.Signal
}

func (c *Command) init() {
	c.flagSet = flag.NewFlagSet("", flag.ContinueOnError)
	c.flagSet.StringVar(&c.flagCNIBinDir, "cni-bin-dir", config.DefaultCNIBinDir, "Location of CNI plugin binaries.")
	c.flagSet.StringVar(&c.flagCNINetDir, "cni-net-dir", config.DefaultCNINetDir, "Location to write the CNI plugin configuration.")
	c.flagSet.StringVar(&c.flagCNIBinSourceDir, "bin-source-dir", defaultCNIBinSourceDir, "Host location to copy the binary from")
	c.flagSet.StringVar(&c.flagKubeconfig, "kubeconfig", config.DefaultKubeconfig, "Name of the kubernetes config file")
	c.flagSet.StringVar(&c.flagLogLevel, "log-level", config.DefaultLogLevel,
		"Log verbosity level. Supported values (in order of detail) are \"trace\", "+
			"\"debug\", \"info\", \"warn\", and \"error\".")
	c.flagSet.BoolVar(&c.flagK8sAutorotateToken, "autorotate-token", config.DefaultAutorotateToken, "Enable or disable token autorotate feature.")
	c.flagSet.BoolVar(&c.flagLogJSON, "log-json", defaultLogJSON, "Enable or disable JSON output format for logging.")
	c.flagSet.BoolVar(&c.flagMultus, "multus", config.DefaultMultus, "If the plugin is a multus plugin (default = false)")
	c.flagSet.StringVar(&c.flagInstallationID, "installation-id", "", "Unique identifier for this installation instance (auto-generated if not provided)")
	c.flagSet.StringVar(&c.flagCNITokenPath, "cni-token-path", "", "Path to the CNI token file for testing purposes.")

	c.help = flags.Usage(help, c.flagSet)

	// Wait on an interrupt or terminate to exit. This channel must be initialized before
	// Run() is called so that there are no race conditions where the channel
	// is not defined.
	if c.sigCh == nil {
		c.sigCh = make(chan os.Signal, 1)
		signal.Notify(c.sigCh, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	}
}

// Run runs the command.
func (c *Command) Run(args []string) int {
	c.once.Do(c.init)
	if err := c.flagSet.Parse(args); err != nil {
		return 1
	}

	// Set up logging.
	if c.logger == nil {
		var err error
		c.logger, err = common.Logger(c.flagLogLevel, c.flagLogJSON)
		if err != nil {
			c.UI.Error(err.Error())
			return 1
		}
	}

	// Generate or use provided installation ID
	var installationID string
	if c.flagInstallationID != "" {
		installationID = c.flagInstallationID
	} else {
		installationID = fmt.Sprintf("%d", time.Now().UnixNano())
	}

	// Create the CNI Config from command flags.
	cfg := &config.CNIConfig{
		Name: config.DefaultPluginName,
		Type: config.DefaultPluginType + "-" + installationID,
		CNITokenPath: func() string {
			dir := c.flagCNITokenPath
			if dir == "" {
				dir = config.DefaultCNITokenDir
			}
			return filepath.Join(dir, config.DefaultCNITokenFilename)
		}(),
		CNIHostTokenPath: func() string {
			if c.flagK8sAutorotateToken {
				return filepath.Join(c.flagCNINetDir, config.DefaultCNIHostTokenFilename+"-"+installationID)
			}
			return ""
		}(),
		AutorotateToken: c.flagK8sAutorotateToken,
		CNIBinDir:       c.flagCNIBinDir,
		CNINetDir:       c.flagCNINetDir,
		Kubeconfig:      c.flagKubeconfig + "-" + installationID,
		LogLevel:        c.flagLogLevel,
		Multus:          c.flagMultus,
	}

	c.logger.Info("Running CNI install with configuration",
		"name", cfg.Name,
		"type", cfg.Type,
		"cni_bin_dir", cfg.CNIBinDir,
		"cni_net_dir", cfg.CNINetDir,
		"multus", cfg.Multus,
		"kubeconfig", cfg.Kubeconfig,
		"log_level", cfg.LogLevel,
		"cni_token_path:", cfg.CNITokenPath,
		"cni_host_token_path", cfg.CNIHostTokenPath,
		"autorotate_token:", cfg.AutorotateToken,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Copy the consul-cni binary from the installer container to the host.
	c.logger.Info("Copying consul-cni binary", "destination", cfg.CNIBinDir)
	srcFile := filepath.Join(c.flagCNIBinSourceDir, consulCNIName)

	//type is what the kubelet tries to lookup as filename in cniNetDir
	err := copyFile(srcFile, cfg.CNIBinDir, cfg.Type)
	if err != nil {
		c.logger.Error("could not copy consul-cni binary", "error", err)
		return 1
	}

	// Get the config file that is on the host.
	c.logger.Info("Getting default config file from", "destination", cfg.CNINetDir)
	cfgFile, err := defaultCNIConfigFile(cfg.CNINetDir)
	if err != nil {
		c.logger.Error("could not get default CNI config file", "error", err)
		return 1
	}

	// Install as a chained plugin.
	if !cfg.Multus {
		if strings.HasSuffix(cfgFile, ".conf") {
			c.logger.Info("Converting .conf file to .conflist file", "file", cfgFile)
			cfgFile, err = confListFileFromConfFile(cfgFile)
			if err != nil {
				c.logger.Error("could convert .conf file to .conflist file", "error", err)
				return 1
			}
		}

		// The config file does not exist and it probably means that the consul-cni plugin was installed or scheduled
		// before other cni plugins on the node. We will add a directory watcher and wait for another plugin to
		// be installed.
		if cfgFile == "" {
			c.logger.Info("CNI config file not found. Consul-cni is a chained plugin and another plugin must be installed first. Waiting...", "directory", cfg.CNINetDir)
		} else {
			// Check if there is valid config in the config file. It is invalid if no consul-cni config exists,
			// the consul-cni config is not the last in the plugin chain or the consul-cni config is different from
			// what is passed into helm (it could happen in a helm upgrade).
			c.logger.Info("Using config file", "file", cfgFile)
			err := validConfig(cfg, cfgFile)
			if err != nil {
				// The invalid config is not critical and we can recover from it.
				c.logger.Info("Installing plugin", "reason", err)
				err = appendCNIConfig(cfg, cfgFile)
				if err != nil {
					c.logger.Error("could not append configuration to config file", "error", err)
					return 1
				}
			}
		}
	} else {
		// When multus is enabled, the plugin configuration is set in a NetworkAttachementDefinition CRD and multus
		// handles the configuration and running of the consul-cni plugin. Also, we add a `k8s.v1.cni.cncf.io/networks: consul-cni`
		// annotation during connect inject so that multus knows to run the consul-cni plugin.
		c.logger.Info("Multus enabled, using multus NetworkAttachementDefinition for configuration")
	}

	errCh := make(chan error, 2)
	var wg sync.WaitGroup

	// watch for changes in the default cni serviceaccount token directory
	if cfg.AutorotateToken {
		// if autorotate-token is enabled, we need to watch the token file for changes and copy it to the host
		// as newly rotated projected tokens are only available in the cni-pod and not on the host
		sourceTokenPath := cfg.CNITokenPath
		hostTokenPath := cfg.CNIHostTokenPath
		go func() {
			wg.Add(1)
			defer wg.Done()
			if err := c.tokenFileWatcher(ctx, sourceTokenPath, hostTokenPath); err != nil {
				c.logger.Error("Token file watcher failed", "error", err)
				errCh <- err
			}
		}()
	}

	// Watch for changes in the cniNetDir directory and fix/install the config files if need be.
	go func() {
		wg.Add(1)
		defer wg.Done()
		if err := c.directoryWatcher(ctx, cfg, cfg.CNINetDir, cfgFile); err != nil {
			c.logger.Error("error with directory watcher", "error", err)
			errCh <- err
		}
	}()

	// Wait for either a shutdown signal or an error from watchers
	var responseCode = 0
	select {
	case sig := <-c.sigCh:
		c.logger.Info("Received shutdown signal", "signal", sig)
	case err := <-errCh:
		c.logger.Error("Received error from watcher", "error", err)
		responseCode = 1
	}
	cancel()
	wg.Wait()
	// wait for watchers to finish as they regenerate pluginconfs/tokens/kubeconfigs
	c.cleanup(cfg, cfgFile)
	return responseCode
}

// cleanup removes the consul-cni configuration, kubeconfig and cni-host-token-<uid> file from cniNetDir and cniBinDir.
func (c *Command) cleanup(cfg *config.CNIConfig, cfgFile string) {
	var err error
	c.logger.Info("Shutdown received, cleaning up")
	// Its important to cleanup in this order as plugin conf binds cni in the workflow
	if cfgFile != "" {
		err = removeCNIConfig(cfgFile)
		c.logger.Info("Removed CNI Config", "file", cfgFile)
		if err != nil {
			c.logger.Error("Unable to cleanup CNI Config: %w", err)
		}
	}

	cniBinaryPath := filepath.Join(cfg.CNIBinDir, cfg.Type)
	c.logger.Info("Removing file", "file", cniBinaryPath)
	err = removeFile(cniBinaryPath)
	if err != nil {
		c.logger.Error("Unable to remove %s file: %w", cniBinaryPath, err)
	}

	c.logger.Info("Removing file", "file", cfg.CNIHostTokenPath)
	err = removeFile(cfg.CNIHostTokenPath)
	if err != nil {
		c.logger.Error("Unable to remove %s file: %w", cfg.CNIHostTokenPath, err)
	}

	kubeconfig := filepath.Join(cfg.CNINetDir, cfg.Kubeconfig)
	c.logger.Info("Removing file", "file", kubeconfig)

	err = removeFile(kubeconfig)
	if err != nil {
		c.logger.Error("Unable to remove %s file: %w", kubeconfig, err)
	}
}

// directoryWatcher watches for changes in the cniNetDir forever. We watch the directory because there is a case where
// the installer could be the first cni plugin installed and we need to wait for another plugin to show up. Once
// installed we watch for changes, verify that our plugin installation is valid and re-install the consul-cni config.
func (c *Command) directoryWatcher(ctx context.Context, cfg *config.CNIConfig, dir, cfgFile string) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("could not create dirwatcher: %w", err)
	}
	c.logger.Info("Creating directory watcher for", "directory", dir)
	err = watcher.Add(dir)
	if err != nil {
		return fmt.Errorf("could not watch %s directory: %w", dir, err)
	}
	defer func() {
		_ = watcher.Close()
	}()

	// Generate the initial kubeconfig file that will be used by the plugin to communicate with the kubernetes api.
	c.logger.Info("Creating kubeconfig", "file", cfg.Kubeconfig)
	kubeConfigFile := filepath.Join(dir, cfg.Kubeconfig)
	err = createKubeConfig(dir, cfg)
	if err != nil {
		c.logger.Error("could not create kube config", "error", err)
	}

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				c.logger.Error("Event watcher event is not ok", "event", event)
			}
			// For every event, get the config file, validate it and append the CNI configuration. If no
			// config file is available, do nothing. This can happen if the consul-cni daemonset was
			// created before other CNI plugins were installed.
			if event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove) != 0 {
				// Separate tokenFileWatcher updates the token file in the host path
				// older daemonset can delete this token on SIGTERM as cleanup
				if event.Name == cfg.CNIHostTokenPath {
					c.logger.Info("Token file updated", "file", event.Name)
					break
				}
				// older daemonset can delete this kubeconfig on SIGTERM as cleanup
				// new pod should listen to remove and regenerate it.
				// currently this is not unit-testable as createKubeConfig is not mockable
				if event.Name == kubeConfigFile {
					if event.Op&fsnotify.Remove != 0 {
						c.logger.Info("Creating kubeconfig", "file", cfg.Kubeconfig)
						err := createKubeConfig(dir, cfg)
						if err != nil {
							c.logger.Error("could not create kube config", "error", err)
							return err
						}
					}
					break
				}

				// Only repair things if this is a non-multus setup. Multus config is handled differently
				// than chained plugins
				if !cfg.Multus {
					c.logger.Info("Modified event", "event", event)
					// Always get the config file that is on the host as we do not know if it was deleted
					// or not.
					cfgFile, err = defaultCNIConfigFile(dir)
					if err != nil {
						c.logger.Error("Unable get default config file", "error", err)
						return err
					}

					if strings.HasSuffix(cfgFile, ".conf") {
						cfgFile, err = confListFileFromConfFile(cfgFile)
						if err != nil {
							c.logger.Error("could convert .conf file to .conflist file", "error", err)
							return err
						}
					}

					if cfgFile != "" {
						c.logger.Info("Using config file", "file", cfgFile)

						err = validConfig(cfg, cfgFile)
						if err != nil {
							// The invalid config is not critical and we can recover from it.
							c.logger.Info("Installing plugin", "reason", err)
							err = appendCNIConfig(cfg, cfgFile)
							if err != nil {
								c.logger.Error("Unable to install consul-cni config", "error", err)
								return err
							}
						} else {
							c.logger.Info("Valid config file detected, nothing to do")
						}
					}
				}
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				c.logger.Error("Event watcher event is not ok", "error", err)
			}
		case <-ctx.Done():
			return nil
		}
	}
}

// In case of autorotate-token, we are using projected tokens which doesn't support hostpath mount,
// we need to watch the token file for changes and copy it to the host.
func (c *Command) tokenFileWatcher(ctx context.Context, sourceTokenPath, hostTokenPath string) error {
	sourceTokenWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("could not create sourcetokenWatcher: %w", err)
	}
	if err != nil {
		return fmt.Errorf("could not create desttokenWatcher: %w", err)
	}
	defer func() {
		_ = sourceTokenWatcher.Close()
	}()

	c.logger.Info("Creating sourceTokenWatcher for", "file", sourceTokenPath)
	if err := sourceTokenWatcher.Add(sourceTokenPath); err != nil {
		return fmt.Errorf("could not watch token file %s: %w", sourceTokenPath, err)
	}

	if err := copyToken(sourceTokenPath, hostTokenPath); err != nil {
		c.logger.Info("Failed to perform initial copy token", "error", err)
	}

	for {
		select {
		case event, ok := <-sourceTokenWatcher.Events:
			if !ok {
				c.logger.Error("Token sourceTokenWatcher event is not ok", "event", event)
				return fmt.Errorf("token sourceTokenWatcher event channel closed unexpectedly")
			}

			// Only handle events for the specific token file
			c.logger.Info("Received file event",
				"event_type", event.Op.String(),
				"file", event.Name)

			// Handle Write event on symlink update to point to a new file
			// but the symlink's creation timestamp changes on doing such update as well.

			if event.Op&(fsnotify.Remove|fsnotify.Chmod) != 0 {
				// Re-add sourceTokenWatcher after remove/chmod
				backoff := time.Second
				waitCount := 5
				for i := 1; i <= waitCount; i++ {
					if err := sourceTokenWatcher.Add(sourceTokenPath); err != nil {
						c.logger.Error("Failed to re-add sourceTokenWatcher after remove/chmod", "error", err, "attempt", i+1)
						time.Sleep(backoff)
						backoff *= 2
						if waitCount == i {
							return fmt.Errorf("failed to re-add sourceTokenWatcher after remove/chmod after %d attempts", waitCount)
						}
					}
				}

				if err := copyToken(sourceTokenPath, hostTokenPath); err != nil {
					c.logger.Error("Failed to copy token after symlink update", "error", err)
				} else {
					c.logger.Info("Successfully copied new token from source")
				}
			}

		case err, ok := <-sourceTokenWatcher.Errors:
			if !ok {
				c.logger.Error("SourceTokenWatcher error channel closed")
				return fmt.Errorf("SourceTokenWatcher error channel closed unexpectedly")
			}
			c.logger.Error("SourceTokenWatcher error", "error", err)

		case <-ctx.Done():
			return nil
		}
	}
}

func copyToken(src, dst string) error {
	// Read source token
	if _, err := os.Stat(src); err != nil {
		return err
	}
	content, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("failed to read source token: %w", err)
	}

	// Write to destination with correct permissions
	if err := os.WriteFile(dst, content, 0644); err != nil {
		return fmt.Errorf("failed to write destination token: %w", err)
	}

	return nil
}

// Synopsis returns the summary of the cni install command.
func (c *Command) Synopsis() string { return synopsis }

// Help returns the help output of the command.
func (c *Command) Help() string {
	c.once.Do(c.init)
	return c.help
}

const (
	synopsis = "Consul CNI plugin installer"
	help     = `
Usage: consul-k8s-control-plane cni-install [options]

  Install Consul CNI plugin
  Not intended for stand-alone use.
`
)
