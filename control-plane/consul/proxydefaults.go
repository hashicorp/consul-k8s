package consul

import (
	"fmt"
	"strings"

	capi "github.com/hashicorp/consul/api"
)

// fetches the global proxy-defaults config from consul and checks if access logs are enabled.
// If enabled and of file type, it returns the access log path to be used for creating volume mount.
// Returns nil if proxy-defaults not found.
func FetchProxyDefaultsFromConsul(config *Config, serverConnMgr ServerConnectionManager) (*capi.ProxyConfigEntry, error) {
	if config == nil {
		return nil, fmt.Errorf("consul config is not defined")
	}

	var consulClient *capi.Client
	var err error
	if serverConnMgr != nil {
		consulClient, err = NewClientFromConnMgr(config, serverConnMgr)
		if err != nil {
			return nil, fmt.Errorf("unable to connect with consul client %s", err)
		}
	} else {
		consulClient, err = NewClient(config.APIClientConfig, config.APITimeout)
		if err != nil {
			return nil, fmt.Errorf("unable to connect with consul client %s", err)
		}
	}

	cfgEntry, _, err := consulClient.ConfigEntries().Get(capi.ProxyDefaults, capi.ProxyConfigGlobal, nil)
	if err != nil && !strings.Contains(err.Error(), "404") {
		return nil, fmt.Errorf("error fetching global proxy-defaults: %s", err)
	}

	// If proxy-defaults not found, return empty string.
	if err != nil && strings.Contains(err.Error(), "404") {
		return nil, nil
	}

	proxyDefaults, ok := cfgEntry.(*capi.ProxyConfigEntry)
	if !ok {
		return nil, fmt.Errorf("unexpected type for proxy-defaults: %T", cfgEntry)
	}

	return proxyDefaults, nil
}
