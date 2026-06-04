// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

const RouteTLSSDSFilterKind = "RouteTLSSDSFilter"

func init() {
	SchemeBuilder.Register(&RouteTLSSDSFilter{}, &RouteTLSSDSFilterList{})
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// RouteTLSSDSFilter configures route/backend TLS SDS settings for API Gateway HTTPRoute backend refs.
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.conditions[?(@.type==\"Synced\")].status",description="The sync status of the resource with Consul"
// +kubebuilder:printcolumn:name="Last Synced",type="date",JSONPath=".status.lastSyncedTime",description="The last successful synced time of the resource with Consul"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of the resource"
type RouteTLSSDSFilter struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RouteTLSSDSFilterSpec `json:"spec,omitempty"`
	Status `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// RouteTLSSDSFilterList contains a list of RouteTLSSDSFilter.
type RouteTLSSDSFilterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RouteTLSSDSFilter `json:"items"`
}

// RouteTLSSDSFilterSpec defines the desired state of RouteTLSSDSFilter.
type RouteTLSSDSFilterSpec struct {
	// SDS allows configuring TLS certificate from an SDS service.
	SDS *GatewayTLSSDSConfig `json:"sds,omitempty"`
}

func (h *RouteTLSSDSFilter) GetNamespace() string {
	return h.Namespace
}
