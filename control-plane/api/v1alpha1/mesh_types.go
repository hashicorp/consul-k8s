package v1alpha1

import (
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	capi "github.com/hashicorp/consul/api"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	MeshKubeKind = "mesh"
)

func init() {
	SchemeBuilder.Register(&Mesh{}, &MeshList{})
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Mesh is the Schema for the mesh API
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.conditions[?(@.type==\"Synced\")].status",description="The sync status of the resource with Consul"
// +kubebuilder:printcolumn:name="Last Synced",type="date",JSONPath=".status.lastSyncedTime",description="The last successful synced time of the resource with Consul"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of the resource"
type Mesh struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MeshSpec `json:"spec,omitempty"`
	Status `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// MeshList contains a list of Mesh
type MeshList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Mesh `json:"items"`
}

// MeshSpec defines the desired state of Mesh
type MeshSpec struct {
	TransparentProxy TransparentProxyMeshConfig `json:"transparentProxy,omitempty"`
}

// TransparentProxyMeshConfig controls configuration specific to proxies in "transparent" mode. Added in v1.10.0.
type TransparentProxyMeshConfig struct {
	// MeshDestinationsOnly determines whether sidecar proxies operating in "transparent" mode can proxy traffic
	// to IP addresses not registered in Consul's catalog. If enabled, traffic will only be proxied to upstreams
	// with service registrations in the catalog.
	MeshDestinationsOnly bool `json:"meshDestinationsOnly,omitempty"`
}

func (in *TransparentProxyMeshConfig) toConsul() capi.TransparentProxyMeshConfig {
	return capi.TransparentProxyMeshConfig{MeshDestinationsOnly: in.MeshDestinationsOnly}
}

func (in *Mesh) GetObjectMeta() metav1.ObjectMeta {
	return in.ObjectMeta
}

func (in *Mesh) AddFinalizer(name string) {
	in.ObjectMeta.Finalizers = append(in.Finalizers(), name)
}

func (in *Mesh) RemoveFinalizer(name string) {
	var newFinalizers []string
	for _, oldF := range in.Finalizers() {
		if oldF != name {
			newFinalizers = append(newFinalizers, oldF)
		}
	}
	in.ObjectMeta.Finalizers = newFinalizers

}

func (in *Mesh) Finalizers() []string {
	return in.ObjectMeta.Finalizers
}

func (in *Mesh) ConsulKind() string {
	return capi.MeshConfig
}

func (in *Mesh) ConsulMirroringNS() string {
	return common.DefaultConsulNamespace
}

func (in *Mesh) KubeKind() string {
	return MeshKubeKind
}

func (in *Mesh) SyncedCondition() (status corev1.ConditionStatus, reason, message string) {
	cond := in.Status.GetCondition(ConditionSynced)
	if cond == nil {
		return corev1.ConditionUnknown, "", ""
	}
	return cond.Status, cond.Reason, cond.Message
}

func (in *Mesh) SyncedConditionStatus() corev1.ConditionStatus {
	cond := in.Status.GetCondition(ConditionSynced)
	if cond == nil {
		return corev1.ConditionUnknown
	}
	return cond.Status
}

func (in *Mesh) ConsulName() string {
	return in.ObjectMeta.Name
}

func (in *Mesh) ConsulGlobalResource() bool {
	return true
}

func (in *Mesh) KubernetesName() string {
	return in.ObjectMeta.Name
}

func (in *Mesh) SetSyncedCondition(status corev1.ConditionStatus, reason string, message string) {
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

func (in *Mesh) SetLastSyncedTime(time *metav1.Time) {
	in.Status.LastSyncedTime = time
}

func (in *Mesh) ToConsul(datacenter string) capi.ConfigEntry {
	return &capi.MeshConfigEntry{
		TransparentProxy: in.Spec.TransparentProxy.toConsul(),
		Meta:             meta(datacenter),
	}
}

func (in *Mesh) MatchesConsul(candidate capi.ConfigEntry) bool {
	configEntry, ok := candidate.(*capi.MeshConfigEntry)
	if !ok {
		return false
	}
	// No datacenter is passed to ToConsul as we ignore the Meta field when checking for equality.
	return cmp.Equal(in.ToConsul(""), configEntry, cmpopts.IgnoreFields(capi.MeshConfigEntry{}, "Partition", "Namespace", "Meta", "ModifyIndex", "CreateIndex"), cmpopts.IgnoreUnexported(), cmpopts.EquateEmpty())
}

func (in *Mesh) Validate(_ common.ConsulMeta) error {
	return nil
}

// DefaultNamespaceFields has no behaviour here as meshes have no namespace specific fields.
func (in *Mesh) DefaultNamespaceFields(_ common.ConsulMeta) {
}
