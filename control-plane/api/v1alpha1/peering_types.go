package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

func init() {
	SchemeBuilder.Register(&Peering{}, &PeeringList{})
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Peering is the Schema for the peerings API
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of the resource"
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

// PeeringSpec defines the desired state of Peering
type PeeringSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Peer describes the information needed to create a peering.
	Peer *Peer `json:"peer"`
}

type Peer struct {
	Generator *Generator `json:"generator,omitempty"`
	Requester *Requester `json:"requester,omitempty"`
}
type Generator struct {
	Secret *Secret `json:"secret,omitempty"`
}
type Requester struct {
	Secret *Secret `json:"secret,omitempty"`
}
type Secret struct {
	Name    string `json:"name,omitempty"`
	Key     string `json:"key,omitempty"`
	Backend string `json:"backend,omitempty"`
}

// PeeringStatus defines the observed state of Peering
type PeeringStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// LastReconcileTime is the last time the resource was reconciled.
	// +optional
	LastReconcileTime *metav1.Time `json:"lastReconcileTime,omitempty" description:"last time the resource was reconciled"`
	Secret            *SecretStatus
}
type SecretStatus struct {
	// TODO(peering): add additional status fields
	Name       string                  `json:"name,omitempty"`
	Key        string                  `json:"key,omitempty"`
	Backend    string                  `json:"backend,omitempty"`
	LatestHash string                  `json:"latestHash,omitempty"`
	Kubernetes *KubernetesSecretStatus `json:"kubernetes,omitempty"`
}

type KubernetesSecretStatus struct {
	SecretRef *corev1.ObjectReference `json:"secretRef,omitempty"`
}
