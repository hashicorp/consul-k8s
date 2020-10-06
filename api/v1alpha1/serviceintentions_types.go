package v1alpha1

import (
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/hashicorp/consul-k8s/api/common"
	"github.com/hashicorp/consul/api"
	capi "github.com/hashicorp/consul/api"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// ServiceIntentionsSpec defines the desired state of ServiceIntentions
type ServiceIntentionsSpec struct {
	Destination Destination      `json:"destination,omitempty"`
	Sources     SourceIntentions `json:"sources,omitempty"`
}

type Destination struct {
	Name      string `json:"name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
}

type SourceIntentions []*SourceIntention

type SourceIntention struct {
	Name        string          `json:"name,omitempty"`
	Namespace   string          `json:"namespace,omitempty"`
	Action      IntentionAction `json:"action,omitempty"`
	Description string          `json:"description,omitempty"`
}

// IntentionAction is the action that the intention represents. This
// can be "allow" or "deny" to allowlist or denylist intentions.
type IntentionAction string

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// ServiceIntentions is the Schema for the serviceintentions API
type ServiceIntentions struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ServiceIntentionsSpec `json:"spec,omitempty"`
	Status `json:"status,omitempty"`
}

func (in *ServiceIntentions) ConsulMirroringNS() string {
	return in.Spec.Destination.Namespace
}

func (in *ServiceIntentions) GetObjectMeta() metav1.ObjectMeta {
	return in.ObjectMeta
}

func (in *ServiceIntentions) AddFinalizer(f string) {
	in.ObjectMeta.Finalizers = append(in.Finalizers(), f)
}

func (in *ServiceIntentions) RemoveFinalizer(f string) {
	var newFinalizers []string
	for _, oldF := range in.Finalizers() {
		if oldF != f {
			newFinalizers = append(newFinalizers, oldF)
		}
	}
	in.ObjectMeta.Finalizers = newFinalizers
}

func (in *ServiceIntentions) Finalizers() []string {
	return in.ObjectMeta.Finalizers
}

func (in *ServiceIntentions) ConsulKind() string {
	return capi.ServiceIntentions
}

func (in *ServiceIntentions) KubeKind() string {
	return common.ServiceIntentions
}

func (in *ServiceIntentions) ConsulName() string {
	return in.Spec.Destination.Name
}

func (in *ServiceIntentions) KubernetesName() string {
	return in.ObjectMeta.Name
}

func (in *ServiceIntentions) SetSyncedCondition(status corev1.ConditionStatus, reason, message string) {
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

func (in *ServiceIntentions) SyncedCondition() (status corev1.ConditionStatus, reason, message string) {
	cond := in.Status.GetCondition(ConditionSynced)
	if cond == nil {
		return corev1.ConditionUnknown, "", ""
	}
	return cond.Status, cond.Reason, cond.Message
}

func (in *ServiceIntentions) SyncedConditionStatus() corev1.ConditionStatus {
	condition := in.Status.GetCondition(ConditionSynced)
	if condition == nil {
		return corev1.ConditionUnknown
	}
	return condition.Status
}

func (in *ServiceIntentions) ToConsul(datacenter string) api.ConfigEntry {
	return &capi.ServiceIntentionsConfigEntry{
		Kind:    in.ConsulKind(),
		Name:    in.Spec.Destination.Name,
		Sources: in.Spec.Sources.toConsul(),
		Meta:    meta(datacenter),
	}
}

func (in SourceIntentions) toConsul() []*capi.SourceIntention {
	var consulSourceIntentions []*capi.SourceIntention
	for _, intention := range in {
		consulSourceIntentions = append(consulSourceIntentions, intention.toConsul())
	}
	return consulSourceIntentions
}

func (in *ServiceIntentions) ConsulGlobalResource() bool {
	return false
}

func (in *SourceIntention) toConsul() *capi.SourceIntention {
	if in == nil {
		return nil
	}
	return &capi.SourceIntention{
		Name:        in.Name,
		Namespace:   in.Namespace,
		Action:      in.Action.toConsul(),
		Description: in.Description,
	}
}

func (in IntentionAction) toConsul() capi.IntentionAction {
	return capi.IntentionAction(in)
}
func (in *ServiceIntentions) MatchesConsul(candidate api.ConfigEntry) bool {
	configEntry, ok := candidate.(*capi.ServiceIntentionsConfigEntry)
	if !ok {
		return false
	}
	// No datacenter is passed to ToConsul as we ignore the Meta field when checking for equality.
	return cmp.Equal(
		in.ToConsul(""),
		configEntry,
		cmpopts.IgnoreFields(capi.ServiceIntentionsConfigEntry{}, "Namespace", "Meta", "ModifyIndex", "CreateIndex"),
		cmpopts.IgnoreFields(capi.SourceIntention{}, "LegacyID", "LegacyMeta", "LegacyCreateTime", "LegacyUpdateTime", "Precedence", "Type"),
		cmpopts.IgnoreUnexported(),
		cmpopts.EquateEmpty(),
	)
}

func (in *ServiceIntentions) Validate() error {
	var errs field.ErrorList
	path := field.NewPath("spec")
	for i, source := range in.Spec.Sources {
		if err := source.Action.validate(path.Child("sources").Index(i)); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return apierrors.NewInvalid(
			schema.GroupKind{Group: ConsulHashicorpGroup, Kind: common.ServiceIntentions},
			in.KubernetesName(), errs)
	}
	return nil
}

func (in *ServiceIntentions) Default() {
	if in.Spec.Destination.Namespace == "" {
		in.Spec.Destination.Namespace = in.Namespace
	}
	for _, source := range in.Spec.Sources {
		if source.Namespace == "" {
			source.Namespace = in.Namespace
		}
	}
}

func (in IntentionAction) validate(path *field.Path) *field.Error {
	actions := []string{"allow", "deny"}
	if !sliceContains(actions, string(in)) {
		return field.Invalid(path.Child("action"), in, notInSliceMessage(actions))
	}
	return nil
}

// +kubebuilder:object:root=true

// ServiceIntentionsList contains a list of ServiceIntentions
type ServiceIntentionsList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ServiceIntentions `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ServiceIntentions{}, &ServiceIntentionsList{})
}
