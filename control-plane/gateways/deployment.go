package gateways

import (
	"fmt"
	meshv2beta1 "github.com/hashicorp/consul-k8s/control-plane/api/mesh/v2beta1"
	pbmesh "github.com/hashicorp/consul/proto-public/pbmesh/v2beta1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

const (
	globalDefaultInstances int32 = 1
)

func (b *meshGatewayBuilder) Deployment() (*appsv1.Deployment, error) {
	fmt.Println("---- deployment ----")
	spec, err := b.deploymentSpec()
	fmt.Printf("%+v\n", spec)
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      b.gateway.Name,
			Namespace: b.gateway.Namespace,
			Labels:    b.Labels(),
		},
		Spec: *spec,
	}, err
}

func (b *meshGatewayBuilder) deploymentSpec() (*appsv1.DeploymentSpec, error) {
	initContainer, err := initContainer(b.config, b.gateway.Name, b.gateway.Namespace)
	if err != nil {
		return nil, err
	}

	//TODO @sarah.alsmiller get resources from gatewayclassconfig spec (?)
	container, err := consulDataplaneContainer(b.config, nil, b.gateway.Name, b.gateway.Namespace)
	if err != nil {
		return nil, err
	}

	fmt.Println("deployment spec-------")
	fmt.Printf("%+v\n", b.gcc)
	return &appsv1.DeploymentSpec{
		//TODO get min/max/default from GCC
		Replicas: deploymentReplicaCount(b.gcc.Spec.Deployment, nil),
		Selector: &metav1.LabelSelector{
			MatchLabels: b.Labels(),
		},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: b.Labels(),
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
										MatchLabels: b.Labels(),
									},
									TopologyKey: "kubernetes.io/hostname",
								},
							},
						},
					},
				},
				NodeSelector:       nil,
				Tolerations:        nil,
				ServiceAccountName: b.serviceAccountName(),
			},
		},
	}, nil
}

func (b *meshGatewayBuilder) MergeDeployments(gcc *meshv2beta1.GatewayClassConfig, old, new *appsv1.Deployment) *appsv1.Deployment {
	if !compareDeployments(old, new) {
		old.Spec.Template = new.Spec.Template
		new.Spec.Replicas = deploymentReplicaCount(gcc.Spec.Deployment, old.Spec.Replicas)
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

func deploymentReplicaCount(deployment *pbmesh.Deployment, currentReplicas *int32) *int32 {
	instanceValue := globalDefaultInstances
	if currentReplicas != nil {
		return currentReplicas
	}
	return pointer.Int32(instanceValue)
}
