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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"

	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	inject "github.com/hashicorp/consul-k8s/control-plane/connect-inject/common"
)

const (
	tcpRouteKubeKind = "tcproute"
)

func init() {
	MeshSchemeBuilder.Register(&TCPRoute{}, &TCPRouteList{})
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// TCPRoute is the Schema for the TCP Route API
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.conditions[?(@.type==\"Synced\")].status",description="The sync status of the resource with Consul"
// +kubebuilder:printcolumn:name="Last Synced",type="date",JSONPath=".status.lastSyncedTime",description="The last successful synced time of the resource with Consul"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of the resource"
// +kubebuilder:resource:shortName="tcp-route"
type TCPRoute struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   pbmesh.TCPRoute `json:"spec,omitempty"`
	Status `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TCPRouteList contains a list of TCPRoute.
type TCPRouteList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []*TCPRoute `json:"items"`
}

func (in *TCPRoute) ResourceID(namespace, partition string) *pbresource.ID {
	return &pbresource.ID{
		Name: in.Name,
		Type: pbmesh.TCPRouteType,
		Tenancy: &pbresource.Tenancy{
			Partition: partition,
			Namespace: namespace,
		},
	}
}

func (in *TCPRoute) Resource(namespace, partition string) *pbresource.Resource {
	return &pbresource.Resource{
		Id:       in.ResourceID(namespace, partition),
		Data:     inject.ToProtoAny(&in.Spec),
		Metadata: meshConfigMeta(),
	}
}

func (in *TCPRoute) AddFinalizer(f string) {
	in.ObjectMeta.Finalizers = append(in.Finalizers(), f)
}

func (in *TCPRoute) RemoveFinalizer(f string) {
	var newFinalizers []string
	for _, oldF := range in.Finalizers() {
		if oldF != f {
			newFinalizers = append(newFinalizers, oldF)
		}
	}
	in.ObjectMeta.Finalizers = newFinalizers
}

func (in *TCPRoute) Finalizers() []string {
	return in.ObjectMeta.Finalizers
}

func (in *TCPRoute) MatchesConsul(candidate *pbresource.Resource, namespace, partition string) bool {
	return cmp.Equal(
		in.Resource(namespace, partition),
		candidate,
		protocmp.IgnoreFields(&pbresource.Resource{}, "status", "generation", "version"),
		protocmp.IgnoreFields(&pbresource.ID{}, "uid"),
		protocmp.Transform(),
		cmpopts.SortSlices(func(a, b any) bool { return fmt.Sprintf("%v", a) < fmt.Sprintf("%v", b) }),
	)
}

func (in *TCPRoute) KubeKind() string {
	return tcpRouteKubeKind
}

func (in *TCPRoute) KubernetesName() string {
	return in.ObjectMeta.Name
}

func (in *TCPRoute) SetSyncedCondition(status corev1.ConditionStatus, reason, message string) {
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

func (in *TCPRoute) SetLastSyncedTime(time *metav1.Time) {
	in.Status.LastSyncedTime = time
}

func (in *TCPRoute) SyncedCondition() (status corev1.ConditionStatus, reason, message string) {
	cond := in.Status.GetCondition(ConditionSynced)
	if cond == nil {
		return corev1.ConditionUnknown, "", ""
	}
	return cond.Status, cond.Reason, cond.Message
}

func (in *TCPRoute) SyncedConditionStatus() corev1.ConditionStatus {
	condition := in.Status.GetCondition(ConditionSynced)
	if condition == nil {
		return corev1.ConditionUnknown
	}
	return condition.Status
}

func (in *TCPRoute) Validate(tenancy common.ConsulTenancyConfig) error {
	var errs field.ErrorList
	var route pbmesh.TCPRoute
	path := field.NewPath("spec")
	res := in.Resource(tenancy.ConsulDestinationNamespace, tenancy.ConsulPartition)

	if err := res.Data.UnmarshalTo(&route); err != nil {
		return fmt.Errorf("error parsing resource data as type %q: %s", &route, err)
	}

	if len(route.ParentRefs) == 0 {
		errs = append(errs, field.Required(path.Child("parentRefs"), "cannot be empty"))
	}

	if len(route.Rules) > 1 {
		errs = append(errs, field.Invalid(path.Child("rules"), route.Rules, "must only specify a single rule for now"))
	}

	for i, rule := range route.Rules {
		rulePath := path.Child("rules").Index(i)

		if len(rule.BackendRefs) == 0 {
			errs = append(errs, field.Required(rulePath.Child("backendRefs"), "cannot be empty"))
		}
		for j, hbref := range rule.BackendRefs {
			ruleBackendRefsPath := rulePath.Child("backendRefs").Index(j)
			if hbref.BackendRef == nil {
				errs = append(errs, field.Required(ruleBackendRefsPath.Child("backendRef"), "missing required field"))
				continue
			}

			if hbref.BackendRef.Datacenter != "" {
				errs = append(errs, field.Invalid(ruleBackendRefsPath.Child("backendRef").Child("datacenter"), hbref.BackendRef.Datacenter, "datacenter is not yet supported on backend refs"))
			}
		}
	}

	if len(errs) > 0 {
		return apierrors.NewInvalid(
			schema.GroupKind{Group: MeshGroup, Kind: common.TCPRoute},
			in.KubernetesName(), errs)
	}
	return nil
}

// DefaultNamespaceFields is required as part of the common.MeshConfig interface.
func (in *TCPRoute) DefaultNamespaceFields(tenancy common.ConsulTenancyConfig) {}
