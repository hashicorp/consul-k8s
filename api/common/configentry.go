package common

import (
	"github.com/hashicorp/consul/api"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// ConfigEntryResource is a generic config entry custom resource. It is implemented
// by each config entry type so that they can be acted upon generically.
// It is not tied to a specific CRD version.
type ConfigEntryResource interface {
	// GetObjectMeta returns object meta.
	GetObjectMeta() metav1.ObjectMeta
	// AddFinalizer adds a finalizer to the list of finalizers.
	AddFinalizer(string)
	// RemoveFinalizer removes this finalizer from the list.
	RemoveFinalizer(string)
	// Finalizers returns the list of finalizers for this object.
	Finalizers() []string
	// Kind returns the Consul config entry kind, i.e. service-defaults, not
	// ServiceDefaults.
	Kind() string
	// Name returns the name of the config entry.
	Name() string
	// SetSyncedCondition update the synced condition.
	SetSyncedCondition(status corev1.ConditionStatus, reason string, message string)
	// GetSyncedCondition gets the synced condition.
	GetSyncedCondition() (status corev1.ConditionStatus, reason string, message string)
	// GetSyncedConditionStatus returns the status of the synced condition.
	GetSyncedConditionStatus() corev1.ConditionStatus
	// ToConsul converts the CRD to the corresponding Consul API definition.
	// Its return type is the generic ConfigEntry but a specific config entry
	// type should be constructed e.g. ServiceConfigEntry.
	ToConsul() api.ConfigEntry
	// MatchesConsul returns true if the CRD has the same fields as the Consul
	// config entry.
	MatchesConsul(api.ConfigEntry) bool
	// GetObjectKind should be implemented by the generated code.
	GetObjectKind() schema.ObjectKind
	// DeepCopyObject should be implemented by the generated code.
	DeepCopyObject() runtime.Object
	// Validate returns an error if the CRD is invalid.
	Validate() error
}
