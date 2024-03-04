// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0
package v2beta1

import (
	"fmt"
	"net/http"
	"strings"

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
	httpRouteKubeKind = "httproute"
)

func init() {
	MeshSchemeBuilder.Register(&HTTPRoute{}, &HTTPRouteList{})
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// HTTPRoute is the Schema for the HTTP Route API
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.conditions[?(@.type==\"Synced\")].status",description="The sync status of the resource with Consul"
// +kubebuilder:printcolumn:name="Last Synced",type="date",JSONPath=".status.lastSyncedTime",description="The last successful synced time of the resource with Consul"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of the resource"
// +kubebuilder:resource:shortName="http-route"
type HTTPRoute struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   pbmesh.HTTPRoute `json:"spec,omitempty"`
	Status `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// HTTPRouteList contains a list of HTTPRoute.
type HTTPRouteList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []*HTTPRoute `json:"items"`
}

func (in *HTTPRoute) ResourceID(namespace, partition string) *pbresource.ID {
	return &pbresource.ID{
		Name: in.Name,
		Type: pbmesh.HTTPRouteType,
		Tenancy: &pbresource.Tenancy{
			Partition: partition,
			Namespace: namespace,
		},
	}
}

func (in *HTTPRoute) Resource(namespace, partition string) *pbresource.Resource {
	return &pbresource.Resource{
		Id:       in.ResourceID(namespace, partition),
		Data:     inject.ToProtoAny(&in.Spec),
		Metadata: meshConfigMeta(),
	}
}

func (in *HTTPRoute) AddFinalizer(f string) {
	in.ObjectMeta.Finalizers = append(in.Finalizers(), f)
}

func (in *HTTPRoute) RemoveFinalizer(f string) {
	var newFinalizers []string
	for _, oldF := range in.Finalizers() {
		if oldF != f {
			newFinalizers = append(newFinalizers, oldF)
		}
	}
	in.ObjectMeta.Finalizers = newFinalizers
}

func (in *HTTPRoute) Finalizers() []string {
	return in.ObjectMeta.Finalizers
}

func (in *HTTPRoute) MatchesConsul(candidate *pbresource.Resource, namespace, partition string) bool {
	return cmp.Equal(
		in.Resource(namespace, partition),
		candidate,
		protocmp.IgnoreFields(&pbresource.Resource{}, "status", "generation", "version"),
		protocmp.IgnoreFields(&pbresource.ID{}, "uid"),
		protocmp.Transform(),
		cmpopts.SortSlices(func(a, b any) bool { return fmt.Sprintf("%v", a) < fmt.Sprintf("%v", b) }),
	)
}

func (in *HTTPRoute) KubeKind() string {
	return httpRouteKubeKind
}

func (in *HTTPRoute) KubernetesName() string {
	return in.ObjectMeta.Name
}

func (in *HTTPRoute) SetSyncedCondition(status corev1.ConditionStatus, reason, message string) {
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

func (in *HTTPRoute) SetLastSyncedTime(time *metav1.Time) {
	in.Status.LastSyncedTime = time
}

func (in *HTTPRoute) SyncedCondition() (status corev1.ConditionStatus, reason, message string) {
	cond := in.Status.GetCondition(ConditionSynced)
	if cond == nil {
		return corev1.ConditionUnknown, "", ""
	}
	return cond.Status, cond.Reason, cond.Message
}

func (in *HTTPRoute) SyncedConditionStatus() corev1.ConditionStatus {
	condition := in.Status.GetCondition(ConditionSynced)
	if condition == nil {
		return corev1.ConditionUnknown
	}
	return condition.Status
}

func (in *HTTPRoute) Validate(tenancy common.ConsulTenancyConfig) error {
	var errs field.ErrorList
	var route pbmesh.HTTPRoute
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
			if match.Path != nil {
				switch match.Path.Type {
				case pbmesh.PathMatchType_PATH_MATCH_TYPE_UNSPECIFIED:
					errs = append(errs, field.Invalid(ruleMatchPath.Child("path").Child("type"), pbmesh.PathMatchType_PATH_MATCH_TYPE_UNSPECIFIED, "missing required field"))
				case pbmesh.PathMatchType_PATH_MATCH_TYPE_EXACT:
					if !strings.HasPrefix(match.Path.Value, "/") {
						errs = append(errs, field.Invalid(ruleMatchPath.Child("path").Child("value"), match.Path.Value, "exact patch value does not start with '/'"))
					}
				case pbmesh.PathMatchType_PATH_MATCH_TYPE_PREFIX:
					if !strings.HasPrefix(match.Path.Value, "/") {
						errs = append(errs, field.Invalid(ruleMatchPath.Child("path").Child("value"), match.Path.Value, "prefix patch value does not start with '/'"))
					}
				case pbmesh.PathMatchType_PATH_MATCH_TYPE_REGEX:
					if match.Path.Value == "" {
						errs = append(errs, field.Required(ruleMatchPath.Child("path").Child("value"), "missing required field"))
					}
				default:
					errs = append(errs, field.Invalid(ruleMatchPath.Child("path").Child("type"), match.Path, "not a supported enum value"))
				}
			}

			for k, hdr := range match.Headers {
				if err := validateHeaderMatchType(hdr.Type); err != nil {
					errs = append(errs, field.Invalid(ruleMatchPath.Child("headers").Index(k).Child("type"), hdr.Type, err.Error()))
				}

				if hdr.Name == "" {
					errs = append(errs, field.Required(ruleMatchPath.Child("headers").Index(k).Child("name"), "missing required field"))
				}
			}

			for k, qm := range match.QueryParams {
				switch qm.Type {
				case pbmesh.QueryParamMatchType_QUERY_PARAM_MATCH_TYPE_UNSPECIFIED:
					errs = append(errs, field.Invalid(ruleMatchPath.Child("queryParams").Index(k).Child("type"), pbmesh.QueryParamMatchType_QUERY_PARAM_MATCH_TYPE_UNSPECIFIED, "missing required field"))
				case pbmesh.QueryParamMatchType_QUERY_PARAM_MATCH_TYPE_EXACT:
				case pbmesh.QueryParamMatchType_QUERY_PARAM_MATCH_TYPE_REGEX:
				case pbmesh.QueryParamMatchType_QUERY_PARAM_MATCH_TYPE_PRESENT:
				default:
					errs = append(errs, field.Invalid(ruleMatchPath.Child("queryParams").Index(k).Child("type"), qm.Type, "not a supported enum value"))
				}

				if qm.Name == "" {
					errs = append(errs, field.Required(ruleMatchPath.Child("queryParams").Index(k).Child("name"), "missing required field"))
				}
			}

			if match.Method != "" && !isValidHTTPMethod(match.Method) {
				errs = append(errs, field.Invalid(ruleMatchPath.Child("method"), match.Method, "not a valid http method"))
			}
		}

		var (
			hasReqMod     bool
			hasUrlRewrite bool
		)
		for j, filter := range rule.Filters {
			ruleFilterPath := path.Child("filters").Index(j)
			set := 0
			if filter.RequestHeaderModifier != nil {
				set++
				hasReqMod = true
			}
			if filter.ResponseHeaderModifier != nil {
				set++
			}
			if filter.UrlRewrite != nil {
				set++
				hasUrlRewrite = true
				if filter.UrlRewrite.PathPrefix == "" {
					errs = append(errs, field.Invalid(ruleFilterPath.Child("urlRewrite").Child("pathPrefix"), filter.UrlRewrite.PathPrefix, "field should not be empty if enclosing section is set"))
				}
			}
			if set != 1 {
				errs = append(errs, field.Invalid(ruleFilterPath, filter, "exactly one of request_header_modifier, response_header_modifier, or url_rewrite is required"))
			}
		}

		if hasReqMod && hasUrlRewrite {
			errs = append(errs, field.Invalid(rulePath.Child("filters"), rule.Filters, "exactly one of request_header_modifier or url_rewrite can be set at a time"))
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
			schema.GroupKind{Group: MeshGroup, Kind: common.HTTPRoute},
			in.KubernetesName(), errs)
	}
	return nil
}

func isValidHTTPMethod(method string) bool {
	switch method {
	case http.MethodGet,
		http.MethodHead,
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
		http.MethodConnect,
		http.MethodOptions,
		http.MethodTrace:
		return true
	default:
		return false
	}
}

// DefaultNamespaceFields is required as part of the common.MeshConfig interface.
func (in *HTTPRoute) DefaultNamespaceFields(tenancy common.ConsulTenancyConfig) {}
