// // Copyright (c) HashiCorp, Inc.
// // SPDX-License-Identifier: MPL-2.0
package v2beta1

import (
	"github.com/hashicorp/consul/proto-public/pbresource"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hashicorp/consul-k8s/control-plane/api/common"
)

const (
	grpcRoutesKubeKind = "grpcRoutes"
)

func init() {
	MeshSchemeBuilder.Register(&GRPCRoutes{}, &GRPCRoutesList{})
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// GRPCRoutes is the Schema for the GRPC Routes API
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.conditions[?(@.type==\"Synced\")].status",description="The sync status of the resource with Consul"
// +kubebuilder:printcolumn:name="Last Synced",type="date",JSONPath=".status.lastSyncedTime",description="The last successful synced time of the resource with Consul"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of the resource"
// +kubebuilder:resource:shortName="grpc-routes"
type GRPCRoutes struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GRPCRoutesSpec `json:"spec,omitempty"`
	Status `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// GRPCRoutesList contains a list of GRPCRoutes.
type GRPCRoutesList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GRPCRoutes `json:"items"`
}

type GRPCRoutesSpec struct {
}

func (in *GRPCRoutes) ResourceID(namespace, partition string) *pbresource.ID {
	//TODO implement me
	panic("implement me")
}

func (in *GRPCRoutes) Resource(namespace, partition string) *pbresource.Resource {
	//TODO implement me
	panic("implement me")
}

func (in *GRPCRoutes) AddFinalizer(name string) {
	//TODO implement me
	panic("implement me")
}

func (in *GRPCRoutes) RemoveFinalizer(name string) {
	//TODO implement me
	panic("implement me")
}

func (in *GRPCRoutes) Finalizers() []string {
	//TODO implement me
	panic("implement me")
}

func (in *GRPCRoutes) MatchesConsul(candidate *pbresource.Resource, namespace, partition string) bool {
	//TODO implement me
	panic("implement me")
}

func (in *GRPCRoutes) KubeKind() string {
	//TODO implement me
	panic("implement me")
}

func (in *GRPCRoutes) KubernetesName() string {
	//TODO implement me
	panic("implement me")
}

func (in *GRPCRoutes) SetSyncedCondition(status corev1.ConditionStatus, reason, message string) {
	//TODO implement me
	panic("implement me")
}

func (in *GRPCRoutes) SetLastSyncedTime(time *metav1.Time) {
	//TODO implement me
	panic("implement me")
}

func (in *GRPCRoutes) SyncedCondition() (status corev1.ConditionStatus, reason, message string) {
	//TODO implement me
	panic("implement me")
}

func (in *GRPCRoutes) SyncedConditionStatus() corev1.ConditionStatus {
	//TODO implement me
	panic("implement me")
}

func (in *GRPCRoutes) Validate(tenancy common.ConsulTenancyConfig) error {
	//TODO implement me
	panic("implement me")
}

func (in *GRPCRoutes) DefaultNamespaceFields(tenancy common.ConsulTenancyConfig) {
	//TODO implement me
	panic("implement me")
}
