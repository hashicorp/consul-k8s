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
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
)

const (
	trafficpermissionsKubeKind = "trafficpermissions"
)

func init() {
	AuthSchemeBuilder.Register(&TrafficPermissions{}, &TrafficPermissionsList{})
}

var _ common.MeshConfig = &TrafficPermissions{}

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

	Spec   TrafficPermissionsSpec `json:"spec,omitempty"`
	Status `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TrafficPermissionsList contains a list of TrafficPermissions.
type TrafficPermissionsList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TrafficPermissions `json:"items"`
}

// TrafficPermissionsSpec defines the desired state of TrafficPermissions.
type TrafficPermissionsSpec struct {
	// Destination is a configuration of the destination proxies
	// where these traffic permissions should apply.
	Destination *Destination `json:"destination,omitempty"`
	// Action can be either allow or deny for the entire object. It will default to allow.
	//
	// If action is allow,
	// we will allow the connection if one of the rules in Rules matches, in other words, we will deny
	// all requests except for the ones that match Rules. If Consul is in default allow mode, then allow
	// actions have no effect without a deny permission as everything is allowed by default.
	//
	// If action is deny,
	// we will deny the connection if one of the rules in Rules match, in other words,
	// we will allow all requests except for the ones that match Rules. If Consul is default deny mode,
	// then deny permissions have no effect without an allow permission as everything is denied by default.
	//
	// Action unspecified is reserved for compatibility with the addition of future actions.
	Action IntentionAction `json:"action,omitempty"`
	// Permissions is a list of permissions to match on.
	// They are applied using OR semantics.
	Permissions Permissions `json:"permissions,omitempty"`
}

type Destination struct {
	// Name is the destination of all intentions defined in this config entry.
	// This may be set to the wildcard character (*) to match
	// all services that don't otherwise have intentions defined.
	IdentityName string `json:"identityName,omitempty"`
}

func (in *Destination) validate(path *field.Path) *field.Error {
	if in == nil {
		return field.Required(path, `destination and destination.identityName are required`)
	}
	if in.IdentityName == "" {
		return field.Required(path.Child("identityName"), `identityName is required`)
	}
	return nil
}

// IntentionAction is the action that the intention represents. This
// can be "allow" or "deny" to allowlist or denylist intentions.
type IntentionAction string

const (
	ActionDeny        IntentionAction = "deny"
	ActionAllow       IntentionAction = "allow"
	ActionUnspecified IntentionAction = ""
)

func (in IntentionAction) validate(path *field.Path) *field.Error {
	switch in {
	case ActionDeny, ActionAllow:
		return nil
	default:
		return field.Invalid(path.Child("action"), in, "must be one of \"allow\" or \"deny\"")
	}
}

type Permissions []*Permission

type Permission struct {
	// sources is a list of sources in this traffic permission.
	Sources Sources `json:"sources,omitempty"`
	// destinationRules is a list of rules to apply for matching sources in this Permission.
	// These rules are specific to the request or connection that is going to the destination(s)
	// selected by the TrafficPermissions resource.
	DestinationRules DestinationRules `json:"destinationRules,omitempty"`
}

type Sources []*Source

type DestinationRules []*DestinationRule

// Source represents the source identity.
// To specify any of the wildcard sources, the specific fields need to be omitted.
// For example, for a wildcard namespace, identityName should be omitted.
type Source struct {
	IdentityName  string `json:"identityName,omitempty"`
	Namespace     string `json:"namespace,omitempty"`
	Partition     string `json:"partition,omitempty"`
	Peer          string `json:"peer,omitempty"`
	SamenessGroup string `json:"samenessGroup,omitempty"`
	// exclude is a list of sources to exclude from this source.
	Exclude Exclude `json:"exclude,omitempty"`
}

// DestinationRule contains rules to apply to the incoming connection.
type DestinationRule struct {
	PathExact  string `json:"pathExact,omitempty"`
	PathPrefix string `json:"pathPrefix,omitempty"`
	PathRegex  string `json:"pathRegex,omitempty"`
	// methods is the list of HTTP methods. If no methods are specified,
	// this rule will apply to all methods.
	Methods   []string               `json:"methods,omitempty"`
	Header    *DestinationRuleHeader `json:"header,omitempty"`
	PortNames []string               `json:"portNames,omitempty"`
	// exclude contains a list of rules to exclude when evaluating rules for the incoming connection.
	Exclude ExcludePermissions `json:"exclude,omitempty"`
}

type Exclude []*ExcludeSource

// ExcludeSource is almost the same as source but it prevents the addition of
// matchiing sources.
type ExcludeSource struct {
	IdentityName  string `json:"identityName,omitempty"`
	Namespace     string `json:"namespace,omitempty"`
	Partition     string `json:"partition,omitempty"`
	Peer          string `json:"peer,omitempty"`
	SamenessGroup string `json:"samenessGroup,omitempty"`
}

type DestinationRuleHeader struct {
	Name    string `json:"name,omitempty"`
	Present bool   `json:"present,omitempty"`
	Exact   string `json:"exact,omitempty"`
	Prefix  string `json:"prefix,omitempty"`
	Suffix  string `json:"suffix,omitempty"`
	Regex   string `json:"regex,omitempty"`
	Invert  bool   `json:"invert,omitempty"`
}

type ExcludePermissions []*ExcludePermissionRule

type ExcludePermissionRule struct {
	PathExact  string `json:"pathExact,omitempty"`
	PathPrefix string `json:"pathPrefix,omitempty"`
	PathRegex  string `json:"pathRegex,omitempty"`
	// methods is the list of HTTP methods.
	Methods []string               `json:"methods,omitempty"`
	Header  *DestinationRuleHeader `json:"header,omitempty"`
	// portNames is a list of workload ports to apply this rule to. The ports specified here
	// must be the ports used in the connection.
	PortNames []string `json:"portNames,omitempty"`
}

func (in *TrafficPermissions) ResourceID(namespace, partition string) *pbresource.ID {
	return &pbresource.ID{
		Name: in.Name,
		Type: pbauth.TrafficPermissionsType,
		Tenancy: &pbresource.Tenancy{
			Partition: partition,
			Namespace: namespace,

			// Because we are explicitly defining NS/partition, this will not default and must be explicit.
			// At a future point, this will move out of the Tenancy block.
			PeerName: constants.DefaultConsulPeer,
		},
	}
}

func (in *TrafficPermissions) Resource(namespace, partition string) *pbresource.Resource {
	return &pbresource.Resource{
		Id: in.ResourceID(namespace, partition),
		Data: inject.ToProtoAny(&pbauth.TrafficPermissions{
			Destination: in.Spec.Destination.toProto(),
			Action:      in.Spec.Action.toProto(),
			Permissions: in.Spec.Permissions.toProto(),
		}),
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

func (in *TrafficPermissions) Validate(_ common.ConsulTenancyConfig) error {
	var errs field.ErrorList
	path := field.NewPath("spec")

	if in.Spec.Action == ActionUnspecified {
		errs = append(errs, field.Required(path.Child("action"), `action is required`))
	}
	if err := in.Spec.Action.validate(path); err != nil {
		errs = append(errs, err)
	}

	// Validate Destinations
	if err := in.Spec.Destination.validate(path.Child("destination")); err != nil {
		errs = append(errs, err)
	}

	// TODO: add validation for permissions
	// Validate permissions in Consul:
	// https://github.com/hashicorp/consul/blob/203a36821ef6182b2d2b30c1012ca5a42c7dd8f3/internal/auth/internal/types/traffic_permissions.go#L59-L141

	if len(errs) > 0 {
		return apierrors.NewInvalid(
			schema.GroupKind{Group: AuthGroup, Kind: common.TrafficPermissions},
			in.KubernetesName(), errs)
	}
	return nil
}

// DefaultNamespaceFields is required as part of the common.MeshConfig interface.
func (in *TrafficPermissions) DefaultNamespaceFields(tenancy common.ConsulTenancyConfig) {}

func (p Permissions) toProto() []*pbauth.Permission {
	var perms []*pbauth.Permission
	for _, permission := range p {
		perms = append(perms, &pbauth.Permission{
			Sources:          permission.Sources.toProto(),
			DestinationRules: permission.DestinationRules.toProto(),
		})
	}
	return perms
}

func (s Sources) toProto() []*pbauth.Source {
	var srcs []*pbauth.Source
	for _, source := range s {
		srcs = append(srcs, &pbauth.Source{
			IdentityName:  source.IdentityName,
			Namespace:     source.Namespace,
			Partition:     source.Partition,
			Peer:          source.Peer,
			SamenessGroup: source.SamenessGroup,
			Exclude:       source.Exclude.toProto(),
		})
	}
	return srcs
}

func (r DestinationRules) toProto() []*pbauth.DestinationRule {
	var dstnRules []*pbauth.DestinationRule
	for _, rule := range r {
		dstnRules = append(dstnRules, &pbauth.DestinationRule{
			PathExact:  rule.PathExact,
			PathPrefix: rule.PathPrefix,
			PathRegex:  rule.PathRegex,
			Methods:    rule.Methods,
			Header:     rule.Header.toProto(),
			PortNames:  rule.PortNames,
			Exclude:    rule.Exclude.toProto(),
		})
	}
	return dstnRules
}

func (e Exclude) toProto() []*pbauth.ExcludeSource {
	var exSrcs []*pbauth.ExcludeSource
	for _, source := range e {
		exSrcs = append(exSrcs, &pbauth.ExcludeSource{
			IdentityName:  source.IdentityName,
			Namespace:     source.Namespace,
			Partition:     source.Partition,
			Peer:          source.Peer,
			SamenessGroup: source.SamenessGroup,
		})
	}
	return exSrcs
}

func (p ExcludePermissions) toProto() []*pbauth.ExcludePermissionRule {
	var exclPerms []*pbauth.ExcludePermissionRule
	for _, rule := range p {
		exclPerms = append(exclPerms, &pbauth.ExcludePermissionRule{
			PathExact:  rule.PathExact,
			PathPrefix: rule.PathPrefix,
			PathRegex:  rule.PathRegex,
			Methods:    rule.Methods,
			Header:     rule.Header.toProto(),
			PortNames:  rule.PortNames,
		})
	}
	return exclPerms
}

func (h *DestinationRuleHeader) toProto() *pbauth.DestinationRuleHeader {
	if h == nil {
		return nil
	}
	return &pbauth.DestinationRuleHeader{
		Name:    h.Name,
		Present: h.Present,
		Exact:   h.Exact,
		Prefix:  h.Prefix,
		Suffix:  h.Suffix,
		Regex:   h.Regex,
		Invert:  h.Invert,
	}
}

func (in *Destination) toProto() *pbauth.Destination {
	if in == nil {
		return nil
	}
	return &pbauth.Destination{
		IdentityName: in.IdentityName,
	}
}

func (in IntentionAction) toProto() pbauth.Action {
	if in == ActionAllow {
		return pbauth.Action_ACTION_ALLOW
	} else if in == ActionDeny {
		return pbauth.Action_ACTION_DENY
	}
	return pbauth.Action_ACTION_UNSPECIFIED
}
