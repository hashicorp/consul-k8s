package v1alpha1

import (
	"encoding/json"
	"fmt"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/hashicorp/consul-k8s/api/common"
	capi "github.com/hashicorp/consul/api"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

func init() {
	SchemeBuilder.Register(&ServiceSplitter{}, &ServiceSplitterList{})
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// ServiceSplitter is the Schema for the servicesplitters API
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.conditions[?(@.type==\"Synced\")].status",description="The sync status of the resource with Consul"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of the resource"
type ServiceSplitter struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ServiceSplitterSpec `json:"spec,omitempty"`
	Status `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ServiceSplitterList contains a list of ServiceSplitter
type ServiceSplitterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ServiceSplitter `json:"items"`
}

type ServiceSplits []ServiceSplit

// ServiceSplitterSpec defines the desired state of ServiceSplitter
type ServiceSplitterSpec struct {
	// Splits defines how much traffic to send to which set of service instances during a traffic split.
	// The sum of weights across all splits must add up to 100.
	Splits ServiceSplits `json:"splits,omitempty"`
}

type ServiceSplit struct {
	// Weight is a value between 0 and 100 reflecting what portion of traffic should be directed to this split.
	// The smallest representable weight is 1/10000 or .01%.
	Weight float32 `json:"weight,omitempty"`
	// Service is the service to resolve instead of the default.
	Service string `json:"service,omitempty"`
	// ServiceSubset is a named subset of the given service to resolve instead of one defined
	// as that service's DefaultSubset. If empty the default subset is used.
	ServiceSubset string `json:"serviceSubset,omitempty"`
	// The namespace to resolve the service from instead of the current namespace.
	// If empty the current namespace is assumed.
	Namespace string `json:"namespace,omitempty"`
}

func (in *ServiceSplitter) GetObjectMeta() metav1.ObjectMeta {
	return in.ObjectMeta
}

func (in *ServiceSplitter) ConsulMirroringNS() string {
	return in.Namespace
}

func (in *ServiceSplitter) AddFinalizer(name string) {
	in.ObjectMeta.Finalizers = append(in.Finalizers(), name)
}

func (in *ServiceSplitter) RemoveFinalizer(name string) {
	var newFinalizers []string
	for _, oldF := range in.Finalizers() {
		if oldF != name {
			newFinalizers = append(newFinalizers, oldF)
		}
	}
	in.ObjectMeta.Finalizers = newFinalizers
}

func (in *ServiceSplitter) Finalizers() []string {
	return in.ObjectMeta.Finalizers
}

func (in *ServiceSplitter) ConsulKind() string {
	return capi.ServiceSplitter
}

func (in *ServiceSplitter) KubeKind() string {
	return common.ServiceSplitter
}

func (in *ServiceSplitter) ConsulName() string {
	return in.ObjectMeta.Name
}

func (in *ServiceSplitter) SetSyncedCondition(status corev1.ConditionStatus, reason, message string) {
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

func (in *ServiceSplitter) SyncedCondition() (status corev1.ConditionStatus, reason, message string) {
	cond := in.Status.GetCondition(ConditionSynced)
	if cond == nil {
		return corev1.ConditionUnknown, "", ""
	}
	return cond.Status, cond.Reason, cond.Message
}

func (in *ServiceSplitter) SyncedConditionStatus() corev1.ConditionStatus {
	condition := in.Status.GetCondition(ConditionSynced)
	if condition == nil {
		return corev1.ConditionUnknown
	}
	return condition.Status
}

func (in *ServiceSplitter) ToConsul(datacenter string) capi.ConfigEntry {
	return &capi.ServiceSplitterConfigEntry{
		Kind:   in.ConsulKind(),
		Name:   in.ConsulName(),
		Splits: in.Spec.Splits.toConsul(),
		Meta:   meta(datacenter),
	}
}

func (in *ServiceSplitter) ConsulGlobalResource() bool {
	return false
}

func (in *ServiceSplitter) KubernetesName() string {
	return in.ObjectMeta.Name
}

func (in *ServiceSplitter) MatchesConsul(candidate capi.ConfigEntry) bool {
	configEntry, ok := candidate.(*capi.ServiceSplitterConfigEntry)
	if !ok {
		return false
	}
	// No datacenter is passed to ToConsul as we ignore the Meta field when checking for equality.
	return cmp.Equal(in.ToConsul(""), configEntry, cmpopts.IgnoreFields(capi.ServiceSplitterConfigEntry{}, "Namespace", "Meta", "ModifyIndex", "CreateIndex"), cmpopts.IgnoreUnexported(), cmpopts.EquateEmpty())
}

func (in *ServiceSplitter) Validate(namespacesEnabled bool) error {
	errs := in.Spec.Splits.validate(field.NewPath("spec").Child("splits"))

	errs = append(errs, in.validateNamespaces(namespacesEnabled)...)

	if len(errs) > 0 {
		return apierrors.NewInvalid(
			schema.GroupKind{Group: ConsulHashicorpGroup, Kind: in.KubeKind()},
			in.KubernetesName(), errs)
	}
	return nil
}

func (in ServiceSplits) toConsul() []capi.ServiceSplit {
	var consulServiceSplits []capi.ServiceSplit
	for _, split := range in {
		consulServiceSplits = append(consulServiceSplits, split.toConsul())
	}

	return consulServiceSplits
}

func (in ServiceSplit) toConsul() capi.ServiceSplit {
	return capi.ServiceSplit{
		Weight:        in.Weight,
		Service:       in.Service,
		ServiceSubset: in.ServiceSubset,
		Namespace:     in.Namespace,
	}
}

func (in *ServiceSplitter) validateNamespaces(namespacesEnabled bool) field.ErrorList {
	var errs field.ErrorList
	path := field.NewPath("spec")
	if !namespacesEnabled {
		for i, s := range in.Spec.Splits {
			if s.Namespace != "" {
				errs = append(errs, field.Invalid(path.Child("splits").Index(i).Child("namespace"), s.Namespace, `Consul Enterprise namespaces must be enabled to set split.namespace`))
			}

		}
	}
	return errs
}

func (in ServiceSplits) validate(path *field.Path) field.ErrorList {
	var errs field.ErrorList

	// The sum of weights across all splits must add up to 100.
	sumOfWeights := float32(0)
	for i, split := range in {
		// First, validate each split.
		if err := split.validate(path.Index(i).Child("weight")); err != nil {
			errs = append(errs, err)
		}

		// If valid, add its weight to sumOfWeights.
		sumOfWeights += split.Weight
	}

	if sumOfWeights != 100 {
		asJSON, _ := json.Marshal(in)
		errs = append(errs, field.Invalid(path, string(asJSON),
			fmt.Sprintf("the sum of weights across all splits must add up to 100 percent, but adds up to %f", sumOfWeights)))
	}

	return errs
}

func (in ServiceSplit) validate(path *field.Path) *field.Error {
	// Validate that the weight value is between 0.01 and 100 but allow a weight to be 0.
	if in.Weight != 0 && (in.Weight > 100 || in.Weight < 0.01) {
		return field.Invalid(path, in.Weight, "weight must be a percentage between 0.01 and 100")
	}

	return nil
}
