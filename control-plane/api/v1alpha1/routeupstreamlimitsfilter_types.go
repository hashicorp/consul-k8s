// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func init() {
	SchemeBuilder.Register(&RouteUpstreamLimitsFilter{}, &RouteUpstreamLimitsFilterList{})
}

const RouteUpstreamLimitsFilterKind = "RouteUpstreamLimitsFilter"

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// RouteUpstreamLimitsFilter is the Schema for the routeupstreamlimitsfilters API.
// It is referenced from an HTTPRoute backendRef via an extensionRef filter to
// apply per-service upstream circuit-breaker limits and passive health checking
// (Envoy outlier detection) to the routed service behind an API Gateway.
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.conditions[?(@.type==\"Synced\")].status",description="The sync status of the resource with Consul"
// +kubebuilder:printcolumn:name="Last Synced",type="date",JSONPath=".status.lastSyncedTime",description="The last successful synced time of the resource with Consul"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of the resource"
type RouteUpstreamLimitsFilter struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RouteUpstreamLimitsFilterSpec `json:"spec,omitempty"`
	Status `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// RouteUpstreamLimitsFilterList contains a list of RouteUpstreamLimitsFilter.
type RouteUpstreamLimitsFilterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RouteUpstreamLimitsFilter `json:"items"`
}

// RouteUpstreamLimitsFilterSpec defines the desired state of RouteUpstreamLimitsFilter.
// The fields mirror the Consul api-gateway route service `Limits` (UpstreamLimits)
// contract. Any field left unset inherits the gateway-wide default.
type RouteUpstreamLimitsFilterSpec struct {
	// MaxConnections is the maximum number of connections the gateway proxy can
	// make to the upstream service.
	// +kubebuilder:validation:Minimum:=0
	// +kubebuilder:validation:Optional
	MaxConnections *int `json:"maxConnections,omitempty"`
	// MaxPendingRequests is the maximum number of requests that are queued while
	// waiting for an available connection.
	// +kubebuilder:validation:Minimum:=0
	// +kubebuilder:validation:Optional
	MaxPendingRequests *int `json:"maxPendingRequests,omitempty"`
	// MaxConcurrentRequests is the maximum number of in-flight requests allowed to
	// the upstream cluster at a point in time.
	// +kubebuilder:validation:Minimum:=0
	// +kubebuilder:validation:Optional
	MaxConcurrentRequests *int `json:"maxConcurrentRequests,omitempty"`
	// PassiveHealthCheck configures how the gateway monitors the upstream for
	// removal from the load-balancing pool (Envoy outlier detection).
	// +kubebuilder:validation:Optional
	PassiveHealthCheck *PassiveHealthCheck `json:"passiveHealthCheck,omitempty"`
}

func (h *RouteUpstreamLimitsFilter) GetNamespace() string {
	return h.Namespace
}
