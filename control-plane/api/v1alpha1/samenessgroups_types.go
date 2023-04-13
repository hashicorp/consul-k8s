package v1alpha1

import (
	"encoding/json"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	"github.com/hashicorp/consul/api"
	capi "github.com/hashicorp/consul/api"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

const (
	SamenessGroupsKubeKind string = "samenessgroups"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

func init() {
	SchemeBuilder.Register(&SamenessGroups{}, &SamenessGroupsList{})
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// SamenessGroups is the Schema for the samenessgroups API
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.conditions[?(@.type==\"Synced\")].status",description="The sync status of the resource with Consul"
// +kubebuilder:printcolumn:name="Last Synced",type="date",JSONPath=".status.lastSyncedTime",description="The last successful synced time of the resource with Consul"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of the resource"
// +kubebuilder:resource:shortName="sameness-groups"
type SamenessGroups struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              SamenessGroupsSpec `json:"spec,omitempty"`
	Status            `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// SamenessGroupsList contains a list of SamenessGroups.
type SamenessGroupsList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SamenessGroups `json:"items"`
}

// SamenessGroupsSpec defines the desired state of SamenessGroups.
type SamenessGroupsSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// DefaultForFailover is
	DefaultForFailover bool `json:"defaultForFailover,omitempty"`
	// IncludeLocal
	IncludeLocal bool `json:"includeLocal,omitempty"`
	// Members
	Members []SamenessGroupMember `json:"members,omitempty"`
}

type SamenessGroupMember struct {
	Partition string `json:"partition,omitempty"`
	Peer      string `json:"peer,omitempty"`
}

func (in *SamenessGroups) GetObjectMeta() metav1.ObjectMeta {
	return in.ObjectMeta
}

func (in *SamenessGroups) AddFinalizer(name string) {
	in.ObjectMeta.Finalizers = append(in.Finalizers(), name)
}

func (in *SamenessGroups) RemoveFinalizer(name string) {
	var newFinalizers []string
	for _, oldF := range in.Finalizers() {
		if oldF != name {
			newFinalizers = append(newFinalizers, oldF)
		}
	}
	in.ObjectMeta.Finalizers = newFinalizers
}

func (in *SamenessGroups) Finalizers() []string {
	return in.ObjectMeta.Finalizers
}

func (in *SamenessGroups) ConsulKind() string {
	return capi.SamenessGroup
}

func (in *SamenessGroups) ConsulGlobalResource() bool {
	return false
}

func (in *SamenessGroups) ConsulMirroringNS() string {
	return common.DefaultConsulNamespace
}

func (in *SamenessGroups) KubeKind() string {
	return SamenessGroupsKubeKind
}

func (in *SamenessGroups) ConsulName() string {
	return in.ObjectMeta.Name
}

func (in *SamenessGroups) KubernetesName() string {
	return in.ObjectMeta.Name
}

func (in *SamenessGroups) SetSyncedCondition(status corev1.ConditionStatus, reason, message string) {
	in.Status.Conditions = Conditions{
		{
			Type:               ConditionSynced,
			Status:             status,
			LastTransitionTime: metav1.Now(),
			Reason:             reason,
			Message:            message,
		},
	}
}

func (in *SamenessGroups) SetLastSyncedTime(time *metav1.Time) {
	in.Status.LastSyncedTime = time
}

func (in *SamenessGroups) SyncedCondition() (status corev1.ConditionStatus, reason, message string) {
	cond := in.Status.GetCondition(ConditionSynced)
	if cond == nil {
		return corev1.ConditionUnknown, "", ""
	}
	return cond.Status, cond.Reason, cond.Message
}

func (in *SamenessGroups) SyncedConditionStatus() corev1.ConditionStatus {
	cond := in.Status.GetCondition(ConditionSynced)
	if cond == nil {
		return corev1.ConditionUnknown
	}
	return cond.Status
}

func (in *SamenessGroups) ToConsul(datacenter string) api.ConfigEntry {
	//consulConfig := in.convertConfig()
	return &capi.SamenessGroupConfigEntry{
		Kind:               in.ConsulKind(),
		Name:               in.ConsulName(),
		DefaultForFailover: in.Spec.DefaultForFailover,
		IncludeLocal:       in.Spec.IncludeLocal,
		Members:            SamenessGroupMembers(in.Spec.Members).toConsul(),
		Meta:               meta(datacenter),
	}
}

func (in *SamenessGroups) MatchesConsul(candidate api.ConfigEntry) bool {
	configEntry, ok := candidate.(*capi.SamenessGroupConfigEntry)
	if !ok {
		return false
	}
	return cmp.Equal(in.ToConsul(""), configEntry, cmpopts.IgnoreFields(capi.SamenessGroupConfigEntry{}, "Partition", "Meta", "ModifyIndex", "CreateIndex"), cmpopts.IgnoreUnexported(), cmpopts.EquateEmpty(),
		cmp.Comparer(transparentProxyConfigComparer))
}

func (in *SamenessGroups) Validate(consulMeta common.ConsulMeta) error {
	var allErrs field.ErrorList
	path := field.NewPath("spec")

	asJSON, _ := json.Marshal(in)
	if in == nil {
		allErrs = append(allErrs, field.Invalid(path, string(asJSON), "config entry is nil"))
	}
	if in.Name == "" {
		allErrs = append(allErrs, field.Invalid(path.Child("name"), in.Name, "sameness groups must have a name defined"))
	}

	if len(in.Spec.Members) == 0 {
		asJSON, _ := json.Marshal(in.Spec.Members)
		allErrs = append(allErrs, field.Invalid(path.Child("members"), string(asJSON), "sameness groups must have at least one member"))
	}

	for i, m := range in.Spec.Members {
		if err := m.validate(path.Child("members").Index(i)); err != nil {
			allErrs = append(allErrs, err)
		}
	}

	if len(allErrs) > 0 {
		return apierrors.NewInvalid(
			schema.GroupKind{Group: ConsulHashicorpGroup, Kind: SamenessGroupsKubeKind},
			in.KubernetesName(), allErrs)
	}

	return nil
}

// DefaultNamespaceFields has no behaviour here as sameness-groups have no namespace specific fields.
func (in *SamenessGroups) DefaultNamespaceFields(_ common.ConsulMeta) {
}

type SamenessGroupMembers []SamenessGroupMember

func (in SamenessGroupMembers) toConsul() []capi.SamenessGroupMember {
	if in == nil {
		return nil
	}

	outMembers := make([]capi.SamenessGroupMember, 0, len(in))
	for _, e := range in {
		consulMember := capi.SamenessGroupMember{
			Peer:      e.Peer,
			Partition: e.Partition,
		}
		outMembers = append(outMembers, consulMember)
	}
	return outMembers
}

func (in *SamenessGroupMember) validate(path *field.Path) *field.Error {
	asJSON, _ := json.Marshal(in)

	if in == nil {
		return field.Invalid(path, string(asJSON), "sameness group member is nil")
	}
	if in.isEmpty() {
		return field.Invalid(path, string(asJSON), "sameness group members must specify either partition or peer")
	}
	// We do not allow referencing peer connections in other partitions.
	if in.Peer != "" && in.Partition != "" {
		return field.Invalid(path, string(asJSON), "sameness group members cannot specify both partition and peer in the same entry")
	}
	return nil
}

func (in *SamenessGroupMember) isEmpty() bool {
	return in.Peer == "" && in.Partition == ""
}
