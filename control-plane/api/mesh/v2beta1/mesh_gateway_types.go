// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0
package v2beta1

import (
	"fmt"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	pbmesh "github.com/hashicorp/consul/proto-public/pbmesh/v2beta1"
	"github.com/hashicorp/consul/proto-public/pbresource"
	"google.golang.org/protobuf/testing/protocmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	inject "github.com/hashicorp/consul-k8s/control-plane/connect-inject/common"
)

const (
	meshGatewayKubeKind = "meshgateway"
)

func init() {
	MeshSchemeBuilder.Register(&MeshGateway{}, &MeshGatewayList{})
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// MeshGateway is the Schema for the Mesh Gateway API
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.conditions[?(@.type==\"Synced\")].status",description="The sync status of the resource with Consul"
// +kubebuilder:printcolumn:name="Last Synced",type="date",JSONPath=".status.lastSyncedTime",description="The last successful synced time of the resource with Consul"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of the resource"
// +kubebuilder:resource:scope="Namespaced"
type MeshGateway struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   pbmesh.MeshGateway `json:"spec,omitempty"`
	Status `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// MeshGatewayList contains a list of MeshGateway.
type MeshGatewayList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []*MeshGateway `json:"items"`
}

func (in *MeshGatewayList) ReconcileRequests() []reconcile.Request {
	requests := make([]reconcile.Request, 0, len(in.Items))

	for _, item := range in.Items {
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      item.Name,
				Namespace: item.Namespace,
			},
		})
	}
	return requests
}

func (in *MeshGateway) ResourceID(_, partition string) *pbresource.ID {
	return &pbresource.ID{
		Name: in.Name,
		Type: pbmesh.MeshGatewayType,
		Tenancy: &pbresource.Tenancy{
			Partition: partition,
			Namespace: "", // Namespace is always unset because MeshGateway is partition-scoped
		},
	}
}

func (in *MeshGateway) Resource(namespace, partition string) *pbresource.Resource {
	return &pbresource.Resource{
		Id:       in.ResourceID(namespace, partition),
		Data:     inject.ToProtoAny(&in.Spec),
		Metadata: meshConfigMeta(),
	}
}

func (in *MeshGateway) AddFinalizer(f string) {
	in.ObjectMeta.Finalizers = append(in.Finalizers(), f)
}

func (in *MeshGateway) RemoveFinalizer(f string) {
	var newFinalizers []string
	for _, oldF := range in.Finalizers() {
		if oldF != f {
			newFinalizers = append(newFinalizers, oldF)
		}
	}
	in.ObjectMeta.Finalizers = newFinalizers
}

func (in *MeshGateway) Finalizers() []string {
	return in.ObjectMeta.Finalizers
}

func (in *MeshGateway) MatchesConsul(candidate *pbresource.Resource, namespace, partition string) bool {
	return cmp.Equal(
		in.Resource(namespace, partition),
		candidate,
		protocmp.IgnoreFields(&pbresource.Resource{}, "status", "generation", "version"),
		protocmp.IgnoreFields(&pbresource.ID{}, "uid"),
		protocmp.Transform(),
		cmpopts.SortSlices(func(a, b any) bool { return fmt.Sprintf("%v", a) < fmt.Sprintf("%v", b) }),
	)
}

func (in *MeshGateway) KubeKind() string {
	return meshGatewayKubeKind
}

func (in *MeshGateway) KubernetesName() string {
	return in.ObjectMeta.Name
}

func (in *MeshGateway) SetSyncedCondition(status corev1.ConditionStatus, reason, message string) {
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

func (in *MeshGateway) SetLastSyncedTime(time *metav1.Time) {
	in.Status.LastSyncedTime = time
}

func (in *MeshGateway) SyncedCondition() (status corev1.ConditionStatus, reason, message string) {
	cond := in.Status.GetCondition(ConditionSynced)
	if cond == nil {
		return corev1.ConditionUnknown, "", ""
	}
	return cond.Status, cond.Reason, cond.Message
}

func (in *MeshGateway) SyncedConditionStatus() corev1.ConditionStatus {
	condition := in.Status.GetCondition(ConditionSynced)
	if condition == nil {
		return corev1.ConditionUnknown
	}
	return condition.Status
}

func (in *MeshGateway) Validate(tenancy common.ConsulTenancyConfig) error {
	// TODO add validation logic that ensures we only ever write this to the default namespace.
	return nil
}

// DefaultNamespaceFields is required as part of the common.MeshConfig interface.
func (in *MeshGateway) DefaultNamespaceFields(tenancy common.ConsulTenancyConfig) {}

// ListenersToServicePorts converts the MeshGateway listeners to ServicePorts.
func (in *MeshGateway) ListenersToServicePorts(portModifier int32) []corev1.ServicePort {
	ports := []corev1.ServicePort{}

	for _, listener := range in.Spec.Listeners {
		port := int32(listener.Port)

		ports = append(ports, corev1.ServicePort{
			Name: listener.Name,
			Port: port,
			TargetPort: intstr.IntOrString{
				IntVal: port + portModifier,
			},
			Protocol: corev1.Protocol(listener.Protocol),
		})
	}
	return ports
}

func (in *MeshGateway) ListenersToContainerPorts(portModifier int32, hostPort int32) []corev1.ContainerPort {
	ports := []corev1.ContainerPort{}

	for _, listener := range in.Spec.Listeners {
		port := int32(listener.Port)

		ports = append(ports, corev1.ContainerPort{
			Name:          listener.Name,
			ContainerPort: port + portModifier,
			HostPort:      hostPort,
			Protocol:      corev1.Protocol(listener.Protocol),
		})
	}
	return ports
}
