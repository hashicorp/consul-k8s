// // Copyright (c) HashiCorp, Inc.
// // SPDX-License-Identifier: MPL-2.0
package v2beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func init() {
	MeshSchemeBuilder.Register(&GatewayClass{}, &GatewayClassList{})
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// GatewayClass is the Schema for the Gateway Class API
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.conditions[?(@.type==\"Synced\")].status",description="The sync status of the resource with Consul"
// +kubebuilder:printcolumn:name="Last Synced",type="date",JSONPath=".status.lastSyncedTime",description="The last successful synced time of the resource with Consul"
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
	ControllerName string               `json:"controllerName"`
	ParametersRef  *ParametersReference `json:"parametersRef"`
	Description    string               `json:"description"`
}

type ParametersReference struct {
	// Group is the Kubernetes group that the referenced resource belongs to.
	Group string `json:"group"`

	// Kind is the Kubernetes kind of the referenced resource.
	Kind string `json:"kind"`

	// Name is the name of the referenced resource.
	Name string `json:"name"`

	// Namespace is the namespace of the referenced resource. If empty, the referenced
	// resource is assumed to be in the same namespace as the GatewayClass.
	Namespace *string `json:"namespace,omitempty"`
}
