// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	GatewayClassConfigKind = "GatewayClassConfig"
)

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster

// GatewayClassConfig defines the values that may be set on a GatewayClass for Consul API Gateway.
type GatewayClassConfig struct {
	// Standard Kubernetes resource metadata.
	metav1.TypeMeta `json:",inline"`

	// Standard object's metadata.
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the desired state of GatewayClassConfig.
	Spec GatewayClassConfigSpec `json:"spec,omitempty"`
}

// +k8s:deepcopy-gen=true

// GatewayClassConfigSpec specifies the desired state of the Config CRD.
type GatewayClassConfigSpec struct {
	// +kubebuilder:validation:Enum=ClusterIP;NodePort;LoadBalancer
	ServiceType *corev1.ServiceType `json:"serviceType,omitempty"`

	// NodeSelector is a selector which must be true for the pod to fit on a node.
	// Selector which must match a node's labels for the pod to be scheduled on that node.
	// More info: https://kubernetes.io/docs/concepts/configuration/assign-pod-node/
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Tolerations allow the scheduler to schedule nodes with matching taints.
	// More Info: https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// Annotation Information to copy to services or deployments
	CopyAnnotations CopyAnnotationsSpec `json:"copyAnnotations,omitempty"`
}

//+kubebuilder:object:generate=true

// CopyAnnotationsSpec defines the annotations that should be copied to the gateway service.
type CopyAnnotationsSpec struct {
	// List of annotations to copy to the gateway service.
	Service []string `json:"service,omitempty"`
}

// +kubebuilder:object:root=true

// GatewayClassConfigList is a list of Config resources.
type GatewayClassConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	// Items is the list of Configs.
	Items []GatewayClassConfig `json:"items"`
}
