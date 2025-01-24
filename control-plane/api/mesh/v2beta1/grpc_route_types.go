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
	grpcRouteKubeKind = "grpcroute"
)

func init() {
	MeshSchemeBuilder.Register(&GRPCRoute{}, &GRPCRouteList{})
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// GRPCRoute is the Schema for the GRPC Route API
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.conditions[?(@.type==\"Synced\")].status",description="The sync status of the resource with Consul"
// +kubebuilder:printcolumn:name="Last Synced",type="date",JSONPath=".status.lastSyncedTime",description="The last successful synced time of the resource with Consul"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of the resource"
// +kubebuilder:resource:shortName="grpc-route"
type GRPCRoute struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   pbmesh.GRPCRoute `json:"spec,omitempty"`
	Status `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// GRPCRouteList contains a list of GRPCRoute.
type GRPCRouteList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []*GRPCRoute `json:"items"`
}

func (in *GRPCRoute) ResourceID(namespace, partition string) *pbresource.ID {
	return &pbresource.ID{
		Name: in.Name,
		Type: pbmesh.GRPCRouteType,
		Tenancy: &pbresource.Tenancy{
			Partition: partition,
			Namespace: namespace,
		},
	}
}

func (in *GRPCRoute) Resource(namespace, partition string) *pbresource.Resource {
	return &pbresource.Resource{
		Id:       in.ResourceID(namespace, partition),
		Data:     inject.ToProtoAny(&in.Spec),
		Metadata: meshConfigMeta(),
	}
}

func (in *GRPCRoute) AddFinalizer(f string) {
	in.ObjectMeta.Finalizers = append(in.Finalizers(), f)
}

func (in *GRPCRoute) RemoveFinalizer(f string) {
	var newFinalizers []string
	for _, oldF := range in.Finalizers() {
		if oldF != f {
			newFinalizers = append(newFinalizers, oldF)
		}
	}
	in.ObjectMeta.Finalizers = newFinalizers
}

func (in *GRPCRoute) Finalizers() []string {
	return in.ObjectMeta.Finalizers
}

func (in *GRPCRoute) MatchesConsul(candidate *pbresource.Resource, namespace, partition string) bool {
	return cmp.Equal(
		in.Resource(namespace, partition),
		candidate,
		protocmp.IgnoreFields(&pbresource.Resource{}, "status", "generation", "version"),
		protocmp.IgnoreFields(&pbresource.ID{}, "uid"),
		protocmp.Transform(),
		cmpopts.SortSlices(func(a, b any) bool { return fmt.Sprintf("%v", a) < fmt.Sprintf("%v", b) }),
	)
}

func (in *GRPCRoute) KubeKind() string {
	return grpcRouteKubeKind
}

func (in *GRPCRoute) KubernetesName() string {
	return in.ObjectMeta.Name
}

func (in *GRPCRoute) SetSyncedCondition(status corev1.ConditionStatus, reason, message string) {
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

func (in *GRPCRoute) SetLastSyncedTime(time *metav1.Time) {
	in.Status.LastSyncedTime = time
}

func (in *GRPCRoute) SyncedCondition() (status corev1.ConditionStatus, reason, message string) {
	cond := in.Status.GetCondition(ConditionSynced)
	if cond == nil {
		return corev1.ConditionUnknown, "", ""
	}
	return cond.Status, cond.Reason, cond.Message
}

func (in *GRPCRoute) SyncedConditionStatus() corev1.ConditionStatus {
	condition := in.Status.GetCondition(ConditionSynced)
	if condition == nil {
		return corev1.ConditionUnknown
	}
	return condition.Status
}

func (in *GRPCRoute) Validate(tenancy common.ConsulTenancyConfig) error {
	var errs field.ErrorList
	var route pbmesh.GRPCRoute
	path := field.NewPath("spec")

	res := in.Resource(tenancy.ConsulDestinationNamespace, tenancy.ConsulPartition)

	if err := res.Data.UnmarshalTo(&route); err != nil {
		return fmt.Errorf("error parsing resource data as type %q: %s", &route, err)
	}

	if len(route.ParentRefs) == 0 {
		errs = append(errs, field.Required(path.Child("parentRefs"), "cannot be empty"))
	}

	if len(route.Hostnames) > 0 {
		errs = append(errs, field.Invalid(path.Child("hostnames"), route.Hostnames, "should not populate hostnames"))
	}

	for i, rule := range route.Rules {
		rulePath := path.Child("rules").Index(i)
		for j, match := range rule.Matches {
			ruleMatchPath := rulePath.Child("matches").Index(j)
			if match.Method != nil {
				switch match.Method.Type {
				case pbmesh.GRPCMethodMatchType_GRPC_METHOD_MATCH_TYPE_UNSPECIFIED:
					errs = append(errs, field.Invalid(ruleMatchPath.Child("method").Child("type"), match.Method.Type, "missing required field"))
				case pbmesh.GRPCMethodMatchType_GRPC_METHOD_MATCH_TYPE_EXACT:
				case pbmesh.GRPCMethodMatchType_GRPC_METHOD_MATCH_TYPE_REGEX:
				default:
					errs = append(errs, field.Invalid(ruleMatchPath.Child("method").Child("type"), match.Method.Type, fmt.Sprintf("not a supported enum value: %v", match.Method.Type)))
				}
				if match.Method.Service == "" && match.Method.Method == "" {
					errs = append(errs, field.Invalid(ruleMatchPath.Child("method").Child("service"), match.Method.Service, "at least one of \"service\" or \"method\" must be set"))
				}
			}

			for k, header := range match.Headers {
				ruleHeaderPath := ruleMatchPath.Child("headers").Index(k)
				if err := validateHeaderMatchType(header.Type); err != nil {
					errs = append(errs, field.Invalid(ruleHeaderPath.Child("type"), header.Type, err.Error()))
				}

				if header.Name == "" {
					errs = append(errs, field.Required(ruleHeaderPath.Child("name"), "missing required field"))
				}
			}
		}

		for j, filter := range rule.Filters {
			set := 0
			if filter.RequestHeaderModifier != nil {
				set++
			}
			if filter.ResponseHeaderModifier != nil {
				set++
			}
			if filter.UrlRewrite != nil {
				set++
				if filter.UrlRewrite.PathPrefix == "" {
					errs = append(errs, field.Required(rulePath.Child("filters").Index(j).Child("urlRewrite").Child("pathPrefix"), "field should not be empty if enclosing section is set"))
				}
			}
			if set != 1 {
				errs = append(errs, field.Invalid(rulePath.Child("filters").Index(j), filter, "exactly one of request_header_modifier, response_header_modifier, or url_rewrite is required"))
			}
		}

		if len(rule.BackendRefs) == 0 {
			errs = append(errs, field.Required(rulePath.Child("backendRefs"), "missing required field"))
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

			if len(hbref.Filters) > 0 {
				errs = append(errs, field.Invalid(ruleBackendRefsPath.Child("filters"), hbref.Filters, "filters are not supported at this level yet"))
			}
		}

		if rule.Timeouts != nil {
			errs = append(errs, validateHTTPTimeouts(rule.Timeouts, rulePath.Child("timeouts"))...)
		}
		if rule.Retries != nil {
			errs = append(errs, validateHTTPRetries(rule.Retries, rulePath.Child("retries"))...)
		}
	}

	if len(errs) > 0 {
		return apierrors.NewInvalid(
			schema.GroupKind{Group: MeshGroup, Kind: common.GRPCRoute},
			in.KubernetesName(), errs)
	}
	return nil
}

func validateHeaderMatchType(typ pbmesh.HeaderMatchType) error {
	switch typ {
	case pbmesh.HeaderMatchType_HEADER_MATCH_TYPE_UNSPECIFIED:
		return fmt.Errorf("missing required field")
	case pbmesh.HeaderMatchType_HEADER_MATCH_TYPE_EXACT:
	case pbmesh.HeaderMatchType_HEADER_MATCH_TYPE_REGEX:
	case pbmesh.HeaderMatchType_HEADER_MATCH_TYPE_PRESENT:
	case pbmesh.HeaderMatchType_HEADER_MATCH_TYPE_PREFIX:
	case pbmesh.HeaderMatchType_HEADER_MATCH_TYPE_SUFFIX:
	default:
		return fmt.Errorf("not a supported enum value: %v", typ)
	}
	return nil
}

func validateHTTPTimeouts(timeouts *pbmesh.HTTPRouteTimeouts, path *field.Path) field.ErrorList {
	if timeouts == nil {
		return nil
	}

	var errs field.ErrorList

	if timeouts.Request != nil {
		val := timeouts.Request.AsDuration()
		if val < 0 {
			errs = append(errs, field.Invalid(path.Child("request"), val, "timeout cannot be negative"))
		}
	}
	if timeouts.Idle != nil {
		val := timeouts.Idle.AsDuration()
		if val < 0 {
			errs = append(errs, field.Invalid(path.Child("idle"), val, "timeout cannot be negative"))
		}
	}

	return errs
}

func validateHTTPRetries(retries *pbmesh.HTTPRouteRetries, path *field.Path) field.ErrorList {
	if retries == nil {
		return nil
	}

	var errs field.ErrorList

	for i, condition := range retries.OnConditions {
		if !isValidRetryCondition(condition) {
			errs = append(errs, field.Invalid(path.Child("onConditions").Index(i), condition, "not a valid retry condition"))
		}
	}

	return errs
}

func isValidRetryCondition(retryOn string) bool {
	switch retryOn {
	case "5xx",
		"gateway-error",
		"reset",
		"connect-failure",
		"envoy-ratelimited",
		"retriable-4xx",
		"refused-stream",
		"cancelled",
		"deadline-exceeded",
		"internal",
		"resource-exhausted",
		"unavailable":
		return true
	default:
		return false
	}
}

// DefaultNamespaceFields is required as part of the common.MeshConfig interface.
func (in *GRPCRoute) DefaultNamespaceFields(tenancy common.ConsulTenancyConfig) {}
