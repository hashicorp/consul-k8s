package installcni

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/containernetworking/cni/libcni"
	"github.com/hashicorp/consul-k8s/control-plane/cni/config"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/common"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/flags"
	"github.com/hashicorp/go-hclog"
	"github.com/mitchellh/cli"
	"github.com/mitchellh/mapstructure"
)

const (
	pluginName       = "consul-cni"
	pluginType       = "consul-cni"
	defaultCNIBinDir = "/opt/cni/bin"
	defaultCNINetDir = "/etc/cni/net.d"
	defaultMultus    = false
	// defaultKubeconfig is named ZZZ-.. as part of a convention that other CNI plugins use.
	defaultKubeconfig      = "ZZZZ-consul-cni-kubeconfig"
	defaultLogLevel        = "info"
	defaultCNIBinSourceDir = "/bin"
	consulCNIBinName       = "consul-cni"
)

// TODO: Add description that explains the difference between CNIConfig and installConfig

// installConfig are the values by the installer when running inside a container.
type installConfig struct {
	// MountedCNIBinDir is the location of config files for the installer to use .
	MountedCNIBinDir string
	// MountedCNINetDir is location of the cni binaries for the installer to use.
	MountedCNINetDir string
	// CNIBinSourceDir is the location of the consul-cni binary from inside the installer container. Where
	// the binaries are copied to during consul-k8s docker build.
	CNIBinSourceDir string
}

// Command flags and structure.
type Command struct {
	UI cli.Ui

	// flagCNIBinDir is the location on the host of the consul-cni binary
	flagCNIBinDir string
	// flagCNINetDir is the location on the host of cni configuration
	flagCNINetDir string
	// flagMultus is a boolean flag for multus support.
	flagMultus bool
	// flageKubeconfig is the filename of the generated kubeconfig that the plugin will use to communicate with the kubernetes api
	flagKubeconfig string
	// flagCNIBinSourceDir is the location of consul-cni binary inside the installer container (/bin)
	flagCNIBinSourceDir string
	// flagLogLevel is the logging level
	flagLogLevel string
	// flagLogJson is a boolean flag for json logging  format
	flagLogJSON bool

	flagSet *flag.FlagSet

	once   sync.Once
	help   string
	logger hclog.Logger
}

func (c *Command) init() {
	c.flagSet = flag.NewFlagSet("", flag.ContinueOnError)
	c.flagSet.StringVar(&c.flagCNIBinDir, "cni-bin-dir", defaultCNIBinDir, "Location of CNI plugin binaries.")
	c.flagSet.StringVar(
		&c.flagCNINetDir,
		"cni-net-dir",
		defaultCNINetDir,
		"Location to write the CNI plugin configuration.",
	)
	c.flagSet.StringVar(
		&c.flagCNIBinSourceDir,
		"bin-source-dir",
		defaultCNIBinSourceDir,
		"Host location to copy the binary from",
	)
	c.flagSet.StringVar(&c.flagKubeconfig, "kubeconfig", defaultKubeconfig, "Name of the kubernetes config file")
	c.flagSet.BoolVar(&c.flagMultus, "multus", false, "If the plugin is a multus plugin (default = false)")
	c.flagSet.StringVar(
		&c.flagLogLevel,
		"log-level",
		"debug",
		"Log verbosity level. Supported values (in order of detail) are \"trace\", "+
			"\"debug\", \"info\", \"warn\", and \"error\".",
	)
	c.flagSet.BoolVar(&c.flagLogJSON, "log-json", false, "Enable or disable JSON output format for logging.")

	c.help = flags.Usage(help, c.flagSet)
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

	// Create the CNI Config from command flags
	cfg := &config.CNIConfig{
		Name:       pluginName,
		Type:       pluginType,
		CNIBinDir:  c.flagCNIBinDir,
		CNINetDir:  c.flagCNINetDir,
		Multus:     c.flagMultus,
		Kubeconfig: c.flagKubeconfig,
		LogLevel:   c.flagLogLevel,
	}

	// TODO: Validate config, especially log level

	c.logger.Info("Running CNI install with configuration",
		"name", cfg.Name,
		"type", cfg.Type,
		"cni_bin_dir", cfg.CNIBinDir,
		"cni_net_dir", cfg.CNINetDir,
		"multus", cfg.Multus,
		"kubeconfig", cfg.Kubeconfig,
		"log_level", cfg.LogLevel)

	// Create the install Config for working with files
	install := &installConfig{
		MountedCNIBinDir: "/host" + c.flagCNIBinDir,
		MountedCNINetDir: "/host" + c.flagCNINetDir,
		CNIBinSourceDir:  c.flagCNIBinSourceDir,
	}

	// Get the config file that is on the host
	srcFileName, err := defaultCNIConfigFile(install.MountedCNINetDir, c.logger)
	if err != nil {
		c.logger.Error("Unable get default config file", "error", err)
		return 1
	}

	// Get the dest file we will write to (the name can change)
	destFileName, err := destConfigFile(srcFileName, c.logger)
	if err != nil {
		c.logger.Error("Unable get destination config file", "error", err)
		return 1
	}

	// Get the correct mounted file paths from inside the container
	absSrcFilePath := filepath.Join(install.MountedCNINetDir, srcFileName)
	absDestFilePath := filepath.Join(install.MountedCNINetDir, destFileName)

	// Append the consul configuration to the config that is there
	err = appendCNIConfig(cfg, absSrcFilePath, absDestFilePath, c.logger)
	if err != nil {
		c.logger.Error("Unable add the consul-cni config to the config file", "error", err)
		return 1
	}

	// Generate the kubeconfig file that will be used by the plugin to communicate with the kubernetes api
	err = createKubeConfig(install.MountedCNINetDir, cfg.Kubeconfig, c.logger)
	if err != nil {
		c.logger.Error("Unable to create kubeconfig file", "error", err)
		return 1
	}

	// copy the consul-cni binary from the installer container to the host
	err = copyCNIBinary(install.CNIBinSourceDir, install.MountedCNIBinDir, c.logger)
	if err != nil {
		c.logger.Error("Unable to copy cni binary", "error", err)
		return 1
	}

	// TODO: Do not exit, should run in loop with file watcher
	return 0
}

