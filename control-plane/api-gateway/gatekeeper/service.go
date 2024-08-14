// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gatekeeper

import (
	"context"
	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"k8s.io/apimachinery/pkg/types"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
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

	desiredService := g.service(gateway, gcc)

	existingService := desiredService.DeepCopy()
	mutator := newServiceMutator(existingService, desiredService, gateway, g.Client.Scheme())

	result, err := controllerutil.CreateOrUpdate(ctx, g.Client, existingService, mutator)
	if err != nil {
		return err
	}

	switch result {
	case controllerutil.OperationResultCreated:
		g.Log.V(1).Info("Created Service")
	case controllerutil.OperationResultUpdated:
		g.Log.V(1).Info("Updated Service")
	case controllerutil.OperationResultNone:
		g.Log.V(1).Info("No change to service")
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
	seenPorts := map[gwv1beta1.PortNumber]struct{}{}
	ports := []corev1.ServicePort{}
	for _, listener := range gateway.Spec.Listeners {
		if _, seen := seenPorts[listener.Port]; seen {
			// We've already added this listener's port to the Service
			continue
		}

		ports = append(ports, corev1.ServicePort{
			Name: string(listener.Name),
			// only TCP-based services are supported for now
			Protocol:   corev1.ProtocolTCP,
			Port:       int32(listener.Port),
			TargetPort: intstr.FromInt(common.ToContainerPort(listener.Port, gcc.Spec.MapPrivilegedContainerPorts)),
		})

		seenPorts[listener.Port] = struct{}{}
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

// mergeService is used to keep annotations and ports from the `existing` Service
// to the `desired` service. This prevents an infinite reconciliation loop when
// Kubernetes adds this configuration back in.
func mergeServiceInto(existing, desired *corev1.Service) {
	duplicate := existing.DeepCopy()

	// Reset the existing object in kubernetes to have the same base spec as
	// our generated service.
	existing.Spec = desired.Spec

	// For NodePort services, kubernetes will internally set the ports[*].NodePort
	// we don't want to override that, so reset it to what exists in the store.
	if hasEqualPorts(duplicate, desired) {
		existing.Spec.Ports = duplicate.Spec.Ports
	}

	// If the Service already exists, add any desired annotations + labels to existing set

	// Note: the annotations could be empty if an external controller decided to remove them all
	// do not want to panic in that case.
	if existing.ObjectMeta.Annotations == nil {
		existing.Annotations = desired.Annotations
	} else {
		for k, v := range desired.ObjectMeta.Annotations {
			existing.ObjectMeta.Annotations[k] = v
		}
	}

	// Note: the labels could be empty if an external controller decided to remove them all
	// do not want to panic in that case.
	if existing.ObjectMeta.Labels == nil {
		existing.Labels = desired.Labels
	} else {
		for k, v := range desired.ObjectMeta.Labels {
			existing.ObjectMeta.Labels[k] = v
		}
	}
}

// hasEqualPorts does a fuzzy comparison of the ports on a service spec
// ignoring any fields set internally by Kubernetes.
func hasEqualPorts(a, b *corev1.Service) bool {
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

func newServiceMutator(existing, desired *corev1.Service, gateway gwv1beta1.Gateway, scheme *runtime.Scheme) resourceMutator {
	return func() error {
		mergeServiceInto(existing, desired)
		return ctrl.SetControllerReference(&gateway, existing, scheme)
	}
}
