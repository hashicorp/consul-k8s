package v1alpha1

import (
	"errors"
	"fmt"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	"github.com/hashicorp/consul/api"
	capi "github.com/hashicorp/consul/api"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

const PartitionExportsKubeKind = "partitionexports"

func init() {
	SchemeBuilder.Register(&PartitionExports{}, &PartitionExportsList{})
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// PartitionExports is the Schema for the partitionexports API
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.conditions[?(@.type==\"Synced\")].status",description="The sync status of the resource with Consul"
// +kubebuilder:printcolumn:name="Last Synced",type="date",JSONPath=".status.lastSyncedTime",description="The last successful synced time of the resource with Consul"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of the resource"
type PartitionExports struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PartitionExportsSpec `json:"spec,omitempty"`
	Status `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// PartitionExportsList contains a list of PartitionExports
type PartitionExportsList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PartitionExports `json:"items"`
}

// PartitionExportsSpec defines the desired state of PartitionExports
type PartitionExportsSpec struct {
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

func (in *PartitionExports) GetObjectMeta() metav1.ObjectMeta {
	return in.ObjectMeta
}

func (in *PartitionExports) AddFinalizer(name string) {
	in.ObjectMeta.Finalizers = append(in.Finalizers(), name)
}

func (in *PartitionExports) RemoveFinalizer(name string) {
	var newFinalizers []string
	for _, oldF := range in.Finalizers() {
		if oldF != name {
			newFinalizers = append(newFinalizers, oldF)
		}
	}
	in.ObjectMeta.Finalizers = newFinalizers
}

func (in *PartitionExports) Finalizers() []string {
	return in.ObjectMeta.Finalizers
}

func (in *PartitionExports) ConsulKind() string {
	return capi.PartitionExports
}

func (in *PartitionExports) ConsulGlobalResource() bool {
	return true
}

func (in *PartitionExports) ConsulMirroringNS() string {
	return common.DefaultConsulNamespace
}

func (in *PartitionExports) KubeKind() string {
	return PartitionExportsKubeKind
}

func (in *PartitionExports) ConsulName() string {
	return in.ObjectMeta.Name
}

func (in *PartitionExports) KubernetesName() string {
	return in.ObjectMeta.Name
}

func (in *PartitionExports) SetSyncedCondition(status corev1.ConditionStatus, reason, message string) {
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

func (in *PartitionExports) SetLastSyncedTime(time *metav1.Time) {
	in.Status.LastSyncedTime = time
}

func (in *PartitionExports) SyncedCondition() (status corev1.ConditionStatus, reason, message string) {
	cond := in.Status.GetCondition(ConditionSynced)
	if cond == nil {
		return corev1.ConditionUnknown, "", ""
	}
	return cond.Status, cond.Reason, cond.Message
}

func (in *PartitionExports) SyncedConditionStatus() corev1.ConditionStatus {
	cond := in.Status.GetCondition(ConditionSynced)
	if cond == nil {
		return corev1.ConditionUnknown
	}
	return cond.Status
}

func (in *PartitionExports) ToConsul(datacenter string) api.ConfigEntry {
	var services []capi.ExportedService
	for _, service := range in.Spec.Services {
		services = append(services, service.toConsul())
	}
	return &capi.PartitionExportsConfigEntry{
		Name:     in.Name,
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

func (in *PartitionExports) MatchesConsul(candidate api.ConfigEntry) bool {
	configEntry, ok := candidate.(*capi.PartitionExportsConfigEntry)
	if !ok {
		return false
	}
	// No datacenter is passed to ToConsul as we ignore the Meta field when checking for equality.
	return cmp.Equal(in.ToConsul(""), configEntry, cmpopts.IgnoreFields(capi.PartitionExportsConfigEntry{}, "Partition", "Meta", "ModifyIndex", "CreateIndex"), cmpopts.IgnoreUnexported(), cmpopts.EquateEmpty())

}

func (in *PartitionExports) Validate(consulMeta common.ConsulMeta) error {
	var errs field.ErrorList
	if !consulMeta.PartitionsEnabled {
		return apierrors.NewForbidden(
			schema.GroupResource{Group: ConsulHashicorpGroup, Resource: common.PartitionExports},
			in.KubernetesName(),
			errors.New("Consul Enterprise Admin Partitions must be enabled to create PartitionExports"))
	}
	if in.Name != consulMeta.Partition {
		errs = append(errs, field.Invalid(field.NewPath("name"), in.Name, fmt.Sprintf(`%s resource name must be the same name as the partition, "%s"`, in.KubeKind(), consulMeta.Partition)))
	}
	if len(in.Spec.Services) == 0 {
		errs = append(errs, field.Invalid(field.NewPath("spec").Child("services"), in.Spec.Services, "at least one service must be exported"))
	}
	for i, service := range in.Spec.Services {
		if err := service.validate(field.NewPath("spec").Child("services").Index(i)); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return apierrors.NewInvalid(
			schema.GroupKind{Group: ConsulHashicorpGroup, Kind: PartitionExportsKubeKind},
			in.KubernetesName(), errs)
	}
	return nil
}

func (in *ExportedService) validate(path *field.Path) *field.Error {
	if len(in.Consumers) == 0 {
		return field.Invalid(path, in.Consumers, "service must have at least 1 consumer.")
	}
	return nil
}

func (in *PartitionExports) DefaultNamespaceFields(_ common.ConsulMeta) {
}
