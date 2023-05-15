package gatekeeper

import (
	"context"

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

func (g *Gatekeeper) upsertDeployment(ctx context.Context) error {
	// Get Deployment if it exists.
	existingDeployment := &appsv1.Deployment{}
	exists := false

	err := g.Client.Get(ctx, g.namespacedName(), existingDeployment)
	if err != nil && !k8serrors.IsNotFound(err) {
		return err
	} else if k8serrors.IsNotFound(err) {
		exists = false
	} else {
		exists = true
	}

	deployment := g.deployment()

	if exists {
		g.Log.Info("Existing Gateway Deployment found.")

		// If the user has set the number of replicas, let's respect that.
		deployment.Spec.Replicas = existingDeployment.Spec.Replicas
	}

	mutated := deployment.DeepCopy()
	mutator := newDeploymentMutator(deployment, mutated, g.Gateway, g.Client.Scheme())

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

func (g *Gatekeeper) deleteDeployment(ctx context.Context) error {
	err := g.Client.Delete(ctx, g.deployment())
	if k8serrors.IsNotFound(err) {
		return nil
	}

	return err
}

func (g *Gatekeeper) deployment() *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:        g.Gateway.Name,
			Namespace:   g.Gateway.Namespace,
			Labels:      apigateway.LabelsForGateway(&g.Gateway),
			Annotations: g.HelmConfig.CopyAnnotations,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &g.HelmConfig.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: apigateway.LabelsForGateway(&g.Gateway),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: apigateway.LabelsForGateway(&g.Gateway),
					Annotations: map[string]string{
						"consul.hashicorp.com/connect-inject": "false",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Image: g.HelmConfig.Image,
						},
					},
					Affinity: &corev1.Affinity{
						PodAntiAffinity: &corev1.PodAntiAffinity{
							PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
								{
									Weight: 1,
									PodAffinityTerm: corev1.PodAffinityTerm{
										LabelSelector: &metav1.LabelSelector{
											MatchLabels: apigateway.LabelsForGateway(&g.Gateway),
										},
										TopologyKey: "kubernetes.io/hostname",
									},
								},
							},
						},
					},
					NodeSelector:       g.GatewayClassConfig.Spec.NodeSelector, // TODO should I grab this from here or Helm?
					Tolerations:        g.GatewayClassConfig.Spec.Tolerations,
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
