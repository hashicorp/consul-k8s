// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

func TestResolveListenerTLSSDSConfig(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		gateway        gwv1beta1.Gateway
		listenerTLS    *gwv1beta1.GatewayTLSConfig
		expectConfig   bool
		expectResolved string
		expectResource string
		expectSet      bool
	}{
		"not configured": {
			gateway:     gwv1beta1.Gateway{},
			listenerTLS: &gwv1beta1.GatewayTLSConfig{},
			expectSet:   false,
		},
		"gateway defaults": {
			gateway: gwv1beta1.Gateway{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
				TLSSDSClusterNameAnnotationKey:  "sds-cluster",
				TLSSDSCertResourceAnnotationKey: "default-cert",
			}}},
			listenerTLS:    &gwv1beta1.GatewayTLSConfig{},
			expectConfig:   true,
			expectResolved: "sds-cluster",
			expectResource: "default-cert",
			expectSet:      true,
		},
		"listener override": {
			gateway: gwv1beta1.Gateway{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
				TLSSDSClusterNameAnnotationKey:  "sds-cluster",
				TLSSDSCertResourceAnnotationKey: "default-cert",
			}}},
			listenerTLS: &gwv1beta1.GatewayTLSConfig{Options: map[gwv1beta1.AnnotationKey]gwv1beta1.AnnotationValue{
				gwv1beta1.AnnotationKey(TLSSDSClusterNameAnnotationKey):  "listener-cluster",
				gwv1beta1.AnnotationKey(TLSSDSCertResourceAnnotationKey): "listener-cert",
			}},
			expectConfig:   true,
			expectResolved: "listener-cluster",
			expectResource: "listener-cert",
			expectSet:      true,
		},
		"partial config": {
			gateway: gwv1beta1.Gateway{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
				TLSSDSClusterNameAnnotationKey: "sds-cluster",
			}}},
			listenerTLS: &gwv1beta1.GatewayTLSConfig{},
			expectSet:   true,
		},
	} {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			resolved := ResolveListenerTLSSDSConfig(tc.gateway, tc.listenerTLS)
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
