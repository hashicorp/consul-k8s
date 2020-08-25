package v1alpha1

import (
	consulapi "github.com/hashicorp/consul/api"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

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

func (s *ServiceDefaults) ToConsul() *consulapi.ServiceConfigEntry {
	return &consulapi.ServiceConfigEntry{
		Kind: consulapi.ServiceDefaults,
		Name: s.Name,
		//Namespace: s.Namespace, // todo: don't set this unless enterprise
		Protocol: s.Spec.Protocol,
		MeshGateway: consulapi.MeshGatewayConfig{
			Mode: s.gatewayMode(),
		},
		Expose: consulapi.ExposeConfig{
			Checks: s.Spec.Expose.Checks,
			Paths:  s.parseExposePath(),
		},
		ExternalSNI: s.Spec.ExternalSNI,
	}
}

func (s *ServiceDefaults) parseExposePath() []consulapi.ExposePath {
	var paths []consulapi.ExposePath
	for _, path := range s.Spec.Expose.Paths {
		paths = append(paths, consulapi.ExposePath{
			ListenerPort:    path.ListenerPort,
			Path:            path.Path,
			LocalPathPort:   path.LocalPathPort,
			Protocol:        path.Protocol,
			ParsedFromCheck: path.ParsedFromCheck,
		})
	}
	return paths
}

func (s *ServiceDefaults) gatewayMode() consulapi.MeshGatewayMode {
	switch s.Spec.MeshGateway.Mode {
	case "local":
		return consulapi.MeshGatewayModeLocal
	case "none":
		return consulapi.MeshGatewayModeNone
	case "remote":
		return consulapi.MeshGatewayModeRemote
	default:
		return consulapi.MeshGatewayModeDefault
	}
}

// MatchesConsul returns true if entry has the same config as this struct.
func (s *ServiceDefaults) MatchesConsul(entry *consulapi.ServiceConfigEntry) bool {
	matches := s.Name == entry.GetName() &&
		s.Spec.Protocol == entry.Protocol &&
		s.Spec.MeshGateway.Mode == string(entry.MeshGateway.Mode) &&
		s.Spec.Expose.Matches(entry.Expose) &&
		s.Spec.ExternalSNI == entry.ExternalSNI
	if !matches {
		return false
	}
	return true
}
