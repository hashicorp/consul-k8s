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
	// DefaultForFailover indicates that upstream requests to members of the given sameness group will implicitly failover between members of this sameness group.
	// When DefaultForFailover is true, the local partition must be a member of the sameness group or IncludeLocal must be set to true.
	DefaultForFailover bool `json:"defaultForFailover,omitempty"`
	// IncludeLocal is used to include the local partition as the first member of the sameness group.
	// The local partition can only be a member of a single sameness group.
	IncludeLocal bool `json:"includeLocal,omitempty"`
	// Members are the partitions and peers that are part of the sameness group.
	// If a member of a sameness group does not exist, it will be ignored.
	Members []SamenessGroupMember `json:"members,omitempty"`
}

type SamenessGroupMember struct {
	// The partitions and peers that are part of the sameness group.
	// A sameness group member cannot define both peer and partition at the same time.
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

	if in == nil {
		return nil
	}
	if in.Name == "" {
		allErrs = append(allErrs, field.Invalid(path.Child("name"), in.Name, "sameness groups must have a name defined"))
	}

	partition := consulMeta.Partition
	includesLocal := in.Spec.IncludeLocal

	if in.ObjectMeta.Namespace != "default" && in.ObjectMeta.Namespace != "" {
		allErrs = append(allErrs, field.Invalid(path.Child("name"), consulMeta.DestinationNamespace, "sameness groups must reside in the default namespace"))
	}

	if len(in.Spec.Members) == 0 {
		asJSON, _ := json.Marshal(in.Spec.Members)
		allErrs = append(allErrs, field.Invalid(path.Child("members"), string(asJSON), "sameness groups must have at least one member"))
	}

	seenMembers := make(map[SamenessGroupMember]struct{})
	for i, m := range in.Spec.Members {
		if partition == m.Partition {
			includesLocal = true
		}
		if err := m.validate(path.Child("members").Index(i)); err != nil {
			allErrs = append(allErrs, err)
		}
		if _, ok := seenMembers[m]; ok {
			asJSON, _ := json.Marshal(m)
			allErrs = append(allErrs, field.Invalid(path.Child("members").Index(i), string(asJSON), "sameness group members must be unique"))
		}
		seenMembers[m] = struct{}{}

	}

	if !includesLocal {
		allErrs = append(allErrs, field.Invalid(path.Child("members"), in.Spec.IncludeLocal, "the local partition must be a member of sameness groups"))
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
