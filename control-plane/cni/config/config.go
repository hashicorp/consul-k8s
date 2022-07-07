package config

// CNIConfig is the configuration that both the CNI installer and plugin will use.
type CNIConfig struct {
	// Name of the plugin
	Name string `json:"name"        mapstructure:"name"`
	// Type of plugin (consul-cni)
	Type string `json:"type"        mapstructure:"type"`
	// CNIBinDir is the location of the cni config files on the node. Can bet as a cli flag.
	CNIBinDir string `json:"cni_bin_dir" mapstructure:"cni_bin_dir"`
	// CNINetDir is the locaion of the cni plugin on the node. Can be set as a cli flag.
	CNINetDir string `json:"cni_net_dir" mapstructure:"cni_net_dir"`
	// DNSPrefix is used to determine the Consul Server DNS IP. The IP is set as an environment variable and the prefix allows us
	// to search for it
	DNSPrefix string `json:"dns_prefix" mapstructure:"dns_prefix"`
	// Kubeconfig file name. Can be set as a cli flag.
	Kubeconfig string `json:"kubeconfig"  mapstructure:"kubeconfig"`
	// LogLevl is the logging level. Can be set as a cli flag.
	LogLevel string `json:"log_level"   mapstructure:"log_level"`
	// Multus is if the plugin is a multus plugin. Can be set as a cli flag.
	Multus bool `json:"multus"      mapstructure:"multus"`
}
