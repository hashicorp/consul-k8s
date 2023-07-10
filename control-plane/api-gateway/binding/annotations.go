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

	if annotatedConfig, ok := gw.Annotations[common.AnnotationGatewayClassConfig]; ok {
		var config v1alpha1.GatewayClassConfig
		if err := json.Unmarshal([]byte(annotatedConfig), &config.Spec); err == nil {
			// if we can unmarshal the gateway, return it
			return &config, false
		}
	}

	// otherwise if we failed to unmarshal or there was no annotation, marshal it onto
	// the gateway
	marshaled, _ := json.Marshal(gwcc.Spec)
	gw.Annotations[common.AnnotationGatewayClassConfig] = string(marshaled)
	return gwcc, true
}
