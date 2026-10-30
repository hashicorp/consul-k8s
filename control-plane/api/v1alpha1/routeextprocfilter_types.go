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
	// StatPrefix selects which attached builtin/ext-proc instance this filter targets.
	// It must match the StatPrefix of a builtin/ext-proc extension on the gateway's
	// service-defaults. Omit to target the default (unnamed) instance whose filter
	// name is "envoy.filters.http.ext_proc".
	// +kubebuilder:validation:Optional
	StatPrefix string `json:"statPrefix,omitempty"`
	// Mode controls the behavior applied to the targeted ext-proc instance on this route.
	//   "disabled" -> skip this ext_proc instance for requests matching this route
	//   "enabled"  -> force-enable this instance on the route
	//   "override" -> keep this instance on the route but apply Overrides
	// +kubebuilder:validation:Enum=enabled;disabled;override
	// +kubebuilder:validation:Required
	Mode string `json:"mode"`
	// Overrides is honored only when Mode is "override". It applies per-route
	// ext_proc overrides materialized as an Envoy ExtProcPerRoute override.
	// +kubebuilder:validation:Optional
	Overrides *RouteExtProcOverrides `json:"overrides,omitempty"`
}

// RouteExtProcOverrides defines per-route overrides for a builtin/ext-proc instance.
type RouteExtProcOverrides struct {
	// Processing controls which phases of the request/response lifecycle are intercepted.
	// +kubebuilder:validation:Optional
	Processing *RouteExtProcProcessing `json:"processing,omitempty"`
	// // MessageTimeout bounds how long Envoy waits for a response from the processor.
	// // +kubebuilder:validation:Optional
	// MessageTimeout string `json:"messageTimeout,omitempty"`
	// // FailureModeAllow controls whether the request proceeds when the processor is
	// // unreachable or errors.
	// // +kubebuilder:validation:Optional
	// FailureModeAllow *bool `json:"failureModeAllow,omitempty"`
}

// RouteExtProcProcessing configures request and response processing modes.
type RouteExtProcProcessing struct {
	// +kubebuilder:validation:Optional
	Request *RouteExtProcProcessingDirection `json:"request,omitempty"`
	// +kubebuilder:validation:Optional
	Response *RouteExtProcProcessingDirection `json:"response,omitempty"`
}

// RouteExtProcProcessingDirection configures processing modes for one direction.
type RouteExtProcProcessingDirection struct {
	// HeadersMode controls whether request/response headers are sent to the processor.
	// +kubebuilder:validation:Enum=SEND;SKIP
	// +kubebuilder:validation:Optional
	HeadersMode string `json:"headersMode,omitempty"`
	// BodyMode controls whether and how the body is sent to the processor.
	// +kubebuilder:validation:Enum=SKIP;BUFFERED;BUFFERED_PARTIAL;STREAMED
	// +kubebuilder:validation:Optional
	BodyMode string `json:"bodyMode,omitempty"`
	// TrailersMode controls whether trailers are sent to the processor.
	// +kubebuilder:validation:Enum=SEND;SKIP
	// +kubebuilder:validation:Optional
	TrailersMode string `json:"trailersMode,omitempty"`
	// MaxBodyBytes is the max bytes buffered when BodyMode is BUFFERED/BUFFERED_PARTIAL.
	// +kubebuilder:validation:Optional
	MaxBodyBytes int64 `json:"maxBodyBytes,omitempty"`
}

func (h *RouteExtProc) GetNamespace() string {
	return h.Namespace
}
