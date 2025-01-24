// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package namespace

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	injectcommon "github.com/hashicorp/consul-k8s/control-plane/connect-inject/common"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
)

// Namespace syncing between K8s and Consul is vastly simplified when V2 tenancy is enabled.
// Put simply, a K8s namespace maps 1:1 to a Consul namespace of the same name and that is
// the only supported behavior.
//
// The plethora of configuration options available when using V1 tenancy have been removed
// to simplify the user experience and mapping rules.
//
// Hence, the following V1 tenancy namespace helm configuration values are ignored:
// - global.enableConsulNamespaces
// - connectInject.consulNamespaces.consulDestinationNamespace
// - connectInject.consulNamespaces.mirroringK8S
// - connectInject.consulNamespaces.mirroringK8SPrefix.
type Controller struct {
	client.Client
	// ConsulServerConnMgr is the watcher for the Consul server addresses.
	ConsulServerConnMgr consul.ServerConnectionManager
	// K8sNamespaceConfig manages allow/deny Kubernetes namespaces.
	common.K8sNamespaceConfig
	// ConsulTenancyConfig contains the destination partition.
	common.ConsulTenancyConfig
	Log logr.Logger
}

// Reconcile reads a Kubernetes Namespace and reconciles the mapped namespace in Consul.
func (r *Controller) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var namespace corev1.Namespace

	// Ignore the request if the namespace should not be synced to consul.
	if injectcommon.ShouldIgnore(req.Name, r.DenyK8sNamespacesSet, r.AllowK8sNamespacesSet) {
		return ctrl.Result{}, nil
	}

	// Create a gRPC resource service client
	resourceClient, err := consul.NewResourceServiceClient(r.ConsulServerConnMgr)
	if err != nil {
		r.Log.Error(err, "failed to create Consul resource service client", "name", req.Name)
		return ctrl.Result{}, err
	}

	// Target consul tenancy
	consulAP := r.ConsulPartition
	consulNS := req.Name

	// Re-read the k8s namespace object
	err = r.Client.Get(ctx, req.NamespacedName, &namespace)

	// If the namespace object has been deleted (we get an IsNotFound error),
	// we need to remove the Namespace from Consul.
	if k8serrors.IsNotFound(err) {
		if err := EnsureDeleted(ctx, resourceClient, consulAP, consulNS); err != nil {
			return ctrl.Result{}, fmt.Errorf("error deleting consul namespace: %w", err)
		}

		return ctrl.Result{}, nil
	} else if err != nil {
		r.Log.Error(err, "failed to get k8s namespace", "name", req.Name)
		return ctrl.Result{}, err
	}

	// k8s namespace found, so make sure it is mapped correctly and exists in Consul.
	r.Log.Info("retrieved", "k8s namespace", namespace.GetName())

	if _, err := EnsureExists(ctx, resourceClient, consulAP, consulNS); err != nil {
		r.Log.Error(err, "error checking or creating consul namespace", "namespace", consulNS)
		return ctrl.Result{}, fmt.Errorf("error checking or creating consul namespace: %w", err)
	}
	return ctrl.Result{}, nil
}

// SetupWithManager registers this controller with the manager.
func (r *Controller) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Namespace{}).
		Complete(r)
}
