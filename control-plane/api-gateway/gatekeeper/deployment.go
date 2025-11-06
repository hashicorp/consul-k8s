// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gatekeeper

import (
	"context"
	"strconv"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
)

const (
	defaultInstances int32 = 1
)

func (g *Gatekeeper) upsertDeployment(ctx context.Context, gateway gwv1beta1.Gateway, gcc v1alpha1.GatewayClassConfig, config common.HelmConfig) error {
	// Get Deployment if it exists.
	existingDeployment := &appsv1.Deployment{}
	exists := false

	err := g.Client.Get(ctx, g.namespacedName(gateway), existingDeployment)
	if err != nil && !k8serrors.IsNotFound(err) {
		return err
	} else if k8serrors.IsNotFound(err) {
		exists = false
	} else {
		exists = true
	}

	var currentReplicas *int32
	if exists {
		currentReplicas = existingDeployment.Spec.Replicas
	}

	deployment, err := g.deployment(gateway, gcc, config, currentReplicas)
	if err != nil {
		return err
	}

	if exists {
		g.Log.V(1).Info("Existing Gateway Deployment found.")

		// If the user has set the number of replicas, let's respect that.
		deployment.Spec.Replicas = existingDeployment.Spec.Replicas
	}

	mutated := deployment.DeepCopy()
	mutator := newDeploymentMutator(deployment, mutated, existingDeployment, exists, gcc, gateway, g.Client.Scheme(), g.Log)

	result, err := controllerutil.CreateOrUpdate(ctx, g.Client, mutated, mutator)
	if err != nil {
		return err
	}

	switch result {
	case controllerutil.OperationResultCreated:
		g.Log.V(1).Info("Created Deployment")
	case controllerutil.OperationResultUpdated:
		g.Log.V(1).Info("Updated Deployment")
	case controllerutil.OperationResultNone:
		g.Log.V(1).Info("No change to deployment")
	}

	return nil
}

func (g *Gatekeeper) deleteDeployment(ctx context.Context, gwName types.NamespacedName) error {
	err := g.Client.Delete(ctx, &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: gwName.Name, Namespace: gwName.Namespace}})
	if k8serrors.IsNotFound(err) {
		return nil
	}

	return err
}

func (g *Gatekeeper) deployment(gateway gwv1beta1.Gateway, gcc v1alpha1.GatewayClassConfig, config common.HelmConfig, currentReplicas *int32) (*appsv1.Deployment, error) {
	initContainer, err := g.initContainer(config, gateway.Name, gateway.Namespace)
	if err != nil {
		return nil, err
	}

	annotations := map[string]string{
		"consul.hashicorp.com/connect-inject":        "false",
		constants.AnnotationGatewayConsulServiceName: gateway.Name,
		constants.AnnotationGatewayKind:              "api-gateway",
	}

	metrics := common.GatewayMetricsConfig(gateway, gcc, config)

	if metrics.Enabled {
		annotations[constants.AnnotationPrometheusScrape] = "true"
		annotations[constants.AnnotationPrometheusPath] = metrics.Path
		annotations[constants.AnnotationPrometheusPort] = strconv.Itoa(metrics.Port)
	}

	volumes, mounts := volumesAndMounts(gateway)

	container, err := consulDataplaneContainer(metrics, config, gcc, gateway, mounts)
	if err != nil {
		return nil, err
	}

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gateway.Name,
			Namespace: gateway.Namespace,
			Labels:    common.LabelsForGateway(&gateway),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: deploymentReplicas(gcc, currentReplicas),
			Selector: &metav1.LabelSelector{
				MatchLabels: common.LabelsForGateway(&gateway),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      common.LabelsForGateway(&gateway),
					Annotations: annotations,
				},
				Spec: corev1.PodSpec{
					Volumes: volumes,
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
		},
	}, nil
}

func mergeDeployments(log logr.Logger, gcc v1alpha1.GatewayClassConfig, a, b *appsv1.Deployment) *appsv1.Deployment {
	if !compareDeployments(a, b) {
		// Replace template
		b.Spec.Template = a.Spec.Template
		b.Spec.Replicas = deploymentReplicas(gcc, a.Spec.Replicas)
	}

	// Always apply probes from GatewayClassConfig to ensure they stay in sync
	if gcc.Spec.Probes != nil {
		for i, c := range b.Spec.Template.Spec.Containers {
			if i > 0 { // only primary container gets managed probes
				continue
			}
			if gcc.Spec.Probes.Liveness != nil {
				c.LivenessProbe = gcc.Spec.Probes.Liveness.DeepCopy()
			}
			if gcc.Spec.Probes.Readiness != nil {
				c.ReadinessProbe = gcc.Spec.Probes.Readiness.DeepCopy()
			}
			if gcc.Spec.Probes.Startup != nil {
				c.StartupProbe = gcc.Spec.Probes.Startup.DeepCopy()
			}
			b.Spec.Template.Spec.Containers[i] = c
		}
	}

	return b
}

// compareDeployments determines whether two Deployments are equal for all
// of the fields that we care about. There are some differences between a
// Deployment returned by the Kubernetes API and one that we would create
// in memory which are perfectly fine. We want to ignore those differences.
func compareDeployments(a, b *appsv1.Deployment) bool {
	if len(b.Spec.Template.Spec.InitContainers) != len(a.Spec.Template.Spec.InitContainers) {
		return false
	}

	for i, containerA := range a.Spec.Template.Spec.InitContainers {
		containerB := b.Spec.Template.Spec.InitContainers[i]
		if !cmp.Equal(containerA.Resources.Limits, containerB.Resources.Limits) {
			return false
		}

		if !cmp.Equal(containerA.Resources.Requests, containerB.Resources.Requests) {
			return false
		}
	}

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

		// Compare probe initialDelaySeconds for rollout restart functionality
		otherContainer := b.Spec.Template.Spec.Containers[i]

		// Compare readiness probe initialDelaySeconds
		if container.ReadinessProbe != nil && otherContainer.ReadinessProbe != nil {
			if container.ReadinessProbe.InitialDelaySeconds != otherContainer.ReadinessProbe.InitialDelaySeconds {
				return false
			}
		} else if (container.ReadinessProbe == nil) != (otherContainer.ReadinessProbe == nil) {
			return false
		}

		// Compare startup probe initialDelaySeconds
		if container.StartupProbe != nil && otherContainer.StartupProbe != nil {
			if container.StartupProbe.InitialDelaySeconds != otherContainer.StartupProbe.InitialDelaySeconds {
				return false
			}
		} else if (container.StartupProbe == nil) != (otherContainer.StartupProbe == nil) {
			return false
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

func mergeAnnotation(b *appsv1.Deployment, annotations map[string]string) {
	if b.Spec.Template.Annotations == nil {
		b.Spec.Template.Annotations = make(map[string]string)
	}
	for k, v := range annotations {
		if _, ok := b.Spec.Template.Annotations[k]; !ok {
			b.Spec.Template.Annotations[k] = v
		}
	}

}

func newDeploymentMutator(deployment, mutated, existingDeployment *appsv1.Deployment, deploymentExists bool, gcc v1alpha1.GatewayClassConfig, gateway gwv1beta1.Gateway, scheme *runtime.Scheme, log logr.Logger) resourceMutator {
	return func() error {
		mutated = mergeDeployments(log, gcc, deployment, mutated)
		if deploymentExists {
			mergeAnnotation(mutated, existingDeployment.Spec.Template.Annotations)
		}
		return ctrl.SetControllerReference(&gateway, mutated, scheme)
	}
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
