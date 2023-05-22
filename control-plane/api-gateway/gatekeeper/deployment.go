// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gatekeeper

import (
	"context"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"k8s.io/apimachinery/pkg/types"

	apigateway "github.com/hashicorp/consul-k8s/control-plane/api-gateway"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

func (g *Gatekeeper) upsertDeployment(ctx context.Context, gateway gwv1beta1.Gateway, gcc v1alpha1.GatewayClassConfig, config apigateway.HelmConfig) error {
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

	deployment := g.deployment(gateway, gcc, config)

	if exists {
		g.Log.Info("Existing Gateway Deployment found.")

		// If the user has set the number of replicas, let's respect that.
		deployment.Spec.Replicas = existingDeployment.Spec.Replicas
	}

	mutated := deployment.DeepCopy()
	mutator := newDeploymentMutator(deployment, mutated, gateway, g.Client.Scheme())

	result, err := controllerutil.CreateOrUpdate(ctx, g.Client, mutated, mutator)
	if err != nil {
		return err
	}

	switch result {
	case controllerutil.OperationResultCreated:
		g.Log.Info("Created Deployment")
	case controllerutil.OperationResultUpdated:
		g.Log.Info("Updated Deployment")
	case controllerutil.OperationResultNone:
		g.Log.Info("No change to deployment")
	}

	return nil
}

func (g *Gatekeeper) deleteDeployment(ctx context.Context, nsname types.NamespacedName) error {
	err := g.Client.Delete(ctx, &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: nsname.Name, Namespace: nsname.Namespace}})
	if k8serrors.IsNotFound(err) {
		return nil
	}

	return err
}

func (g *Gatekeeper) deployment(gateway gwv1beta1.Gateway, gcc v1alpha1.GatewayClassConfig, config apigateway.HelmConfig) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gateway.Name,
			Namespace: gateway.Namespace,
			Labels:    apigateway.LabelsForGateway(&gateway),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: gcc.Spec.DeploymentSpec.DefaultInstances,
			Selector: &metav1.LabelSelector{
				MatchLabels: apigateway.LabelsForGateway(&gateway),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: apigateway.LabelsForGateway(&gateway),
					Annotations: map[string]string{
						"consul.hashicorp.com/connect-inject": "false",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Image: config.Image,
							Name:  "consul-dataplane",
						},
					},
					Affinity: &corev1.Affinity{
						PodAntiAffinity: &corev1.PodAntiAffinity{
							PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
								{
									Weight: 1,
									PodAffinityTerm: corev1.PodAffinityTerm{
										LabelSelector: &metav1.LabelSelector{
											MatchLabels: apigateway.LabelsForGateway(&gateway),
										},
										TopologyKey: "kubernetes.io/hostname",
									},
								},
							},
						},
					},
					NodeSelector:       gcc.Spec.NodeSelector,
					Tolerations:        gcc.Spec.Tolerations,
					ServiceAccountName: g.serviceAccountName(),
				},
			},
		},
	}
}

func mergeDeployments(a, b *appsv1.Deployment) *appsv1.Deployment {
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

	return *b.Spec.Replicas == *a.Spec.Replicas
}

func newDeploymentMutator(deployment, mutated *appsv1.Deployment, gateway gwv1beta1.Gateway, scheme *runtime.Scheme) resourceMutator {
	return func() error {
		mutated = mergeDeployments(deployment, mutated)
		return ctrl.SetControllerReference(&gateway, mutated, scheme)
	}
}
