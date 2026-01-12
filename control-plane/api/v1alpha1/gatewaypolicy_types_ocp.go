// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gwv1beta1 "sigs.k8s.io/gateway-api-exp/apis/v1beta1"
)

func init() {
	SchemeBuilder.Register(&OcpGatewayPolicy{}, &OcpGatewayPolicyList{})
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// OcpGatewayPolicy is the Schema for the gatewaypolicies API.
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.conditions[?(@.type==\"Synced\")].status",description="The sync status of the resource with Consul"
// +kubebuilder:printcolumn:name="Last Synced",type="date",JSONPath=".status.lastSyncedTime",description="The last successful synced time of the resource with Consul"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of the resource"
type OcpGatewayPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OcpGatewayPolicySpec   `json:"spec,omitempty"`
	Status OcpGatewayPolicyStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// OcpGatewayPolicyList contains a list of OcpGatewayPolicy.
type OcpGatewayPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []OcpGatewayPolicy `json:"items"`
}

// GatewayPolicySpec defines the desired state of GatewayPolicy.
type OcpGatewayPolicySpec struct {
	// TargetRef identifies an API object to apply policy to.
	TargetRef OcpPolicyTargetReference `json:"targetRef"`
	//+kubebuilder:validation:Optional
	Override *OcpGatewayPolicyConfig `json:"override,omitempty"`
	//+kubebuilder:validation:Optional
	Default *OcpGatewayPolicyConfig `json:"default,omitempty"`
}

// PolicyTargetReference identifies the target that the policy applies to.
type OcpPolicyTargetReference struct {
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

type OcpGatewayPolicyConfig struct {
	//+kubebuilder:validation:Optional
	JWT *OcpGatewayJWTRequirement `json:"jwt,omitempty"`
}

// GatewayJWTRequirement holds the list of JWT providers to be verified against.
type OcpGatewayJWTRequirement struct {
	// Providers is a list of providers to consider when verifying a JWT.
	Providers []*OcpGatewayJWTProvider `json:"providers"`
}

// GatewayJWTProvider holds the provider and claim verification information.
type OcpGatewayJWTProvider struct {
	// Name is the name of the JWT provider. There MUST be a corresponding
	// "jwt-provider" config entry with this name.
	Name string `json:"name"`

	// VerifyClaims is a list of additional claims to verify in a JWT's payload.
	VerifyClaims []*OcpGatewayJWTClaimVerification `json:"verifyClaims,omitempty"`
}

// GatewayJWTClaimVerification holds the actual claim information to be verified.
type OcpGatewayJWTClaimVerification struct {
	// Path is the path to the claim in the token JSON.
	Path []string `json:"path"`

	// Value is the expected value at the given path:
	// - If the type at the path is a list then we verify
	//   that this value is contained in the list.
	//
	// - If the type at the path is a string then we verify
	//   that this value matches.
	Value string `json:"value"`
}

// OcpGatewayPolicyStatus defines the observed state of the gateway.
type OcpGatewayPolicyStatus struct {
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
