package gatekeeper

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (g *Gatekeeper) upsertDeployment(ctx context.Context) error {
	return nil
}

func (g *Gatekeeper) deleteDeployment(ctx context.Context) error {
	return nil
}

func (g Gatekeeper) deployment() *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      g.Gateway.Name,
			Namespace: g.Gateway.Name,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &g.HelmConfig.Replicas,
		},
	}
}
