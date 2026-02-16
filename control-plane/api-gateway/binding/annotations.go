// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package binding

import (
	"encoding/json"
	"fmt"

	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
)

type gatewayClassConfigSnapshot struct {
	Config     v1alpha1.GatewayClassConfig `json:"config"`
	Generation int64                       `json:"generation"`
}

var log = ctrl.Log.WithName("serialize-gatewayclassconfig")

func serializeGatewayClassConfig(gw *gwv1beta1.Gateway, gwcc *v1alpha1.GatewayClassConfig) (*v1alpha1.GatewayClassConfig, bool) {
	// If the gateway class config is nil, we can't serialize it, so return early.
	if gwcc == nil {
		return nil, false
	}

	if gw.Annotations == nil {
		gw.Annotations = make(map[string]string)
	}
	log = log.WithValues("gatewayClassConfigSerialize", gwcc.Name)

	if annotatedConfig, ok := gw.Annotations[common.AnnotationGatewayClassConfig]; ok {

		var gwccs gatewayClassConfigSnapshot
		if err := json.Unmarshal([]byte(annotatedConfig), &gwccs); err == nil {
			log.Info("gatewayClassSnapshot: " + fmt.Sprintf("%+v", gwccs))
			// if we can unmarshal the gateway, check if the generation matches
			if gwccs.Generation == gwcc.Generation {
				// if the generation matches, return the annotated config
				return &gwccs.Config, false
			}
		}

		// var config v1alpha1.GatewayClassConfig
		// if err := json.Unmarshal([]byte(annotatedConfig), &config.Spec); err == nil {
		// 	// if we can unmarshal the gateway, return it
		// 	return &config, false
		// }
	}

	// update the snapshot annotation with the new config and generation.
	// If we failed to unmarshal or there was no annotation, this will overwrite the annotation with the new config and generation.
	// If we successfully unmarshaled but the generation was different, this will update the annotation with the new config and generation.

	snapshot := gatewayClassConfigSnapshot{
		Config:     *gwcc,
		Generation: gwcc.Generation,
	}
	marshaledSnapshot, err := json.Marshal(snapshot)
	if err != nil {
		// if we failed to marshal the snapshot, fallback to marshaling the config without the generation
		marshaled, _ := json.Marshal(gwcc.Spec)
		gw.Annotations[common.AnnotationGatewayClassConfig] = string(marshaled)
		log.Info("fallback marshal: " + fmt.Sprintf("%+v", marshaled))
		return gwcc, false
	}
	log.Info("marshaledSnapshot: " + string(marshaledSnapshot))
	gw.Annotations[common.AnnotationGatewayClassConfig] = string(marshaledSnapshot)

	return gwcc, true
}
