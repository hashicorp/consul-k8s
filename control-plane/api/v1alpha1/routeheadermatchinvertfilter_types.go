// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const RouteHeaderMatchInvertFilterKind = "RouteHeaderMatchInvertFilter"

func init() {
	SchemeBuilder.Register(&RouteHeaderMatchInvertFilter{}, &RouteHeaderMatchInvertFilterList{})
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// RouteHeaderMatchInvertFilter is referenced as an ExtensionRef filter on an
// HTTPRoute rule. It lists the header names whose match condition should be
// negated (Invert = true) when the rule is translated into a Consul http-route
// config entry.
//
// Because the native Gateway API HTTPHeaderMatch struct has no invert/negate
// field, this CRD provides a Consul-specific extension for that capability.
//
// Usage: add a filter of type ExtensionRef pointing to a
// RouteHeaderMatchInvertFilter in the same namespace. The Spec.HeaderNames
// list declares which header names within that rule's matches should be
// inverted. All other headers in the rule are translated normally.
//
// Example:
//
//	filters:
//	  - type: ExtensionRef
//	    extensionRef:
//	      group: consul.hashicorp.com
//	      kind: RouteHeaderMatchInvertFilter
//	      name: invert-x-canary
//
// Where the CRD spec is:
//
//	spec:
//	  headerNames: ["x-canary"]
//
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of the resource"
type RouteHeaderMatchInvertFilter struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RouteHeaderMatchInvertFilterSpec   `json:"spec,omitempty"`
	Status RouteHeaderMatchInvertFilterStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// RouteHeaderMatchInvertFilterList contains a list of RouteHeaderMatchInvertFilter.
type RouteHeaderMatchInvertFilterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RouteHeaderMatchInvertFilter `json:"items"`
}

// RouteHeaderMatchInvertFilterSpec defines the desired state of
// RouteHeaderMatchInvertFilter.
type RouteHeaderMatchInvertFilterSpec struct {
	// HeaderNames is the list of header names whose match condition should be
	// negated in the containing HTTPRoute rule. Names are matched
	// case-insensitively, consistent with HTTP semantics.
	// +kubebuilder:validation:MinItems=1
	HeaderNames []string `json:"headerNames"`
}

// RouteHeaderMatchInvertFilterStatus defines the observed state of
// RouteHeaderMatchInvertFilter.
type RouteHeaderMatchInvertFilterStatus struct {
	// Conditions describe the current conditions of the Filter.
	// +optional
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:MaxItems=8
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

func (h *RouteHeaderMatchInvertFilter) GetNamespace() string {
	return h.Namespace
}
