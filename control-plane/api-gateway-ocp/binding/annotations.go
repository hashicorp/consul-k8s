// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package binding

import (
	"encoding/json"
	"reflect"

	gwv1beta1 "sigs.k8s.io/gateway-api-exp/apis/v1beta1"

	"github.com/hashicorp/consul-k8s/control-plane/api-gateway-ocp/common"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
)

var log = ctrl.Log.WithName("serialize-gatewayclassconfig-custom")

func serializeGatewayClassConfig(gw *gwv1beta1.Gateway, gwcc *v1alpha1.GatewayClassConfig) (*v1alpha1.GatewayClassConfig, bool) {
	if gwcc == nil {
		return nil, false
	}

	if gw.Annotations == nil {
		gw.Annotations = make(map[string]string)
	}
	key := common.AnnotationGatewayClassConfig
	if annotatedConfig, ok := gw.Annotations[key]; ok {
		var config v1alpha1.GatewayClassConfigSpec
		if err := json.Unmarshal([]byte(annotatedConfig), &config); err == nil {
			if reflect.DeepEqual(config, gwcc.Spec) {
				return gwcc, false
			}

		}
	}

	// otherwise if we failed to unmarshal or there was no annotation, marshal it onto
	// the gateway
	marshaled, _ := json.Marshal(gwcc.Spec)
	gw.Annotations[key] = string(marshaled)
	log.Info("gwcc to be used: " + string(marshaled) + "and generation: " + string(gwcc.Generation))
	return gwcc, true
}
