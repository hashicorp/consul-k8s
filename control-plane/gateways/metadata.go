package gateways

import (
	"golang.org/x/exp/slices"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/hashicorp/consul-k8s/control-plane/api/mesh/v2beta1"
)

const labelManagedBy = "mesh.consul.hashicorp.com/managed-by"

var defaultLabels = map[string]string{labelManagedBy: "consul-k8s"}

// Annotations returns the computed set of annotations for a resource by inheriting
// from the MeshGateway's annotations and adding specified key-values from the
// GatewayClassConfig based on the Kubernetes object type.
func (b *meshGatewayBuilder) Annotations(object client.Object) map[string]string {
	if b.gcc == nil {
		return map[string]string{}
	}

	var (
		primarySource   v2beta1.GatewayClassAnnotationsLabelsConfig
		secondarySource = b.gcc.Spec.Annotations
	)

	switch object.(type) {
	case *appsv1.Deployment:
		primarySource = b.gcc.Spec.Deployment.Annotations
	case *rbacv1.Role:
		primarySource = b.gcc.Spec.Role.Annotations
	case *rbacv1.RoleBinding:
		primarySource = b.gcc.Spec.RoleBinding.Annotations
	case *corev1.Service:
		primarySource = b.gcc.Spec.Service.Annotations
	case *corev1.ServiceAccount:
		primarySource = b.gcc.Spec.ServiceAccount.Annotations
	default:
		return map[string]string{}
	}

	return computeAnnotationsOrLabels(b.gateway.Annotations, primarySource, secondarySource)
}

// Labels returns the computed set of labels for a resource by inheriting
// from the MeshGateway's labels and adding specified key-values from the
// GatewayClassConfig based on the Kubernetes object type.
func (b *meshGatewayBuilder) Labels(object client.Object) map[string]string {
	if b.gcc == nil {
		return defaultLabels
	}

	var (
		primarySource   v2beta1.GatewayClassAnnotationsLabelsConfig
		secondarySource = b.gcc.Spec.Labels
	)

	switch object.(type) {
	case *appsv1.Deployment:
		primarySource = b.gcc.Spec.Deployment.Labels
	case *rbacv1.Role:
		primarySource = b.gcc.Spec.Role.Labels
	case *rbacv1.RoleBinding:
		primarySource = b.gcc.Spec.RoleBinding.Labels
	case *corev1.Service:
		primarySource = b.gcc.Spec.Service.Labels
	case *corev1.ServiceAccount:
		primarySource = b.gcc.Spec.ServiceAccount.Labels
	default:
		return defaultLabels
	}

	labels := computeAnnotationsOrLabels(b.gateway.Labels, primarySource, secondarySource)
	for k, v := range defaultLabels {
		labels[k] = v
	}
	return labels
}

// computeAnnotationsOrLabels compiles a set of annotations or labels
// using the following priority, highest to lowest:
//  1. inherited keys specified on the primary
//  2. added key-values specified on the primary
//  3. inherited keys specified on the secondary
//  4. added key-values specified on the secondary
func computeAnnotationsOrLabels(inheritFrom map[string]string, primary, secondary v2beta1.GatewayClassAnnotationsLabelsConfig) map[string]string {
	out := map[string]string{}

	// Add key-values specified on the secondary
	for k, v := range secondary.Set {
		out[k] = v
	}

	// Inherit keys specified on the secondary
	for k, v := range inheritFrom {
		if slices.Contains(secondary.InheritFromGateway, k) {
			out[k] = v
		}
	}

	// Add key-values specified on the primary
	for k, v := range primary.Set {
		out[k] = v
	}

	// Inherit keys specified on the primary
	for k, v := range inheritFrom {
		if slices.Contains(primary.InheritFromGateway, k) {
			out[k] = v
		}
	}

	return out
}
