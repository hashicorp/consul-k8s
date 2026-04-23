// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"strings"

	"github.com/hashicorp/consul/api"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

// EffectiveTLSSDSConfig captures resolved listener SDS settings after applying
// gateway defaults and listener-level overrides.
type EffectiveTLSSDSConfig struct {
	Config     *api.GatewayTLSSDSConfig
	Configured bool
}

func ResolveListenerTLSSDSConfig(gateway gwv1beta1.Gateway, listener gwv1beta1.Listener, _ *ResourceMap) EffectiveTLSSDSConfig {
	var tls *gwv1beta1.GatewayTLSConfig
	tls = listener.TLS

	clusterName, clusterSet := stringValueFromAnnotations(gateway.Annotations, TLSSDSClusterNameAnnotationKey)
	certResource, certSet := stringValueFromAnnotations(gateway.Annotations, TLSSDSCertResourceAnnotationKey)

	if tls != nil {
		if value, ok := tls.Options[gwv1beta1.AnnotationKey(TLSSDSClusterNameAnnotationKey)]; ok {
			clusterName = strings.TrimSpace(string(value))
			clusterSet = true
		}
		if value, ok := tls.Options[gwv1beta1.AnnotationKey(TLSSDSCertResourceAnnotationKey)]; ok {
			certResource = strings.TrimSpace(string(value))
			certSet = true
		}
	}

	configured := clusterSet || certSet
	if !configured {
		return EffectiveTLSSDSConfig{}
	}

	if clusterName == "" || certResource == "" {
		return EffectiveTLSSDSConfig{Configured: true}
	}

	return EffectiveTLSSDSConfig{
		Config: &api.GatewayTLSSDSConfig{
			ClusterName:  clusterName,
			CertResource: certResource,
		},
		Configured: true,
	}
}

func ListenerUsesTLSSDS(gateway gwv1beta1.Gateway, tls *gwv1beta1.GatewayTLSConfig) bool {
	listener := gwv1beta1.Listener{TLS: tls}
	return ResolveListenerTLSSDSConfig(gateway, listener, nil).Config != nil
}

func stringValueFromAnnotations(annotations map[string]string, key string) (string, bool) {
	if annotations == nil {
		return "", false
	}
	value, ok := annotations[key]
	if !ok {
		return "", false
	}
	return strings.TrimSpace(value), true
}
