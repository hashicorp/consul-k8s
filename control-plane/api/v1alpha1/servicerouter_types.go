// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import (
	"encoding/json"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	capi "github.com/hashicorp/consul/api"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"

	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	"github.com/hashicorp/consul-k8s/control-plane/namespaces"
)

func init() {
	SchemeBuilder.Register(&ServiceRouter{}, &ServiceRouterList{})
}

const (
	ServiceRouterKubeKind string = "servicerouter"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// ServiceRouter is the Schema for the servicerouters API
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.conditions[?(@.type==\"Synced\")].status",description="The sync status of the resource with Consul"
// +kubebuilder:printcolumn:name="Last Synced",type="date",JSONPath=".status.lastSyncedTime",description="The last successful synced time of the resource with Consul"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of the resource"
// +kubebuilder:resource:shortName="service-router"
type ServiceRouter struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ServiceRouterSpec `json:"spec,omitempty"`
	Status `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ServiceRouterList contains a list of ServiceRouter.
type ServiceRouterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ServiceRouter `json:"items"`
}

// ServiceRouterSpec defines the desired state of ServiceRouter.
type ServiceRouterSpec struct {
	// Routes are the list of routes to consider when processing L7 requests.
	// The first route to match in the list is terminal and stops further
	// evaluation. Traffic that fails to match any of the provided routes will
	// be routed to the default service.
	Routes []ServiceRoute `json:"routes,omitempty"`
}

type ServiceRoute struct {
	// Match is a set of criteria that can match incoming L7 requests.
	// If empty or omitted it acts as a catch-all.
	Match *ServiceRouteMatch `json:"match,omitempty"`
	// Destination controls how to proxy the matching request(s) to a service.
	Destination *ServiceRouteDestination `json:"destination,omitempty"`
}

type ServiceRouteMatch struct {
	// HTTP is a set of http-specific match criteria.
	HTTP *ServiceRouteHTTPMatch `json:"http,omitempty"`
}

type ServiceRouteHTTPMatch struct {
	// CaseInsensitive configures PathExact and PathPrefix matches to ignore upper/lower casing.
	CaseInsensitive bool `json:"caseInsensitive,omitempty"`
	// PathExact is an exact path to match on the HTTP request path.
	PathExact string `json:"pathExact,omitempty"`
	// PathPrefix is a path prefix to match on the HTTP request path.
	PathPrefix string `json:"pathPrefix,omitempty"`
	// PathRegex is a regular expression to match on the HTTP request path.
	PathRegex string `json:"pathRegex,omitempty"`

	// Header is a set of criteria that can match on HTTP request headers.
	// If more than one is configured all must match for the overall match to apply.
	Header []ServiceRouteHTTPMatchHeader `json:"header,omitempty"`
	// QueryParam is a set of criteria that can match on HTTP query parameters.
	// If more than one is configured all must match for the overall match to apply.
	QueryParam []ServiceRouteHTTPMatchQueryParam `json:"queryParam,omitempty"`
	// Methods is a list of HTTP methods for which this match applies.
	// If unspecified all http methods are matched.
	Methods []string `json:"methods,omitempty"`
}

type ServiceRouteHTTPMatchQueryParam struct {
	// Name is the name of the query parameter to match on.
	Name string `json:"name"`
	// Present will match if the query parameter with the given name is present
	// with any value.
	Present bool `json:"present,omitempty"`
	// Exact will match if the query parameter with the given name is this value.
	Exact string `json:"exact,omitempty"`
	// Regex will match if the query parameter with the given name matches this pattern.
	Regex string `json:"regex,omitempty"`
}

type ServiceRouteHTTPMatchHeader struct {
	// Name is the name of the header to match.
	Name string `json:"name"`
	// Present will match if the header with the given name is present with any value.
	Present bool `json:"present,omitempty"`
	// Exact will match if the header with the given name is this value.
	Exact string `json:"exact,omitempty"`
	// Prefix will match if the header with the given name has this prefix.
	Prefix string `json:"prefix,omitempty"`
	// Suffix will match if the header with the given name has this suffix.
	Suffix string `json:"suffix,omitempty"`
	// Regex will match if the header with the given name matches this pattern.
	Regex string `json:"regex,omitempty"`
	// Invert inverts the logic of the match.
	Invert bool `json:"invert,omitempty"`
}

type ServiceRouteDestination struct {
	// Service is the service to resolve instead of the default service.
	// If empty then the default service name is used.
	Service string `json:"service,omitempty"`
	// ServiceSubset is a named subset of the given service to resolve instead
	// of the one defined as that service's DefaultSubset.
	// If empty, the default subset is used.
	ServiceSubset string `json:"serviceSubset,omitempty"`
	// Namespace is the Consul namespace to resolve the service from instead of
	// the current namespace. If empty the current namespace is assumed.
	Namespace string `json:"namespace,omitempty"`
	// Partition is the Consul partition to resolve the service from instead of
	// the current partition. If empty the current partition is assumed.
	Partition string `json:"partition,omitempty"`
	// PrefixRewrite defines how to rewrite the HTTP request path before proxying
	// it to its final destination.
	// This requires that either match.http.pathPrefix or match.http.pathExact
	// be configured on this route.
	PrefixRewrite string `json:"prefixRewrite,omitempty"`
	// IdleTimeout is total amount of time permitted
	// for the request stream to be idle.
	IdleTimeout metav1.Duration `json:"idleTimeout,omitempty"`
	// RequestTimeout is the total amount of time permitted for the entire
	// downstream request (and retries) to be processed.
	RequestTimeout metav1.Duration `json:"requestTimeout,omitempty"`
	// NumRetries is the number of times to retry the request when a retryable result occurs
	NumRetries uint32 `json:"numRetries,omitempty"`
	// RetryOnConnectFailure allows for connection failure errors to trigger a retry.
	RetryOnConnectFailure bool `json:"retryOnConnectFailure,omitempty"`
	// RetryOn is a flat list of conditions for Consul to retry requests based on the response from an upstream service.
	// Refer to the valid conditions here: https://developer.hashicorp.com/consul/docs/connect/config-entries/service-router#routes-destination-retryon
	RetryOn []string `json:"retryOn,omitempty"`
	// RetryOnStatusCodes is a flat list of http response status codes that are eligible for retry.
	RetryOnStatusCodes []uint32 `json:"retryOnStatusCodes,omitempty"`
	// Allow HTTP header manipulation to be configured.
	RequestHeaders  *HTTPHeaderModifiers `json:"requestHeaders,omitempty"`
	ResponseHeaders *HTTPHeaderModifiers `json:"responseHeaders,omitempty"`
}

func (in *ServiceRouter) ConsulMirroringNS() string {
	return in.Namespace
}

func (in *ServiceRouter) GetObjectMeta() metav1.ObjectMeta {
	return in.ObjectMeta
}

func (in *ServiceRouter) AddFinalizer(name string) {
	in.ObjectMeta.Finalizers = append(in.Finalizers(), name)
}

func (in *ServiceRouter) RemoveFinalizer(name string) {
	var newFinalizers []string
	for _, oldF := range in.Finalizers() {
		if oldF != name {
			newFinalizers = append(newFinalizers, oldF)
		}
	}
	in.ObjectMeta.Finalizers = newFinalizers
}

func (in *ServiceRouter) Finalizers() []string {
	return in.ObjectMeta.Finalizers
}

func (in *ServiceRouter) ConsulKind() string {
	return capi.ServiceRouter
}

func (in *ServiceRouter) KubeKind() string {
	return ServiceRouterKubeKind
}

func (in *ServiceRouter) ConsulName() string {
	return in.ObjectMeta.Name
}

func (in *ServiceRouter) KubernetesName() string {
	return in.ObjectMeta.Name
}

func (in *ServiceRouter) SetSyncedCondition(status corev1.ConditionStatus, reason, message string) {
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

func (in *ServiceRouter) SetLastSyncedTime(time *metav1.Time) {
	in.Status.LastSyncedTime = time
}

func (in *ServiceRouter) SyncedCondition() (status corev1.ConditionStatus, reason, message string) {
	cond := in.Status.GetCondition(ConditionSynced)
	if cond == nil {
		return corev1.ConditionUnknown, "", ""
	}
	return cond.Status, cond.Reason, cond.Message
}

func (in *ServiceRouter) SyncedConditionStatus() corev1.ConditionStatus {
	condition := in.Status.GetCondition(ConditionSynced)
	if condition == nil {
		return corev1.ConditionUnknown
	}
	return condition.Status
}

func (in *ServiceRouter) ConsulGlobalResource() bool {
	return false
}

func (in *ServiceRouter) ToConsul(datacenter string) capi.ConfigEntry {
	var routes []capi.ServiceRoute
	for _, r := range in.Spec.Routes {
		routes = append(routes, r.toConsul())
	}
	return &capi.ServiceRouterConfigEntry{
		Kind:   in.ConsulKind(),
		Name:   in.ConsulName(),
		Routes: routes,
		Meta:   meta(datacenter),
	}
}

func (in *ServiceRouter) MatchesConsul(candidate capi.ConfigEntry) bool {
	configEntry, ok := candidate.(*capi.ServiceRouterConfigEntry)
	if !ok {
		return false
	}

	specialEquality := cmp.Options{
		cmp.FilterPath(func(path cmp.Path) bool {
			return path.String() == "Routes.Destination.Namespace"
		}, cmp.Transformer("NormalizeNamespace", normalizeEmptyToDefault)),
		cmp.FilterPath(func(path cmp.Path) bool {
			return path.String() == "Routes.Destination.Partition"
		}, cmp.Transformer("NormalizePartition", normalizeEmptyToDefault)),
	}

	// No datacenter is passed to ToConsul as we ignore the Meta field when checking for equality.
	return cmp.Equal(in.ToConsul(""), configEntry, cmpopts.IgnoreFields(capi.ServiceRouterConfigEntry{}, "Partition", "Namespace", "Meta", "ModifyIndex", "CreateIndex"), cmpopts.IgnoreUnexported(), cmpopts.EquateEmpty(), specialEquality)
}

func (in *ServiceRouter) Validate(consulMeta common.ConsulMeta) error {
	var errs field.ErrorList
	path := field.NewPath("spec")
	for i, r := range in.Spec.Routes {
		errs = append(errs, r.validate(path.Child("routes").Index(i))...)
	}

	errs = append(errs, in.validateEnterprise(consulMeta)...)

	if len(errs) > 0 {
		return apierrors.NewInvalid(
			schema.GroupKind{Group: ConsulHashicorpGroup, Kind: ServiceRouterKubeKind},
			in.KubernetesName(), errs)
	}
	return nil
}

// DefaultNamespaceFields sets the namespace field on spec.routes[].destination to their default values if namespaces are enabled.
func (in *ServiceRouter) DefaultNamespaceFields(consulMeta common.ConsulMeta) {
	// If namespaces are enabled we want to set the namespace fields to their
	// defaults. If namespaces are not enabled (i.e. OSS) we don't set the
	// namespace fields because this would cause errors
	// making API calls (because namespace fields can't be set in OSS).
	if consulMeta.NamespacesEnabled {
		// Default to the current namespace (i.e. the namespace of the config entry).
		namespace := namespaces.ConsulNamespace(in.Namespace, consulMeta.NamespacesEnabled, consulMeta.DestinationNamespace, consulMeta.Mirroring, consulMeta.Prefix)
		for i, r := range in.Spec.Routes {
			if r.Destination != nil {
				if r.Destination.Namespace == "" {
					in.Spec.Routes[i].Destination.Namespace = namespace
				}
			}
		}
	}
}

func (in ServiceRoute) toConsul() capi.ServiceRoute {
	return capi.ServiceRoute{
		Match:       in.Match.toConsul(),
		Destination: in.Destination.toConsul(),
	}
}

func (in *ServiceRouteMatch) toConsul() *capi.ServiceRouteMatch {
	if in == nil {
		return nil
	}
	return &capi.ServiceRouteMatch{
		HTTP: in.HTTP.toConsul(),
	}
}

func (in ServiceRouteHTTPMatchHeader) toConsul() capi.ServiceRouteHTTPMatchHeader {
	return capi.ServiceRouteHTTPMatchHeader{
		Name:    in.Name,
		Present: in.Present,
		Exact:   in.Exact,
		Prefix:  in.Prefix,
		Suffix:  in.Suffix,
		Regex:   in.Regex,
		Invert:  in.Invert,
	}
}

func (in ServiceRouteHTTPMatchQueryParam) toConsul() capi.ServiceRouteHTTPMatchQueryParam {
	return capi.ServiceRouteHTTPMatchQueryParam{
		Name:    in.Name,
		Present: in.Present,
		Exact:   in.Exact,
		Regex:   in.Regex,
	}
}

func (in *ServiceRouteDestination) toConsul() *capi.ServiceRouteDestination {
	if in == nil {
		return nil
	}
	return &capi.ServiceRouteDestination{
		Service:               in.Service,
		ServiceSubset:         in.ServiceSubset,
		Namespace:             in.Namespace,
		Partition:             in.Partition,
		PrefixRewrite:         in.PrefixRewrite,
		IdleTimeout:           in.IdleTimeout.Duration,
		RequestTimeout:        in.RequestTimeout.Duration,
		NumRetries:            in.NumRetries,
		RetryOnConnectFailure: in.RetryOnConnectFailure,
		RetryOn:               in.RetryOn,
		RetryOnStatusCodes:    in.RetryOnStatusCodes,
		RequestHeaders:        in.RequestHeaders.toConsul(),
		ResponseHeaders:       in.ResponseHeaders.toConsul(),
	}
}

func (in *ServiceRouter) validateEnterprise(consulMeta common.ConsulMeta) field.ErrorList {
	var errs field.ErrorList
	path := field.NewPath("spec")
	if !consulMeta.NamespacesEnabled {
		for i, r := range in.Spec.Routes {
			if r.Destination != nil {
				if r.Destination.Namespace != "" {
					errs = append(errs, field.Invalid(path.Child("routes").Index(i).Child("destination").Child("namespace"), r.Destination.Namespace, `Consul Enterprise namespaces must be enabled to set destination.namespace`))
				}
			}
		}
	}
	if !consulMeta.PartitionsEnabled {
		for i, r := range in.Spec.Routes {
			if r.Destination != nil {
				if r.Destination.Partition != "" {
					errs = append(errs, field.Invalid(path.Child("routes").Index(i).Child("destination").Child("partition"), r.Destination.Partition, `Consul Enterprise partitions must be enabled to set destination.partition`))
				}
			}
		}
	}
	return errs
}

func (in ServiceRoute) validate(path *field.Path) field.ErrorList {
	var errs field.ErrorList
	if in.Destination != nil && in.Destination.PrefixRewrite != "" {
		if in.Match == nil || in.Match.HTTP == nil || (in.Match.HTTP.PathPrefix == "" && in.Match.HTTP.PathExact == "") {
			asJSON, _ := json.Marshal(in)
			errs = append(errs, field.Invalid(path, string(asJSON), "destination.prefixRewrite requires that either match.http.pathPrefix or match.http.pathExact be configured on this route"))
		}
	}
	errs = append(errs, in.Match.validate(path.Child("match"))...)

	return errs
}

func (in *ServiceRouteMatch) validate(path *field.Path) field.ErrorList {
	if in == nil {
		return nil
	}
	return in.HTTP.validate(path.Child("http"))
}

func (in *ServiceRouteHTTPMatch) toConsul() *capi.ServiceRouteHTTPMatch {
	if in == nil {
		return nil
	}
	var header []capi.ServiceRouteHTTPMatchHeader
	for _, h := range in.Header {
		header = append(header, h.toConsul())
	}
	var query []capi.ServiceRouteHTTPMatchQueryParam
	for _, q := range in.QueryParam {
		query = append(query, q.toConsul())
	}
	return &capi.ServiceRouteHTTPMatch{
		CaseInsensitive: in.CaseInsensitive,
		PathExact:       in.PathExact,
		PathPrefix:      in.PathPrefix,
		PathRegex:       in.PathRegex,
		Header:          header,
		QueryParam:      query,
		Methods:         in.Methods,
	}
}

func (in *ServiceRouteHTTPMatch) validate(path *field.Path) field.ErrorList {
	var errs field.ErrorList
	if in == nil {
		return nil
	}
	if numNonZeroValue(in.PathExact, in.PathPrefix, in.PathRegex) > 1 {
		asJSON, _ := json.Marshal(in)
		errs = append(errs, field.Invalid(path, string(asJSON), "at most only one of pathExact, pathPrefix, or pathRegex may be configured"))
	}
	if invalidPathPrefix(in.PathExact) {
		errs = append(errs, field.Invalid(path.Child("pathExact"), in.PathExact, "must begin with a '/'"))
	}
	if invalidPathPrefix(in.PathPrefix) {
		errs = append(errs, field.Invalid(path.Child("pathPrefix"), in.PathPrefix, "must begin with a '/'"))
	}

	for i, h := range in.Header {
		if err := h.validate(path.Child("header").Index(i)); err != nil {
			errs = append(errs, err)
		}
	}

	for i, q := range in.QueryParam {
		if err := q.validate(path.Child("queryParam").Index(i)); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

func (in *ServiceRouteHTTPMatchHeader) validate(path *field.Path) *field.Error {
	if in == nil {
		return nil
	}

	if numNonZeroValue(in.Exact, in.Prefix, in.Suffix, in.Regex, in.Present) > 1 {
		asJSON, _ := json.Marshal(in)
		return field.Invalid(path, string(asJSON), "at most only one of exact, prefix, suffix, regex, or present may be configured")
	}
	return nil
}

func (in *ServiceRouteHTTPMatchQueryParam) validate(path *field.Path) *field.Error {
	if in == nil {
		return nil
	}

	if numNonZeroValue(in.Exact, in.Regex, in.Present) > 1 {
		asJSON, _ := json.Marshal(in)
		return field.Invalid(path, string(asJSON), "at most only one of exact, regex, or present may be configured")
	}
	return nil
}

// numNonZeroValue returns the number of elements that aren't set to their
// zero values.
func numNonZeroValue(elems ...interface{}) int {
	var count int
	for _, elem := range elems {
		switch elem.(type) {
		case string:
			if elem != "" {
				count++
			}
		case bool:
			if elem != false {
				count++
			}
		case int:
			if elem != 0 {
				count++
			}
		}
	}
	return count
}
