// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package controllers

import (
	"context"
	"testing"

	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
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

func TestTransformSecret(t *testing.T) {
	t.Parallel()

	gateway := &gwv1beta1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gateway",
			Namespace: "test",
		},
		Spec: gwv1beta1.GatewaySpec{
			Listeners: []gwv1beta1.Listener{
				{Name: "terminate", TLS: &gwv1beta1.GatewayTLSConfig{
					Mode: common.PointerTo(gwv1beta1.TLSModeTerminate),
					CertificateRefs: []gwv1beta1.SecretObjectReference{
						{Name: "secret-no-namespace"},
						{Name: "secret-namespace", Namespace: common.PointerTo(gwv1beta1.Namespace("other"))},
					},
				}},
				{Name: "passthrough", TLS: &gwv1beta1.GatewayTLSConfig{
					Mode: common.PointerTo(gwv1beta1.TLSModePassthrough),
					CertificateRefs: []gwv1beta1.SecretObjectReference{
						{Name: "passthrough", Namespace: common.PointerTo(gwv1beta1.Namespace("other"))},
					},
				}},
			},
		},
	}

	for name, tt := range map[string]struct {
		secret   *corev1.Secret
		expected []reconcile.Request
	}{
		"explicit namespace from parent": {
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "secret-namespace", Namespace: "other"},
			},
			expected: []reconcile.Request{
				{NamespacedName: types.NamespacedName{Name: "gateway", Namespace: "test"}},
			},
		},
		"implicit namespace from parent": {
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "secret-no-namespace", Namespace: "test"},
			},
			expected: []reconcile.Request{
				{NamespacedName: types.NamespacedName{Name: "gateway", Namespace: "test"}},
			},
		},
		"mismatched namespace": {
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "secret-no-namespace", Namespace: "other"},
			},
		},
		"mismatched names": {
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "something", Namespace: "test"},
			},
		},
		"passthrough ignored": {
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "passthrough", Namespace: "other"},
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			tt := tt

			t.Parallel()

			s := runtime.NewScheme()
			require.NoError(t, clientgoscheme.AddToScheme(s))
			require.NoError(t, gwv1alpha2.Install(s))
			require.NoError(t, gwv1beta1.Install(s))
			require.NoError(t, v1alpha1.AddToScheme(s))

			fakeClient := registerFieldIndexersForTest(fake.NewClientBuilder().WithScheme(s)).WithRuntimeObjects(gateway).Build()

			controller := GatewayController{
				Client: fakeClient,
			}

			fn := controller.transformSecret(context.Background())
			require.ElementsMatch(t, tt.expected, fn(tt.secret))
		})
	}
}
