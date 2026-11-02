// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package k8s

import (
	"context"
	"testing"
	"time"

	gatewaycommon "github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	clientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func TestReferenceGrantForCluster(t *testing.T) {
	grant := &gwv1.ReferenceGrant{
		ObjectMeta: metav1.ObjectMeta{Name: "certificate", Namespace: "certificates"},
		Spec: gwv1.ReferenceGrantSpec{
			From: []gwv1.ReferenceGrantFrom{{
				Group: gwv1.Group(gwv1.GroupVersion.Group), Kind: "Gateway", Namespace: "gateways",
			}},
			To: []gwv1.ReferenceGrantTo{{Kind: "Secret"}},
		},
	}
	for _, tc := range []struct {
		name     string
		versions []string
		want     string
	}{
		{name: "stable", versions: []string{"v1"}, want: "v1"},
		{name: "OCP beta", versions: []string{"v1beta1"}, want: "v1beta1"},
		{name: "prefer stable", versions: []string{"v1beta1", "v1"}, want: "v1"},
		{name: "missing CRD"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mapper := meta.NewDefaultRESTMapper(nil)
			for _, version := range tc.versions {
				mapper.Add(schema.GroupVersionKind{Group: gwv1.GroupVersion.Group, Version: version, Kind: "ReferenceGrant"}, meta.RESTScopeNamespace)
			}
			object, err := referenceGrantForCluster(mapper, grant)
			if tc.want == "" {
				require.ErrorContains(t, err, "not available as v1 or v1beta1")
				return
			}
			require.NoError(t, err)
			if tc.want == "v1" {
				require.Same(t, grant, object)
			}
			expected, err := runtime.DefaultUnstructuredConverter.ToUnstructured(grant)
			require.NoError(t, err)
			actual, err := runtime.DefaultUnstructuredConverter.ToUnstructured(object)
			require.NoError(t, err)
			delete(actual, "apiVersion")
			delete(actual, "kind")
			require.Equal(t, expected, actual)
			require.Equal(t, "certificates", object.GetNamespace())
		})
	}
}

func TestDeleteOpenShiftHTTPRoute(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, gwv1.Install(scheme))
	t.Run("deletes only the named fixture", func(t *testing.T) {
		target := &gwv1.HTTPRoute{ObjectMeta: metav1.ObjectMeta{
			Name: "route-two", Namespace: "test", Finalizers: []string{gatewaycommon.GatewayFinalizer},
		}}
		unrelated := &gwv1.HTTPRoute{ObjectMeta: metav1.ObjectMeta{
			Name: "unrelated", Namespace: "test", Finalizers: []string{gatewaycommon.GatewayFinalizer},
		}}
		kubernetesClient := clientfake.NewClientBuilder().WithScheme(scheme).WithObjects(target, unrelated).Build()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, deleteOpenShiftHTTPRoute(ctx, kubernetesClient, "test", "route-two"))
		require.True(t, apierrors.IsNotFound(kubernetesClient.Get(ctx, client.ObjectKeyFromObject(target), target)))
		require.NoError(t, kubernetesClient.Get(ctx, client.ObjectKeyFromObject(unrelated), unrelated))
		require.Equal(t, []string{gatewaycommon.GatewayFinalizer}, unrelated.Finalizers)
		require.Nil(t, unrelated.DeletionTimestamp)
	})
	t.Run("preserves other controllers finalizers", func(t *testing.T) {
		target := &gwv1.HTTPRoute{ObjectMeta: metav1.ObjectMeta{
			Name: "route-two", Namespace: "test",
			Finalizers: []string{gatewaycommon.GatewayFinalizer, "another-controller.example/finalizer"},
		}}
		kubernetesClient := clientfake.NewClientBuilder().WithScheme(scheme).WithObjects(target).Build()
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		err := deleteOpenShiftHTTPRoute(ctx, kubernetesClient, "test", "route-two")
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.ErrorContains(t, err, "another-controller.example/finalizer")
		require.NoError(t, kubernetesClient.Get(context.Background(), client.ObjectKeyFromObject(target), target))
		require.Equal(t, []string{"another-controller.example/finalizer"}, target.Finalizers)
		require.NotNil(t, target.DeletionTimestamp)
	})
	t.Run("accepts concurrent controller deletion", func(t *testing.T) {
		target := &gwv1.HTTPRoute{ObjectMeta: metav1.ObjectMeta{
			Name: "route-one", Namespace: "test", Finalizers: []string{gatewaycommon.GatewayFinalizer},
		}}
		kubernetesClient := clientfake.NewClientBuilder().WithScheme(scheme).WithObjects(target).
			WithInterceptorFuncs(interceptor.Funcs{
				Update: func(ctx context.Context, underlying client.WithWatch, object client.Object, options ...client.UpdateOption) error {
					if err := underlying.Update(ctx, object, options...); err != nil {
						return err
					}
					return apierrors.NewNotFound(schema.GroupResource{Group: gwv1.GroupVersion.Group, Resource: "httproutes"}, object.GetName())
				},
			}).Build()
		require.NoError(t, deleteOpenShiftHTTPRoute(context.Background(), kubernetesClient, "test", "route-one"))
		require.True(t, apierrors.IsNotFound(kubernetesClient.Get(context.Background(), client.ObjectKeyFromObject(target), target)))
	})
	t.Run("accepts an already deleted fixture", func(t *testing.T) {
		kubernetesClient := clientfake.NewClientBuilder().WithScheme(scheme).Build()
		require.NoError(t, deleteOpenShiftHTTPRoute(context.Background(), kubernetesClient, "test", "route-two"))
	})
}
