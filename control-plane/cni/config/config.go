// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package config

const (
	DefaultPluginName           = "consul-cni"
	DefaultPluginType           = "consul-cni"
	DefaultCNIBinDir            = "/opt/cni/bin"
	DefaultCNINetDir            = "/etc/cni/net.d"
	DefaultCNITokenDir          = "/var/run/secrets/kubernetes.io/serviceaccount"
	DefaultCNITokenFilename     = "token"
	DefaultCNIHostTokenFilename = "cni-host-token"
	DefaultMultus               = false
	// defaultKubeconfig is named ZZZ-.. as part of a convention that other CNI plugins use.
	DefaultKubeconfig      = "ZZZ-consul-cni-kubeconfig"
	DefaultLogLevel        = "info"
	DefaultAutorotateToken = false
)

// CNIConfig is the configuration that both the CNI installer and plugin will use.
type CNIConfig struct {
	// Name of the plugin.
	Name string `json:"name"        mapstructure:"name"`
	// Type of plugin (consul-cni).
	Type string `json:"type"        mapstructure:"type"`
	// CNIBinDir is the location of the cni config files on the node. Can bet as a cli flag.
	CNIBinDir string `json:"cni_bin_dir" mapstructure:"cni_bin_dir"`
	// CNINetDir is the locaion of the cni plugin on the node. Can be set as a cli flag.
	CNINetDir string `json:"cni_net_dir" mapstructure:"cni_net_dir"`
	// Kubeconfig file name. Can be set as a cli flag.
	Kubeconfig string `json:"kubeconfig"  mapstructure:"kubeconfig"`
	// CNITokenPath is the location of the Service Account tokenfile path to pick for api-server auth
	CNITokenPath string `json:"cni_token_path" mapstructure:"cni_token_path"`
	// CNIHostTokenPath is the location of the Service Account tokenfile path on the host to pick for api-server auth
	CNIHostTokenPath string `json:"cni_host_token_path" mapstructure:"cni_host_token_path"`
	// EnableTokenAutorotate is if the plugin should use the token autorotate feature. Can be set as a cli flag.
	AutorotateToken bool `json:"autorotate_token" mapstructure:"autorotate_token"`
	// LogLevel is the logging level. Can be set as a cli flag.
	LogLevel string `json:"log_level"   mapstructure:"log_level"`
	// Multus is if the plugin is a multus plugin. Can be set as a cli flag.
	Multus bool `json:"multus"      mapstructure:"multus"`
}

func NewDefaultCNIConfig() *CNIConfig {
	return &CNIConfig{
		Name:             DefaultPluginName,
		Type:             DefaultPluginType,
		CNIBinDir:        DefaultCNIBinDir,
		CNINetDir:        DefaultCNINetDir,
		Kubeconfig:       DefaultKubeconfig,
		CNITokenPath:     DefaultCNITokenDir + "/" + DefaultCNITokenFilename,
		CNIHostTokenPath: DefaultCNIBinDir + "/" + DefaultCNIHostTokenFilename,
		AutorotateToken:  DefaultAutorotateToken,
		LogLevel:         DefaultLogLevel,
		Multus:           DefaultMultus,
	}
}
