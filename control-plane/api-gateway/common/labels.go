// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

const (
	componentLabel = "component"
	nameLabel      = "gateway.consul.hashicorp.com/name"
	namespaceLabel = "gateway.consul.hashicorp.com/namespace"
	createdAtLabel = "gateway.consul.hashicorp.com/created"
	ManagedLabel   = "gateway.consul.hashicorp.com/managed"
)

// LabelsForGateway formats the default labels that appear on objects managed by the controllers.
func LabelsForGateway(gateway *gwv1beta1.Gateway) map[string]string {
	return map[string]string{
		componentLabel: "api-gateway",
		nameLabel:      gateway.Name,
		namespaceLabel: gateway.Namespace,
		createdAtLabel: fmt.Sprintf("%d", gateway.CreationTimestamp.Unix()),
		ManagedLabel:   "true",
	}
}

func GatewayFromPod(pod *corev1.Pod) (types.NamespacedName, bool) {
	if pod.Labels[ManagedLabel] == "true" {
		return types.NamespacedName{
			Name:      pod.Labels[nameLabel],
			Namespace: pod.Labels[namespaceLabel],
		}, true
	}
	return types.NamespacedName{}, false
}
