package gatekeeper

import (
	"context"

	apigateway "github.com/hashicorp/consul-k8s/control-plane/api-gateway"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
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

	service := g.service()

	mutated := service.DeepCopy()
	mutator := func() error {
		mutated = mergeService(service, mutated)
		return ctrl.SetControllerReference(&g.Gateway, mutated, g.Client.Scheme())
	}

	result, err := controllerutil.CreateOrUpdate(ctx, g.Client, mutated, mutator)
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
	if err := g.Client.Delete(ctx, g.service()); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	return nil
}

func (g *Gatekeeper) service() *corev1.Service {
	ports := []corev1.ServicePort{}
	for _, listener := range g.Gateway.Spec.Listeners {
		ports = append(ports, corev1.ServicePort{
			Name:     string(listener.Name),
			Protocol: corev1.Protocol(listener.Protocol),
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

// mergeService is used to keep annotations and ports from the `from` Service
// to the `to` service. This prevents an infinite reconciliation loop when
// Kubernetes adds this configuration back in.
func mergeService(from, to *corev1.Service) *corev1.Service {
	if areServicesEqual(from, to) {
		return to
	}

	to.Annotations = from.Annotations
	to.Spec.Ports = from.Spec.Ports

	return to
}

func areServicesEqual(a, b *corev1.Service) bool {
	if !equality.Semantic.DeepEqual(a.Annotations, b.Annotations) {
		return false
	}
	if len(b.Spec.Ports) != len(a.Spec.Ports) {
		return false
	}

	for i, port := range a.Spec.Ports {
		otherPort := b.Spec.Ports[i]
		if port.Port != otherPort.Port {
			return false
		}
		if port.Protocol != otherPort.Protocol {
			return false
		}
	}
	return true
}
