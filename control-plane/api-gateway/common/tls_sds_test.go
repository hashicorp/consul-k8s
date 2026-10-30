// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func TestResolveListenerTLSSDSConfig(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		gateway        gwv1.Gateway
		listener       gwv1.Listener
		resources      *ResourceMap
		expectConfig   bool
		expectResolved string
		expectResource string
		expectSet      bool
	}{
		"not configured": {
			gateway:   gwv1.Gateway{},
			listener:  gwv1.Listener{TLS: &gwv1.ListenerTLSConfig{}},
			expectSet: false,
		},
		"gateway defaults": {
			gateway: gwv1.Gateway{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
				TLSSDSClusterNameAnnotationKey:  "sds-cluster",
				TLSSDSCertResourceAnnotationKey: "default-cert",
			}}},
			listener:       gwv1.Listener{TLS: &gwv1.ListenerTLSConfig{}},
			expectConfig:   true,
			expectResolved: "sds-cluster",
			expectResource: "default-cert",
			expectSet:      true,
		},
		"listener override": {
			gateway: gwv1.Gateway{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
				TLSSDSClusterNameAnnotationKey:  "sds-cluster",
				TLSSDSCertResourceAnnotationKey: "default-cert",
			}}},
			listener: gwv1.Listener{TLS: &gwv1.ListenerTLSConfig{Options: map[gwv1.AnnotationKey]gwv1.AnnotationValue{
				gwv1.AnnotationKey(TLSSDSClusterNameAnnotationKey):  "listener-cluster",
				gwv1.AnnotationKey(TLSSDSCertResourceAnnotationKey): "listener-cert",
			}}},
			expectConfig:   true,
			expectResolved: "listener-cluster",
			expectResource: "listener-cert",
			expectSet:      true,
		},
		"partial gateway config - only cluster name": {
			gateway: gwv1.Gateway{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
				TLSSDSClusterNameAnnotationKey: "sds-cluster",
			}}},
			listener:  gwv1.Listener{TLS: &gwv1.ListenerTLSConfig{}},
			expectSet: true,
		},
		// RFC: tls.Options must have BOTH SDS keys or neither.
		// A single key in listener tls.Options is invalid outright — no gateway fallback.
		"partial listener tls options - only cluster - invalid no fallback": {
			gateway: gwv1.Gateway{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
				TLSSDSClusterNameAnnotationKey:  "gateway-cluster",
				TLSSDSCertResourceAnnotationKey: "gateway-cert",
			}}},
			listener: gwv1.Listener{TLS: &gwv1.ListenerTLSConfig{Options: map[gwv1.AnnotationKey]gwv1.AnnotationValue{
				// listener only sets cluster, not cert → must be rejected
				gwv1.AnnotationKey(TLSSDSClusterNameAnnotationKey): "listener-cluster",
			}}},
			// Configured=true, Config=nil signals incomplete/invalid SDS
			expectSet: true,
		},
		// RFC: same rule applies when only cert is set in listener tls.Options.
		"partial listener tls options - only cert - invalid no fallback": {
			gateway: gwv1.Gateway{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
				TLSSDSClusterNameAnnotationKey:  "gateway-cluster",
				TLSSDSCertResourceAnnotationKey: "gateway-cert",
			}}},
			listener: gwv1.Listener{TLS: &gwv1.ListenerTLSConfig{Options: map[gwv1.AnnotationKey]gwv1.AnnotationValue{
				gwv1.AnnotationKey(TLSSDSCertResourceAnnotationKey): "listener-cert",
			}}},
			expectSet: true,
		},
		// RFC: partial listener with no complete gateway — also invalid.
		"partial listener with incomplete gateway - configured but incomplete": {
			gateway: gwv1.Gateway{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
				TLSSDSClusterNameAnnotationKey: "gateway-cluster",
				// no cert in gateway either
			}}},
			listener: gwv1.Listener{TLS: &gwv1.ListenerTLSConfig{Options: map[gwv1.AnnotationKey]gwv1.AnnotationValue{
				gwv1.AnnotationKey(TLSSDSCertResourceAnnotationKey): "listener-cert",
			}}},
			expectSet: true,
		},
		// RFC source-precedence: both listener keys set → listener wins entirely.
		"both listener keys set - listener wins entirely": {
			gateway: gwv1.Gateway{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
				TLSSDSClusterNameAnnotationKey:  "gateway-cluster",
				TLSSDSCertResourceAnnotationKey: "gateway-cert",
			}}},
			listener: gwv1.Listener{TLS: &gwv1.ListenerTLSConfig{Options: map[gwv1.AnnotationKey]gwv1.AnnotationValue{
				gwv1.AnnotationKey(TLSSDSClusterNameAnnotationKey):  "listener-cluster",
				gwv1.AnnotationKey(TLSSDSCertResourceAnnotationKey): "listener-cert",
			}}},
			expectConfig:   true,
			expectResolved: "listener-cluster",
			expectResource: "listener-cert",
			expectSet:      true,
		},
	} {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			resolved := ResolveListenerTLSSDSConfig(tc.gateway, tc.listener, tc.resources)
			require.Equal(t, tc.expectSet, resolved.Configured)
			if tc.expectConfig {
				require.NotNil(t, resolved.Config)
				require.Equal(t, tc.expectResolved, resolved.Config.ClusterName)
				require.Equal(t, tc.expectResource, resolved.Config.CertResource)
			} else {
				require.Nil(t, resolved.Config)
			}
		})
	}
}
