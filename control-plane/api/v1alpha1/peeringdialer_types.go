package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

func init() {
	SchemeBuilder.Register(&PeeringDialer{}, &PeeringDialerList{})
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// PeeringDialer is the Schema for the peeringdialers API.
type PeeringDialer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PeeringDialerSpec   `json:"spec,omitempty"`
	Status PeeringDialerStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// PeeringDialerList contains a list of PeeringDialer.
type PeeringDialerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PeeringDialer `json:"items"`
}

// PeeringDialerSpec defines the desired state of PeeringDialer.
type PeeringDialerSpec struct {
	// Important: Run "make" to regenerate code after modifying this file

	// Peer describes the information needed to create a peering.
	Peer *Peer `json:"peer"`
}

// PeeringDialerStatus defines the observed state of PeeringDialer.
type PeeringDialerStatus struct {
	// Important: Run "make" to regenerate code after modifying this file

	// LastReconcileTime is the last time the resource was reconciled.
	// +optional
	LastReconcileTime *metav1.Time `json:"lastReconcileTime,omitempty" description:"last time the resource was reconciled"`
	// ReconcileError shows any errors during the last reconciliation of this resource.
	// +optional
	ReconcileError *ReconcileErrorStatus `json:"reconcileError,omitempty"`
	// SecretRef shows the status of the secret.
	// +optional
	SecretRef *SecretRefStatus `json:"secret,omitempty"`
}

func (pd *PeeringDialer) Secret() *Secret {
	if pd.Spec.Peer == nil {
		return nil
	}
	return pd.Spec.Peer.Secret
}

func (pd *PeeringDialer) SecretRef() *SecretRefStatus {
	return pd.Status.SecretRef
}
