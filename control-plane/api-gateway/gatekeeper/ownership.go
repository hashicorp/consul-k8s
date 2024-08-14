// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gatekeeper

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

func isOwnedByGateway(o client.Object, gateway gwv1beta1.Gateway) bool {
	for _, ref := range o.GetOwnerReferences() {
		if ref.UID == gateway.GetUID() && ref.Name == gateway.GetName() {
			// We found our gateway!
			return true
		}
	}
	return false
}
