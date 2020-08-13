package v1alpha1

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

	// ParsedFromCheck is set if this path was parsed from a registered check
	ParsedFromCheck bool `json:"parsedFromCheck,omitempty"`
}
