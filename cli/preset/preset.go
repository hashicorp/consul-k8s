// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package preset

import (
	"fmt"
	"os"
)

const (
	PresetSecure     = "secure"
	PresetQuickstart = "quickstart"
	PresetCloud      = "cloud"

	EnvHCPClientID     = "HCP_CLIENT_ID"
	EnvHCPClientSecret = "HCP_CLIENT_SECRET"
	EnvHCPAuthURL      = "HCP_AUTH_URL"
	EnvHCPAPIHost      = "HCP_API_HOST"
	EnvHCPScadaAddress = "HCP_SCADA_ADDRESS"
)

// Presets is a list of all the available presets for use with CLI's install
// and uninstall commands.
var Presets = []string{PresetCloud, PresetQuickstart, PresetSecure}

// Preset is the interface that each instance must implement.  For demo and
// secure presets, they merely return a pre-configred value map.  For cloud,
// it must fetch configuration from HCP, save various secrets from the response,
// and map the secret names into the value map.
type Preset interface {
	GetValueMap() (map[string]interface{}, error)
}

type GetPresetConfig struct {
	Name        string
	CloudPreset *CloudPreset
}

// GetPreset is a factory function that, given a configuration, produces a
// struct that implements the Preset interface based on the name in the
// configuration.  If the string is not recognized an error is returned.  This
// helper function is utilized by both the cli install and upgrade commands.
func GetPreset(config *GetPresetConfig) (Preset, error) {
	switch config.Name {
	case PresetCloud:
		return config.CloudPreset, nil
	case PresetQuickstart:
		return &QuickstartPreset{}, nil
	case PresetSecure:
		return &SecurePreset{}, nil
	}
	return nil, fmt.Errorf("'%s' is not a valid preset", config.Name)
}

func GetHCPPresetFromEnv(resourceID string) *HCPConfig {
	hcpConfig := &HCPConfig{
		ResourceID: resourceID,
	}

	// Read clientID from environment
	if clientID, ok := os.LookupEnv(EnvHCPClientID); ok {
		hcpConfig.ClientID = clientID
	}

	// Read clientSecret from environment
	if clientSecret, ok := os.LookupEnv(EnvHCPClientSecret); ok {
		hcpConfig.ClientSecret = clientSecret
	}

	// Read authURL from environment
	if authURL, ok := os.LookupEnv(EnvHCPAuthURL); ok {
		hcpConfig.AuthURL = authURL
	}

	// Read apiHost from environment
	if apiHost, ok := os.LookupEnv(EnvHCPAPIHost); ok {
		hcpConfig.APIHostname = apiHost
	}

	// Read scadaAddress from environment
	if scadaAddress, ok := os.LookupEnv(EnvHCPScadaAddress); ok {
		hcpConfig.ScadaAddress = scadaAddress
	}

	return hcpConfig
}
