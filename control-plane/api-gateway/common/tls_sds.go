// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"strings"

	"github.com/hashicorp/consul/api"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// EffectiveTLSSDSConfig captures resolved listener SDS settings after applying
// gateway defaults and listener-level overrides.
type EffectiveTLSSDSConfig struct {
	Config     *api.GatewayTLSSDSConfig
	Configured bool
}

// ResolveListenerTLSSDSConfig resolves the effective SDS TLS configuration for a
// listener using source-precedence as described in the RFC:
//
//  1. If both SDS keys are set in the listener's tls.Options, those listener
//     values are used exclusively (no mixing with gateway-level values).
//  2. Otherwise the gateway annotations are used as the source (both values
//     come from the gateway).
//  3. If no source provides any SDS key, SDS is unset.
//
// Partial configurations (where only one of the two required keys is present in
// the selected source) are returned as Configured=true, Config=nil to signal
// that SDS was requested but is incomplete.
func ResolveListenerTLSSDSConfig(gateway gwv1.Gateway, listener gwv1.Listener, _ *ResourceMap) EffectiveTLSSDSConfig {
	tls := listener.TLS

	// Step 1: check whether the listener tls.Options has BOTH SDS keys.
	if tls != nil {
		listenerCluster, listenerClusterSet := listenerOptionValue(tls, TLSSDSClusterNameAnnotationKey)
		listenerCert, listenerCertSet := listenerOptionValue(tls, TLSSDSCertResourceAnnotationKey)

		if listenerClusterSet && listenerCertSet {
			// Both keys present in the listener → use listener values exclusively.
			if listenerCluster == "" || listenerCert == "" {
				// One of the values is empty; treat as incomplete.
				return EffectiveTLSSDSConfig{Configured: true}
			}
			return EffectiveTLSSDSConfig{
				Config: &api.GatewayTLSSDSConfig{
					ClusterName:  listenerCluster,
					CertResource: listenerCert,
				},
				Configured: true,
			}
		}

		// Only one listener key is set (partial listener SDS).  Per the RFC,
		// tls.Options must supply BOTH keys or neither.  A partial set is always
		// invalid — we do NOT fall back to gateway annotations because mixing
		// values from two different sources would produce an undefined SDS pair.
		if listenerClusterSet || listenerCertSet {
			return EffectiveTLSSDSConfig{Configured: true}
		}
	}

	// Step 2: listener has no SDS keys at all — use gateway annotations.
	gwCluster, gwClusterSet := stringValueFromAnnotations(gateway.Annotations, TLSSDSClusterNameAnnotationKey)
	gwCert, gwCertSet := stringValueFromAnnotations(gateway.Annotations, TLSSDSCertResourceAnnotationKey)

	configured := gwClusterSet || gwCertSet
	if !configured {
		return EffectiveTLSSDSConfig{}
	}

	if gwCluster == "" || gwCert == "" {
		return EffectiveTLSSDSConfig{Configured: true}
	}

	return EffectiveTLSSDSConfig{
		Config: &api.GatewayTLSSDSConfig{
			ClusterName:  gwCluster,
			CertResource: gwCert,
		},
		Configured: true,
	}
}

// listenerOptionValue looks up a single annotation-style key inside a
// ListenerTLSConfig's Options map and returns the trimmed value plus a boolean
// indicating whether the key was present.
func listenerOptionValue(tls *gwv1.ListenerTLSConfig, annotationKey string) (string, bool) {
	if tls == nil {
		return "", false
	}
	value, ok := tls.Options[gwv1.AnnotationKey(annotationKey)]
	if !ok {
		return "", false
	}
	return strings.TrimSpace(string(value)), true
}

func ListenerUsesTLSSDS(gateway gwv1.Gateway, tls *gwv1.ListenerTLSConfig) bool {
	listener := gwv1.Listener{TLS: tls}
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
