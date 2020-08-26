package v1alpha1

import (
	capi "github.com/hashicorp/consul/api"
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

	// MeshGatewayModeLocal represents that the Upstrea Connect connections
	// should be made to a mesh gateway in the local datacenter. This is
	MeshGatewayModeLocal MeshGatewayMode = "local"

	// MeshGatewayModeRemote represents that the Upstream Connect connections
	// should be made to a mesh gateway in a remote datacenter.
	MeshGatewayModeRemote MeshGatewayMode = "remote"
)

// MeshGatewayConfig controls how Mesh Gateways are used for upstream Connect
// services
type MeshGatewayConfig struct {
	// Mode is the mode that should be used for the upstream connection.
	Mode string `json:"mode,omitempty"`
}

//ToConsul returns the MeshGatewayConfig for the entry
func (m MeshGatewayConfig) ToConsul() capi.MeshGatewayConfig {
	switch m.Mode {
	case string(capi.MeshGatewayModeLocal):
		return capi.MeshGatewayConfig{
			Mode: capi.MeshGatewayModeLocal,
		}
	case string(capi.MeshGatewayModeNone):
		return capi.MeshGatewayConfig{
			Mode: capi.MeshGatewayModeNone,
		}
	case string(capi.MeshGatewayModeRemote):
		return capi.MeshGatewayConfig{
			Mode: capi.MeshGatewayModeRemote,
		}
	default:
		return capi.MeshGatewayConfig{
			Mode: capi.MeshGatewayModeDefault,
		}
	}
}

// ExposeConfig describes HTTP paths to expose through Envoy outside of Connect.
// Users can expose individual paths and/or all HTTP/GRPC paths for checks.
type ExposeConfig struct {
	// Checks defines whether paths associated with Consul checks will be exposed.
	// This flag triggers exposing all HTTP and GRPC check paths registered for the service.
	Checks bool `json:"checks,omitempty"`

	// Paths is the list of paths exposed through the proxy.
	Paths []ExposePath `json:"paths,omitempty"`
}

type ExposePath struct {
	// ListenerPort defines the port of the proxy's listener for exposed paths.
	ListenerPort int `json:"listenerPort,omitempty"`

	// Path is the path to expose through the proxy, ie. "/metrics."
	Path string `json:"path,omitempty"`

	// LocalPathPort is the port that the service is listening on for the given path.
	LocalPathPort int `json:"localPathPort,omitempty"`

	// Protocol describes the upstream's service protocol.
	// Valid values are "http" and "http2", defaults to "http"
	Protocol string `json:"protocol,omitempty"`
}

//Matches returns true if the expose config of the entry is the same as the struct
func (e ExposeConfig) Matches(expose capi.ExposeConfig) bool {
	if e.Checks != expose.Checks {
		return false
	}

	if len(e.Paths) != len(expose.Paths) {
		return false
	}

	for _, path := range e.Paths {
		found := false
		for _, entryPath := range expose.Paths {
			if path.Protocol == entryPath.Protocol &&
				path.Path == entryPath.Path &&
				path.ListenerPort == entryPath.ListenerPort &&
				path.LocalPathPort == entryPath.LocalPathPort {
				found = true
				break
			}
		}

		if !found {
			return false
		}
	}
	return true
}

//ToConsul returns the ExposeConfig for the entry
func (e ExposeConfig) ToConsul() capi.ExposeConfig {
	return capi.ExposeConfig{
		Checks: e.Checks,
		Paths:  e.parseExposePath(),
	}
}

func (e ExposeConfig) parseExposePath() []capi.ExposePath {
	var paths []capi.ExposePath
	for _, path := range e.Paths {
		paths = append(paths, capi.ExposePath{
			ListenerPort:  path.ListenerPort,
			Path:          path.Path,
			LocalPathPort: path.LocalPathPort,
			Protocol:      path.Protocol,
		})
	}
	return paths
}
