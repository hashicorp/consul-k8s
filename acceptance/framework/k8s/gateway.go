// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package k8s

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/consul-k8s/acceptance/framework/config"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func referenceGrantForCluster(mapper meta.RESTMapper, grant *gwv1.ReferenceGrant) (client.Object, error) {
	kind := schema.GroupKind{Group: gwv1.GroupVersion.Group, Kind: "ReferenceGrant"}
	if _, err := mapper.RESTMapping(kind, "v1"); err == nil {
		return grant, nil
	} else if !meta.IsNoMatchError(err) {
		return nil, err
	}
	if _, err := mapper.RESTMapping(kind, "v1beta1"); err != nil {
		return nil, fmt.Errorf("ReferenceGrant is not available as v1 or v1beta1: %w", err)
	}
	// OCP-managed Gateway API CRDs can still serve only v1beta1. The fields
	// these tests use are shared by both versions, as in the gateway controller.
	object, err := runtime.DefaultUnstructuredConverter.ToUnstructured(grant)
	if err != nil {
		return nil, err
	}
	beta := &unstructured.Unstructured{Object: object}
	beta.SetGroupVersionKind(schema.GroupVersionKind{Group: kind.Group, Version: "v1beta1", Kind: kind.Kind})
	return beta, nil
}

// CreateReferenceGrant creates the grant. On OpenShift it falls back to v1beta1 when the
// cluster-managed Gateway API CRDs do not serve ReferenceGrant v1.
func CreateReferenceGrant(t *testing.T, kubernetesClient client.Client, cfg *config.TestConfig, grant *gwv1.ReferenceGrant) {
	t.Helper()
	if !cfg.EnableOpenshift && !cfg.UseOpenshift {
		require.NoError(t, kubernetesClient.Create(context.Background(), grant))
		return
	}
	object, err := referenceGrantForCluster(kubernetesClient.RESTMapper(), grant)
	require.NoError(t, err)
	require.NoError(t, kubernetesClient.Create(context.Background(), object))
}
