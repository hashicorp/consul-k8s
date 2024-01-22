// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package namespace

import (
	"context"
	"fmt"

	mapset "github.com/deckarep/golang-set"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/common"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/namespaces"
)

type Controller struct {
	client.Client
	// ConsulClientConfig is the config for the Consul API client.
	ConsulClientConfig *consul.Config
	// ConsulServerConnMgr is the watcher for the Consul server addresses.
	ConsulServerConnMgr consul.ServerConnectionManager
	// AllowK8sNamespacesSet determines kube namespace that are reconciled.
	AllowK8sNamespacesSet mapset.Set
	// DenyK8sNamespacesSet determines kube namespace that are ignored.
	DenyK8sNamespacesSet mapset.Set

	// Partition is not required. It should already be set in the API ClientConfig

	// ConsulDestinationNamespace is the name of the Consul namespace to create
	// all config entries in. If EnableNSMirroring is true this is ignored.
	ConsulDestinationNamespace string
	// EnableNSMirroring causes Consul namespaces to be created to match the
	// k8s namespace of any config entry custom resource. Config entries will
	// be created in the matching Consul namespace.
	EnableNSMirroring bool
	// NSMirroringPrefix is an optional prefix that can be added to the Consul
	// namespaces created while mirroring. For example, if it is set to "k8s-",
	// then the k8s `default` namespace will be mirrored in Consul's
	// `k8s-default` namespace.
	NSMirroringPrefix string

	// CrossNamespaceACLPolicy is the name of the ACL policy to attach to
	// any created Consul namespaces to allow cross namespace service discovery.
	// Only necessary if ACLs are enabled.
	CrossNamespaceACLPolicy string

	Log logr.Logger
}

// Reconcile reads a Kubernetes Namespace and reconciles the mapped namespace in Consul.
// TODO: Move the creation of a destination namespace to a dedicated, single-flight goroutine.
func (r *Controller) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var namespace corev1.Namespace

	// Ignore the request if the namespace is not allowed.
	if common.ShouldIgnore(req.Name, r.DenyK8sNamespacesSet, r.AllowK8sNamespacesSet) {
		return ctrl.Result{}, nil
	}

	apiClient, err := consul.NewClientFromConnMgr(r.ConsulClientConfig, r.ConsulServerConnMgr)
	if err != nil {
		r.Log.Error(err, "failed to create Consul API client", "name", req.Name)
		return ctrl.Result{}, err
	}

	err = r.Client.Get(ctx, req.NamespacedName, &namespace)

	// If the namespace object has been deleted (and we get an IsNotFound error),
	// we need to remove the Namespace from Consul.
	if k8serrors.IsNotFound(err) {

		// if we are using a destination namespace, NEVER delete it.
		if !r.EnableNSMirroring {
			return ctrl.Result{}, nil
		}

		if err := namespaces.EnsureDeleted(apiClient, r.getConsulNamespace(req.Name)); err != nil {
			r.Log.Error(err, "error deleting namespace",
				"namespace", r.getConsulNamespace(req.Name))
			return ctrl.Result{}, fmt.Errorf("error deleting namespace: %w", err)
		}

		return ctrl.Result{}, nil
	} else if err != nil {
		r.Log.Error(err, "failed to get namespace", "name", req.Name)
		return ctrl.Result{}, err
	}

	r.Log.Info("retrieved", "namespace", namespace.GetName())

	// TODO: eventually we will want to replace the V1 namespace APIs with the native V2 resource creation for tenancy
	if _, err := namespaces.EnsureExists(apiClient, r.getConsulNamespace(namespace.GetName()), r.CrossNamespaceACLPolicy); err != nil {
		r.Log.Error(err, "error checking or creating namespace",
			"namespace", r.getConsulNamespace(namespace.GetName()))
		return ctrl.Result{}, fmt.Errorf("error checking or creating namespace: %w", err)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager registers this controller with the manager.
func (r *Controller) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Namespace{}).
		Complete(r)
}

// getConsulNamespace returns the Consul destination namespace for a provided Kubernetes namespace
// depending on Consul Namespaces being enabled and the value of namespace mirroring.
func (r *Controller) getConsulNamespace(kubeNamespace string) string {
	ns := namespaces.ConsulNamespace(
		kubeNamespace,
		true,
		r.ConsulDestinationNamespace,
		r.EnableNSMirroring,
		r.NSMirroringPrefix,
	)

	// TODO: remove this if and when the default namespace of resources change.
	if ns == "" {
		ns = constants.DefaultConsulNS
	}
	return ns
}
