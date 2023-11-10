// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func init() {
	SchemeBuilder.Register(&RouteTimeoutFilter{}, &RouteTimeoutFilterList{})
}

const RouteTimeoutFilterKind = "RouteTimeoutFilter"

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// RouteTimeoutFilter is the Schema for the httproutetimeoutfilters API
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.conditions[?(@.type==\"Synced\")].status",description="The sync status of the resource with Consul"
// +kubebuilder:printcolumn:name="Last Synced",type="date",JSONPath=".status.lastSyncedTime",description="The last successful synced time of the resource with Consul"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of the resource"
type RouteTimeoutFilter struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RouteTimeoutFilterSpec `json:"spec,omitempty"`
	Status `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// RouteTimeoutFilterList contains a list of RouteTimeoutFilter.
type RouteTimeoutFilterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RouteTimeoutFilter `json:"items"`
}

// RouteTimeoutFilterSpec defines the desired state of RouteTimeoutFilter.
type RouteTimeoutFilterSpec struct {
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Format=duration
	RequestTimeout metav1.Duration `json:"requestTimeout"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Format=duration
	IdleTimeout metav1.Duration `json:"idleTimeout"`
}

func (h *RouteTimeoutFilter) GetNamespace() string {
	return h.Namespace
}
