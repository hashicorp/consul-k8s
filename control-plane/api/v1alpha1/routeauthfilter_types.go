// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	RouteAuthFilterKind = "RouteAuthFilter"
)

func init() {
	SchemeBuilder.Register(&RouteAuthFilter{}, &RouteAuthFilterList{})
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// RouteAuthFilter is the Schema for the routeauthfilters API.
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.conditions[?(@.type==\"Synced\")].status",description="The sync status of the resource with Consul"
// +kubebuilder:printcolumn:name="Last Synced",type="date",JSONPath=".status.lastSyncedTime",description="The last successful synced time of the resource with Consul"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of the resource"
type RouteAuthFilter struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RouteAuthFilterSpec   `json:"spec,omitempty"`
	Status RouteAuthFilterStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// RouteAuthFilterList contains a list of RouteAuthFilter.
type RouteAuthFilterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RouteAuthFilter `json:"items"`
}

// RouteAuthFilterSpec defines the desired state of RouteAuthFilter.
type RouteAuthFilterSpec struct {
	// This re-uses the JWT requirement type from Gateway Policy Types.
	//+kubebuilder:validation:Optional
	JWT *GatewayJWTRequirement `json:"jwt,omitempty"`
}

// RouteAuthFilterStatus defines the observed state of the gateway.
type RouteAuthFilterStatus struct {
	// Conditions describe the current conditions of the Filter.
	//
	//
	// Known condition types are:
	//
	// * "Accepted"
	// * "ResolvedRefs"
	//
	// +optional
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:MaxItems=8
	// +kubebuilder:default={{type: "Accepted", status: "Unknown", reason:"Pending", message:"Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"},{type: "ResolvedRefs", status: "Unknown", reason:"Pending", message:"Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}
