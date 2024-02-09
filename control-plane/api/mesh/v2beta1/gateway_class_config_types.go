// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v2beta1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const KindGatewayClassConfig = "GatewayClassConfig"

func init() {
	MeshSchemeBuilder.Register(&GatewayClassConfig{}, &GatewayClassConfigList{})
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// GatewayClassConfig is the Schema for the Mesh Gateway API
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of the resource"
// +kubebuilder:resource:scope=Cluster
type GatewayClassConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GatewayClassConfigSpec `json:"spec,omitempty"`
	Status `json:"status,omitempty"`
}

// +k8s:deepcopy-gen=true

// GatewayClassConfigSpec specifies the desired state of the GatewayClassConfig CRD.
type GatewayClassConfigSpec struct {
	GatewayClassAnnotationsAndLabels `json:",inline"`

	// Deployment contains config specific to the Deployment created from this GatewayClass
	Deployment GatewayClassDeploymentConfig `json:"deployment,omitempty"`
	// Role contains config specific to the Role created from this GatewayClass
	Role GatewayClassRoleConfig `json:"role,omitempty"`
	// RoleBinding contains config specific to the RoleBinding created from this GatewayClass
	RoleBinding GatewayClassRoleBindingConfig `json:"roleBinding,omitempty"`
	// Service contains config specific to the Service created from this GatewayClass
	Service GatewayClassServiceConfig `json:"service,omitempty"`
	// ServiceAccount contains config specific to the corev1.ServiceAccount created from this GatewayClass
	ServiceAccount GatewayClassServiceAccountConfig `json:"serviceAccount,omitempty"`
}

// GatewayClassDeploymentConfig specifies the desired state of the Deployment created from the GatewayClassConfig.
type GatewayClassDeploymentConfig struct {
	GatewayClassAnnotationsAndLabels `json:",inline"`

	// Container contains config specific to the created Deployment's container.
	Container *GatewayClassContainerConfig `json:"container,omitempty"`
	// InitContainer contains config specific to the created Deployment's init container.
	InitContainer *GatewayClassInitContainerConfig `json:"initContainer,omitempty"`
	// NodeSelector is a feature that constrains the scheduling of a pod to nodes that
	// match specified labels.
	// By defining NodeSelector in a pod's configuration, you can ensure that the pod is
	// only scheduled to nodes with the corresponding labels, providing a way to
	// influence the placement of workloads based on node attributes.
	// More info: https://kubernetes.io/docs/concepts/configuration/assign-pod-node/
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
	// PriorityClassName specifies the priority class name to use on the created Deployment.
	PriorityClassName string `json:"priorityClassName,omitempty"`
	// Replicas specifies the configuration to control the number of replicas for the created Deployment.
	Replicas *GatewayClassReplicasConfig `json:"replicas,omitempty"`
	// SecurityContext specifies the security context for the created Deployment's Pod.
	SecurityContext *corev1.PodSecurityContext `json:"securityContext,omitempty"`
	// Tolerations specifies the tolerations to use on the created Deployment.
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
	// HostNetwork specifies whether the gateway pods should run on the host network.
	HostNetwork bool `json:"hostNetwork,omitempty"`
	// TopologySpreadConstraints is a feature that controls how pods are spead across your topology.
	// More info: https://kubernetes.io/docs/concepts/scheduling-eviction/topology-spread-constraints/
	TopologySpreadConstraints []corev1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"`
	// DNSPolicy specifies the dns policy to use. These are set on a per pod basis.
	// +kubebuilder:validation:Enum=Default;ClusterFirst;ClusterFirstWithHostNet;None
	DNSPolicy corev1.DNSPolicy `json:"dnsPolicy,omitempty"`
	// Affinity specifies the affinity to use on the created Deployment.
	Affinity *corev1.Affinity `json:"affinity,omitempty"`
}

type GatewayClassReplicasConfig struct {
	// Default is the number of replicas assigned to the Deployment when created
	Default *int32 `json:"default,omitempty"`
	// Min is the minimum number of replicas allowed for a gateway with this class.
	// If the replica count drops below this value due to manual or automated scaling,
	// the replica count will be restored to this value.
	Min *int32 `json:"min,omitempty"`
	// Max is the maximum number of replicas allowed for a gateway with this class.
	// If the replica count exceeds this value due to manual or automated scaling,
	// the replica count will be restored to this value.
	Max *int32 `json:"max,omitempty"`
}

type GatewayClassInitContainerConfig struct {
	// Consul specifies configuration for the consul-k8s-control-plane init container
	Consul GatewayClassConsulConfig `json:"consul,omitempty"`
	// Resources specifies the resource requirements for the created Deployment's init container
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
}

type GatewayClassContainerConfig struct {
	// Consul specifies configuration for the consul-dataplane container
	Consul GatewayClassConsulConfig `json:"consul,omitempty"`
	// Resources specifies the resource requirements for the created Deployment's container
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
	// PortModifier specifies the value to be added to every port value for listeners on this gateway.
	// This is generally used to avoid binding to privileged ports in the container.
	PortModifier int32 `json:"portModifier,omitempty"`
	// HostPort specifies a port to be exposed to the external host network
	HostPort int32 `json:"hostPort,omitempty"`
}

type GatewayClassRoleConfig struct {
	GatewayClassAnnotationsAndLabels `json:",inline"`
}

type GatewayClassRoleBindingConfig struct {
	GatewayClassAnnotationsAndLabels `json:",inline"`
}

type GatewayClassServiceConfig struct {
	GatewayClassAnnotationsAndLabels `json:",inline"`

	// Type specifies the type of Service to use (LoadBalancer, ClusterIP, etc.)
	// +kubebuilder:validation:Enum=ClusterIP;NodePort;LoadBalancer
	Type *corev1.ServiceType `json:"type,omitempty"`
}

type GatewayClassServiceAccountConfig struct {
	GatewayClassAnnotationsAndLabels `json:",inline"`
}

type GatewayClassConsulConfig struct {
	// Logging specifies the logging configuration for Consul Dataplane
	Logging GatewayClassConsulLoggingConfig `json:"logging,omitempty"`
}

type GatewayClassConsulLoggingConfig struct {
	// Level sets the logging level for Consul Dataplane (debug, info, etc.)
	Level string `json:"level,omitempty"`
}

// GatewayClassAnnotationsAndLabels exists to provide a commonly-embedded wrapper
// for Annotations and Labels on a given resource configuration.
type GatewayClassAnnotationsAndLabels struct {
	// Annotations are applied to the created resource
	Annotations GatewayClassAnnotationsLabelsConfig `json:"annotations,omitempty"`
	// Labels are applied to the created resource
	Labels GatewayClassAnnotationsLabelsConfig `json:"labels,omitempty"`
}

type GatewayClassAnnotationsLabelsConfig struct {
	// InheritFromGateway lists the names/keys of annotations or labels to copy from the Gateway resource.
	// Any name/key included here will override those in Set if specified on the Gateway.
	InheritFromGateway []string `json:"inheritFromGateway,omitempty"`
	// Set lists the names/keys and values of annotations or labels to set on the resource.
	// Any name/key included here will be overridden if present in InheritFromGateway and set on the Gateway.
	Set map[string]string `json:"set,omitempty"`
}

// +kubebuilder:object:root=true

// GatewayClassConfigList contains a list of GatewayClassConfig.
type GatewayClassConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []*GatewayClassConfig `json:"items"`
}
