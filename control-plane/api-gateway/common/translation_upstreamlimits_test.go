// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

func intPtr(i int) *int          { return &i }
func uint32Ptr(i uint32) *uint32 { return &i }

func TestTranslateGatewayDefaults(t *testing.T) {
	t.Parallel()

	translator := ResourceTranslator{}

	t.Run("no annotations returns nil", func(t *testing.T) {
		gateway := gwv1.Gateway{}
		require.Nil(t, translator.translateGatewayDefaults(gateway))
	})

	t.Run("unrelated annotations return nil", func(t *testing.T) {
		gateway := gwv1.Gateway{}
		gateway.Annotations = map[string]string{"foo": "bar"}
		require.Nil(t, translator.translateGatewayDefaults(gateway))
	})

	t.Run("all four fields set", func(t *testing.T) {
		gateway := gwv1.Gateway{}
		gateway.Annotations = map[string]string{
			annotationDefaultMaxConnections:           "50",
			annotationDefaultMaxPendingRequests:       "100",
			annotationDefaultMaxConcurrentRequests:    "200",
			annotationDefaultPHCInterval:              "10s",
			annotationDefaultPHCMaxFailures:           "5",
			annotationDefaultPHCEnforcingConsecutive5: "100",
			annotationDefaultPHCMaxEjectionPercent:    "50",
			annotationDefaultPHCBaseEjectionTime:      "30s",
		}

		limits := translator.translateGatewayDefaults(gateway)
		require.NotNil(t, limits)
		require.Equal(t, intPtr(50), limits.MaxConnections)
		require.Equal(t, intPtr(100), limits.MaxPendingRequests)
		require.Equal(t, intPtr(200), limits.MaxConcurrentRequests)

		require.NotNil(t, limits.PassiveHealthCheck)
		require.Equal(t, 10*time.Second, limits.PassiveHealthCheck.Interval)
		require.Equal(t, uint32(5), limits.PassiveHealthCheck.MaxFailures)
		require.Equal(t, uint32Ptr(100), limits.PassiveHealthCheck.EnforcingConsecutive5xx)
		require.Equal(t, uint32Ptr(50), limits.PassiveHealthCheck.MaxEjectionPercent)
		require.NotNil(t, limits.PassiveHealthCheck.BaseEjectionTime)
		require.Equal(t, 30*time.Second, *limits.PassiveHealthCheck.BaseEjectionTime)
	})

	t.Run("only numeric fields set leaves PHC nil", func(t *testing.T) {
		gateway := gwv1.Gateway{}
		gateway.Annotations = map[string]string{
			annotationDefaultMaxConnections: "5",
		}
		limits := translator.translateGatewayDefaults(gateway)
		require.NotNil(t, limits)
		require.Equal(t, intPtr(5), limits.MaxConnections)
		require.Nil(t, limits.PassiveHealthCheck)
	})

	t.Run("invalid values are ignored", func(t *testing.T) {
		gateway := gwv1.Gateway{}
		gateway.Annotations = map[string]string{
			annotationDefaultMaxConnections: "not-a-number",
		}
		require.Nil(t, translator.translateGatewayDefaults(gateway))
	})
}

func TestToConsulUpstreamLimits(t *testing.T) {
	t.Parallel()

	spec := v1alpha1.RouteUpstreamLimitsFilterSpec{
		MaxConnections:        intPtr(20),
		MaxPendingRequests:    intPtr(40),
		MaxConcurrentRequests: intPtr(80),
		PassiveHealthCheck: &v1alpha1.PassiveHealthCheck{
			Interval:                metav1.Duration{Duration: 5 * time.Second},
			MaxFailures:             3,
			EnforcingConsecutive5xx: uint32Ptr(100),
			MaxEjectionPercent:      uint32Ptr(50),
			BaseEjectionTime:        &metav1.Duration{Duration: 30 * time.Second},
		},
	}

	limits := toConsulUpstreamLimits(spec)
	require.Equal(t, intPtr(20), limits.MaxConnections)
	require.Equal(t, intPtr(40), limits.MaxPendingRequests)
	require.Equal(t, intPtr(80), limits.MaxConcurrentRequests)
	require.NotNil(t, limits.PassiveHealthCheck)
	require.Equal(t, 5*time.Second, limits.PassiveHealthCheck.Interval)
	require.Equal(t, uint32(3), limits.PassiveHealthCheck.MaxFailures)
	require.Equal(t, uint32Ptr(100), limits.PassiveHealthCheck.EnforcingConsecutive5xx)
	require.Equal(t, uint32Ptr(50), limits.PassiveHealthCheck.MaxEjectionPercent)
	require.NotNil(t, limits.PassiveHealthCheck.BaseEjectionTime)
	require.Equal(t, 30*time.Second, *limits.PassiveHealthCheck.BaseEjectionTime)
}

func TestToConsulUpstreamLimits_NoPassiveHealthCheck(t *testing.T) {
	t.Parallel()

	spec := v1alpha1.RouteUpstreamLimitsFilterSpec{MaxConnections: intPtr(1)}
	limits := toConsulUpstreamLimits(spec)
	require.Equal(t, intPtr(1), limits.MaxConnections)
	require.Nil(t, limits.PassiveHealthCheck)
}

func TestTranslateBackendRefLimits(t *testing.T) {
	t.Parallel()

	translator := ResourceTranslator{}
	namespace := "default"

	filter := &v1alpha1.RouteUpstreamLimitsFilter{
		TypeMeta:   metav1.TypeMeta{Kind: v1alpha1.RouteUpstreamLimitsFilterKind},
		ObjectMeta: metav1.ObjectMeta{Name: "payments-limits", Namespace: namespace},
		Spec: v1alpha1.RouteUpstreamLimitsFilterSpec{
			MaxConnections: intPtr(20),
			PassiveHealthCheck: &v1alpha1.PassiveHealthCheck{
				Interval:    metav1.Duration{Duration: 5 * time.Second},
				MaxFailures: 3,
			},
		},
	}

	resources := &ResourceMap{}
	resources.AddExternalFilter(filter)

	filters := []gwv1.HTTPRouteFilter{
		{
			Type: gwv1.HTTPRouteFilterExtensionRef,
			ExtensionRef: &gwv1.LocalObjectReference{
				Group: gwv1.Group(v1alpha1.ConsulHashicorpGroup),
				Kind:  v1alpha1.RouteUpstreamLimitsFilterKind,
				Name:  "payments-limits",
			},
		},
	}

	limits := translator.translateBackendRefLimits(filters, resources, namespace)
	require.NotNil(t, limits)
	require.Equal(t, intPtr(20), limits.MaxConnections)
	require.NotNil(t, limits.PassiveHealthCheck)
	require.Equal(t, uint32(3), limits.PassiveHealthCheck.MaxFailures)

	// No matching filter returns nil.
	require.Nil(t, translator.translateBackendRefLimits(nil, resources, namespace))
}
