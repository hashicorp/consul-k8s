package gatekeeper

import (
	"context"

	"github.com/go-logr/logr"
	apigateway "github.com/hashicorp/consul-k8s/control-plane/api-gateway"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

// Gatekeeper is used to manage the lifecycle of Gateway deployments and services.
type Gatekeeper struct {
	GatekeeperConfig
}

type GatekeeperConfig struct {
	Log    logr.Logger
	Client client.Client

	Gateway            gwv1beta1.Gateway
	GatewayClassConfig v1alpha1.GatewayClassConfig
	HelmConfig         apigateway.HelmConfig
}

func New(cfg GatekeeperConfig) *Gatekeeper {
	return &Gatekeeper{cfg}
}

func (g *Gatekeeper) Upsert(ctx context.Context) error {
	return nil
}

func (g *Gatekeeper) Delete(ctx context.Context) error {
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

func (g Gatekeeper) service() *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      g.Gateway.Name,
			Namespace: g.Gateway.Namespace,
		},
	}
}
