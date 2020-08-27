package v1alpha1

import (
	capi "github.com/hashicorp/consul/api"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ServiceDefaultsSpec defines the desired state of ServiceDefaults
type ServiceDefaultsSpec struct {
	Protocol    string            `json:"protocol,omitempty"`
	MeshGateway MeshGatewayConfig `json:"meshGateway,omitempty"`
	Expose      ExposeConfig      `json:"expose,omitempty"`
	ExternalSNI string            `json:"externalSNI,omitempty"`
}

// ServiceDefaultsStatus defines the observed state of ServiceDefaults
type ServiceDefaultsStatus struct {
	Status `json:",inline"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// ServiceDefaults is the Schema for the servicedefaults API
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.conditions[?(@.type==\"Synced\")].status",description="The sync status of the resource with Consul"
type ServiceDefaults struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ServiceDefaultsSpec   `json:"spec,omitempty"`
	Status ServiceDefaultsStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ServiceDefaultsList contains a list of ServiceDefaults
type ServiceDefaultsList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ServiceDefaults `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ServiceDefaults{}, &ServiceDefaultsList{})
}

// ToConsul converts the entry into it's Consul equivalent struct.
func (s *ServiceDefaults) ToConsul() *capi.ServiceConfigEntry {
	return &capi.ServiceConfigEntry{
		Kind: capi.ServiceDefaults,
		Name: s.Name,
		//Namespace: s.Namespace, // todo: don't set this unless enterprise
		Protocol:    s.Spec.Protocol,
		MeshGateway: s.Spec.MeshGateway.toConsul(),
		Expose:      s.Spec.Expose.toConsul(),
		ExternalSNI: s.Spec.ExternalSNI,
	}
}

// MatchesConsul returns true if entry has the same config as this struct.
func (s *ServiceDefaults) MatchesConsul(entry *capi.ServiceConfigEntry) bool {
	return s.Name == entry.GetName() &&
		s.Spec.Protocol == entry.Protocol &&
		s.Spec.MeshGateway.Mode == string(entry.MeshGateway.Mode) &&
		s.Spec.Expose.matches(entry.Expose) &&
		s.Spec.ExternalSNI == entry.ExternalSNI
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

// matches returns true if the expose config of the entry is the same as the struct
func (e ExposeConfig) matches(expose capi.ExposeConfig) bool {
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

// toConsul returns the ExposeConfig for the entry
func (e ExposeConfig) toConsul() capi.ExposeConfig {
	var paths []capi.ExposePath
	for _, path := range e.Paths {
		paths = append(paths, capi.ExposePath{
			ListenerPort:  path.ListenerPort,
			Path:          path.Path,
			LocalPathPort: path.LocalPathPort,
			Protocol:      path.Protocol,
		})
	}
	return capi.ExposeConfig{
		Checks: e.Checks,
		Paths:  paths,
	}
}
