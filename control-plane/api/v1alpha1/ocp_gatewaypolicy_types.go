// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import (
	gwv1beta1 "github.com/hashicorp/consul-k8s/control-plane/custom-gateway-api/apis/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func init() {
	SchemeBuilder.Register(&OCPGatewayPolicy{}, &OCPGatewayPolicyList{})
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// OCPGatewayPolicy is the Schema for the ocpgatewaypolicies API.
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.conditions[?(@.type==\"Synced\")].status",description="The sync status of the resource with Consul"
// +kubebuilder:printcolumn:name="Last Synced",type="date",JSONPath=".status.lastSyncedTime",description="The last successful synced time of the resource with Consul"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of the resource"
type OCPGatewayPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OCPGatewayPolicySpec   `json:"spec,omitempty"`
	Status OCPGatewayPolicyStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// OCPGatewayPolicyList contains a list of OCPGatewayPolicy.
type OCPGatewayPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []OCPGatewayPolicy `json:"items"`
}

// OCPGatewayPolicySpec defines the desired state of OCPGatewayPolicy.
type OCPGatewayPolicySpec struct {
	// TargetRef identifies an API object to apply policy to.
	TargetRef OCPPolicyTargetReference `json:"targetRef"`
	//+kubebuilder:validation:Optional
	Override *GatewayPolicyConfig `json:"override,omitempty"`
	//+kubebuilder:validation:Optional
	Default *GatewayPolicyConfig `json:"default,omitempty"`
}

// OCPPolicyTargetReference identifies the target that the policy applies to.
type OCPPolicyTargetReference struct {
	// Group is the group of the target resource.
	//
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Group string `json:"group"`

	// Kind is kind of the target resource.
	//
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Kind string `json:"kind"`

	// Name is the name of the target resource.
	//
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name"`

	// Namespace is the namespace of the referent. When unspecified, the local
	// namespace is inferred. Even when policy targets a resource in a different
	// namespace, it may only apply to traffic originating from the same
	// namespace as the policy.
	//
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// SectionName refers to the listener targeted by this policy.
	SectionName *gwv1beta1.SectionName `json:"sectionName,omitempty"`
}

// OCPGatewayPolicyStatus defines the observed state of the gateway.
type OCPGatewayPolicyStatus struct {
	// Conditions describe the current conditions of the Policy.
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
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}
