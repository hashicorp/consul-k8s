// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	GatewayClassConfigKind = "GatewayClassConfig"
	MeshServiceKind        = "MeshService"
)

func init() {
	SchemeBuilder.Register(&GatewayClassConfig{}, &GatewayClassConfigList{})
	SchemeBuilder.Register(&MeshService{}, &MeshServiceList{})
}

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

	// Deployment defines the deployment configuration for the gateway.
	DeploymentSpec DeploymentSpec `json:"deployment,omitempty"`

	// Annotation Information to copy to services or deployments
	CopyAnnotations CopyAnnotationsSpec `json:"copyAnnotations,omitempty"`

	// The name of an existing Kubernetes PodSecurityPolicy to bind to the managed ServiceAccount if ACLs are managed.
	PodSecurityPolicy string `json:"podSecurityPolicy,omitempty"`

	// The name of the OpenShift SecurityContextConstraints resource for this gateway class to use.
	OpenshiftSCCName string `json:"openshiftSCCName,omitempty"`

	// The value to add to privileged ports ( ports < 1024) for gateway containers
	MapPrivilegedContainerPorts int32 `json:"mapPrivilegedContainerPorts,omitempty"`

	// Metrics defines how to configure the metrics for a gateway.
	Metrics MetricsSpec `json:"metrics,omitempty"`

	// Probes defines default Kubernetes probes applied to gateway deployments.
	Probes *ProbesSpec `json:"probes,omitempty"`
}

// +k8s:deepcopy-gen=true
// ProbesSpec groups the three standard Kubernetes probes.
type ProbesSpec struct {
	Liveness  *corev1.Probe `json:"liveness,omitempty"`
	Readiness *corev1.Probe `json:"readiness,omitempty"`
	Startup   *corev1.Probe `json:"startup,omitempty"`
}

// +k8s:deepcopy-gen=true

type DeploymentSpec struct {
	// +kubebuilder:default:=1
	// +kubebuilder:validation:Maximum=8
	// +kubebuilder:validation:Minimum=1
	// Number of gateway instances that should be deployed by default
	DefaultInstances *int32 `json:"defaultInstances,omitempty"`
	// +kubebuilder:default:=8
	// +kubebuilder:validation:Maximum=8
	// +kubebuilder:validation:Minimum=1
	// Max allowed number of gateway instances
	MaxInstances *int32 `json:"maxInstances,omitempty"`
	// +kubebuilder:default:=1
	// +kubebuilder:validation:Maximum=8
	// +kubebuilder:validation:Minimum=1
	// Minimum allowed number of gateway instances
	MinInstances *int32 `json:"minInstances,omitempty"`

	// Resources defines the resource requirements for the gateway.
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
}

// +k8s:deepcopy-gen=true

type MetricsSpec struct {
	// +kubebuilder:validation:Maximum=65535
	// +kubebuilder:validation:Minimum=1024
	// The port used for metrics.
	Port *int32 `json:"port,omitempty"`

	// The path used for metrics.
	Path *string `json:"path,omitempty"`

	// Enable metrics for this class of gateways. If unspecified, will inherit
	// behavior from the global Helm configuration.
	Enabled *bool `json:"enabled,omitempty"`
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

// +genclient
// +kubebuilder:object:root=true

// MeshService holds a reference to an externally managed Consul Service Mesh service.
type MeshService struct {
	metav1.TypeMeta `json:",inline"`
	// Standard object's metadata.
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the desired state of MeshService.
	Spec MeshServiceSpec `json:"spec,omitempty"`
}

// +k8s:deepcopy-gen=true

// MeshServiceSpec specifies the 'spec' of the MeshService CRD.
type MeshServiceSpec struct {
	// Name holds the service name for a Consul service.
	Name string `json:"name,omitempty"`
	// Peer optionally specifies the name of the peer exporting the Consul service.
	// If not specified, the Consul service is assumed to be in the local datacenter.
	Peer *string `json:"peer,omitempty"`
}

// +kubebuilder:object:root=true

// MeshServiceList is a list of MeshService resources.
type MeshServiceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []MeshService `json:"items"`
}
