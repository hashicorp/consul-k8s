package v1alpha1

import (
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

const PeeringDialerKubeKind = "peeringdialers"

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
	// Peer describes the information needed to create a peering.
	Peer *Peer `json:"peer"`
}

// PeeringDialerStatus defines the observed state of PeeringDialer.
type PeeringDialerStatus struct {
	// LatestPeeringVersion is the latest version of the resource that was reconciled.
	LatestPeeringVersion *uint64 `json:"latestPeeringVersion,omitempty"`
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
func (pd *PeeringDialer) KubeKind() string {
	return PeeringDialerKubeKind
}
func (pd *PeeringDialer) KubernetesName() string {
	return pd.ObjectMeta.Name
}
func (pd *PeeringDialer) Validate() error {
	var errs field.ErrorList
	// The nil checks must return since you can't do further validations.
	if pd.Spec.Peer == nil {
		errs = append(errs, field.Invalid(field.NewPath("spec").Child("peer"), pd.Spec.Peer, "peer must be specified"))
		return apierrors.NewInvalid(
			schema.GroupKind{Group: ConsulHashicorpGroup, Kind: PeeringDialerKubeKind},
			pd.KubernetesName(), errs)
	}
	if pd.Spec.Peer.Secret == nil {
		errs = append(errs, field.Invalid(field.NewPath("spec").Child("peer").Child("secret"), pd.Spec.Peer.Secret, "secret must be specified"))
		return apierrors.NewInvalid(
			schema.GroupKind{Group: ConsulHashicorpGroup, Kind: PeeringDialerKubeKind},
			pd.KubernetesName(), errs)
	}
	// Currently, the only supported backend is "kubernetes".
	if pd.Spec.Peer.Secret.Backend != "kubernetes" {
		errs = append(errs, field.Invalid(field.NewPath("spec").Child("peer").Child("secret").Child("backend"), pd.Spec.Peer.Secret.Backend, `backend must be "kubernetes"`))
	}
	if len(errs) > 0 {
		return apierrors.NewInvalid(
			schema.GroupKind{Group: ConsulHashicorpGroup, Kind: PeeringDialerKubeKind},
			pd.KubernetesName(), errs)
	}
	return nil
}
