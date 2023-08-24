// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	RouteAuthFilterKind = "HTTPRouteAuthFilter"
)

func init() {
	SchemeBuilder.Register(&RouteAuthFilter{}, &RouteAuthFilterList{})
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// RouteAuthFilter is the Schema for the httpauthfilters API.
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.conditions[?(@.type==\"Synced\")].status",description="The sync status of the resource with Consul"
// +kubebuilder:printcolumn:name="Last Synced",type="date",JSONPath=".status.lastSyncedTime",description="The last successful synced time of the resource with Consul"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of the resource"
type RouteAuthFilter struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RouteAuthFilterSpec `json:"spec,omitempty"`
	Status `json:"status,omitempty"`
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
	//+kubebuilder:validation:Optional
	JWT *RouteJWTRequirement `json:"jwt,omitempty"`
}

// RouteJWTRequirement defines the JWT requirements per provider.
type RouteJWTRequirement struct {
	Providers []RouteJWTProvider `json:"providers"`
}

// RouteJWTProvider defines the configuration for a specific JWT provider.
type RouteJWTProvider struct {
	Name         string                      `json:"name"`
	VerifyClaims []RouteJWTClaimVerification `json:"verifyClaims"`
}

// RouteJWTClaimVerification defines the specific claims to be verified.
type RouteJWTClaimVerification struct {
	Path  []string `json:"path"`
	Value string   `json:"value"`
}
