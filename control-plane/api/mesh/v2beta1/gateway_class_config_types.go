// // Copyright (c) HashiCorp, Inc.
// // SPDX-License-Identifier: MPL-2.0

package v2beta1

import (
	pbmesh "github.com/hashicorp/consul/proto-public/pbmesh/v2beta1"
	"github.com/hashicorp/consul/proto-public/pbresource"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
)

const (
	gatewayClassConfigKubeKind = "gatewayclassconfig"
)

func init() {
	MeshSchemeBuilder.Register(&GatewayClassConfig{}, &GatewayClassConfigList{})
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// GatewayClassConfig is the Schema for the Mesh Gateway API
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.conditions[?(@.type==\"Synced\")].status",description="The sync status of the resource with Consul"
// +kubebuilder:printcolumn:name="Last Synced",type="date",JSONPath=".status.lastSyncedTime",description="The last successful synced time of the resource with Consul"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of the resource"
// +kubebuilder:resource:shortName="gateway-class-config"
// +kubebuilder:resource:scope=Cluster
type GatewayClassConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GatewayClassConfigSpec `json:"spec,omitempty"`
	Status `json:"status,omitempty"`
}

type GatewayClassConfigSpec struct {
	GatewayClassAnnotationsAndLabels `json:",inline"`

	// Deployment contains config specific to the Deployment created from this GatewayClass
	Deployment GatewayClassDeploymentConfig `json:"deployment,omitempty"`
	// Role contains config specific to the Role created from this GatewayClass
	Role GatewayClassRoleConfig `json:"role,omitempty"`
	// RoleBinding contains config specific to the RoleBinding created from this GatewayClass
	RoleBinding GatewayClassRoleBindingConfig `json:"roleBinding,omitempty"`
	// Service contains config specific to the Service created from this GatewayClass
	Service GatewayClassServiceConfig `json:"service,omitempty"`
	// ServiceAccount contains config specific to the corev1.ServiceAccount created from this GatewayClass
	ServiceAccount GatewayClassServiceAccountConfig `json:"serviceAccount,omitempty"`
}

type GatewayClassDeploymentConfig struct {
	GatewayClassAnnotationsAndLabels `json:",inline"`

	// Container contains config specific to the created Deployment's container
	Container GatewayClassContainerConfig `json:"container,omitempty"`
	// InitContainer contains config specific to the created Deployment's init container
	InitContainer GatewayClassInitContainerConfig `json:"initContainer,omitempty"`
	// NodeSelector specifies the node selector to use on the created Deployment
	NodeSelector *corev1.NodeSelector `json:"nodeSelector,omitempty"`
	// PriorityClassName specifies the priority class name to use on the created Deployment
	PriorityClassName string `json:"priorityClassName"`
	// Tolerations specifies the tolerations to use on the created Deployment
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
}

type GatewayClassInitContainerConfig struct {
	// Consul specifies configuration for the consul-k8s-control-plane init container
	Consul GatewayClassConsulConfig `json:"consul,omitempty"`
	// Resources specifies the resource requirements for the created Deployment
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
}

type GatewayClassContainerConfig struct {
	// Consul specifies configuration for the consul-dataplane container
	Consul GatewayClassConsulConfig `json:"consul,omitempty"`
	// Resources specifies the resource requirements for the created Deployment
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
}

type GatewayClassRoleConfig struct {
	GatewayClassAnnotationsAndLabels `json:",inline"`
}

type GatewayClassRoleBindingConfig struct {
	GatewayClassAnnotationsAndLabels `json:",inline"`
}

type GatewayClassServiceConfig struct {
	GatewayClassAnnotationsAndLabels `json:",inline"`

	// Type specifies the type of Service to use (LoadBalancer, ClusterIP, etc.)
	Type *corev1.ServiceType `json:"type,omitempty"`
}

type GatewayClassServiceAccountConfig struct {
	GatewayClassAnnotationsAndLabels `json:",inline"`
}

type GatewayClassConsulConfig struct {
	// Logging specifies the logging configuration for Consul Dataplane
	Logging GatewayClassConsulLoggingConfig `json:"logging,omitempty"`
}

type GatewayClassConsulLoggingConfig struct {
	// Level sets the logging level for Consul Dataplane (debug, info, etc.)
	Level string `json:"level,omitempty"`
}

// GatewayClassAnnotationsAndLabels exists to provide a commonly-embedded wrapper
// for Annotations and Labels on a given resource configuration
type GatewayClassAnnotationsAndLabels struct {
	// Annotations are applied to the created resource
	Annotations GatewayClassAnnotationsLabelsConfig `json:"annotations,omitempty"`
	// Labels are applied to the created resource
	Labels GatewayClassAnnotationsLabelsConfig `json:"labels,omitempty"`
}

type GatewayClassAnnotationsLabelsConfig struct {
	// InheritFromGateway lists the names/keys of annotations or labels to copy from the Gateway resource.
	// Any name/key included here will override those in Set if specified on the Gateway.
	InheritFromGateway []string `json:"inheritFromGateway,omitempty"`
	// Set lists the names/keys and values of annotations or labels to set on the resource.
	// Any name/key included here will be overridden if present in InheritFromGateway and set on the Gateway.
	Set map[string]string `json:"set,omitempty"`
}

// +kubebuilder:object:root=true

// GatewayClassConfigList contains a list of GatewayClassConfig.
type GatewayClassConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []*GatewayClassConfig `json:"items"`
}

func (in *GatewayClassConfig) ResourceID(namespace, partition string) *pbresource.ID {
	return &pbresource.ID{
		Name: in.Name,
		Type: pbmesh.GatewayClassConfigType,
		Tenancy: &pbresource.Tenancy{
			Partition: partition,
			Namespace: namespace,

			// Because we are explicitly defining NS/partition, this will not default and must be explicit.
			// At a future point, this will move out of the Tenancy block.
			PeerName: constants.DefaultConsulPeer,
		},
	}
}

func (in *GatewayClassConfig) Resource(namespace, partition string) *pbresource.Resource {
	// GatewayClassConfig as defined above only exists in Kubernetes and is not synced to Consul
	return nil
}

func (in *GatewayClassConfig) AddFinalizer(f string) {
	in.ObjectMeta.Finalizers = append(in.Finalizers(), f)
}

func (in *GatewayClassConfig) RemoveFinalizer(f string) {
	var newFinalizers []string
	for _, oldF := range in.Finalizers() {
		if oldF != f {
			newFinalizers = append(newFinalizers, oldF)
		}
	}
	in.ObjectMeta.Finalizers = newFinalizers
}

func (in *GatewayClassConfig) Finalizers() []string {
	return in.ObjectMeta.Finalizers
}

func (in *GatewayClassConfig) MatchesConsul(candidate *pbresource.Resource, namespace, partition string) bool {
	// GatewayClassConfig as defined above only exists in Kubernetes and is not synced to Consul
	return true
}

func (in *GatewayClassConfig) KubeKind() string {
	return gatewayClassConfigKubeKind
}

func (in *GatewayClassConfig) KubernetesName() string {
	return in.ObjectMeta.Name
}

func (in *GatewayClassConfig) SetSyncedCondition(status corev1.ConditionStatus, reason, message string) {
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

func (in *GatewayClassConfig) SetLastSyncedTime(time *metav1.Time) {
	in.Status.LastSyncedTime = time
}

func (in *GatewayClassConfig) SyncedCondition() (status corev1.ConditionStatus, reason, message string) {
	cond := in.Status.GetCondition(ConditionSynced)
	if cond == nil {
		return corev1.ConditionUnknown, "", ""
	}
	return cond.Status, cond.Reason, cond.Message
}

func (in *GatewayClassConfig) SyncedConditionStatus() corev1.ConditionStatus {
	condition := in.Status.GetCondition(ConditionSynced)
	if condition == nil {
		return corev1.ConditionUnknown
	}
	return condition.Status
}

func (in *GatewayClassConfig) Validate(tenancy common.ConsulTenancyConfig) error {
	return nil
}

// DefaultNamespaceFields is required as part of the common.MeshConfig interface.
func (in *GatewayClassConfig) DefaultNamespaceFields(tenancy common.ConsulTenancyConfig) {}
