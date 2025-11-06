// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package binding

import (
	"encoding/json"

	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

func serializeGatewayClassConfig(gw *gwv1beta1.Gateway, gwcc *v1alpha1.GatewayClassConfig) (*v1alpha1.GatewayClassConfig, bool) {
	if gwcc == nil {
		return nil, false
	}

	if gw.Annotations == nil {
		gw.Annotations = make(map[string]string)
	}

	// Always marshal the current GCC spec and update the annotation
	// This ensures the annotation reflects the latest GCC from the cluster
	marshaled, _ := json.Marshal(gwcc.Spec)
	currentAnnotation := string(marshaled)

	// Check if the annotation needs updating
	existingAnnotation, exists := gw.Annotations[common.AnnotationGatewayClassConfig]
	needsUpdate := !exists || existingAnnotation != currentAnnotation

	if needsUpdate {
		gw.Annotations[common.AnnotationGatewayClassConfig] = currentAnnotation
	}

	return gwcc, needsUpdate
}
