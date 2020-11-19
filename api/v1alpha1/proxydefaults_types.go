package v1alpha1

import (
	"encoding/json"
	"fmt"

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

const (
	ProxyDefaultsKubeKind string = "proxydefaults"
)

func init() {
	SchemeBuilder.Register(&ProxyDefaults{}, &ProxyDefaultsList{})
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// ProxyDefaults is the Schema for the proxydefaults API
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.conditions[?(@.type==\"Synced\")].status",description="The sync status of the resource with Consul"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of the resource"
type ProxyDefaults struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              ProxyDefaultsSpec `json:"spec,omitempty"`
	Status            `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ProxyDefaultsList contains a list of ProxyDefaults
type ProxyDefaultsList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ProxyDefaults `json:"items"`
}

// RawMessage for Config based on recommendation here: https://github.com/kubernetes-sigs/controller-tools/issues/294#issuecomment-518380816

// ProxyDefaultsSpec defines the desired state of ProxyDefaults
type ProxyDefaultsSpec struct {
	// Config is an arbitrary map of configuration values used by Connect proxies.
	// Any values that your proxy allows can be configured globally here.
	// Supports JSON config values. See https://www.consul.io/docs/connect/proxies/envoy#configuration-formatting
	Config json.RawMessage `json:"config,omitempty"`
	// MeshGateway controls the default mesh gateway configuration for this service.
	MeshGateway MeshGatewayConfig `json:"meshGateway,omitempty"`
	// Expose controls the default expose path configuration for Envoy.
	Expose ExposeConfig `json:"expose,omitempty"`
}

func (in *ProxyDefaults) GetObjectMeta() metav1.ObjectMeta {
	return in.ObjectMeta
}

func (in *ProxyDefaults) AddFinalizer(name string) {
	in.ObjectMeta.Finalizers = append(in.Finalizers(), name)
}

func (in *ProxyDefaults) RemoveFinalizer(name string) {
	var newFinalizers []string
	for _, oldF := range in.Finalizers() {
		if oldF != name {
			newFinalizers = append(newFinalizers, oldF)
		}
	}
	in.ObjectMeta.Finalizers = newFinalizers

}

func (in *ProxyDefaults) Finalizers() []string {
	return in.ObjectMeta.Finalizers
}

func (in *ProxyDefaults) ConsulKind() string {
	return capi.ProxyDefaults
}

func (in *ProxyDefaults) ConsulMirroringNS() string {
	return common.DefaultConsulNamespace
}

func (in *ProxyDefaults) KubeKind() string {
	return ProxyDefaultsKubeKind
}

func (in *ProxyDefaults) SyncedCondition() (status corev1.ConditionStatus, reason, message string) {
	cond := in.Status.GetCondition(ConditionSynced)
	if cond == nil {
		return corev1.ConditionUnknown, "", ""
	}
	return cond.Status, cond.Reason, cond.Message
}

func (in *ProxyDefaults) SyncedConditionStatus() corev1.ConditionStatus {
	cond := in.Status.GetCondition(ConditionSynced)
	if cond == nil {
		return corev1.ConditionUnknown
	}
	return cond.Status
}

func (in *ProxyDefaults) ConsulName() string {
	return in.ObjectMeta.Name
}

func (in *ProxyDefaults) ConsulGlobalResource() bool {
	return true
}

func (in *ProxyDefaults) KubernetesName() string {
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

func (in *ProxyDefaults) ToConsul(datacenter string) capi.ConfigEntry {
	consulConfig := in.convertConfig()
	return &capi.ProxyConfigEntry{
		Kind:        in.ConsulKind(),
		Name:        in.ConsulName(),
		MeshGateway: in.Spec.MeshGateway.toConsul(),
		Expose:      in.Spec.Expose.toConsul(),
		Config:      consulConfig,
		Meta:        meta(datacenter),
	}
}

func (in *ProxyDefaults) MatchesConsul(candidate api.ConfigEntry) bool {
	configEntry, ok := candidate.(*capi.ProxyConfigEntry)
	if !ok {
		return false
	}
	// No datacenter is passed to ToConsul as we ignore the Meta field when checking for equality.
	return cmp.Equal(in.ToConsul(""), configEntry, cmpopts.IgnoreFields(capi.ProxyConfigEntry{}, "Namespace", "Meta", "ModifyIndex", "CreateIndex"), cmpopts.IgnoreUnexported(), cmpopts.EquateEmpty())
}

func (in *ProxyDefaults) Validate(namespacesEnabled bool) error {
	var allErrs field.ErrorList
	path := field.NewPath("spec")

	if err := in.Spec.MeshGateway.validate(path.Child("meshGateway")); err != nil {
		allErrs = append(allErrs, err)
	}
	if err := in.validateConfig(path.Child("config")); err != nil {
		allErrs = append(allErrs, err)
	}
	allErrs = append(allErrs, in.Spec.Expose.validate(path.Child("expose"))...)
	if len(allErrs) > 0 {
		return apierrors.NewInvalid(
			schema.GroupKind{Group: ConsulHashicorpGroup, Kind: ProxyDefaultsKubeKind},
			in.KubernetesName(), allErrs)
	}

	return nil
}

// convertConfig converts the config of type json.RawMessage which is stored
// by the resource into type map[string]interface{} which is saved by the
// consul API.
func (in *ProxyDefaults) convertConfig() map[string]interface{} {
	if in.Spec.Config == nil {
		return nil
	}
	var outConfig map[string]interface{}
	// We explicitly ignore the error returned by Unmarshall
	// because validate() ensures that if we get to here that it
	// won't return an error.
	json.Unmarshal(in.Spec.Config, &outConfig)
	return outConfig
}

// validateConfig attempts to unmarshall the provided config into a map[string]interface{}
// and returns an error if the provided value for config isn't successfully unmarshalled
// and it implies the provided value is an invalid config.
func (in *ProxyDefaults) validateConfig(path *field.Path) *field.Error {
	if in.Spec.Config == nil {
		return nil
	}
	var outConfig map[string]interface{}
	if err := json.Unmarshal(in.Spec.Config, &outConfig); err != nil {
		return field.Invalid(path, in.Spec.Config, fmt.Sprintf(`must be valid map value: %s`, err))
	}
	return nil
}
