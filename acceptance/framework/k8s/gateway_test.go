// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package k8s

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
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
