// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package k8s

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/hashicorp/consul-k8s/acceptance/framework/config"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	gatewaycommon "github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
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

// DeleteOpenShiftHTTPRoute removes a test fixture without deleting OCP-managed
// CRDs or leaving Consul's finalizer on a route with an uncontrolled parent.
func DeleteOpenShiftHTTPRoute(t *testing.T, kubernetesClient client.Client, namespace, name string) {
	t.Helper()
	logger.Logf(t, "deleting fixture HTTPRoute %s/%s and clearing only its Consul finalizer", namespace, name)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	require.NoError(t, deleteOpenShiftHTTPRoute(ctx, kubernetesClient, namespace, name))
}

func deleteOpenShiftHTTPRoute(ctx context.Context, kubernetesClient client.Client, namespace, name string) error {
	route := &gwv1.HTTPRoute{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}}
	if err := kubernetesClient.Delete(ctx, route); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("deleting fixture HTTPRoute %s/%s: %w", namespace, name, err)
	}
	var finalizers []string
	err := wait.PollUntilContextCancel(ctx, time.Second, true, func(ctx context.Context) (bool, error) {
		var current gwv1.HTTPRoute
		if err := kubernetesClient.Get(ctx, client.ObjectKeyFromObject(route), &current); err != nil {
			return apierrors.IsNotFound(err), client.IgnoreNotFound(err)
		}
		if gatewaycommon.RemoveFinalizer(&current) {
			if err := kubernetesClient.Update(ctx, &current); err != nil {
				if apierrors.IsNotFound(err) {
					return true, nil
				}
				if apierrors.IsConflict(err) {
					return false, nil
				}
				return false, err
			}
		}
		finalizers = current.Finalizers
		return false, nil
	})
	if err != nil {
		return fmt.Errorf("waiting for fixture HTTPRoute %s/%s deletion (remaining finalizers: %v): %w", namespace, name, finalizers, err)
	}
	return nil
}
