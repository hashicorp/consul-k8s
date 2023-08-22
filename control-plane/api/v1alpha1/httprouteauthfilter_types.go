// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func init() {
	SchemeBuilder.Register(&HTTPRouteAuthFilter{}, &HTTPRouteAuthFilterList{})
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// HTTPRouteAuthFilter is the Schema for the httprouteretryfilters API.
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.conditions[?(@.type==\"Synced\")].status",description="The sync status of the resource with Consul"
// +kubebuilder:printcolumn:name="Last Synced",type="date",JSONPath=".status.lastSyncedTime",description="The last successful synced time of the resource with Consul"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of the resource"
type HTTPRouteAuthFilter struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   HTTPRouteAuthFilterSpec `json:"spec,omitempty"`
	Status `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// HTTPRouteAuthFilterList contains a list of HTTPRouteAuthFilter.
type HTTPRouteAuthFilterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []HTTPRouteAuthFilter `json:"items"`
}

// HTTPRouteAuthFilterSpec defines the desired state of HTTPRouteAuthFilter.
type HTTPRouteAuthFilterSpec struct {
	JWT RouteJWTRequirement `json:"jwt"`
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
