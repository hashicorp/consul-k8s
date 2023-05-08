package gatekeeper

import (
	"context"

	apigateway "github.com/hashicorp/consul-k8s/control-plane/api-gateway"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

var (
	defaultServiceAnnotations = []string{
		"external-dns.alpha.kubernetes.io/hostname",
	}
)

func (g *Gatekeeper) upsertService(ctx context.Context) error {
	if g.HelmConfig.ServiceType == nil {
		return nil
	}

	mutator := func() error {
		// TODO Talk with Andrew about this.
		return nil
	}

	result, err := controllerutil.CreateOrUpdate(ctx, g.Client, g.service().DeepCopy(), mutator)
	if err != nil {
		return err
	}

	switch result {
	case controllerutil.OperationResultCreated:
		g.Log.Info("Created Service")
	case controllerutil.OperationResultUpdated:
		g.Log.Info("Updated Service")
	case controllerutil.OperationResultNone:
		g.Log.Info("No change to service")
	}

	return nil
}

func (g *Gatekeeper) deleteService(ctx context.Context) error {
	return nil
}

func (g Gatekeeper) service() *corev1.Service {
	ports := []corev1.ServicePort{}
	for _, listener := range g.Gateway.Spec.Listeners {
		ports = append(ports, corev1.ServicePort{
			Name:     string(listener.Name),
			Protocol: "TCP",
			Port:     int32(listener.Port),
		})
	}

	// Copy annotations from the Gateway, filtered by those allowed by the GatewayClassConfig.
	allowedAnnotations := g.GatewayClassConfig.Spec.CopyAnnotations.Service
	if allowedAnnotations == nil {
		allowedAnnotations = defaultServiceAnnotations
	}
	annotations := make(map[string]string)
	for _, allowedAnnotation := range allowedAnnotations {
		if value, found := g.Gateway.Annotations[allowedAnnotation]; found {
			annotations[allowedAnnotation] = value
		}
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        g.Gateway.Name,
			Namespace:   g.Gateway.Namespace,
			Labels:      apigateway.LabelsForGateway(&g.Gateway),
			Annotations: annotations,
		},
		Spec: corev1.ServiceSpec{
			Selector: apigateway.LabelsForGateway(&g.Gateway),
			Type:     *g.GatewayClassConfig.Spec.ServiceType,
			Ports:    ports,
		},
	}
}
