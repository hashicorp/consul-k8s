// // Copyright (c) HashiCorp, Inc.
// // SPDX-License-Identifier: MPL-2.0

package v2beta1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func init() {
	MeshSchemeBuilder.Register(&GatewayClassConfig{}, &GatewayClassConfigList{})
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// GatewayClassConfig is the Schema for the Mesh Gateway API
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.conditions[?(@.type==\"Synced\")].status",description="The sync status of the resource with Consul"
// +kubebuilder:printcolumn:name="Last Synced",type="date",JSONPath=".status.lastSyncedTime",description="The last successful synced time of the resource with Consul"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of the resource"
// +kubebuilder:resource:shortName="gateway-class-config"
// +kubebuilder:resource:scope=Cluster
type GatewayClassConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GatewayClassConfigSpec `json:"spec,omitempty"`
	Status `json:"status,omitempty"`
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

//+kubebuilder:object:generate=true

// CopyAnnotationsSpec defines the annotations that should be copied to the gateway service.
type CopyAnnotationsSpec struct {
	// List of annotations to copy to the gateway service.
	Service []string `json:"service,omitempty"`
}

// +kubebuilder:object:root=true

// GatewayClassConfigList contains a list of GatewayClassConfig.
type GatewayClassConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []*GatewayClassConfig `json:"items"`
}