//The format of the main cni config file is unstructured json consisting of a header and list of plugins
//
// {
//  "cniVersion": "0.3.1",
//  "name": "kindnet",
//  "plugins": [
//    {
//        <plugin 1>
//    },
//    {
//       <plugin 2>
//    }
//   ]
// }
// appendCNIConfig appends the consul-cni configuration to the main configuration file.
func appendCNIConfig(consulCfg *config.CNIConfig, srcFile, destFile string, logger hclog.Logger) error {
	// Check if file exists
	if _, err := os.Stat(srcFile); os.IsNotExist(err) {
		return fmt.Errorf("source cni config file %s does not exist: %v", srcFile, err)
	}
	logger.Debug("appendCNIConfig: using files", "srcFile", srcFile, "destFile", destFile)

	// Read the main config file
	existingCNIConfig, err := os.ReadFile(srcFile)
	if err != nil {
		return err
	}

	// Convert the json config file into a map. The map that is created has 2 parts:
	// [0] the cni header
	// [1] the plugins
	var existingMap map[string]interface{}
	err = json.Unmarshal(existingCNIConfig, &existingMap)
	if err != nil {
		return fmt.Errorf("error unmarshalling existing CNI config: %v", err)
	}

	// convert the consul-cni struct into a map
	var consulMap map[string]interface{}
	err = mapstructure.Decode(consulCfg, &consulMap)
	if err != nil {
		return fmt.Errorf("error loading Consul CNI config: %v", err)
	}

	// Get the 'plugins' map embedded inside of the exisingMap
	plugins, ok := existingMap["plugins"].([]interface{})
	if !ok {
		return fmt.Errorf("error reading plugin list from CNI config")
	}
	logger.Debug("appendCNIConfig: plugins are", "plugins", plugins)

	// Check to see if 'type: consul-cni' already exists and remove it before appending.
	// This can happen in a CrashLoop and we end up with many entries in the config file
	for i, p := range plugins {
		plugin, ok := p.(map[string]interface{})
		if !ok {
			return fmt.Errorf("error reading plugin from plugin list")
		}
		if plugin["type"] == "consul-cni" {
			logger.Debug("appendCNIConfig: found existing consul-cni config, removing it")
			plugins = append(plugins[:i], plugins[i+1:]...)
			break
		}
	}

	// Append the consul-cni map to the already existing plugins
	existingMap["plugins"] = append(plugins, consulMap)

	// Marshal into a new json file
	existingJSON, err := json.MarshalIndent(existingMap, "", "  ")
	if err != nil {
		return fmt.Errorf("error marshalling existing CNI config: %v", err)
	}

	// libcni nuance/bug. If the newline is missing, the cni plugin will throw errors saying that it cannot get parse the config
	existingJSON = append(existingJSON, "\n"...)

	// Write the file out
	err = os.WriteFile(destFile, existingJSON, os.FileMode(0o644))
	if err != nil {
		return fmt.Errorf("error writing config file %s: %v", destFile, err)
	}

	logger.Info("Appended CNI config to default config file", "name", destFile)
	return nil
}

