package gateways

import (
	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	defaultInstances int32 = 1
)

func (b *meshGatewayBuilder) Deployment() (*appsv1.Deployment, error) {
	spec, err := b.deploymentSpec()
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      b.gateway.Name,
			Namespace: b.gateway.Namespace,
			Labels:    b.Labels(),
		},
		Spec: spec,
	}, err
}

func (b *meshGatewayBuilder) deploymentSpec() (appsv1.DeploymentSpec, error) {
	initContainer, err := initContainer(config, b.gateway.Name, b.gateway.Namespace)
	if err != nil {
		return nil, err
	}

	container, err := consulDataplaneContainer(config, gcc, gateway.Name, gateway.Namespace)
	if err != nil {
		return nil, err
	}

	return appsv1.DeploymentSpec{
		Replicas: deploymentReplicas(gcc, currentReplicas),
		Selector: &metav1.LabelSelector{
			MatchLabels: common.LabelsForGateway(&gateway),
		},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: common.LabelsForGateway(&gateway),
				Annotations: map[string]string{
					"consul.hashicorp.com/connect-inject": "false",
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
				Affinity: &corev1.Affinity{
					PodAntiAffinity: &corev1.PodAntiAffinity{
						PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
							{
								Weight: 1,
								PodAffinityTerm: corev1.PodAffinityTerm{
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: common.LabelsForGateway(&gateway),
									},
									TopologyKey: "kubernetes.io/hostname",
								},
							},
						},
					},
				},
				NodeSelector:       gcc.Spec.NodeSelector,
				Tolerations:        gcc.Spec.Tolerations,
				ServiceAccountName: g.serviceAccountName(gateway, config),
			},
		},
	}
}

func MergeDeployments(gcc v1alpha1.GatewayClassConfig, a, b *appsv1.Deployment) *appsv1.Deployment {
	if !compareDeployments(a, b) {
		b.Spec.Template = a.Spec.Template
		b.Spec.Replicas = deploymentReplicas(gcc, a.Spec.Replicas)
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

	if b.Spec.Replicas == nil && a.Spec.Replicas == nil {
		return true
	} else if b.Spec.Replicas == nil {
		return false
	} else if a.Spec.Replicas == nil {
		return false
	}

	return *b.Spec.Replicas == *a.Spec.Replicas
}

func deploymentReplicas(gcc v1alpha1.GatewayClassConfig, currentReplicas *int32) *int32 {
	instanceValue := defaultInstances

	// If currentReplicas is not nil use current value when building deployment...
	if currentReplicas != nil {
		instanceValue = *currentReplicas
	} else if gcc.Spec.DeploymentSpec.DefaultInstances != nil {
		// otherwise use the default value on the GatewayClassConfig if set.
		instanceValue = *gcc.Spec.DeploymentSpec.DefaultInstances
	}

	if gcc.Spec.DeploymentSpec.MaxInstances != nil {
		// Check if the deployment replicas are greater than the maximum and lower to the maximum if so.
		maxValue := *gcc.Spec.DeploymentSpec.MaxInstances
		if instanceValue > maxValue {
			instanceValue = maxValue
		}
	}

	if gcc.Spec.DeploymentSpec.MinInstances != nil {
		// Check if the deployment replicas are less than the minimum and raise to the minimum if so.
		minValue := *gcc.Spec.DeploymentSpec.MinInstances
		if instanceValue < minValue {
			instanceValue = minValue
		}

	}
	return &instanceValue
}
