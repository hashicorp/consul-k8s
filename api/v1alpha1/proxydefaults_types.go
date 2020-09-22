package v1alpha1

import (
	"github.com/hashicorp/consul/api"
	capi "github.com/hashicorp/consul/api"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// ProxyDefaults is the Schema for the proxydefaults API
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.conditions[?(@.type==\"Synced\")].status",description="The sync status of the resource with Consul"
type ProxyDefaults struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              ProxyDefaultsSpec `json:"spec,omitempty"`
	Status            `json:"status,omitempty"`
}

// RawExtension for Config based on recommendation here: https://github.com/kubernetes-sigs/controller-tools/issues/294#issuecomment-518380816

// ProxyDefaultsSpec defines the desired state of ProxyDefaults
type ProxyDefaultsSpec struct {
	// Config is an arbitrary map of configuration values used by Connect proxies.
	// Any values that your proxy allows can be configured globally here.
	Config runtime.RawExtension `json:"config,omitempty"`
	// MeshGateway controls the default mesh gateway configuration for this service.
	MeshGateway MeshGatewayConfig `json:"meshGateway,omitempty"`
	// Expose controls the default expose path configuration for Envoy.
	Expose ExposeConfig `json:"expose,omitempty"`
}

func (in *ProxyDefaults) GetObjectMeta() metav1.ObjectMeta {
	return in.ObjectMeta
}

func (in *ProxyDefaults) AddFinalizer(f string) {
	in.ObjectMeta.Finalizers = append(in.Finalizers(), f)
}

func (in *ProxyDefaults) RemoveFinalizer(f string) {
	var newFinalizers []string
	for _, oldF := range in.Finalizers() {
		if oldF != f {
			newFinalizers = append(newFinalizers, oldF)
		}
	}
	in.ObjectMeta.Finalizers = newFinalizers

}

func (in *ProxyDefaults) Finalizers() []string {
	return in.ObjectMeta.Finalizers
}

func (in *ProxyDefaults) Kind() string {
	return capi.ProxyDefaults
}

func (in *ProxyDefaults) Name() string {
	return in.ObjectMeta.Name
}

func (in *ProxyDefaults) SetSyncedCondition(status corev1.ConditionStatus, reason string, message string) {
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

func (in *ProxyDefaults) GetSyncedCondition() (status corev1.ConditionStatus, reason string, message string) {
	cond := in.Status.GetCondition(ConditionSynced)
	return cond.Status, cond.Reason, cond.Message
}

func (in *ProxyDefaults) GetSyncedConditionStatus() corev1.ConditionStatus {
	return in.Status.GetCondition(ConditionSynced).Status
}

func (in *ProxyDefaults) ToConsul() api.ConfigEntry {
	panic("implement me")
}

func (in *ProxyDefaults) MatchesConsul(entry api.ConfigEntry) bool {
	panic("implement me")
}

func (in *ProxyDefaults) Validate() error {
	panic("implement me")
}

// +kubebuilder:object:root=true

// ProxyDefaultsList contains a list of ProxyDefaults
type ProxyDefaultsList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ProxyDefaults `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ProxyDefaults{}, &ProxyDefaultsList{})
}
