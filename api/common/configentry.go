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
	AddFinalizer(name string)
	// RemoveFinalizer removes this finalizer from the list.
	RemoveFinalizer(name string)
	// Finalizers returns the list of finalizers for this object.
	Finalizers() []string
	// ConsulKind returns the Consul config entry kind, i.e. service-defaults, not
	// servicedefaults.
	ConsulKind() string
	// ConsulGlobalResource returns if the resource exists in the default
	// Consul namespace only.
	ConsulGlobalResource() bool
	// ConsulMirroringNS returns the Consul namespace that the config entry should
	// be created in if namespaces and mirroring are enabled.
	ConsulMirroringNS() string
	// KubeKind returns the Kube config entry kind, i.e. servicedefaults, not
	// service-defaults.
	KubeKind() string
	// ConsulName returns the name of the config entry as saved in Consul.
	// This may be different than KubernetesName() in the case of a ServiceIntentions
	// config entry.
	ConsulName() string
	// KubernetesName returns the name of the Kubernetes resource.
	KubernetesName() string
	// SetSyncedCondition updates the synced condition.
	SetSyncedCondition(status corev1.ConditionStatus, reason, message string)
	// SyncedCondition gets the synced condition.
	SyncedCondition() (status corev1.ConditionStatus, reason, message string)
	// SyncedConditionStatus returns the status of the synced condition.
	SyncedConditionStatus() corev1.ConditionStatus
	// ToConsul converts the resource to the corresponding Consul API definition.
	// Its return type is the generic ConfigEntry but a specific config entry
	// type should be constructed e.g. ServiceConfigEntry.
	ToConsul(datacenter string) api.ConfigEntry
	// MatchesConsul returns true if the resource has the same fields as the Consul
	// config entry.
	MatchesConsul(candidate api.ConfigEntry) bool
	// GetObjectKind should be implemented by the generated code.
	GetObjectKind() schema.ObjectKind
	// DeepCopyObject should be implemented by the generated code.
	DeepCopyObject() runtime.Object
	// Validate returns an error if the resource is invalid.
	Validate(namespacesEnabled bool) error
	// DefaultNamespaceFields sets Consul namespace fields on the config entry
	// spec to their default values if namespaces are enabled.
	DefaultNamespaceFields(consulNamespacesEnabled bool, destinationNamespace string, mirroring bool, prefix string)

	// ConfigEntryResource has to implement metav1.Object so that structs
	// that implement it effectively implement client.Object which is
	// the interface supported by controller-runtime reconcile-able resources.
	metav1.Object
}
