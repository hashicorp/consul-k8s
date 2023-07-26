// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gatekeeper

import (
	"context"

	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"k8s.io/apimachinery/pkg/types"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

var (
	defaultServiceAnnotations = []string{
		"external-dns.alpha.kubernetes.io/hostname",
	}
)

func (g *Gatekeeper) upsertService(ctx context.Context, gateway gwv1beta1.Gateway, gcc v1alpha1.GatewayClassConfig, config common.HelmConfig) error {
	if gcc.Spec.ServiceType == nil {
		return g.deleteService(ctx, types.NamespacedName{Namespace: gateway.Namespace, Name: gateway.Name})
	}

	service := g.service(gateway, gcc)

	mutated := service.DeepCopy()
	mutator := newServiceMutator(service, mutated, gateway, g.Client.Scheme())

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

func (g *Gatekeeper) deleteService(ctx context.Context, gwName types.NamespacedName) error {
	if err := g.Client.Delete(ctx, &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: gwName.Name, Namespace: gwName.Namespace}}); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	return nil
}

func (g *Gatekeeper) service(gateway gwv1beta1.Gateway, gcc v1alpha1.GatewayClassConfig) *corev1.Service {
	ports := []corev1.ServicePort{}
	for _, listener := range gateway.Spec.Listeners {
		ports = append(ports, corev1.ServicePort{
			Name: string(listener.Name),
			// only TCP-based services are supported for now
			Protocol: corev1.ProtocolTCP,
			Port:     int32(listener.Port),
		})
	}

	// Copy annotations from the Gateway, filtered by those allowed by the GatewayClassConfig.
	allowedAnnotations := gcc.Spec.CopyAnnotations.Service
	if allowedAnnotations == nil {
		allowedAnnotations = defaultServiceAnnotations
	}
	annotations := make(map[string]string)
	for _, allowedAnnotation := range allowedAnnotations {
		if value, found := gateway.Annotations[allowedAnnotation]; found {
			annotations[allowedAnnotation] = value
		}
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        gateway.Name,
			Namespace:   gateway.Namespace,
			Labels:      common.LabelsForGateway(&gateway),
			Annotations: annotations,
		},
		Spec: corev1.ServiceSpec{
			Selector: common.LabelsForGateway(&gateway),
			Type:     *gcc.Spec.ServiceType,
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

func newServiceMutator(service, mutated *corev1.Service, gateway gwv1beta1.Gateway, scheme *runtime.Scheme) resourceMutator {
	return func() error {
		mutated = mergeService(service, mutated)
		return ctrl.SetControllerReference(&gateway, mutated, scheme)
	}
}
