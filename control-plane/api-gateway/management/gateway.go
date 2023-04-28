package management

import (
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

type Gateway struct {
	DefaultConfig
	v1alpha1.GatewayClassConfig
	gatewayConfig gwv1beta1.Gateway
}

func (g *Gateway) WithDefaultConfig(cfg DefaultConfig) *Gateway {
	g.DefaultConfig = cfg
	return g
}

func (g *Gateway) WithGatewayClassConfig(cfg v1alpha1.GatewayClassConfig) *Gateway {
	g.GatewayClassConfig = cfg
	return g
}

func (g *Gateway) WithGatewayConfig(cfg gwv1beta1.Gateway) *Gateway {
	g.gatewayConfig = cfg
	return g
}

func (g Gateway) Deployment() *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      g.gatewayConfig.Name,
			Namespace: g.gatewayConfig.Name,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &g.Replicas,
		},
	}
}

func (g Gateway) Service() *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      g.gatewayConfig.Name,
			Namespace: g.gatewayConfig.Namespace,
		},
	}
}
