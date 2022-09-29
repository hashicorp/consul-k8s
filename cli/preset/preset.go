package preset

import (
	"fmt"
)

const (
	PresetSecure     = "secure"
	PresetQuickstart = "quickstart"
	PresetCloud      = "cloud"
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
