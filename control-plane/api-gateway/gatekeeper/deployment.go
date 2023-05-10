package gatekeeper

import (
	"context"

	apigateway "github.com/hashicorp/consul-k8s/control-plane/api-gateway"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

func (g *Gatekeeper) upsertDeployment(ctx context.Context) error {
	deployment := &appsv1.Deployment{}
	exists := false

	// Get Deployment if it exists.
	{
		if err := g.Client.Get(ctx, g.namespacedName(), deployment); err != nil {
			if k8serrors.IsNotFound(err) {
				exists = false
			} else {
				return err
			}
		} else {
			exists = true
		}
	}

	if exists {
		g.Log.Info("Existing Gateway Deployment found.")

		// If the user has set the number of replicas, let's respect that.
		// TODO upsert deployment correctly
	}

	// Create the Deployment.
	deployment = g.deployment()
	if err := ctrl.SetControllerReference(&g.Gateway, deployment, g.Client.Scheme()); err != nil {
		return err
	}
	if err := g.Client.Create(ctx, deployment); err != nil {
		return err
	}

	return nil
}

func (g *Gatekeeper) deleteDeployment(ctx context.Context) error {
	if err := g.Client.Delete(ctx, g.deployment()); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	return nil
}

func (g Gatekeeper) deployment() *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      g.Gateway.Name,
			Namespace: g.Gateway.Namespace,
			Labels:    apigateway.LabelsForGateway(&g.Gateway),
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
					NodeSelector:       g.GatewayClassConfig.Spec.NodeSelector,
					Tolerations:        g.GatewayClassConfig.Spec.Tolerations,
					ServiceAccountName: g.serviceAccountName(),
				},
			},
		},
	}
}
