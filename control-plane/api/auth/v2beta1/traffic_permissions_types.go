// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v2beta1

import (
	"fmt"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	pbauth "github.com/hashicorp/consul/proto-public/pbauth/v2beta1"
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
	trafficpermissionsKubeKind = "trafficpermissions"
)

func init() {
	AuthSchemeBuilder.Register(&TrafficPermissions{}, &TrafficPermissionsList{})
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// TrafficPermissions is the Schema for the traffic-permissions API
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.conditions[?(@.type==\"Synced\")].status",description="The sync status of the resource with Consul"
// +kubebuilder:printcolumn:name="Last Synced",type="date",JSONPath=".status.lastSyncedTime",description="The last successful synced time of the resource with Consul"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of the resource"
// +kubebuilder:resource:shortName="traffic-permissions"
type TrafficPermissions struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   pbauth.TrafficPermissions `json:"spec,omitempty"`
	Status `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TrafficPermissionsList contains a list of TrafficPermissions.
type TrafficPermissionsList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []*TrafficPermissions `json:"items"`
}

func (in *TrafficPermissions) ResourceID(namespace, partition string) *pbresource.ID {
	return &pbresource.ID{
		Name: in.Name,
		Type: pbauth.TrafficPermissionsType,
		Tenancy: &pbresource.Tenancy{
			Partition: partition,
			Namespace: namespace,
		},
	}
}

func (in *TrafficPermissions) Resource(namespace, partition string) *pbresource.Resource {
	return &pbresource.Resource{
		Id:       in.ResourceID(namespace, partition),
		Data:     inject.ToProtoAny(&in.Spec),
		Metadata: meshConfigMeta(),
	}
}

func (in *TrafficPermissions) MatchesConsul(candidate *pbresource.Resource, namespace, partition string) bool {
	return cmp.Equal(
		in.Resource(namespace, partition),
		candidate,
		protocmp.IgnoreFields(&pbresource.Resource{}, "status", "generation", "version"),
		protocmp.IgnoreFields(&pbresource.ID{}, "uid"),
		protocmp.Transform(),
		cmpopts.SortSlices(func(a, b any) bool { return fmt.Sprintf("%v", a) < fmt.Sprintf("%v", b) }),
	)
}

func (in *TrafficPermissions) AddFinalizer(f string) {
	in.ObjectMeta.Finalizers = append(in.Finalizers(), f)
}

func (in *TrafficPermissions) RemoveFinalizer(f string) {
	var newFinalizers []string
	for _, oldF := range in.Finalizers() {
		if oldF != f {
			newFinalizers = append(newFinalizers, oldF)
		}
	}
	in.ObjectMeta.Finalizers = newFinalizers
}

func (in *TrafficPermissions) Finalizers() []string {
	return in.ObjectMeta.Finalizers
}

func (in *TrafficPermissions) KubeKind() string {
	return trafficpermissionsKubeKind
}

func (in *TrafficPermissions) KubernetesName() string {
	return in.ObjectMeta.Name
}

func (in *TrafficPermissions) SetSyncedCondition(status corev1.ConditionStatus, reason, message string) {
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

func (in *TrafficPermissions) SetLastSyncedTime(time *metav1.Time) {
	in.Status.LastSyncedTime = time
}

func (in *TrafficPermissions) SyncedCondition() (status corev1.ConditionStatus, reason, message string) {
	cond := in.Status.GetCondition(ConditionSynced)
	if cond == nil {
		return corev1.ConditionUnknown, "", ""
	}
	return cond.Status, cond.Reason, cond.Message
}

func (in *TrafficPermissions) SyncedConditionStatus() corev1.ConditionStatus {
	condition := in.Status.GetCondition(ConditionSynced)
	if condition == nil {
		return corev1.ConditionUnknown
	}
	return condition.Status
}

func (in *TrafficPermissions) Validate(tenancy common.ConsulTenancyConfig) error {
	var errs field.ErrorList
	path := field.NewPath("spec")
	var tp pbauth.TrafficPermissions
	res := in.Resource(tenancy.ConsulDestinationNamespace, tenancy.ConsulPartition)
	if err := res.Data.UnmarshalTo(&tp); err != nil {
		return fmt.Errorf("error parsing resource data as type %q: %s", &tp, err)
	}

	switch tp.Action {
	case pbauth.Action_ACTION_ALLOW:
	case pbauth.Action_ACTION_DENY:
	case pbauth.Action_ACTION_UNSPECIFIED:
		fallthrough
	default:
		errs = append(errs, field.Invalid(path.Child("action"), tp.Action, "action must be either allow or deny"))
	}

	if tp.Destination == nil || (len(tp.Destination.IdentityName) == 0) {
		errs = append(errs, field.Invalid(path.Child("destination"), tp.Destination, "cannot be empty"))
	}
	// Validate permissions
	for i, permission := range tp.Permissions {
		if err := validatePermission(permission, path.Child("permissions").Index(i)); err != nil {
			errs = append(errs, err...)
		}
	}
	if len(errs) > 0 {
		return apierrors.NewInvalid(
			schema.GroupKind{Group: AuthGroup, Kind: common.TrafficPermissions},
			in.KubernetesName(), errs)
	}
	return nil
}

func validatePermission(p *pbauth.Permission, path *field.Path) field.ErrorList {
	var errs field.ErrorList

	for s, src := range p.Sources {
		if sourceHasIncompatibleTenancies(src) {
			errs = append(errs, field.Invalid(path.Child("sources").Index(s), src, "permission sources may not specify partitions, peers, and sameness_groups together"))
		}

		if src.Namespace == "" && src.IdentityName != "" {
			errs = append(errs, field.Invalid(path.Child("sources").Index(s), src, "permission sources may not have wildcard namespaces and explicit names"))
		}

		// Excludes are only valid for wildcard sources.
		if src.IdentityName != "" && len(src.Exclude) > 0 {
			errs = append(errs, field.Invalid(path.Child("sources").Index(s), src, "must be defined on wildcard sources"))
			continue
		}

		for e, d := range src.Exclude {
			if sourceHasIncompatibleTenancies(d) {
				errs = append(errs, field.Invalid(path.Child("sources").Index(s).Child("exclude").Index(e), d, "permissions sources may not specify partitions, peers, and sameness_groups together"))
			}

			if d.Namespace == "" && d.IdentityName != "" {
				errs = append(errs, field.Invalid(path.Child("sources").Index(s).Child("exclude").Index(e), d, "permission sources may not have wildcard namespaces and explicit names"))
			}
		}
	}
	for d, dest := range p.DestinationRules {
		if (len(dest.PathExact) > 0 && len(dest.PathPrefix) > 0) ||
			(len(dest.PathRegex) > 0 && len(dest.PathExact) > 0) ||
			(len(dest.PathRegex) > 0 && len(dest.PathPrefix) > 0) {
			errs = append(errs, field.Invalid(path.Child("destinationRules").Index(d), dest, "prefix values, regex values, and explicit names must not combined"))
		}
		if len(dest.Exclude) > 0 {
			for e, excl := range dest.Exclude {
				if (len(excl.PathExact) > 0 && len(excl.PathPrefix) > 0) ||
					(len(excl.PathRegex) > 0 && len(excl.PathExact) > 0) ||
					(len(excl.PathRegex) > 0 && len(excl.PathPrefix) > 0) {
					errs = append(errs, field.Invalid(path.Child("destinationRules").Index(d).Child("exclude").Index(e), excl, "prefix values, regex values, and explicit names must not combined"))
				}
			}
		}
	}

	return errs
}

func sourceHasIncompatibleTenancies(src pbauth.SourceToSpiffe) bool {
	peerSet := src.GetPeer() != common.DefaultPeerName && src.GetPeer() != ""
	apSet := src.GetPartition() != common.DefaultPartitionName && src.GetPartition() != ""
	sgSet := src.GetSamenessGroup() != ""

	return (apSet && peerSet) || (apSet && sgSet) || (peerSet && sgSet)
}

// DefaultNamespaceFields is required as part of the common.MeshConfig interface.
func (in *TrafficPermissions) DefaultNamespaceFields(tenancy common.ConsulTenancyConfig) {}
