// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package controllers

import (
	"context"
	"testing"

	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gwv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

func TestTransformHTTPRoute(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct {
		route    *gwv1beta1.HTTPRoute
		expected []reconcile.Request
	}{
		"route with parent empty namespace": {
			route: &gwv1beta1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
				},
				Spec: gwv1beta1.HTTPRouteSpec{
					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
						ParentRefs: []gwv1beta1.ParentReference{
							{Name: "gateway"},
						},
					},
				},
			},
			expected: []reconcile.Request{
				{NamespacedName: types.NamespacedName{Name: "gateway", Namespace: "default"}},
			},
		},
		"route with parent with namespace": {
			route: &gwv1beta1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
				},
				Spec: gwv1beta1.HTTPRouteSpec{
					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
						ParentRefs: []gwv1beta1.ParentReference{
							{Name: "gateway", Namespace: common.PointerTo(gwv1beta1.Namespace("other"))},
						},
					},
				},
			},
			expected: []reconcile.Request{
				{NamespacedName: types.NamespacedName{Name: "gateway", Namespace: "other"}},
			},
		},
		"route with non gateway parent with namespace": {
			route: &gwv1beta1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
				},
				Spec: gwv1beta1.HTTPRouteSpec{
					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
						ParentRefs: []gwv1beta1.ParentReference{
							{Name: "gateway", Group: common.PointerTo(gwv1beta1.Group("group"))},
						},
					},
				},
			},
			expected: []reconcile.Request{},
		},
		"route with parent in status and no namespace": {
			route: &gwv1beta1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
				},
				Status: gwv1beta1.HTTPRouteStatus{
					RouteStatus: gwv1beta1.RouteStatus{
						Parents: []gwv1beta1.RouteParentStatus{
							{ParentRef: gwv1beta1.ParentReference{Name: "gateway"}},
						},
					},
				},
			},
			expected: []reconcile.Request{
				{NamespacedName: types.NamespacedName{Name: "gateway", Namespace: "default"}},
			},
		},
		"route with parent in status and namespace": {
			route: &gwv1beta1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
				},
				Status: gwv1beta1.HTTPRouteStatus{
					RouteStatus: gwv1beta1.RouteStatus{
						Parents: []gwv1beta1.RouteParentStatus{
							{ParentRef: gwv1beta1.ParentReference{Name: "gateway", Namespace: common.PointerTo(gwv1beta1.Namespace("other"))}},
						},
					},
				},
			},
			expected: []reconcile.Request{
				{NamespacedName: types.NamespacedName{Name: "gateway", Namespace: "other"}},
			},
		},
		"route with non gateway parent in status": {
			route: &gwv1beta1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
				},
				Status: gwv1beta1.HTTPRouteStatus{
					RouteStatus: gwv1beta1.RouteStatus{
						Parents: []gwv1beta1.RouteParentStatus{
							{ParentRef: gwv1beta1.ParentReference{Name: "gateway", Group: common.PointerTo(gwv1beta1.Group("group"))}},
						},
					},
				},
			},
			expected: []reconcile.Request{},
		},
		"route parent in spec and in status": {
			route: &gwv1beta1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
				},
				Spec: gwv1beta1.HTTPRouteSpec{
					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
						ParentRefs: []gwv1beta1.ParentReference{
							{Name: "gateway-one"},
						},
					},
				},
				Status: gwv1beta1.HTTPRouteStatus{
					RouteStatus: gwv1beta1.RouteStatus{
						Parents: []gwv1beta1.RouteParentStatus{
							{ParentRef: gwv1beta1.ParentReference{Name: "gateway-two"}},
						},
					},
				},
			},
			expected: []reconcile.Request{
				{NamespacedName: types.NamespacedName{Name: "gateway-one", Namespace: "default"}},
				{NamespacedName: types.NamespacedName{Name: "gateway-two", Namespace: "default"}},
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			controller := GatewayController{}

			fn := controller.transformHTTPRoute(context.Background())
			require.ElementsMatch(t, tt.expected, fn(tt.route))
		})
	}
}

