// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const RouteExtProcKind = "RouteExtProc"

func init() {
	SchemeBuilder.Register(&RouteExtProc{}, &RouteExtProcList{})
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// RouteExtProc is the Schema for the routeextprocs API.
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.conditions[?(@.type==\"Synced\")].status",description="The sync status of the resource with Consul"
// +kubebuilder:printcolumn:name="Last Synced",type="date",JSONPath=".status.lastSyncedTime",description="The last successful synced time of the resource with Consul"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of the resource"
type RouteExtProc struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RouteExtProcSpec `json:"spec,omitempty"`
	Status `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// RouteExtProcList contains a list of RouteExtProc.
type RouteExtProcList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RouteExtProc `json:"items"`
}

// RouteExtProcSpec defines the desired state of RouteExtProc.
type RouteExtProcSpec struct {
	// StatPrefix is the StatPrefix of the builtin/ext-proc instance to act on.
	// +kubebuilder:validation:Required
	StatPrefix string `json:"statPrefix"`
	// Mode controls the behavior applied to the targeted ext-proc instance.
	// +kubebuilder:validation:Enum=enabled;disabled
	// +kubebuilder:validation:Required
	Mode string `json:"mode"`
}

func (h *RouteExtProc) GetNamespace() string {
	return h.Namespace
}
