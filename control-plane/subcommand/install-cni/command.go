package installcni

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/fsnotify/fsnotify"
	"github.com/hashicorp/consul-k8s/control-plane/cni/config"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/common"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/flags"
	"github.com/hashicorp/go-hclog"
	"github.com/mitchellh/cli"
)

const (
	pluginName       = "consul-cni"
	pluginType       = "consul-cni"
	defaultCNIBinDir = "/opt/cni/bin"
	defaultCNINetDir = "/etc/cni/net.d"
	defaultMultus    = false
	// defaultKubeconfig is named ZZZ-.. as part of a convention that other CNI plugins use.
	defaultKubeconfig      = "ZZZ-consul-cni-kubeconfig"
	defaultLogLevel        = "info"
	defaultCNIBinSourceDir = "/bin"
	consulCNIBinName       = "consul-cni"
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
	// flagDNSPrefix is used to determine the Consul Server DNS IP. The IP is set as an environment variable and
	// the prefix allows us to search for it.
	flagDNSPrefix string
	// flageKubeconfig is the filename of the generated kubeconfig that the plugin will use to communicate with
	// the kubernetes api.
	flagKubeconfig string
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
	c.flagSet.StringVar(&c.flagCNIBinDir, "cni-bin-dir", defaultCNIBinDir, "Location of CNI plugin binaries.")
	c.flagSet.StringVar(&c.flagCNINetDir, "cni-net-dir", defaultCNINetDir, "Location to write the CNI plugin configuration.")
	c.flagSet.StringVar(&c.flagCNIBinSourceDir, "bin-source-dir", defaultCNIBinSourceDir, "Host location to copy the binary from")
	c.flagSet.StringVar(&c.flagDNSPrefix, "dns-prefix", "", "Prefix of the environment variables used to determine the Consul Server DNS IP")
	c.flagSet.StringVar(&c.flagKubeconfig, "kubeconfig", defaultKubeconfig, "Name of the kubernetes config file")
	c.flagSet.StringVar(&c.flagLogLevel, "log-level", "info",
		"Log verbosity level. Supported values (in order of detail) are \"trace\", "+
			"\"debug\", \"info\", \"warn\", and \"error\".")
	c.flagSet.BoolVar(&c.flagLogJSON, "log-json", false, "Enable or disable JSON output format for logging.")
	c.flagSet.BoolVar(&c.flagMultus, "multus", false, "If the plugin is a multus plugin (default = false)")

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

	// Create the CNI Config from command flags.
	cfg := &config.CNIConfig{
		Name:       pluginName,
		Type:       pluginType,
		CNIBinDir:  c.flagCNIBinDir,
		CNINetDir:  c.flagCNINetDir,
		DNSPrefix:  c.flagDNSPrefix,
		Kubeconfig: c.flagKubeconfig,
		LogLevel:   c.flagLogLevel,
		Multus:     c.flagMultus,
	}

	c.logger.Info("Running CNI install with configuration",
		"name", cfg.Name,
		"type", cfg.Type,
		"cni_bin_dir", cfg.CNIBinDir,
		"cni_net_dir", cfg.CNINetDir,
		"multus", cfg.Multus,
		"kubeconfig", cfg.Kubeconfig,
		"log_level", cfg.LogLevel)

	ctx, cancel := context.WithCancel(context.Background())
	go func(sigChan chan os.Signal, cancel context.CancelFunc) {
		<-sigChan
		cancel()
	}(c.sigCh, cancel)

	// Generate the kubeconfig file that will be used by the plugin to communicate with the kubernetes api.
	err := createKubeConfig(cfg.CNINetDir, cfg.Kubeconfig)
	if err != nil {
		c.UI.Error(err.Error())
		return 1
	}

	// Copy the consul-cni binary from the installer container to the host.
	srcFile := filepath.Join(c.flagCNIBinSourceDir, consulCNIBinName)
	err = copyFile(srcFile, cfg.CNIBinDir)
	if err != nil {
		c.UI.Error(err.Error())
		return 1
	}

	// Get the config file that is on the host.
	cfgFile, err := defaultCNIConfigFile(cfg.CNINetDir)
	if err != nil {
		c.UI.Error(err.Error())
		return 1
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
		err := validConfig(cfg, cfgFile)
		if err != nil {
			// The invalid config is not critical and we can recover from it.
			c.logger.Info("Installing plugin", "reason", err)
			err = appendCNIConfig(cfg, cfgFile)
			if err != nil {
				c.UI.Error(err.Error())
				return 1
			}
		}
	}

	// Watch for changes in the cniNetDir directory and fix/install the config file if need be.
	err = c.directoryWatcher(ctx, cfg, cfg.CNINetDir, cfgFile)
	if err != nil {
		c.UI.Error(err.Error())
		return 1
	}
	return 0
}

// cleanup removes the consul-cni configuration and kubeconfig file from cniNetDir and cniBinDir.
func (c *Command) cleanup(cfg *config.CNIConfig, cfgFile string) {
	var err error
	c.logger.Info("Shutdown received, cleaning up")
	if cfgFile != "" {
		err = removeCNIConfig(cfgFile)
		if err != nil {
			c.logger.Error("Unable to cleanup CNI Config: %v", err)
		}
	}

	kubeconfig := filepath.Join(cfg.CNINetDir, cfg.Kubeconfig)
	err = removeFile(kubeconfig)
	if err != nil {
		c.logger.Error("Unable to remove %s file: %v", kubeconfig, err)
	}
}

// directoryWatcher watches for changes in the cniNetDir forever. We watch the directory because there is a case where
// the installer could be the first cni plugin installed and we need to wait for another plugin to show up. Once
// installed we watch for changes, verify that our plugin installation is valid and re-install the consul-cni config.
func (c *Command) directoryWatcher(ctx context.Context, cfg *config.CNIConfig, dir, cfgFile string) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("could not create watcher: %v", err)
	}

	c.logger.Info("Creating directory watcher for", "directory", dir)
	err = watcher.Add(dir)
	if err != nil {
		return fmt.Errorf("could not watch %s directory: %v", dir, err)
	}
	//}

	// Cannot do "_ = defer watcher.Close()".
	defer func() {
		_ = watcher.Close()
	}()

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
				c.logger.Debug("Modified event", "event", event)
				// Always get the config file that is on the host as we do not know if it was deleted
				// or not.
				cfgFile, err = defaultCNIConfigFile(dir)
				if err != nil {
					c.logger.Error("Unable get default config file", "error", err)
					return err
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
					}
				}
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				c.logger.Error("Event watcher event is not ok", "error", err)
			}
		case <-ctx.Done():
			c.cleanup(cfg, cfgFile)
			return nil
		}
	}
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
