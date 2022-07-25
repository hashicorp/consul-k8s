package installcni

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"

	"github.com/containernetworking/cni/libcni"
	"github.com/hashicorp/consul-k8s/control-plane/cni/config"
	"github.com/mitchellh/mapstructure"
)

// defaultCNIConfigFile gets the the correct config file from the cni net dir.
// Adapted from kubelet: https://github.com/kubernetes/kubernetes/blob/954996e231074dc7429f7be1256a579bedd8344c/pkg/kubelet/dockershim/network/cni/cni.go#L134.
func defaultCNIConfigFile(dir string) (string, error) {
	files, err := libcni.ConfFiles(dir, []string{".conf", ".conflist", ".json"})
	switch {
	case err != nil:
		// A real error has been found.
		return "", fmt.Errorf("error while trying to find files in %s: %v", dir, err)
	case len(files) == 0:
		// No config files have shown up yet and it is ok to run this function again.
		return "", nil
	}

	sort.Strings(files)
	for _, confFile := range files {
		var confList *libcni.NetworkConfigList
		if strings.HasSuffix(confFile, ".conflist") {
			confList, err = libcni.ConfListFromFile(confFile)
			if err != nil {
				// Error loading CNI config list file.
				continue
			}
		} else {
			conf, err := libcni.ConfFromFile(confFile)
			if err != nil {
				// Error loading CNI config file.
				continue
			}
			// Ensure the config has a "type" so we know what plugin to run.
			// Also catches the case where somebody put a conflist into a conf file.
			if conf.Network.Type == "" {
				// Error loading CNI config file: no 'type'.
				continue
			}

			confList, err = libcni.ConfListFromConf(conf)
			if err != nil {
				// Error converting CNI config file to list.
				continue
			}
		}
		if len(confList.Plugins) == 0 {
			// CNI config list has no networks, skipping".
			continue
		}

		return confFile, nil
	}
	// There were files but none of them were valid
	return "", fmt.Errorf("no valid config files found in %s", dir)
}

// The format of the main cni config file is unstructured json consisting of a header and list of plugins
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
func appendCNIConfig(consulCfg *config.CNIConfig, cfgFile string) error {
	// Read the config file and convert it to a map.
	cfgMap, err := configFileToMap(cfgFile)
	if err != nil {
		return fmt.Errorf("could not convert config file to map: %v", err)
	}

	// Get the 'plugins' map embedded inside of the exisingMap.
	plugins, err := pluginsFromMap(cfgMap)
	if err != nil {
		return err
	}

	// Check to see if 'type: consul-cni' already exists and remove it before appending.
	// This can happen in a CrashLoop and we prevents many duplicate entries in the config file.
	for i, p := range plugins {
		plugin, ok := p.(map[string]interface{})
		if !ok {
			return fmt.Errorf("error reading plugin from plugin list")
		}
		if plugin["type"] == "consul-cni" {
			plugins = append(plugins[:i], plugins[i+1:]...)
			break
		}
	}

	// Take the consul cni config object and convert it to a map so that we can use it with the other maps.
	consulMap, err := consulMapFromConfig(consulCfg)
	if err != nil {
		return fmt.Errorf("error converting consul config into map: %v", err)
	}

	// Append the consul-cni map to the already existing plugins.
	cfgMap["plugins"] = append(plugins, consulMap)

	// Marshal into a new json object
	cfgJSON, err := json.MarshalIndent(cfgMap, "", "  ")
	if err != nil {
		return fmt.Errorf("error marshalling existing CNI config: %v", err)
	}

	// Libcni nuance/bug. If the newline is missing, the cni plugin will throw errors saying that it cannot get parse the config.
	cfgJSON = append(cfgJSON, "\n"...)

	// Write the file out.
	err = os.WriteFile(cfgFile, cfgJSON, os.FileMode(0o644))
	if err != nil {
		return fmt.Errorf("error writing config file %s: %v", cfgFile, err)
	}
	return nil
}