func TestTransformTCPRoute(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct {
		route    *gwv1alpha2.TCPRoute
		expected []reconcile.Request
	}{
		"route with parent empty namespace": {
			route: &gwv1alpha2.TCPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
				},
				Spec: gwv1alpha2.TCPRouteSpec{
					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
						ParentRefs: []gwv1beta1.ParentReference{
							{Name: "gateway"},
						},
					},
				},
			},
			expected: []reconcile.Request{
				{NamespacedName: types.NamespacedName{Name: "gateway", Namespace: "default"}},
			},
		},
		"route with parent with namespace": {
			route: &gwv1alpha2.TCPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
				},
				Spec: gwv1alpha2.TCPRouteSpec{
					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
						ParentRefs: []gwv1beta1.ParentReference{
							{Name: "gateway", Namespace: common.PointerTo(gwv1beta1.Namespace("other"))},
						},
					},
				},
			},
			expected: []reconcile.Request{
				{NamespacedName: types.NamespacedName{Name: "gateway", Namespace: "other"}},
			},
		},
		"route with non gateway parent with namespace": {
			route: &gwv1alpha2.TCPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
				},
				Spec: gwv1alpha2.TCPRouteSpec{
					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
						ParentRefs: []gwv1beta1.ParentReference{
							{Name: "gateway", Group: common.PointerTo(gwv1beta1.Group("group"))},
						},
					},
				},
			},
			expected: []reconcile.Request{},
		},
		"route with parent in status and no namespace": {
			route: &gwv1alpha2.TCPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
				},
				Status: gwv1alpha2.TCPRouteStatus{
					RouteStatus: gwv1beta1.RouteStatus{
						Parents: []gwv1beta1.RouteParentStatus{
							{ParentRef: gwv1beta1.ParentReference{Name: "gateway"}},
						},
					},
				},
			},
			expected: []reconcile.Request{
				{NamespacedName: types.NamespacedName{Name: "gateway", Namespace: "default"}},
			},
		},
		"route with parent in status and namespace": {
			route: &gwv1alpha2.TCPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
				},
				Status: gwv1alpha2.TCPRouteStatus{
					RouteStatus: gwv1beta1.RouteStatus{
						Parents: []gwv1beta1.RouteParentStatus{
							{ParentRef: gwv1beta1.ParentReference{Name: "gateway", Namespace: common.PointerTo(gwv1beta1.Namespace("other"))}},
						},
					},
				},
			},
			expected: []reconcile.Request{
				{NamespacedName: types.NamespacedName{Name: "gateway", Namespace: "other"}},
			},
		},
		"route with non gateway parent in status": {
			route: &gwv1alpha2.TCPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
				},
				Status: gwv1alpha2.TCPRouteStatus{
					RouteStatus: gwv1beta1.RouteStatus{
						Parents: []gwv1beta1.RouteParentStatus{
							{ParentRef: gwv1beta1.ParentReference{Name: "gateway", Group: common.PointerTo(gwv1beta1.Group("group"))}},
						},
					},
				},
			},
			expected: []reconcile.Request{},
		},
		"route parent in spec and in status": {
			route: &gwv1alpha2.TCPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
				},
				Spec: gwv1alpha2.TCPRouteSpec{
					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
						ParentRefs: []gwv1beta1.ParentReference{
							{Name: "gateway-one"},
						},
					},
				},
				Status: gwv1alpha2.TCPRouteStatus{
					RouteStatus: gwv1beta1.RouteStatus{
						Parents: []gwv1beta1.RouteParentStatus{
							{ParentRef: gwv1beta1.ParentReference{Name: "gateway-two"}},
						},
					},
				},
			},
			expected: []reconcile.Request{
				{NamespacedName: types.NamespacedName{Name: "gateway-one", Namespace: "default"}},
				{NamespacedName: types.NamespacedName{Name: "gateway-two", Namespace: "default"}},
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			controller := GatewayController{}

			fn := controller.transformTCPRoute(context.Background())
			require.ElementsMatch(t, tt.expected, fn(tt.route))
		})
	}
}
