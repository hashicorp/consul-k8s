package v1alpha1

import (
	"fmt"
	"strings"

	capi "github.com/hashicorp/consul/api"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

type MeshGatewayMode string

const (
	// MeshGatewayModeDefault represents no specific mode and should
	// be used to indicate that a different layer of the configuration
	// chain should take precedence
	MeshGatewayModeDefault MeshGatewayMode = ""

	// MeshGatewayModeNone represents that the Upstream Connect connections
	// should be direct and not flow through a mesh gateway.
	MeshGatewayModeNone MeshGatewayMode = "none"

	// MeshGatewayModeLocal represents that the Upstream Connect connections
	// should be made to a mesh gateway in the local datacenter.
	MeshGatewayModeLocal MeshGatewayMode = "local"

	// MeshGatewayModeRemote represents that the Upstream Connect connections
	// should be made to a mesh gateway in a remote datacenter.
	MeshGatewayModeRemote MeshGatewayMode = "remote"
)

// MeshGatewayConfig controls how Mesh Gateways are used for upstream Connect
// services
type MeshGatewayConfig struct {
	// Mode is the mode that should be used for the upstream connection.
	// One of none, local, or remote.
	Mode string `json:"mode,omitempty"`
}

// toConsul returns the MeshGatewayConfig for the entry
func (m MeshGatewayConfig) toConsul() capi.MeshGatewayConfig {
	mode := capi.MeshGatewayMode(m.Mode)
	switch mode {
	case capi.MeshGatewayModeLocal, capi.MeshGatewayModeRemote, capi.MeshGatewayModeNone:
		return capi.MeshGatewayConfig{
			Mode: mode,
		}
	default:
		return capi.MeshGatewayConfig{
			Mode: capi.MeshGatewayModeDefault,
		}
	}
}

func (m MeshGatewayConfig) validate() *field.Error {
	modes := []string{"remote", "local", "none", ""}
	if !sliceContains(modes, m.Mode) {
		return field.Invalid(field.NewPath("spec").Child("meshGateway").Child("mode"), m.Mode, notInSliceMessage(modes))
	}
	return nil
}

func notInSliceMessage(slice []string) string {
	return fmt.Sprintf(`must be one of "%s"`, strings.Join(slice, `", "`))
}

func sliceContains(slice []string, entry string) bool {
	for _, s := range slice {
		if entry == s {
			return true
		}
	}
	return false
}
