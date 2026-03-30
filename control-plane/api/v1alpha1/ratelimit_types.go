// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import (
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	"github.com/hashicorp/consul/api"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

const (
	RateLimitKubeKind = "ratelimit"
)

func init() {
	SchemeBuilder.Register(&RateLimit{}, &RateLimitList{})
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.conditions[?(@.type==\"Synced\")].status",description="The sync status of the resource with Consul"
// +kubebuilder:printcolumn:name="Last Synced",type="date",JSONPath=".status.lastSyncedTime",description="The last successful synced time of the resource with Consul"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of the resource"

// RateLimit is the Schema for the ratelimits API.
type RateLimit struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RateLimitSpec `json:"spec,omitempty"`
	Status `json:"status,omitempty"`
}

func (r *RateLimit) GetObjectMeta() metav1.ObjectMeta {
	return r.ObjectMeta
}

func (r *RateLimit) AddFinalizer(name string) {
	r.ObjectMeta.Finalizers = append(r.ObjectMeta.Finalizers, name)
}

func (r *RateLimit) RemoveFinalizer(name string) {
	for i, n := range r.ObjectMeta.Finalizers {
		if n == name {
			r.ObjectMeta.Finalizers = append(r.ObjectMeta.Finalizers[:i], r.ObjectMeta.Finalizers[i+1:]...)
			return
		}
	}
}

func (r *RateLimit) Finalizers() []string {
	return r.ObjectMeta.Finalizers
}

func (r *RateLimit) ConsulKind() string {
	return api.RateLimit
}

func (r *RateLimit) ConsulGlobalResource() bool {
	return true
}

func (r *RateLimit) ConsulMirroringNS() string {
	return common.DefaultConsulNamespace
}

func (r *RateLimit) KubeKind() string {
	return RateLimitKubeKind
}

func (r *RateLimit) ConsulName() string {
	return r.ObjectMeta.Name
}

func (r *RateLimit) KubernetesName() string {
	return r.ObjectMeta.Name
}

func (r *RateLimit) SetSyncedCondition(status corev1.ConditionStatus, reason, message string) {
	r.Status.Conditions = Conditions{
		{
			Type:               ConditionSynced,
			Status:             status,
			LastTransitionTime: metav1.Now(),
			Reason:             reason,
			Message:            message,
		},
	}
}

func (r *RateLimit) SetLastSyncedTime(time *metav1.Time) {
	r.Status.LastSyncedTime = time
}

func (r *RateLimit) SyncedCondition() (status corev1.ConditionStatus, reason, message string) {
	cond := r.Status.GetCondition(ConditionSynced)
	if cond == nil {
		return corev1.ConditionUnknown, "", ""
	}
	return cond.Status, cond.Reason, cond.Message
}

func (r *RateLimit) SyncedConditionStatus() corev1.ConditionStatus {
	condition := r.Status.GetCondition(ConditionSynced)
	if condition == nil {
		return corev1.ConditionUnknown
	}
	return condition.Status
}

func (r *RateLimit) ToConsul(datacenter string) api.ConfigEntry {
	return &api.GlobalRateLimitConfigEntry{
		Kind:   r.ConsulKind(),
		Name:   "global",
		Config: r.Spec.Config.toConsul(),
		Meta:   meta(datacenter),
	}
}

func (r *RateLimit) MatchesConsul(candidate api.ConfigEntry) bool {
	configEntry, ok := candidate.(*api.GlobalRateLimitConfigEntry)
	if !ok {
		return false
	}
	// No datacenter is passed to ToConsul as we ignore the Meta field when checking for equality.
	return cmp.Equal(r.ToConsul(""), configEntry, cmpopts.IgnoreFields(api.GlobalRateLimitConfigEntry{}, "Partition", "Namespace", "Meta", "ModifyIndex", "CreateIndex"), cmpopts.IgnoreUnexported(), cmpopts.EquateEmpty())
}

func (r *RateLimit) Validate(consulMeta common.ConsulMeta) error {
	path := field.NewPath("spec")
	errs := r.Spec.Config.validate(path.Child("config"))
	if len(errs) > 0 {
		return errs.ToAggregate()
	}
	return nil
}

func (r *RateLimit) DefaultNamespaceFields(consulMeta common.ConsulMeta) {
}

// +kubebuilder:object:root=true

// RateLimitList contains a list of RateLimit.
type RateLimitList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RateLimit `json:"items"`
}

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// RateLimitSpec defines the desired state of RateLimit.
type RateLimitSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	Config GlobalRateLimitConfig `json:"config"`
}

type GlobalRateLimitConfig struct {
	// RequestLimits allows separate configuration for read and write operations.
	// This follows the same pattern as limits.request_limits in agent configuration.
	// If set, takes precedence over the legacy MaxRPS field.
	// If nil, defaults to infinity (unlimited).
	ReadRate  *float64 `json:"readRate"`
	WriteRate *float64 `json:"writeRate"`

	// Priority enables stricter rate limiting in emergency situations.
	Priority bool `json:"priority"`

	// ExcludeEndpoints lists RPC methods that should bypass rate limiting.
	// Example: ["Health.Check", "Status.Leader"]
	ExcludeEndpoints []string `json:"excludeEndpoints"`
}

func (j *GlobalRateLimitConfig) toConsul() *api.GlobalRateLimitConfig {
	if j == nil {
		return nil
	}

	return &api.GlobalRateLimitConfig{
		ReadRate:         j.ReadRate,
		WriteRate:        j.WriteRate,
		Priority:         j.Priority,
		ExcludeEndpoints: j.ExcludeEndpoints,
	}
}

func (j *GlobalRateLimitConfig) validate(path *field.Path) field.ErrorList {
	var errs field.ErrorList
	if j == nil {
		return errs
	}

	if j.ReadRate != nil && *j.ReadRate < 0 {
		errs = append(errs, field.Invalid(path.Child("readRate"), *j.ReadRate, "readRate must be non-negative"))
	}

	if j.WriteRate != nil && *j.WriteRate < 0 {
		errs = append(errs, field.Invalid(path.Child("writeRate"), *j.WriteRate, "writeRate must be non-negative"))
	}

	for i, endpoint := range j.ExcludeEndpoints {
		if endpoint == "" {
			errs = append(errs, field.Invalid(path.Child("excludeEndpoints").Index(i), endpoint, "excludeEndpoints must not contain empty strings"))
		}
	}

	return errs
}
