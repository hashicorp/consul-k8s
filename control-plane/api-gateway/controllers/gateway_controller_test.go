// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package controllers

import (
	"context"
	"testing"

	mapset "github.com/deckarep/golang-set"
	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
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

func TestTransformEndpoints(t *testing.T) {
	t.Parallel()

	httpRoute := &gwv1beta1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "http",
			Namespace: "test",
		},
		Spec: gwv1beta1.HTTPRouteSpec{
			Rules: []gwv1beta1.HTTPRouteRule{
				{BackendRefs: []gwv1beta1.HTTPBackendRef{
					{BackendRef: gwv1beta1.BackendRef{
						BackendObjectReference: gwv1beta1.BackendObjectReference{Name: "http-test-namespace"},
					}},
					{BackendRef: gwv1beta1.BackendRef{
						BackendObjectReference: gwv1beta1.BackendObjectReference{Name: "http-other-namespace", Namespace: common.PointerTo(gwv1beta1.Namespace("other"))},
					}},
					{BackendRef: gwv1beta1.BackendRef{
						BackendObjectReference: gwv1beta1.BackendObjectReference{Name: "http-system-namespace", Namespace: common.PointerTo(gwv1beta1.Namespace("system"))},
					}},
					{BackendRef: gwv1beta1.BackendRef{
						BackendObjectReference: gwv1beta1.BackendObjectReference{Name: "http-public-namespace", Namespace: common.PointerTo(gwv1beta1.Namespace("public"))},
					}},
					{BackendRef: gwv1beta1.BackendRef{
						BackendObjectReference: gwv1beta1.BackendObjectReference{Name: "http-local-path-storage-namespace", Namespace: common.PointerTo(gwv1beta1.Namespace("local-path-storage"))},
					}}},
				},
			},
			CommonRouteSpec: gwv1beta1.CommonRouteSpec{
				ParentRefs: []gwv1beta1.ParentReference{
					{Name: "http-gateway"},
					{Name: "general-gateway"},
				},
			},
		},
	}

	tcpRoute := &gwv1alpha2.TCPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tcp",
			Namespace: "test",
		},
		Spec: gwv1alpha2.TCPRouteSpec{
			Rules: []gwv1alpha2.TCPRouteRule{
				{BackendRefs: []gwv1beta1.BackendRef{
					{BackendObjectReference: gwv1beta1.BackendObjectReference{Name: "tcp-test-namespace"}},
					{BackendObjectReference: gwv1beta1.BackendObjectReference{Name: "tcp-other-namespace", Namespace: common.PointerTo(gwv1beta1.Namespace("other"))}},
					{BackendObjectReference: gwv1beta1.BackendObjectReference{Name: "tcp-system-namespace", Namespace: common.PointerTo(gwv1beta1.Namespace("system"))}},
					{BackendObjectReference: gwv1beta1.BackendObjectReference{Name: "tcp-public-namespace", Namespace: common.PointerTo(gwv1beta1.Namespace("public"))}},
					{BackendObjectReference: gwv1beta1.BackendObjectReference{Name: "tcp-local-path-storage-namespace", Namespace: common.PointerTo(gwv1beta1.Namespace("local-path-storage"))}},
				}},
			},
			CommonRouteSpec: gwv1beta1.CommonRouteSpec{
				ParentRefs: []gwv1beta1.ParentReference{
					{Name: "tcp-gateway"},
					{Name: "general-gateway"},
				},
			},
		},
	}

	for name, tt := range map[string]struct {
		endpoints         *corev1.Endpoints
		expected          []reconcile.Request
		allowedNamespaces []string
		denyNamespaces    []string
	}{
		"ignore system namespace": {
			endpoints: &corev1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "http-system-namespace",
					Namespace: metav1.NamespaceSystem,
				},
			},
			allowedNamespaces: []string{"*"},
		},
		"ignore public namespace": {
			endpoints: &corev1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "http-public-namespace",
					Namespace: metav1.NamespacePublic,
				},
			},
			allowedNamespaces: []string{"*"},
		},
		"ignore local-path-storage namespace": {
			endpoints: &corev1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "http-local-path-storage-namespace",
					Namespace: "local-path-storage",
				},
			},
			allowedNamespaces: []string{"*"},
		},
		"explicit deny namespace": {
			endpoints: &corev1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "http-test-namespace",
					Namespace: "test",
				},
			},
			allowedNamespaces: []string{"*"},
			denyNamespaces:    []string{"test"},
		},
		"ignore labels": {
			endpoints: &corev1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "http-test-namespace",
					Namespace: "test",
					Labels: map[string]string{
						constants.LabelServiceIgnore: "true",
					},
				},
			},
			allowedNamespaces: []string{"test"},
		},
		"http same namespace wildcard allow": {
			endpoints: &corev1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "http-test-namespace",
					Namespace: "test",
				},
			},
			allowedNamespaces: []string{"*"},
			expected: []reconcile.Request{
				{NamespacedName: types.NamespacedName{Name: "http-gateway", Namespace: "test"}},
				{NamespacedName: types.NamespacedName{Name: "general-gateway", Namespace: "test"}},
			},
		},
		"http same namespace explicit allow": {
			endpoints: &corev1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "http-test-namespace",
					Namespace: "test",
				},
			},
			allowedNamespaces: []string{"test"},
			expected: []reconcile.Request{
				{NamespacedName: types.NamespacedName{Name: "http-gateway", Namespace: "test"}},
				{NamespacedName: types.NamespacedName{Name: "general-gateway", Namespace: "test"}},
			},
		},
		"http other namespace wildcard allow": {
			endpoints: &corev1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "http-other-namespace",
					Namespace: "other",
				},
			},
			allowedNamespaces: []string{"*"},
			expected: []reconcile.Request{
				{NamespacedName: types.NamespacedName{Name: "http-gateway", Namespace: "test"}},
				{NamespacedName: types.NamespacedName{Name: "general-gateway", Namespace: "test"}},
			},
		},
		"http other namespace explicit allow": {
			endpoints: &corev1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "http-other-namespace",
					Namespace: "other",
				},
			},
			allowedNamespaces: []string{"other"},
			expected: []reconcile.Request{
				{NamespacedName: types.NamespacedName{Name: "http-gateway", Namespace: "test"}},
				{NamespacedName: types.NamespacedName{Name: "general-gateway", Namespace: "test"}},
			},
		},
		"tcp same namespace wildcard allow": {
			endpoints: &corev1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "tcp-test-namespace",
					Namespace: "test",
				},
			},
			allowedNamespaces: []string{"*"},
			expected: []reconcile.Request{
				{NamespacedName: types.NamespacedName{Name: "tcp-gateway", Namespace: "test"}},
				{NamespacedName: types.NamespacedName{Name: "general-gateway", Namespace: "test"}},
			},
		},
		"tcp same namespace explicit allow": {
			endpoints: &corev1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "tcp-test-namespace",
					Namespace: "test",
				},
			},
			allowedNamespaces: []string{"test"},
			expected: []reconcile.Request{
				{NamespacedName: types.NamespacedName{Name: "tcp-gateway", Namespace: "test"}},
				{NamespacedName: types.NamespacedName{Name: "general-gateway", Namespace: "test"}},
			},
		},
		"tcp other namespace wildcard allow": {
			endpoints: &corev1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "tcp-other-namespace",
					Namespace: "other",
				},
			},
			allowedNamespaces: []string{"*"},
			expected: []reconcile.Request{
				{NamespacedName: types.NamespacedName{Name: "tcp-gateway", Namespace: "test"}},
				{NamespacedName: types.NamespacedName{Name: "general-gateway", Namespace: "test"}},
			},
		},
		"tcp other namespace explicit allow": {
			endpoints: &corev1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "tcp-other-namespace",
					Namespace: "other",
				},
			},
			allowedNamespaces: []string{"other"},
			expected: []reconcile.Request{
				{NamespacedName: types.NamespacedName{Name: "tcp-gateway", Namespace: "test"}},
				{NamespacedName: types.NamespacedName{Name: "general-gateway", Namespace: "test"}},
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			s := runtime.NewScheme()
			require.NoError(t, clientgoscheme.AddToScheme(s))
			require.NoError(t, gwv1alpha2.Install(s))
			require.NoError(t, gwv1beta1.Install(s))
			require.NoError(t, v1alpha1.AddToScheme(s))

			denySet := mapset.NewSet()
			for _, v := range tt.denyNamespaces {
				denySet.Add(v)
			}
			allowSet := mapset.NewSet()
			for _, v := range tt.allowedNamespaces {
				allowSet.Add(v)
			}

			fakeClient := registerFieldIndexersForTest(fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(httpRoute, tcpRoute)).Build()

			controller := GatewayController{
				Client:                fakeClient,
				denyK8sNamespacesSet:  denySet,
				allowK8sNamespacesSet: allowSet,
			}

			fn := controller.transformEndpoints(context.Background())
			require.ElementsMatch(t, tt.expected, fn(tt.endpoints))
		})
	}
}
