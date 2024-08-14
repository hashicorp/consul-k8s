// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func init() {
	SchemeBuilder.Register(&RouteRetryFilter{}, &RouteRetryFilterList{})
}

const RouteRetryFilterKind = "RouteRetryFilter"

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// RouteRetryFilter is the Schema for the routeretryfilters API
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.conditions[?(@.type==\"Synced\")].status",description="The sync status of the resource with Consul"
// +kubebuilder:printcolumn:name="Last Synced",type="date",JSONPath=".status.lastSyncedTime",description="The last successful synced time of the resource with Consul"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of the resource"
type RouteRetryFilter struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RouteRetryFilterSpec `json:"spec,omitempty"`
	Status `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// RouteRetryFilterList contains a list of RouteRetryFilter.
type RouteRetryFilterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RouteRetryFilter `json:"items"`
}

// RouteRetryFilterSpec defines the desired state of RouteRetryFilter.
type RouteRetryFilterSpec struct {
	// +kubebuilder:validation:Minimum:=0
	// +kubebuilder:validation:Optional
	NumRetries *uint32 `json:"numRetries"`
	// +kubebuilder:validation:Optional
	RetryOn []string `json:"retryOn"`
	// +kubebuilder:validation:Optional
	RetryOnStatusCodes []uint32 `json:"retryOnStatusCodes"`
	// +kubebuilder:validation:Optional
	RetryOnConnectFailure *bool `json:"retryOnConnectFailure"`
}

func (h *RouteRetryFilter) GetNamespace() string {
	return h.Namespace
}
