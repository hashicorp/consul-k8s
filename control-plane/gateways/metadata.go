// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gateways

import (
	"golang.org/x/exp/slices"

	"github.com/hashicorp/consul-k8s/control-plane/api/mesh/v2beta1"
)

const labelManagedBy = "mesh.consul.hashicorp.com/managed-by"

var defaultLabels = map[string]string{labelManagedBy: "consul-k8s"}

func (b *meshGatewayBuilder) annotationsForDeployment() map[string]string {
	if b.gcc == nil {
		return map[string]string{}
	}
	return computeAnnotationsOrLabels(b.gateway.Annotations, b.gcc.Spec.Deployment.Annotations, b.gcc.Spec.Annotations)
}

func (b *meshGatewayBuilder) annotationsForRole() map[string]string {
	if b.gcc == nil {
		return map[string]string{}
	}
	return computeAnnotationsOrLabels(b.gateway.Annotations, b.gcc.Spec.Role.Annotations, b.gcc.Spec.Annotations)
}

func (b *meshGatewayBuilder) annotationsForRoleBinding() map[string]string {
	if b.gcc == nil {
		return map[string]string{}
	}
	return computeAnnotationsOrLabels(b.gateway.Annotations, b.gcc.Spec.RoleBinding.Annotations, b.gcc.Spec.Annotations)
}

func (b *meshGatewayBuilder) annotationsForService() map[string]string {
	if b.gcc == nil {
		return map[string]string{}
	}
	return computeAnnotationsOrLabels(b.gateway.Annotations, b.gcc.Spec.Service.Annotations, b.gcc.Spec.Annotations)
}

func (b *meshGatewayBuilder) annotationsForServiceAccount() map[string]string {
	if b.gcc == nil {
		return map[string]string{}
	}
	return computeAnnotationsOrLabels(b.gateway.Annotations, b.gcc.Spec.ServiceAccount.Annotations, b.gcc.Spec.Annotations)
}

func (b *meshGatewayBuilder) labelsForDeployment() map[string]string {
	if b.gcc == nil {
		return defaultLabels
	}

	labels := computeAnnotationsOrLabels(b.gateway.Labels, b.gcc.Spec.Deployment.Labels, b.gcc.Spec.Labels)
	for k, v := range defaultLabels {
		labels[k] = v
	}
	return labels
}

func (b *meshGatewayBuilder) logLevelForDataplaneContainer() string {
	if b.config.LogLevel != "" {
		return b.config.LogLevel
	}

	if b.gcc == nil || b.gcc.Spec.Deployment.Container == nil {
		return ""
	}

	return b.gcc.Spec.Deployment.Container.Consul.Logging.Level
}

func (b *meshGatewayBuilder) logLevelForInitContainer() string {
	if b.config.LogLevel != "" {
		return b.config.LogLevel
	}

	if b.gcc == nil || b.gcc.Spec.Deployment.InitContainer == nil {
		return ""
	}

	return b.gcc.Spec.Deployment.InitContainer.Consul.Logging.Level
}

func (b *meshGatewayBuilder) labelsForRole() map[string]string {
	if b.gcc == nil {
		return defaultLabels
	}

	labels := computeAnnotationsOrLabels(b.gateway.Labels, b.gcc.Spec.Role.Labels, b.gcc.Spec.Labels)
	for k, v := range defaultLabels {
		labels[k] = v
	}
	return labels
}

func (b *meshGatewayBuilder) labelsForRoleBinding() map[string]string {
	if b.gcc == nil {
		return defaultLabels
	}

	labels := computeAnnotationsOrLabels(b.gateway.Labels, b.gcc.Spec.RoleBinding.Labels, b.gcc.Spec.Labels)
	for k, v := range defaultLabels {
		labels[k] = v
	}
	return labels
}

func (b *meshGatewayBuilder) labelsForService() map[string]string {
	if b.gcc == nil {
		return defaultLabels
	}

	labels := computeAnnotationsOrLabels(b.gateway.Labels, b.gcc.Spec.Service.Labels, b.gcc.Spec.Labels)
	for k, v := range defaultLabels {
		labels[k] = v
	}
	return labels
}

func (b *meshGatewayBuilder) labelsForServiceAccount() map[string]string {
	if b.gcc == nil {
		return defaultLabels
	}

	labels := computeAnnotationsOrLabels(b.gateway.Labels, b.gcc.Spec.ServiceAccount.Labels, b.gcc.Spec.Labels)
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
