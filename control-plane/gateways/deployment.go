// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gateways

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"

	meshv2beta1 "github.com/hashicorp/consul-k8s/control-plane/api/mesh/v2beta1"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
)

const (
	globalDefaultInstances    int32 = 1
	meshGatewayAnnotationKind       = "mesh-gateway"
)

func (b *meshGatewayBuilder) Deployment() (*appsv1.Deployment, error) {
	spec, err := b.deploymentSpec()
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:        b.gateway.Name,
			Namespace:   b.gateway.Namespace,
			Labels:      b.labelsForDeployment(),
			Annotations: b.annotationsForDeployment(),
		},
		Spec: *spec,
	}, err
}

func (b *meshGatewayBuilder) deploymentSpec() (*appsv1.DeploymentSpec, error) {
	var (
		deploymentConfig meshv2beta1.GatewayClassDeploymentConfig
		containerConfig  meshv2beta1.GatewayClassContainerConfig
	)

	// If GatewayClassConfig is not nil, use it to override the defaults for
	// the deployment and container configs.
	if b.gcc != nil {
		deploymentConfig = b.gcc.Spec.Deployment
		if deploymentConfig.Container != nil {
			containerConfig = *b.gcc.Spec.Deployment.Container
		}
	}

	initContainer, err := b.initContainer()
	if err != nil {
		return nil, err
	}

	container, err := consulDataplaneContainer(b.config, containerConfig, b.gateway.Name, b.gateway.Namespace)
	if err != nil {
		return nil, err
	}

	return &appsv1.DeploymentSpec{
		Replicas: deploymentReplicaCount(deploymentConfig.Replicas, nil),
		Selector: &metav1.LabelSelector{
			MatchLabels: b.labelsForDeployment(),
		},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: b.labelsForDeployment(),
				Annotations: map[string]string{
					// Indicate that this pod is a mesh gateway pod so that the Pod controller,
					// consul-k8s CLI, etc. can key off of it
					constants.AnnotationGatewayKind: meshGatewayAnnotationKind,
					// It's not logical to add a proxy sidecar since our workload is itself a proxy
					constants.AnnotationMeshInject: "false",
					// This functionality only applies when proxy sidecars are used
					constants.AnnotationTransparentProxyOverwriteProbes: "false",
					// This annotation determines which source to use to set the
					// WAN address and WAN port for the Mesh Gateway service registration.
					constants.AnnotationGatewayWANSource: b.gateway.Annotations[constants.AnnotationGatewayWANSource],
					// This annotation determines the WAN port for the Mesh Gateway service registration.
					constants.AnnotationGatewayWANPort: b.gateway.Annotations[constants.AnnotationGatewayWANPort],
					// This annotation determines the address for the gateway when the source annotation is "Static".
					constants.AnnotationGatewayWANAddress: b.gateway.Annotations[constants.AnnotationGatewayWANAddress],
				},
			},
			Spec: corev1.PodSpec{
				Volumes: []corev1.Volume{
					{
						Name: volumeName,
						VolumeSource: corev1.VolumeSource{
							EmptyDir: &corev1.EmptyDirVolumeSource{Medium: corev1.StorageMediumMemory},
						},
					},
				},
				InitContainers: []corev1.Container{
					initContainer,
				},
				Containers: []corev1.Container{
					container,
				},
				Affinity:                  deploymentConfig.Affinity,
				NodeSelector:              deploymentConfig.NodeSelector,
				PriorityClassName:         deploymentConfig.PriorityClassName,
				TopologySpreadConstraints: deploymentConfig.TopologySpreadConstraints,
				HostNetwork:               deploymentConfig.HostNetwork,
				Tolerations:               deploymentConfig.Tolerations,
				ServiceAccountName:        b.serviceAccountName(),
				DNSPolicy:                 deploymentConfig.DNSPolicy,
			},
		},
	}, nil
}

func (b *meshGatewayBuilder) MergeDeployments(gcc *meshv2beta1.GatewayClassConfig, old, new *appsv1.Deployment) *appsv1.Deployment {
	if old == nil {
		return new
	}
	if !compareDeployments(old, new) {
		old.Spec.Template = new.Spec.Template
		new.Spec.Replicas = deploymentReplicaCount(nil, old.Spec.Replicas)
	}

	return new
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

	if b.Spec.Replicas == nil && a.Spec.Replicas == nil {
		return true
	} else if b.Spec.Replicas == nil {
		return false
	} else if a.Spec.Replicas == nil {
		return false
	}

	return *b.Spec.Replicas == *a.Spec.Replicas
}

func deploymentReplicaCount(replicas *meshv2beta1.GatewayClassReplicasConfig, currentReplicas *int32) *int32 {
	// if we have the replicas config, use it
	if replicas != nil && replicas.Default != nil && currentReplicas == nil {
		return replicas.Default
	}

	// if we have the replicas config and the current replicas, use the min/max to ensure
	// the current replicas are within the min/max range
	if replicas != nil && currentReplicas != nil {
		if replicas.Max != nil && *currentReplicas > *replicas.Max {
			return replicas.Max
		}

		if replicas.Min != nil && *currentReplicas < *replicas.Min {
			return replicas.Min
		}

		return currentReplicas
	}

	// if we don't have the replicas config, use the current replicas if we have them
	if currentReplicas != nil {
		return currentReplicas
	}

	// otherwise use the global default
	return pointer.Int32(globalDefaultInstances)
}
