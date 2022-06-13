package v1alpha1

import (
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

const ExportedServicesKubeKind = "exportedservices"

func init() {
	SchemeBuilder.Register(&ExportedServices{}, &ExportedServicesList{})
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// ExportedServices is the Schema for the exportedservices API
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.conditions[?(@.type==\"Synced\")].status",description="The sync status of the resource with Consul"
// +kubebuilder:printcolumn:name="Last Synced",type="date",JSONPath=".status.lastSyncedTime",description="The last successful synced time of the resource with Consul"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of the resource"
// +kubebuilder:resource:shortName="exported-services"
type ExportedServices struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ExportedServicesSpec `json:"spec,omitempty"`
	Status `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ExportedServicesList contains a list of ExportedServices.
type ExportedServicesList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ExportedServices `json:"items"`
}

// ExportedServicesSpec defines the desired state of ExportedServices.
type ExportedServicesSpec struct {
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
	// [Experimental] PeerName is the name of the peer to export the service to.
	PeerName string `json:"peerName,omitempty"`
}

func (in *ExportedServices) GetObjectMeta() metav1.ObjectMeta {
	return in.ObjectMeta
}

func (in *ExportedServices) AddFinalizer(name string) {
	in.ObjectMeta.Finalizers = append(in.Finalizers(), name)
}

func (in *ExportedServices) RemoveFinalizer(name string) {
	var newFinalizers []string
	for _, oldF := range in.Finalizers() {
		if oldF != name {
			newFinalizers = append(newFinalizers, oldF)
		}
	}
	in.ObjectMeta.Finalizers = newFinalizers
}

func (in *ExportedServices) Finalizers() []string {
	return in.ObjectMeta.Finalizers
}

func (in *ExportedServices) ConsulKind() string {
	return capi.ExportedServices
}

func (in *ExportedServices) ConsulGlobalResource() bool {
	return true
}

func (in *ExportedServices) ConsulMirroringNS() string {
	return common.DefaultConsulNamespace
}

func (in *ExportedServices) KubeKind() string {
	return ExportedServicesKubeKind
}

func (in *ExportedServices) ConsulName() string {
	return in.ObjectMeta.Name
}

func (in *ExportedServices) KubernetesName() string {
	return in.ObjectMeta.Name
}

func (in *ExportedServices) SetSyncedCondition(status corev1.ConditionStatus, reason, message string) {
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

func (in *ExportedServices) SetLastSyncedTime(time *metav1.Time) {
	in.Status.LastSyncedTime = time
}

func (in *ExportedServices) SyncedCondition() (status corev1.ConditionStatus, reason, message string) {
	cond := in.Status.GetCondition(ConditionSynced)
	if cond == nil {
		return corev1.ConditionUnknown, "", ""
	}
	return cond.Status, cond.Reason, cond.Message
}

func (in *ExportedServices) SyncedConditionStatus() corev1.ConditionStatus {
	cond := in.Status.GetCondition(ConditionSynced)
	if cond == nil {
		return corev1.ConditionUnknown
	}
	return cond.Status
}

func (in *ExportedServices) ToConsul(datacenter string) api.ConfigEntry {
	var services []capi.ExportedService
	for _, service := range in.Spec.Services {
		services = append(services, service.toConsul())
	}
	return &capi.ExportedServicesConfigEntry{
		Name:     in.Name,
		Services: services,
		Meta:     meta(datacenter),
	}
}

func (in *ExportedService) toConsul() capi.ExportedService {
	var consumers []capi.ServiceConsumer
	for _, consumer := range in.Consumers {
		if consumer.PeerName != "" {
			consumers = append(consumers, capi.ServiceConsumer{PeerName: consumer.PeerName})
		} else {
			consumers = append(consumers, capi.ServiceConsumer{Partition: consumer.Partition})
		}
	}
	return capi.ExportedService{
		Name:      in.Name,
		Namespace: in.Namespace,
		Consumers: consumers,
	}
}

func (in *ExportedServices) MatchesConsul(candidate api.ConfigEntry) bool {
	configEntry, ok := candidate.(*capi.ExportedServicesConfigEntry)
	if !ok {
		return false
	}
	// No datacenter is passed to ToConsul as we ignore the Meta field when checking for equality.
	return cmp.Equal(in.ToConsul(""), configEntry, cmpopts.IgnoreFields(capi.ExportedServicesConfigEntry{}, "Partition", "Meta", "ModifyIndex", "CreateIndex"), cmpopts.IgnoreUnexported(), cmpopts.EquateEmpty())

}

func (in *ExportedServices) Validate(consulMeta common.ConsulMeta) error {
	var errs field.ErrorList
	if consulMeta.PartitionsEnabled && in.Name != consulMeta.Partition {
		errs = append(errs, field.Invalid(field.NewPath("name"), in.Name, fmt.Sprintf(`%s resource name must be the same name as the partition, "%s"`, in.KubeKind(), consulMeta.Partition)))
	} else if !consulMeta.PartitionsEnabled && in.Name != "default" {
		errs = append(errs, field.Invalid(field.NewPath("name"), in.Name, fmt.Sprintf(`%s resource name must be "default"`, in.KubeKind())))
	}
	if len(in.Spec.Services) == 0 {
		errs = append(errs, field.Invalid(field.NewPath("spec").Child("services"), in.Spec.Services, "at least one service must be exported"))
	}
	for i, service := range in.Spec.Services {
		if err := service.validate(field.NewPath("spec").Child("services").Index(i), consulMeta); err != nil {
			errs = append(errs, err...)
		}
	}
	if len(errs) > 0 {
		return apierrors.NewInvalid(
			schema.GroupKind{Group: ConsulHashicorpGroup, Kind: ExportedServicesKubeKind},
			in.KubernetesName(), errs)
	}
	return nil
}

func (in *ExportedService) validate(path *field.Path, consulMeta common.ConsulMeta) field.ErrorList {
	var errs field.ErrorList
	if len(in.Consumers) == 0 {
		errs = append(errs, field.Invalid(path, in.Consumers, "service must have at least 1 consumer."))
	}
	if !consulMeta.NamespacesEnabled && in.Namespace != "" {
		errs = append(errs, field.Invalid(path, in.Namespace, "Consul Namespaces must be enabled to specify service namespace."))
	}
	for i, consumer := range in.Consumers {
		if err := consumer.validate(path.Child("consumers").Index(i), consulMeta); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

func (in *ServiceConsumer) validate(path *field.Path, consulMeta common.ConsulMeta) *field.Error {
	if in.Partition != "" && in.PeerName != "" {
		return field.Invalid(path, *in, "both partition and peerName cannot be specified.")
	}
	if in.Partition == "" && in.PeerName == "" {
		return field.Invalid(path, *in, "either partition or peerName must be specified.")
	}
	if !consulMeta.PartitionsEnabled && in.Partition != "" {
		return field.Invalid(path.Child("partitions"), in.Partition, "Consul Admin Partitions need to be enabled to specify partition.")
	}
	return nil
}

func (in *ExportedServices) DefaultNamespaceFields(_ common.ConsulMeta) {
}
