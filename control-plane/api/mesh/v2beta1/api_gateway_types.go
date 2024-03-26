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
	apiGatewayKubeKind = "gateway"
)

func init() {
	MeshSchemeBuilder.Register(&APIGateway{}, &APIGatewayList{})
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// APIGateway is the Schema for the API Gateway
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.conditions[?(@.type==\"Synced\")].status",description="The sync status of the resource with Consul"
// +kubebuilder:printcolumn:name="Last Synced",type="date",JSONPath=".status.lastSyncedTime",description="The last successful synced time of the resource with Consul"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of the resource"
// +kubebuilder:resource:scope=Cluster
type APIGateway struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec             pbmesh.APIGateway `json:"spec,omitempty"`
	APIGatewayStatus `json:"status,omitempty"`
}

type APIGatewayStatus struct {
	Status    `json:"status,omitempty"`
	Addresses []GatewayAddress `json:"addresses,omitempty"`
	Listeners []ListenerStatus `json:"listeners,omitempty"`
}

type ListenerStatus struct {
	Status         `json:"status,omitempty"`
	Name           string `json:"name"`
	AttachedRoutes int32  `json:"attachedRoutes"`
}

type GatewayAddress struct {
	// +kubebuilder:default=IPAddress
	Type  string `json:"type"`
	Value string `json:"value"`
}

// +kubebuilder:object:root=true

// APIGatewayList contains a list of APIGateway.
type APIGatewayList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []*APIGateway `json:"items"`
}

func (in *APIGatewayList) ReconcileRequests() []reconcile.Request {
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

func (in *APIGateway) ResourceID(namespace, partition string) *pbresource.ID {
	return &pbresource.ID{
		Name: in.Name,
		Type: pbmesh.APIGatewayType,
		Tenancy: &pbresource.Tenancy{
			Partition: partition,
			Namespace: namespace,
		},
	}
}

func (in *APIGateway) Resource(namespace, partition string) *pbresource.Resource {
	return &pbresource.Resource{
		Id:       in.ResourceID(namespace, partition),
		Data:     inject.ToProtoAny(&in.Spec),
		Metadata: meshConfigMeta(),
	}
}

func (in *APIGateway) AddFinalizer(f string) {
	in.ObjectMeta.Finalizers = append(in.Finalizers(), f)
}

func (in *APIGateway) RemoveFinalizer(f string) {
	var newFinalizers []string
	for _, oldF := range in.Finalizers() {
		if oldF != f {
			newFinalizers = append(newFinalizers, oldF)
		}
	}
	in.ObjectMeta.Finalizers = newFinalizers
}

func (in *APIGateway) Finalizers() []string {
	return in.ObjectMeta.Finalizers
}

func (in *APIGateway) MatchesConsul(candidate *pbresource.Resource, namespace, partition string) bool {
	return cmp.Equal(
		in.Resource(namespace, partition),
		candidate,
		protocmp.IgnoreFields(&pbresource.Resource{}, "status", "generation", "version"),
		protocmp.IgnoreFields(&pbresource.ID{}, "uid"),
		protocmp.Transform(),
		cmpopts.SortSlices(func(a, b any) bool { return fmt.Sprintf("%v", a) < fmt.Sprintf("%v", b) }),
	)
}

func (in *APIGateway) KubeKind() string {
	return apiGatewayKubeKind
}

func (in *APIGateway) KubernetesName() string {
	return in.ObjectMeta.Name
}

func (in *APIGateway) SetSyncedCondition(status corev1.ConditionStatus, reason, message string) {
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

func (in *APIGateway) SetLastSyncedTime(time *metav1.Time) {
	in.Status.LastSyncedTime = time
}

func (in *APIGateway) SyncedCondition() (status corev1.ConditionStatus, reason, message string) {
	cond := in.Status.GetCondition(ConditionSynced)
	if cond == nil {
		return corev1.ConditionUnknown, "", ""
	}
	return cond.Status, cond.Reason, cond.Message
}

func (in *APIGateway) SyncedConditionStatus() corev1.ConditionStatus {
	condition := in.Status.GetCondition(ConditionSynced)
	if condition == nil {
		return corev1.ConditionUnknown
	}
	return condition.Status
}

func (in *APIGateway) Validate(tenancy common.ConsulTenancyConfig) error {
	return nil
}

// DefaultNamespaceFields is required as part of the common.MeshConfig interface.
func (in *APIGateway) DefaultNamespaceFields(tenancy common.ConsulTenancyConfig) {}

// ListenersToServicePorts converts the APIGateway listeners to ServicePorts.
func (in *APIGateway) ListenersToServicePorts(portModifier int32) []corev1.ServicePort {
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

func (in *APIGateway) ListenersToContainerPorts(_ int32, _ int32) []corev1.ContainerPort {
	// TODO: check if this is actually needed: we don't map any container ports in v1
	return []corev1.ContainerPort{}
}
