package gatekeeper

import (
	"context"

	apigateway "github.com/hashicorp/consul-k8s/control-plane/api-gateway"
	appsv1 "k8s.io/api/apps/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

func (g *Gatekeeper) upsertDeployment(ctx context.Context) error {
	var (
		deployment *appsv1.Deployment
		exists     bool
	)

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
		// TODO what do we need to do if the deployment exists?
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
			Namespace: g.Gateway.Name,
			Labels:    apigateway.LabelsForGateway(&g.Gateway),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &g.HelmConfig.Replicas,
		},
	}
}
