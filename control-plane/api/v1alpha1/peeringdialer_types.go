package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
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
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.conditions[?(@.type==\"Synced\")].status",description="The sync status of the resource with Consul"
// +kubebuilder:printcolumn:name="Last Synced",type="date",JSONPath=".status.lastSyncedTime",description="The last successful synced time of the resource with Consul"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of the resource"
// +kubebuilder:resource:shortName="peering-dialer"
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
	// SecretRef shows the status of the secret.
	// +optional
	SecretRef *SecretRefStatus `json:"secret,omitempty"`
	// Conditions indicate the latest available observations of a resource's current state.
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions Conditions `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
	// LastSyncedTime is the last time the resource successfully synced with Consul.
	// +optional
	LastSyncedTime *metav1.Time `json:"lastSyncedTime,omitempty" description:"last time the condition transitioned from one status to another"`
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

func (pd *PeeringDialer) SetSyncedCondition(status corev1.ConditionStatus, reason string, message string) {
	pd.Status.Conditions = Conditions{
		{
			Type:               ConditionSynced,
			Status:             status,
			LastTransitionTime: metav1.Now(),
			Reason:             reason,
			Message:            message,
		},
	}
}
