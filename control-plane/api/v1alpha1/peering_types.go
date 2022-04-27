package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// PeeringSpec defines the desired state of Peering
type PeeringSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Foo is an example field of Peering. Edit peering_types.go to remove/update
	Foo string `json:"foo,omitempty"`
}

// PeeringStatus defines the observed state of Peering
type PeeringStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Peering is the Schema for the peerings API
type Peering struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PeeringSpec   `json:"spec,omitempty"`
	Status PeeringStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// PeeringList contains a list of Peering
type PeeringList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Peering `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Peering{}, &PeeringList{})
}
