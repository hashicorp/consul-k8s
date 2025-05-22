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
	defaultCNIBinSourceDir = "/bin"
	consulCNIName          = "consul-cni" // Name of the plugin and binary. They must be the same as per the CNI spec.
	defaultLogJSON         = false
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

	uid := fmt.Sprintf("%d", time.Now().UnixNano())
	// Create the CNI Config from command flags.
	cfg := &config.CNIConfig{
		Name:         config.DefaultPluginName,
		Type:         config.DefaultPluginType,
		CNITokenPath: config.DefaultCNITokenDir + "/" + config.DefaultCNITokenFilename,
		CNIHostTokenPath: func() string {
			if c.flagK8sAutorotateToken {
				return c.flagCNINetDir + "/" + config.DefaultCNIHostTokenFilename + "-" + uid
			}
			return ""
		}(),
		AutorotateToken: c.flagK8sAutorotateToken,
		CNIBinDir:       c.flagCNIBinDir,
		CNINetDir:       c.flagCNINetDir,
		Kubeconfig:      c.flagKubeconfig,
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
	err := copyFile(srcFile, cfg.CNIBinDir)
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
	// watch for changes in the default cni serviceaccount token directory
	if cfg.AutorotateToken {
		// if autorotate-token is enabled, we need to watch the token file for changes and copy it to the host
		// as newly rotated projected tokens are only available in the cni-pod and not on the host
		sourceTokenPath := cfg.CNITokenPath
		hostTokenPath := cfg.CNIHostTokenPath
		go func() {
			if err := c.tokenFileWatcher(ctx, sourceTokenPath, hostTokenPath); err != nil {
				c.logger.Error("Token file watcher failed", "error", err)
			}
		}()
	}

	// Watch for changes in the cniNetDir directory and fix/install the config file if need be.
	err = c.directoryWatcher(ctx, cfg, cfg.CNINetDir, cfgFile)
	if err != nil {
		c.logger.Error("error with directory watcher", "error", err)
		return 1
	}
	return 0
}

// cleanup removes the consul-cni configuration, kubeconfig and cni-host-token-<uid> file from cniNetDir and cniBinDir.
func (c *Command) cleanup(cfg *config.CNIConfig, cfgFile string) {
	var err error
	c.logger.Info("Shutdown received, cleaning up")
	if cfgFile != "" {
		err = removeCNIConfig(cfgFile)
		if err != nil {
			c.logger.Error("Unable to cleanup CNI Config: %w", err)
		}
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
		return fmt.Errorf("could not create watcher: %w", err)
	}

	c.logger.Info("Creating directory watcher for", "directory", dir)
	err = watcher.Add(dir)
	if err != nil {
		return fmt.Errorf("could not watch %s directory: %w", dir, err)
	}
	defer func() {
		_ = watcher.Close()
		c.cleanup(cfg, cfgFile)
	}()

	// Generate the initial kubeconfig file that will be used by the plugin to communicate with the kubernetes api.
	c.logger.Info("Creating kubeconfig", "file", cfg.Kubeconfig)
	kubeConfigFile := filepath.Join(cfg.CNINetDir, cfg.Kubeconfig)
	err = createKubeConfig(cfg)
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
				// This directory watcher doesn't need to handle anything related to token
				if strings.Contains(event.Name, config.DefaultCNIHostTokenFilename) {
					c.logger.Info("Skipping event for host token path. Nothing to do.", "event_path", event.Name)
					break
				}

				// older daemonset can delete this kubeconfig on SIGTERM as cleanup
				// new pod should listen to remove and regenerate it.
				if event.Name == kubeConfigFile {
					if event.Op&fsnotify.Remove != 0 {
						c.logger.Info("Creating kubeconfig", "file", cfg.Kubeconfig)
						err := createKubeConfig(cfg)
						if err != nil {
							c.logger.Error("could not create kube config", "error", err)
							break
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
						break
					}

					if strings.HasSuffix(cfgFile, ".conf") {
						cfgFile, err = confListFileFromConfFile(cfgFile)
						if err != nil {
							c.logger.Error("could convert .conf file to .conflist file", "error", err)
							break
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
							break
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
		case <-c.sigCh:
			return nil
		}
	}
}

// In case of autorotate-token, we are using projected tokens which doesn't support hostpath mount,
// we need to watch the token file for changes and copy it to the host.
func (c *Command) tokenFileWatcher(ctx context.Context, sourceTokenPath, hostTokenPath string) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("could not create token watcher: %w", err)
	}

	defer func() {
		_ = watcher.Close()
	}()

	// Watch the directory containing the token instead of the symlink
	c.logger.Info("Creating token watcher for", "file", sourceTokenPath)
	if err := watcher.Add(sourceTokenPath); err != nil {
		return fmt.Errorf("could not watch token file %s: %w", sourceTokenPath, err)
	}

	// Copy token if initial access is possible
	if _, err := os.Stat(sourceTokenPath); err == nil {
		c.logger.Info("Initial token copy to host", "sourcePath", sourceTokenPath, "destinationPath", hostTokenPath)
		if err := copyToken(sourceTokenPath, hostTokenPath); err != nil {
			c.logger.Error("Failed initial token copy", "error", err)
		}
	}

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				c.logger.Error("Token watcher event is not ok", "event", event)
				break
			}

			// Only handle events for the specific token file
			c.logger.Info("Received file event",
				"event_type", event.Op.String(),
				"file", event.Name)

			if event.Name != sourceTokenPath {
				c.logger.Info("Skipping event as it's not for the source token path", "event_path", event.Name, "source_token_path", sourceTokenPath)
				break
			}
			// Handle Write event on symlink update to point to a new file
			// but the symlink's creation timestamp changes on doing such update as well.

			if event.Op&(fsnotify.Remove|fsnotify.Chmod) != 0 {
				// Re-add watcher after remove/chmod
				backoff := time.Second
				for i := 0; i < 5; i++ {
					if err := watcher.Add(sourceTokenPath); err != nil {
						c.logger.Error("Failed to re-add watcher after remove/chmod", "error", err, "attempt", i+1)
						time.Sleep(backoff)
						backoff *= 2
						continue
					}
					break
				}
				if _, err := os.Stat(sourceTokenPath); err == nil {
					if err := copyToken(sourceTokenPath, hostTokenPath); err != nil {
						c.logger.Error("Failed to copy token after symlink update", "error", err)
					} else {
						c.logger.Info("Successfully copied new token after symlink update")
					}
				}
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				c.logger.Error("Token watcher error channel closed")
				continue
			}
			c.logger.Error("Token watcher error", "error", err)

		case <-ctx.Done():
			return nil
		case <-c.sigCh:
			return nil
		}
	}
}

func copyToken(src, dst string) error {
	// Read source token
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
