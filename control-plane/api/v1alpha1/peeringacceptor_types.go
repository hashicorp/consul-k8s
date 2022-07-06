package v1alpha1

import (
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

const PeeringAcceptorKubeKind = "peeringacceptors"
const SecretBackendTypeKubernetes = "kubernetes"

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
	// Peer describes the information needed to create a peering.
	Peer *Peer `json:"peer"`
}

type Peer struct {
	// Secret describes how to store the generated peering token.
	Secret *Secret `json:"secret,omitempty"`
}

type Secret struct {
	// Name is the name of the secret generated.
	Name string `json:"name,omitempty"`
	// Key is the key of the secret generated.
	Key string `json:"key,omitempty"`
	// Backend is where the generated secret is stored. Currently supports the value: "kubernetes".
	Backend string `json:"backend,omitempty"`
}

// PeeringAcceptorStatus defines the observed state of PeeringAcceptor.
type PeeringAcceptorStatus struct {
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

type ReconcileErrorStatus struct {
	// Error is a boolean indicating if there was an error during the last reconcile of this resource.
	Error *bool `json:"error,omitempty"`
	// Message displays the error message from the last reconcile.
	Message *string `json:"message,omitempty"`
}

type SecretRefStatus struct {
	Secret `json:",inline"`
	// ResourceVersion is the resource version for the secret.
	ResourceVersion string `json:"resourceVersion,omitempty"`
}

func (pa *PeeringAcceptor) Secret() *Secret {
	return pa.Spec.Peer.Secret
}

func (pa *PeeringAcceptor) SecretRef() *SecretRefStatus {
	return pa.Status.SecretRef
}
func (pa *PeeringAcceptor) KubeKind() string {
	return PeeringAcceptorKubeKind
}
func (pa *PeeringAcceptor) KubernetesName() string {
	return pa.ObjectMeta.Name
}
func (pa *PeeringAcceptor) Validate() error {
	var errs field.ErrorList
	// The nil checks must return since you can't do further validations.
	if pa.Spec.Peer == nil {
		errs = append(errs, field.Invalid(field.NewPath("spec").Child("peer"), pa.Spec.Peer, "peer must be specified"))
		return apierrors.NewInvalid(
			schema.GroupKind{Group: ConsulHashicorpGroup, Kind: PeeringAcceptorKubeKind},
			pa.KubernetesName(), errs)
	}
	if pa.Spec.Peer.Secret == nil {
		errs = append(errs, field.Invalid(field.NewPath("spec").Child("peer").Child("secret"), pa.Spec.Peer.Secret, "secret must be specified"))
		return apierrors.NewInvalid(
			schema.GroupKind{Group: ConsulHashicorpGroup, Kind: PeeringAcceptorKubeKind},
			pa.KubernetesName(), errs)
	}
	// Currently, the only supported backend is "kubernetes".
	if pa.Spec.Peer.Secret.Backend != SecretBackendTypeKubernetes {
		errs = append(errs, field.Invalid(field.NewPath("spec").Child("peer").Child("secret").Child("backend"), pa.Spec.Peer.Secret.Backend, `backend must be "kubernetes"`))
	}
	if len(errs) > 0 {
		return apierrors.NewInvalid(
			schema.GroupKind{Group: ConsulHashicorpGroup, Kind: PeeringAcceptorKubeKind},
			pa.KubernetesName(), errs)
	}
	return nil
}
