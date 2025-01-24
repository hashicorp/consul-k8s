// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0
package v2beta1

import (
	"fmt"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	inject "github.com/hashicorp/consul-k8s/control-plane/connect-inject/common"
	pbmesh "github.com/hashicorp/consul/proto-public/pbmesh/v2beta1"
	"github.com/hashicorp/consul/proto-public/pbresource"
	"google.golang.org/protobuf/testing/protocmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	meshConfigurationKind = "meshconfiguration"
)

func init() {
	MeshSchemeBuilder.Register(&MeshConfiguration{}, &MeshConfigurationList{})
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// MeshConfiguration is the Schema for the Mesh Configuration
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.conditions[?(@.type==\"Synced\")].status",description="The sync status of the resource with Consul"
// +kubebuilder:printcolumn:name="Last Synced",type="date",JSONPath=".status.lastSyncedTime",description="The last successful synced time of the resource with Consul"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of the resource"
// +kubebuilder:resource:scope=Cluster
type MeshConfiguration struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   pbmesh.MeshConfiguration `json:"spec,omitempty"`
	Status `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// MeshConfigurationList contains a list of MeshConfiguration.
type MeshConfigurationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []*MeshConfiguration `json:"items"`
}

func (in *MeshConfiguration) ResourceID(_, partition string) *pbresource.ID {
	return &pbresource.ID{
		Name: in.Name,
		Type: pbmesh.MeshConfigurationType,
		Tenancy: &pbresource.Tenancy{
			// we don't pass a namespace here because MeshConfiguration is partition-scoped
			Partition: partition,
		},
	}
}

func (in *MeshConfiguration) Resource(_, partition string) *pbresource.Resource {
	return &pbresource.Resource{
		Id:       in.ResourceID("", partition),
		Data:     inject.ToProtoAny(&in.Spec),
		Metadata: meshConfigMeta(),
	}
}

func (in *MeshConfiguration) AddFinalizer(f string) {
	in.ObjectMeta.Finalizers = append(in.Finalizers(), f)
}

func (in *MeshConfiguration) RemoveFinalizer(f string) {
	var newFinalizers []string
	for _, oldF := range in.Finalizers() {
		if oldF != f {
			newFinalizers = append(newFinalizers, oldF)
		}
	}
	in.ObjectMeta.Finalizers = newFinalizers
}

func (in *MeshConfiguration) Finalizers() []string {
	return in.ObjectMeta.Finalizers
}

func (in *MeshConfiguration) MatchesConsul(candidate *pbresource.Resource, _, partition string) bool {
	return cmp.Equal(
		in.Resource("", partition),
		candidate,
		protocmp.IgnoreFields(&pbresource.Resource{}, "status", "generation", "version"),
		protocmp.IgnoreFields(&pbresource.ID{}, "uid"),
		protocmp.Transform(),
		cmpopts.SortSlices(func(a, b any) bool { return fmt.Sprintf("%v", a) < fmt.Sprintf("%v", b) }),
	)
}

func (in *MeshConfiguration) KubeKind() string {
	return meshConfigurationKind
}

func (in *MeshConfiguration) KubernetesName() string {
	return in.ObjectMeta.Name
}

func (in *MeshConfiguration) SetSyncedCondition(status corev1.ConditionStatus, reason, message string) {
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

func (in *MeshConfiguration) SetLastSyncedTime(time *metav1.Time) {
	in.Status.LastSyncedTime = time
}

func (in *MeshConfiguration) SyncedCondition() (status corev1.ConditionStatus, reason, message string) {
	cond := in.Status.GetCondition(ConditionSynced)
	if cond == nil {
		return corev1.ConditionUnknown, "", ""
	}
	return cond.Status, cond.Reason, cond.Message
}

func (in *MeshConfiguration) SyncedConditionStatus() corev1.ConditionStatus {
	condition := in.Status.GetCondition(ConditionSynced)
	if condition == nil {
		return corev1.ConditionUnknown
	}
	return condition.Status
}

func (in *MeshConfiguration) Validate(tenancy common.ConsulTenancyConfig) error {
	return nil
}

// DefaultNamespaceFields is required as part of the common.MeshConfig interface.
func (in *MeshConfiguration) DefaultNamespaceFields(tenancy common.ConsulTenancyConfig) {}
