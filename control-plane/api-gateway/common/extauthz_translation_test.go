// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hashicorp/consul/api"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

func TestGatewayExtAuthz(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		annotations map[string]string
		expected    *api.APIGatewayExtAuthz
	}{
		"absent annotation defaults to nil (enabled)": {
			annotations: nil,
			expected:    nil,
		},
		"unrecognized value is treated as nil": {
			annotations: map[string]string{AnnotationExtAuthz: "bogus"},
			expected:    nil,
		},
		"disabled value": {
			annotations: map[string]string{AnnotationExtAuthz: ExtAuthzDisabledValue},
			expected:    &api.APIGatewayExtAuthz{Enabled: false},
		},
		"enabled value": {
			annotations: map[string]string{AnnotationExtAuthz: ExtAuthzEnabledValue},
			expected:    &api.APIGatewayExtAuthz{Enabled: true},
		},
	}

	for name, tc := range cases {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.expected, gatewayExtAuthz(tc.annotations))
		})
	}
}

func TestRouteExtAuthzFromAnnotations(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		annotations map[string]string
		expected    *api.HTTPRouteExtAuthzFilter
	}{
		"absent annotation defaults to nil": {
			annotations: nil,
			expected:    nil,
		},
		"unrecognized value is treated as nil": {
			annotations: map[string]string{AnnotationExtAuthz: "bogus"},
			expected:    nil,
		},
		"enabled value": {
			annotations: map[string]string{AnnotationExtAuthz: ExtAuthzEnabledValue},
			expected:    &api.HTTPRouteExtAuthzFilter{Enabled: true},
		},
		"disabled value": {
			annotations: map[string]string{AnnotationExtAuthz: ExtAuthzDisabledValue},
			expected:    &api.HTTPRouteExtAuthzFilter{Enabled: false},
		},
	}

	for name, tc := range cases {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.expected, routeExtAuthzFromAnnotations(tc.annotations))
		})
	}
}

func TestTranslateRouteExtAuthzFilter(t *testing.T) {
	t.Parallel()

	tr := ResourceTranslator{}

	cases := map[string]struct {
		filter   *v1alpha1.RouteAuthFilter
		expected *api.HTTPRouteExtAuthzFilter
	}{
		"no ext_authz configured": {
			filter:   &v1alpha1.RouteAuthFilter{},
			expected: nil,
		},
		"ext_authz enabled": {
			filter: &v1alpha1.RouteAuthFilter{
				Spec: v1alpha1.RouteAuthFilterSpec{
					ExtAuthz: &v1alpha1.RouteAuthFilterExtAuthz{Enabled: true},
				},
			},
			expected: &api.HTTPRouteExtAuthzFilter{Enabled: true},
		},
		"ext_authz disabled": {
			filter: &v1alpha1.RouteAuthFilter{
				Spec: v1alpha1.RouteAuthFilterSpec{
					ExtAuthz: &v1alpha1.RouteAuthFilterExtAuthz{Enabled: false},
				},
			},
			expected: &api.HTTPRouteExtAuthzFilter{Enabled: false},
		},
	}

	for name, tc := range cases {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.expected, tr.translateRouteExtAuthzFilter(tc.filter))
		})
	}
}
