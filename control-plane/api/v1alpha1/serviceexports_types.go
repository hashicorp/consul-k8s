package v1alpha1

import (
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	"github.com/hashicorp/consul/api"
	capi "github.com/hashicorp/consul/api"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const ServiceExportsKubeKind = "serviceexports"

func init() {
	SchemeBuilder.Register(&ServiceExports{}, &ServiceExportsList{})
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// ServiceExports is the Schema for the serviceexports API
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.conditions[?(@.type==\"Synced\")].status",description="The sync status of the resource with Consul"
// +kubebuilder:printcolumn:name="Last Synced",type="date",JSONPath=".status.lastSyncedTime",description="The last successful synced time of the resource with Consul"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of the resource"
type ServiceExports struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ServiceExportsSpec `json:"spec,omitempty"`
	Status `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ServiceExportsList contains a list of ServiceExports
type ServiceExportsList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ServiceExports `json:"items"`
}

// ServiceExportsSpec defines the desired state of ServiceExports
type ServiceExportsSpec struct {
	// Services is a list of services to be exported and the list of partitions
	// to expose them to.
	Services []ExportedService `json:"services,omitempty"`
}

// ExportedService manages the exporting of a service in the local partition to
// other partitions.
type ExportedService struct {
	// Name is the name of the service to be exported.
	Name string `json:"name,omitempty"`

	// Namespace is the namespace to export the service from.
	Namespace string `json:"namespace,omitempty"`

	// Consumers is a list of downstream consumers of the service to be exported.
	Consumers []ServiceConsumer `json:"consumers,omitempty"`
}

// ServiceConsumer represents a downstream consumer of the service to be exported.
type ServiceConsumer struct {
	// Partition is the admin partition to export the service to.
	Partition string `json:"partition,omitempty"`
}

func (in *ServiceExports) GetObjectMeta() metav1.ObjectMeta {
	return in.ObjectMeta
}

func (in *ServiceExports) AddFinalizer(name string) {
	in.ObjectMeta.Finalizers = append(in.Finalizers(), name)
}

func (in *ServiceExports) RemoveFinalizer(name string) {
	var newFinalizers []string
	for _, oldF := range in.Finalizers() {
		if oldF != name {
			newFinalizers = append(newFinalizers, oldF)
		}
	}
	in.ObjectMeta.Finalizers = newFinalizers
}

func (in *ServiceExports) Finalizers() []string {
	return in.ObjectMeta.Finalizers
}

func (in *ServiceExports) ConsulKind() string {
	return capi.ServiceExports
}

func (in *ServiceExports) ConsulGlobalResource() bool {
	return true
}

func (in *ServiceExports) ConsulMirroringNS() string {
	return common.DefaultConsulNamespace
}

func (in *ServiceExports) KubeKind() string {
	return ServiceExportsKubeKind
}

func (in *ServiceExports) ConsulName() string {
	return in.ObjectMeta.Name
}

func (in *ServiceExports) KubernetesName() string {
	return in.ObjectMeta.Name
}

func (in *ServiceExports) SetSyncedCondition(status corev1.ConditionStatus, reason, message string) {
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

func (in *ServiceExports) SetLastSyncedTime(time *metav1.Time) {
	in.Status.LastSyncedTime = time
}

func (in *ServiceExports) SyncedCondition() (status corev1.ConditionStatus, reason, message string) {
	cond := in.Status.GetCondition(ConditionSynced)
	if cond == nil {
		return corev1.ConditionUnknown, "", ""
	}
	return cond.Status, cond.Reason, cond.Message
}

func (in *ServiceExports) SyncedConditionStatus() corev1.ConditionStatus {
	cond := in.Status.GetCondition(ConditionSynced)
	if cond == nil {
		return corev1.ConditionUnknown
	}
	return cond.Status
}

func (in *ServiceExports) ToConsul(datacenter string) api.ConfigEntry {
	var services []capi.ExportedService
	for _, service := range in.Spec.Services {
		services = append(services, service.toConsul())
	}
	return &capi.ServiceExportsConfigEntry{
		Services: services,
		Meta:     meta(datacenter),
	}
}

func (in *ExportedService) toConsul() capi.ExportedService {
	var consumers []capi.ServiceConsumer
	for _, consumer := range in.Consumers {
		consumers = append(consumers, capi.ServiceConsumer{Partition: consumer.Partition})
	}
	return capi.ExportedService{
		Name:      in.Name,
		Namespace: in.Namespace,
		Consumers: consumers,
	}
}

func (in *ServiceExports) MatchesConsul(candidate api.ConfigEntry) bool {
	configEntry, ok := candidate.(*capi.ServiceExportsConfigEntry)
	if !ok {
		return false
	}
	// No datacenter is passed to ToConsul as we ignore the Meta field when checking for equality.
	return cmp.Equal(in.ToConsul(""), configEntry, cmpopts.IgnoreFields(capi.ServiceExportsConfigEntry{}, "Partition", "Meta", "ModifyIndex", "CreateIndex"), cmpopts.IgnoreUnexported(), cmpopts.EquateEmpty())

}

func (in *ServiceExports) Validate(_ bool) error {
	return nil
}

func (in *ServiceExports) DefaultNamespaceFields(_ bool, _ string, _ bool, _ string) {
}
