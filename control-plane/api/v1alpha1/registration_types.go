// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func init() {
	SchemeBuilder.Register(&Registration{}, &RegistrationList{})
}

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster

// Registration defines the.
type Registration struct {
	// Standard Kubernetes resource metadata.
	metav1.TypeMeta `json:",inline"`

	// Standard object's metadata.
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the desired state of Registration.
	Spec RegistrationSpec `json:"spec,omitempty"`
}

// +k8s:deepcopy-gen=true

// RegistrationSpec specifies the desired state of the Config CRD.
type RegistrationSpec struct {
	Name      string `json:"name"`
	Port      int    `json:"port"`
	Address   string `json:"address"`
	Namespace string `json:"namespace,omitempty"`
	Partition string `json:"partition,omitempty"`
}

// +kubebuilder:object:root=true

// RegistrationList is a list of Config resources.
type RegistrationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	// Items is the list of Configs.
	Items []Registration `json:"items"`
}
