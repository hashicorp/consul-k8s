package gatekeeper

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (g *Gatekeeper) upsertService(ctx context.Context) error {
	return nil
}

func (g Gatekeeper) service() *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      g.Gateway.Name,
			Namespace: g.Gateway.Namespace,
		},
	}
}
