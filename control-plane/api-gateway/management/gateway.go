package management

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

type Gateway struct {
	defaultCfg    DefaultConfig
	gatewayClass  gwv1beta1.GatewayClass
	gatewayConfig gwv1beta1.Gateway
}

func NewGateway(defaultCfg DefaultConfig, gatewayClassCfg gwv1beta1.GatewayClass, gatewayCfg gwv1beta1.Gateway) *Gateway {
	return &Gateway{
		defaultCfg:    defaultCfg,
		gatewayClass:  gatewayClassCfg,
		gatewayConfig: gatewayCfg,
	}
}

func (g Gateway) Deployment() *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      g.gatewayConfig.Name,
			Namespace: g.gatewayConfig.Name,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &g.defaultCfg.Replicas,
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