// configFileToMap takes an unstructure JSON config file and converts it into a map.
func configFileToMap(cfgFile string) (map[string]interface{}, error) {
	if _, err := os.Stat(cfgFile); os.IsNotExist(err) {
		return nil, fmt.Errorf("config file %s does not exist: %v", cfgFile, err)
	}

	// Read the main config file.
	cfgBytes, err := os.ReadFile(cfgFile)
	if err != nil {
		return nil, fmt.Errorf("could not read file %s: %v", cfgFile, err)
	}

	// Convert the json config file into a map. The map that is created has 2 parts:
	// [0] the cni header
	// [1] the plugins
	var cfgMap map[string]interface{}
	err = json.Unmarshal(cfgBytes, &cfgMap)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling existing config file %s: %v", cfgFile, err)
	}
	return cfgMap, nil
}

// pluginsFromMap takes an unmarshalled config JSON map, return the plugin list asserted as a []interface{}.
func pluginsFromMap(cfgMap map[string]interface{}) ([]interface{}, error) {
	plugins, ok := cfgMap["plugins"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("error getting plugins from config map")
	}
	return plugins, nil
}

// consulMapFromConfig converts the consul CNI config into a map.
func consulMapFromConfig(consulCfg *config.CNIConfig) (map[string]interface{}, error) {
	var consulMap map[string]interface{}
	err := mapstructure.Decode(consulCfg, &consulMap)
	if err != nil {
		return nil, fmt.Errorf("error decoding consul config into a map: %v", err)
	}
	return consulMap, nil
}

// removeCNIConfig removes the consul-cni config from the CNI config file. Used as part of cleanup.
func removeCNIConfig(cfgFile string) error {
	// Read the config file and convert it to a map.
	cfgMap, err := configFileToMap(cfgFile)
	if err != nil {
		return fmt.Errorf("could not convert config file to map: %v", err)
	}

	// Get the 'plugins' map embedded inside of the exisingMap.
	plugins, err := pluginsFromMap(cfgMap)
	if err != nil {
		return err
	}

	// Find the 'consul-cni' plugin and remove it.
	for i, p := range plugins {
		plugin, ok := p.(map[string]interface{})
		if !ok {
			return fmt.Errorf("error reading plugin from plugin list")
		}
		if plugin["type"] == "consul-cni" {
			cfgMap["plugins"] = append(plugins[:i], plugins[i+1:]...)
			break
		}
	}

	// Marshal into a new json file.
	cfgJSON, err := json.MarshalIndent(cfgMap, "", "  ")
	if err != nil {
		return fmt.Errorf("error marshalling existing CNI config: %v", err)
	}

	cfgJSON = append(cfgJSON, "\n"...)

	// Write the file out.
	err = os.WriteFile(cfgFile, cfgJSON, os.FileMode(0o644))
	if err != nil {
		return fmt.Errorf("error writing config file %s: %v", cfgFile, err)
	}
	return nil
}

// validConfig validates that the consul-cni config exists in the config file and it is valid. It should be the
// last plugin in the plugin chain.
func validConfig(cfg *config.CNIConfig, cfgFile string) error {
	// Convert the config file into a map.
	cfgMap, err := configFileToMap(cfgFile)
	if err != nil {
		return fmt.Errorf("could not convert config file to map: %v", err)
	}

	// Get the 'plugins' map embedded inside of the exisingMap.
	plugins, err := pluginsFromMap(cfgMap)
	if err != nil {
		return err
	}

	// Create an empty config so that we can populate it if found.
	existingCfg := &config.CNIConfig{}
	// Find the 'consul-cni' plugin in the list of plugins.
	found := false
	num_plugins := len(plugins)
	for i, p := range plugins {
		plugin, ok := p.(map[string]interface{})
		if !ok {
			return fmt.Errorf("error reading plugin from plugin list")
		}
		if plugin["type"] == "consul-cni" {
			// Populate existingCfg with the consul-cni plugin info so that we can compare it with what
			// is expected.
			err := mapstructure.Decode(plugin, &existingCfg)
			if err != nil {
				return fmt.Errorf("error decoding consul config into a map: %v", err)
			}
			found = true
			// Check to see that consul-cni plugin is the last plugin in the chain.
			if !(num_plugins-1 == i) {
				return fmt.Errorf("consul-cni config is not the last plugin in plugin chain")
			}
			break
		}
	}

	if !found {
		return fmt.Errorf("consul-cni config missing from config file")
	}

	// Compare the config that is passed to the installer to what is in the config file. There could be a
	// difference if the config was corrupted or during a helm update or upgrade.
	equal := reflect.DeepEqual(existingCfg, cfg)
	if !equal {
		return fmt.Errorf("consul-cni config has changed")
	}

	return nil
}
