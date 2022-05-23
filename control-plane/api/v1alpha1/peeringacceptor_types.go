package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

func init() {
	SchemeBuilder.Register(&PeeringAcceptor{}, &PeeringAcceptorList{})
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// PeeringAcceptor is the Schema for the peeringacceptors API.
type PeeringAcceptor struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PeeringAcceptorSpec   `json:"spec,omitempty"`
	Status PeeringAcceptorStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// PeeringAcceptorList contains a list of PeeringAcceptor.
type PeeringAcceptorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PeeringAcceptor `json:"items"`
}

// PeeringAcceptorSpec defines the desired state of PeeringAcceptor.
type PeeringAcceptorSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Peer describes the information needed to create a peering.
	Peer *Peer `json:"peer"`
}

type Peer struct {
	Secret *Secret `json:"secret,omitempty"`
}

type Secret struct {
	Name    string `json:"name,omitempty"`
	Key     string `json:"key,omitempty"`
	Backend string `json:"backend,omitempty"`
}

// PeeringAcceptorStatus defines the observed state of PeeringAcceptor.
type PeeringAcceptorStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// LastReconcileTime is the last time the resource was reconciled.
	// +optional
	LastReconcileTime *metav1.Time          `json:"lastReconcileTime,omitempty" description:"last time the resource was reconciled"`
	ReconcileError    *ReconcileErrorStatus `json:"reconcileError,omitempty"`
	Secret            *SecretStatus         `json:"secret,omitempty"`
}

type ReconcileErrorStatus struct {
	Error   *bool   `json:"error,omitempty"`
	Message *string `json:"message,omitempty"`
}

type SecretStatus struct {
	// TODO(peering): add additional status fields
	Name       string `json:"name,omitempty"`
	Key        string `json:"key,omitempty"`
	Backend    string `json:"backend,omitempty"`
	LatestHash string `json:"latestHash,omitempty"`
}
