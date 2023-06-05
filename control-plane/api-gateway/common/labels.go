// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"fmt"

	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

const (
	nameLabel      = "gateway.consul.hashicorp.com/name"
	namespaceLabel = "gateway.consul.hashicorp.com/namespace"
	createdAtLabel = "gateway.consul.hashicorp.com/created"
	managedLabel   = "gateway.consul.hashicorp.com/managed"
)

// LabelsForGateway formats the default labels that appear on objects managed by the controllers.
func LabelsForGateway(gateway *gwv1beta1.Gateway) map[string]string {
	return map[string]string{
		nameLabel:      gateway.Name,
		namespaceLabel: gateway.Namespace,
		createdAtLabel: fmt.Sprintf("%d", gateway.CreationTimestamp.Unix()),
		managedLabel:   "true",
	}
}
