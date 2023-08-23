// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func init() {
	SchemeBuilder.Register(&GatewayPolicy{}, &GatewayPolicyList{})
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// GatewayPolicy is the Schema for the gatewaypolicies API.
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.conditions[?(@.type==\"Synced\")].status",description="The sync status of the resource with Consul"
// +kubebuilder:printcolumn:name="Last Synced",type="date",JSONPath=".status.lastSyncedTime",description="The last successful synced time of the resource with Consul"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of the resource"
type GatewayPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GatewayPolicySpec `json:"spec,omitempty"`
	Status `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// GatewayPolicyList contains a list of GatewayPolicy.
type GatewayPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GatewayPolicy `json:"items"`
}

// GatewayPolicySpec defines the desired state of GatewayPolicy.
type GatewayPolicySpec struct {
	//+kubebuilder:validation:Optional
	Override *GatewayPolicyConfig `json:"override,omitempty"`
	//+kubebuilder:validation:Optional
	Default *GatewayPolicyConfig `json:"default,omitempty"`
}

type GatewayPolicyConfig struct {
	//+kubebuilder:validation:Optional
	JWT *APIGatewayJWTRequirement `json:"jwt,omitemtpy"`
}

// APIGatewayJWTRequirement holds the list of JWT providers to be verified against.
type APIGatewayJWTRequirement struct {
	// Providers is a list of providers to consider when verifying a JWT.
	Providers []*APIGatewayJWTProvider `json:"providers"`
}

// APIGatewayJWTProvider holds the provider and claim verification information.
type APIGatewayJWTProvider struct {
	// Name is the name of the JWT provider. There MUST be a corresponding
	// "jwt-provider" config entry with this name.
	Name string `json:"name"`

	// VerifyClaims is a list of additional claims to verify in a JWT's payload.
	VerifyClaims []*APIGatewayJWTClaimVerification `json:"verifyClaims,omitempty"`
}

// APIGatewayJWTClaimVerification holds the actual claim information to be verified.
type APIGatewayJWTClaimVerification struct {
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
