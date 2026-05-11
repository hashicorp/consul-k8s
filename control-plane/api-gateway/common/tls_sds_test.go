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
		"partial config": {
			gateway: gwv1.Gateway{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
				TLSSDSClusterNameAnnotationKey: "sds-cluster",
			}}},
			listener:  gwv1.Listener{TLS: &gwv1.ListenerTLSConfig{}},
			expectSet: true,
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
