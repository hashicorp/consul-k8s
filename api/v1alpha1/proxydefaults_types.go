package v1alpha1

import (
	"encoding/json"
	"reflect"

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

func (in *ProxyDefaults) ConsulKind() string {
	return capi.ProxyDefaults
}

func (in *ProxyDefaults) KubeKind() string {
	return ProxyDefaultsKubeKind
}

func (in *ProxyDefaults) SyncedCondition() (status corev1.ConditionStatus, reason, message string) {
	cond := in.Status.GetCondition(ConditionSynced)
	return cond.Status, cond.Reason, cond.Message
}

func (in *ProxyDefaults) SyncedConditionStatus() corev1.ConditionStatus {
	return in.Status.GetCondition(ConditionSynced).Status
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

func (in *ProxyDefaults) ToConsul() api.ConfigEntry {
	consulConfig := in.convertConfig()
	return &capi.ProxyConfigEntry{
		Kind:        capi.ProxyDefaults,
		Name:        in.Name(),
		MeshGateway: in.Spec.MeshGateway.toConsul(),
		Expose:      in.Spec.Expose.toConsul(),
		Config:      consulConfig,
	}
}

func (in *ProxyDefaults) MatchesConsul(candidate api.ConfigEntry) bool {
	proxyDefCand, ok := candidate.(*capi.ProxyConfigEntry)
	if !ok {
		return false
	}
	return in.Name() == proxyDefCand.Name &&
		in.Spec.MeshGateway.Mode == string(proxyDefCand.MeshGateway.Mode) &&
		in.Spec.Expose.matches(proxyDefCand.Expose) &&
		in.matchesConfig(proxyDefCand.Config)
}

func (in *ProxyDefaults) Validate() error {
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
			schema.GroupKind{Group: ConsulHashicorpGroup, Kind: "proxydefaults"},
			in.Name(), allErrs)
	}

	return nil
}

// matchesConfig compares the values of the config on the spec and that on the
// the consul proxy-default and returns true if they match and false otherwise
func (in *ProxyDefaults) matchesConfig(config map[string]interface{}) bool {
	if in.Spec.Config == nil || config == nil {
		return in.Spec.Config == nil && config == nil
	}
	var inConfig map[string]interface{}
	if err := json.Unmarshal(in.Spec.Config, &inConfig); err != nil {
		return false
	}
	return reflect.DeepEqual(inConfig, config)
}

// convertConfig converts the config of type json.RawMessage which is stored
// by the resource into type map[string]interface{} which is saved by the
// consul API.
func (in *ProxyDefaults) convertConfig() map[string]interface{} {
	if in.Spec.Config == nil {
		return nil
	}
	var outConfig map[string]interface{}
	if err := json.Unmarshal(in.Spec.Config, &outConfig); err != nil {
		return nil
	}
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
		return field.Invalid(path, in.Spec.Config, `must be valid map value`)
	}
	return nil
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