// defaultCNIConfigFile gets the the correct config file from the cni net dir
// Adapted from kubelet: https://github.com/kubernetes/kubernetes/blob/954996e231074dc7429f7be1256a579bedd8344c/pkg/kubelet/dockershim/network/cni/cni.go#L134
func defaultCNIConfigFile(confDir string, logger hclog.Logger) (string, error) {
	files, err := libcni.ConfFiles(confDir, []string{".conf", ".conflist", ".json"})
	switch {
	case err != nil:
		return "", err
	case len(files) == 0:
		return "", fmt.Errorf("no networks found in %s", confDir)
	}

	sort.Strings(files)
	for _, confFile := range files {
		var confList *libcni.NetworkConfigList
		if strings.HasSuffix(confFile, ".conflist") {
			confList, err = libcni.ConfListFromFile(confFile)
			if err != nil {
				logger.Warn("Error loading CNI config list file", "file", confFile, "error", err)
				continue
			}
		} else {
			conf, err := libcni.ConfFromFile(confFile)
			if err != nil {
				logger.Warn("Error loading CNI config file", "file", confFile, "error", err)
				continue
			}
			// Ensure the config has a "type" so we know what plugin to run.
			// Also catches the case where somebody put a conflist into a conf file.
			if conf.Network.Type == "" {
				logger.Warn("Error loading CNI config file: no 'type'; perhaps this is a .conflist?", "file", confFile)
				continue
			}

			confList, err = libcni.ConfListFromConf(conf)
			if err != nil {
				logger.Warn("Error converting CNI config file to list", "error", err)
				continue
			}
		}
		if len(confList.Plugins) == 0 {
			logger.Warn("CNI config list has no networks, skipping", "file", confFile)
			continue
		}

		cFile := filepath.Base(confFile)
		logger.Info("Using CNI configuration file", "file", cFile)
		return cFile, nil
	}
	return "", fmt.Errorf("no valid networks found in %s", confDir)
}

// destConfigFile determines the name of the destination config file. The name depends on if the source is a .conf file or .conflist.
func destConfigFile(srcFile string, logger hclog.Logger) (string, error) {
	// TODO: There should be more checks here and the file name can change depending on the main
	// source file. The name will change from .conf to .conflist
	destFile := srcFile
	logger.Info("CNI configuration destination file", "name", destFile)
	return destFile, nil
}

// copyCNIBinary copies the cni plugin from inside the installer container to the host.
func copyCNIBinary(srcDir, destDir string, logger hclog.Logger) error {
	// If the src file does not exist then either the incorrect command line argument was used or
	// the docker container we built is broken somehow.
	logger.Info("Copying CNI binary", "name", consulCNIBinName, "source", srcDir, "dest", destDir)

	srcFile := filepath.Join(srcDir, consulCNIBinName)
	if _, err := os.Stat(srcFile); os.IsNotExist(err) {
		return fmt.Errorf("source cni binary %s does not exist: %v", srcFile, err)
	}

	// If the destDir does not exist then the incorrect command line argument was used or
	// the CNI settings for the kubelet are not correct
	if _, err := os.Stat(destDir); os.IsNotExist(err) {
		return fmt.Errorf("destination directory %s does not exist: %v", destDir, err)
	}

	srcBytes, err := os.ReadFile(srcFile)
	if err != nil {
		return fmt.Errorf("could not read %s file: %v", srcFile, err)
	}

	err = os.WriteFile(filepath.Join(destDir, consulCNIBinName), srcBytes, os.FileMode(0o755))
	if err != nil {
		return fmt.Errorf("error copying consul-cni binary to %s: %v", destDir, err)
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
