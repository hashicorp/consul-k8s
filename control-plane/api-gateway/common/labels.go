// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	gwv1beta1exp "sigs.k8s.io/gateway-api-exp/apis/v1beta1"
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

func LabelsForGateway(gateway any) map[string]string {
	data := make(map[string]string)
	switch g := gateway.(type) {
	case *gwv1beta1.Gateway:
		data = map[string]string{
			componentLabel: "api-gateway",
			nameLabel:      g.Name,
			namespaceLabel: g.Namespace,
			createdAtLabel: fmt.Sprintf("%d", g.CreationTimestamp.Unix()),
			ManagedLabel:   "true",
		}
	case *gwv1beta1exp.Gateway:
		data = map[string]string{
			componentLabel: "api-gateway",
			nameLabel:      g.Name,
			namespaceLabel: g.Namespace,
			createdAtLabel: fmt.Sprintf("%d", g.CreationTimestamp.Unix()),
			ManagedLabel:   "true",
		}
	}
	return data
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
