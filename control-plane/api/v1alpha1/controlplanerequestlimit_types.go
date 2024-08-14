// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import (
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	consul "github.com/hashicorp/consul/api"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"

	"github.com/hashicorp/consul-k8s/control-plane/api/common"
)

const (
	ControlPlaneRequestLimitKubeKind = "controlplanerequestlimit"
)

func init() {
	SchemeBuilder.Register(&ControlPlaneRequestLimit{}, &ControlPlaneRequestLimitList{})
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// ControlPlaneRequestLimit is the Schema for the controlplanerequestlimits API.
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.conditions[?(@.type==\"Synced\")].status",description="The sync status of the resource with Consul"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of the resource"
type ControlPlaneRequestLimit struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ControlPlaneRequestLimitSpec `json:"spec,omitempty"`
	Status `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ControlPlaneRequestLimitList contains a list of ControlPlaneRequestLimit.
type ControlPlaneRequestLimitList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ControlPlaneRequestLimit `json:"items"`
}

type ReadWriteRatesConfig struct {
	ReadRate  float64 `json:"readRate,omitempty"`
	WriteRate float64 `json:"writeRate,omitempty"`
}

func (c *ReadWriteRatesConfig) toConsul() *consul.ReadWriteRatesConfig {
	if c == nil {
		return nil
	}
	return &consul.ReadWriteRatesConfig{
		ReadRate:  c.ReadRate,
		WriteRate: c.WriteRate,
	}
}

func (c *ReadWriteRatesConfig) validate(path *field.Path) field.ErrorList {
	if c == nil {
		return nil
	}

	var errs field.ErrorList

	if c.ReadRate < 0 {
		errs = append(errs, field.Invalid(path.Child("readRate"), c.ReadRate, "readRate must be >= 0"))
	}

	if c.WriteRate <= 0 {
		errs = append(errs, field.Invalid(path.Child("writeRate"), c.WriteRate, "writeRate must be > 0"))
	}
	return errs
}

// ControlPlaneRequestLimitSpec defines the desired state of ControlPlaneRequestLimit.
type ControlPlaneRequestLimitSpec struct {
	Mode                 string `json:"mode,omitempty"`
	ReadWriteRatesConfig `json:",inline"`
	ACL                  *ReadWriteRatesConfig `json:"acl,omitempty"`
	Catalog              *ReadWriteRatesConfig `json:"catalog,omitempty"`
	ConfigEntry          *ReadWriteRatesConfig `json:"configEntry,omitempty"`
	ConnectCA            *ReadWriteRatesConfig `json:"connectCA,omitempty"`
	Coordinate           *ReadWriteRatesConfig `json:"coordinate,omitempty"`
	DiscoveryChain       *ReadWriteRatesConfig `json:"discoveryChain,omitempty"`
	Health               *ReadWriteRatesConfig `json:"health,omitempty"`
	Intention            *ReadWriteRatesConfig `json:"intention,omitempty"`
	KV                   *ReadWriteRatesConfig `json:"kv,omitempty"`
	Tenancy              *ReadWriteRatesConfig `json:"tenancy,omitempty"`
	PreparedQuery        *ReadWriteRatesConfig `json:"preparedQuery,omitempty"`
	Session              *ReadWriteRatesConfig `json:"session,omitempty"`
	Txn                  *ReadWriteRatesConfig `json:"txn,omitempty"`
}

// GetObjectMeta returns object meta.
func (c *ControlPlaneRequestLimit) GetObjectMeta() metav1.ObjectMeta {
	return c.ObjectMeta
}

// AddFinalizer adds a finalizer to the list of finalizers.
func (c *ControlPlaneRequestLimit) AddFinalizer(name string) {
	c.ObjectMeta.Finalizers = append(c.ObjectMeta.Finalizers, name)
}

// RemoveFinalizer removes this finalizer from the list.
func (c *ControlPlaneRequestLimit) RemoveFinalizer(name string) {
	for i, n := range c.ObjectMeta.Finalizers {
		if n == name {
			c.ObjectMeta.Finalizers = append(c.ObjectMeta.Finalizers[:i], c.ObjectMeta.Finalizers[i+1:]...)
			return
		}
	}
}

// Finalizers returns the list of finalizers for this object.
func (c *ControlPlaneRequestLimit) Finalizers() []string {
	return c.ObjectMeta.Finalizers
}

// ConsulKind returns the Consul config entry kind, i.e. service-defaults, not
// servicedefaults.
func (c *ControlPlaneRequestLimit) ConsulKind() string {
	return consul.RateLimitIPConfig
}

// ConsulGlobalResource returns if the resource exists in the default
// Consul namespace only.
func (c *ControlPlaneRequestLimit) ConsulGlobalResource() bool {
	return true
}

// ConsulMirroringNS returns the Consul namespace that the config entry should
// be created in if namespaces and mirroring are enabled.
func (c *ControlPlaneRequestLimit) ConsulMirroringNS() string {
	return common.DefaultConsulNamespace
}

// KubeKind returns the Kube config entry kind, i.e. servicedefaults, not
// service-defaults.
func (c *ControlPlaneRequestLimit) KubeKind() string {
	return ControlPlaneRequestLimitKubeKind
}

// ConsulName returns the name of the config entry as saved in Consul.
// This may be different than KubernetesName() in the case of a ServiceIntentions
// config entry.
func (c *ControlPlaneRequestLimit) ConsulName() string {
	return c.ObjectMeta.Name
}

// KubernetesName returns the name of the Kubernetes resource.
func (c *ControlPlaneRequestLimit) KubernetesName() string {
	return c.ObjectMeta.Name
}

// SetSyncedCondition updates the synced condition.
func (c *ControlPlaneRequestLimit) SetSyncedCondition(status corev1.ConditionStatus, reason, message string) {
	c.Status.Conditions = Conditions{
		{
			Type:               ConditionSynced,
			Status:             status,
			LastTransitionTime: metav1.Now(),
			Reason:             reason,
			Message:            message,
		},
	}
}

// SetLastSyncedTime updates the last synced time.
func (c *ControlPlaneRequestLimit) SetLastSyncedTime(time *metav1.Time) {
	c.Status.LastSyncedTime = time
}

// SyncedCondition gets the synced condition.
func (c *ControlPlaneRequestLimit) SyncedCondition() (status corev1.ConditionStatus, reason, message string) {
	cond := c.Status.GetCondition(ConditionSynced)
	if cond == nil {
		return corev1.ConditionUnknown, "", ""
	}
	return cond.Status, cond.Reason, cond.Message
}

// SyncedConditionStatus returns the status of the synced condition.
func (c *ControlPlaneRequestLimit) SyncedConditionStatus() corev1.ConditionStatus {
	condition := c.Status.GetCondition(ConditionSynced)
	if condition == nil {
		return corev1.ConditionUnknown
	}
	return condition.Status
}

// ToConsul converts the resource to the corresponding Consul API definition.
// Its return type is the generic ConfigEntry but a specific config entry
// type should be constructed e.g. ServiceConfigEntry.
func (c *ControlPlaneRequestLimit) ToConsul(datacenter string) consul.ConfigEntry {
	return &consul.RateLimitIPConfigEntry{
		Kind:           c.ConsulKind(),
		Name:           c.ConsulName(),
		Mode:           c.Spec.Mode,
		ReadRate:       c.Spec.ReadRate,
		WriteRate:      c.Spec.WriteRate,
		Meta:           meta(datacenter),
		ACL:            c.Spec.ACL.toConsul(),
		Catalog:        c.Spec.Catalog.toConsul(),
		ConfigEntry:    c.Spec.ConfigEntry.toConsul(),
		ConnectCA:      c.Spec.ConnectCA.toConsul(),
		Coordinate:     c.Spec.Coordinate.toConsul(),
		DiscoveryChain: c.Spec.DiscoveryChain.toConsul(),
		Health:         c.Spec.Health.toConsul(),
		Intention:      c.Spec.Intention.toConsul(),
		KV:             c.Spec.KV.toConsul(),
		Tenancy:        c.Spec.Tenancy.toConsul(),
		PreparedQuery:  c.Spec.PreparedQuery.toConsul(),
		Session:        c.Spec.Session.toConsul(),
		Txn:            c.Spec.Txn.toConsul(),
	}
}

// MatchesConsul returns true if the resource has the same fields as the Consul
// config entry.
func (c *ControlPlaneRequestLimit) MatchesConsul(candidate consul.ConfigEntry) bool {
	configEntry, ok := candidate.(*consul.RateLimitIPConfigEntry)
	if !ok {
		return false
	}
	// No datacenter is passed to ToConsul as we ignore the Meta field when checking for equality.
	return cmp.Equal(c.ToConsul(""), configEntry, cmpopts.IgnoreFields(consul.RateLimitIPConfigEntry{}, "Partition", "Namespace", "Meta", "ModifyIndex", "CreateIndex"), cmpopts.IgnoreUnexported(), cmpopts.EquateEmpty())
}

// Validate returns an error if the resource is invalid.
func (c *ControlPlaneRequestLimit) Validate(consulMeta common.ConsulMeta) error {
	var errs field.ErrorList
	path := field.NewPath("spec")

	if c.Spec.Mode != "permissive" && c.Spec.Mode != "enforcing" && c.Spec.Mode != "disabled" {
		errs = append(errs, field.Invalid(path.Child("mode"), c.Spec.Mode, "mode must be one of: permissive, enforcing, disabled"))
	}

	errs = append(errs, c.Spec.ReadWriteRatesConfig.validate(path)...)
	errs = append(errs, c.Spec.ACL.validate(path.Child("acl"))...)
	errs = append(errs, c.Spec.Catalog.validate(path.Child("catalog"))...)
	errs = append(errs, c.Spec.ConfigEntry.validate(path.Child("configEntry"))...)
	errs = append(errs, c.Spec.ConnectCA.validate(path.Child("connectCA"))...)
	errs = append(errs, c.Spec.Coordinate.validate(path.Child("coordinate"))...)
	errs = append(errs, c.Spec.DiscoveryChain.validate(path.Child("discoveryChain"))...)
	errs = append(errs, c.Spec.Health.validate(path.Child("health"))...)
	errs = append(errs, c.Spec.Intention.validate(path.Child("intention"))...)
	errs = append(errs, c.Spec.KV.validate(path.Child("kv"))...)
	errs = append(errs, c.Spec.Tenancy.validate(path.Child("tenancy"))...)
	errs = append(errs, c.Spec.PreparedQuery.validate(path.Child("preparedQuery"))...)
	errs = append(errs, c.Spec.Session.validate(path.Child("session"))...)
	errs = append(errs, c.Spec.Txn.validate(path.Child("txn"))...)

	if len(errs) > 0 {
		return apierrors.NewInvalid(
			schema.GroupKind{Group: ConsulHashicorpGroup, Kind: ControlPlaneRequestLimitKubeKind},
			c.KubernetesName(), errs)
	}

	return nil
}

// DefaultNamespaceFields has no behaviour here as control-plane-request-limit have no namespace specific fields.
func (s *ControlPlaneRequestLimit) DefaultNamespaceFields(_ common.ConsulMeta) {
}
