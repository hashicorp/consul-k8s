// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0
package v2beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const KindGatewayClass = "GatewayClass"

func init() {
	MeshSchemeBuilder.Register(&GatewayClass{}, &GatewayClassList{})
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// GatewayClass is the Schema for the Gateway Class API
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of the resource"
// +kubebuilder:resource:scope=Cluster
type GatewayClass struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GatewayClassSpec `json:"spec,omitempty"`
	Status `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// GatewayClassList contains a list of GatewayClass.
type GatewayClassList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []*GatewayClass `json:"items"`
}

type GatewayClassSpec struct {
	// ControllerName is the name of the Kubernetes controller
	// that manages Gateways of this class
	ControllerName string `json:"controllerName"`

	// ParametersRef refers to a resource responsible for configuring
	// the behavior of the GatewayClass.
	ParametersRef *ParametersReference `json:"parametersRef"`

	// Description of GatewayClass
	Description string `json:"description,omitempty"`
}

type ParametersReference struct {
	// The Kubernetes Group that the referred object belongs to
	Group string `json:"group,omitempty"`

	// The Kubernetes Kind that the referred object is
	Kind string `json:"kind,omitempty"`

	// The Name of the referred object
	Name string `json:"name"`

	// The kubernetes namespace that the referred object is in
	Namespace *string `json:"namespace,omitempty"`
}
