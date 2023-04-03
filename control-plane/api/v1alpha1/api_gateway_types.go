// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

const (
	GatewayClassConfigKind = "GatewayClassConfig"
	managedLabel           = "api-gateway.consul.hashicorp.com/managed"
	nameLabel              = "api-gateway.consul.hashicorp.com/name"
	namespaceLabel         = "api-gateway.consul.hashicorp.com/namespace"
	createdAtLabel         = "api-gateway.consul.hashicorp.com/created"
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

// PortSpec defines the ports that the API Gateway will listen on.
type PortSpec struct {
	// The port for Consul's HTTP server.
	HTTP int `json:"http,omitempty"`
	// The grpc port for Consul's xDS server.
	GRPC int `json:"grpc,omitempty"`
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

// RoleFor constructs a Kubernetes Role for the specified Gateway based
// on the GatewayClassConfig.
func (c *GatewayClassConfig) RoleFor(gw *gwv1beta1.Gateway) *rbac.Role {
	return &rbac.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gw.Name,
			Namespace: gw.Namespace,
			Labels:    labelsForGateway(gw),
		},
		Rules: []rbac.PolicyRule{{
			APIGroups: []string{"policy"},
			Resources: []string{"podsecuritypolicies"},
			Verbs:     []string{"use"},
		}},
	}
}

// RoleBindingFor constructs a Kubernetes RoleBinding for the specified Gateway
// based on the GatewayClassConfig. If the GatewayClassConfig is configured in
// such a way that does not require a RoleBinding, nil is returned.
func (c *GatewayClassConfig) RoleBindingFor(gw *gwv1beta1.Gateway) *rbac.RoleBinding {
	serviceAccount := c.ServiceAccountFor(gw)
	if serviceAccount == nil {
		return nil
	}

	role := c.RoleFor(gw)
	if role == nil {
		return nil
	}

	return &rbac.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gw.Name,
			Namespace: gw.Namespace,
			Labels:    labelsForGateway(gw),
		},
		RoleRef: rbac.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     role.Name,
		},
		Subjects: []rbac.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      serviceAccount.Name,
				Namespace: serviceAccount.Namespace,
			},
		},
	}
}

// ServiceAccountFor constructs a Kubernetes ServiceAccount for the specified
// Gateway based on the GatewayClassConfig.
func (c *GatewayClassConfig) ServiceAccountFor(gw *gwv1beta1.Gateway) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gw.Name,
			Namespace: gw.Namespace,
			Labels:    labelsForGateway(gw),
		},
	}
}

func MergeSecret(a, b *corev1.Secret) *corev1.Secret {
	if !compareSecrets(a, b) {
		b.Annotations = a.Annotations
		b.Data = a.Data
	}

	return b
}

func compareSecrets(a, b *corev1.Secret) bool {
	if !equality.Semantic.DeepEqual(a.Annotations, b.Annotations) {
		return false
	}

	if len(b.Data) != len(a.Data) {
		return false
	}

	for k, v := range a.Data {
		otherV, ok := b.Data[k]
		if !ok || string(v) != string(otherV) {
			return false
		}
	}
	return true
}

// MergeService merges a gateway service a onto b and returns b, overriding all of
// the fields that we'd normally set for a service deployment. It does not attempt
// to change the service type
func MergeService(a, b *corev1.Service) *corev1.Service {
	if !compareServices(a, b) {
		b.Annotations = a.Annotations
		b.Spec.Ports = a.Spec.Ports
	}

	return b
}

func compareServices(a, b *corev1.Service) bool {
	// since K8s adds a bunch of defaults when we create a service, check that
	// they don't differ by the things that we may actually change, namely container
	// ports and propagated annotations
	if !equality.Semantic.DeepEqual(a.Annotations, b.Annotations) {
		return false
	}
	if len(b.Spec.Ports) != len(a.Spec.Ports) {
		return false
	}

	for i, port := range a.Spec.Ports {
		otherPort := b.Spec.Ports[i]
		if port.Port != otherPort.Port {
			return false
		}
		if port.Protocol != otherPort.Protocol {
			return false
		}
	}
	return true
}

// MergeDeployment merges a gateway deployment a onto b and returns b, overriding all of
// the fields that we'd normally set for a service deployment. It does not attempt
// to change the service type
func MergeDeployment(a, b *appsv1.Deployment) *appsv1.Deployment {
	if !compareDeployments(a, b) {
		b.Spec.Template = a.Spec.Template
		b.Spec.Replicas = a.Spec.Replicas
	}

	return b
}

func compareDeployments(a, b *appsv1.Deployment) bool {
	// since K8s adds a bunch of defaults when we create a deployment, check that
	// they don't differ by the things that we may actually change, namely container
	// ports
	if len(b.Spec.Template.Spec.Containers) != len(a.Spec.Template.Spec.Containers) {
		return false
	}
	for i, container := range a.Spec.Template.Spec.Containers {
		otherPorts := b.Spec.Template.Spec.Containers[i].Ports
		if len(container.Ports) != len(otherPorts) {
			return false
		}
		for j, port := range container.Ports {
			otherPort := otherPorts[j]
			if port.ContainerPort != otherPort.ContainerPort {
				return false
			}
			if port.Protocol != otherPort.Protocol {
				return false
			}
		}
	}

	if *b.Spec.Replicas != *a.Spec.Replicas {
		return false
	}

	return true
}

// labelsForGateway formats the correct configuration labels for a Gateway resource.
func labelsForGateway(gw *gwv1beta1.Gateway) map[string]string {
	return map[string]string{
		nameLabel:      gw.Name,
		namespaceLabel: gw.Namespace,
		createdAtLabel: fmt.Sprintf("%d", gw.CreationTimestamp.Unix()),
		managedLabel:   "true",
	}
}
