// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package controllersv2

import (
	"context"
	"errors"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	meshv2beta1 "github.com/hashicorp/consul-k8s/control-plane/api/mesh/v2beta1"
	"github.com/hashicorp/consul-k8s/control-plane/gateways"
)

// MeshGatewayController reconciles a MeshGateway object.
type MeshGatewayController struct {
	client.Client
	Log                  logr.Logger
	Scheme               *runtime.Scheme
	MeshConfigController *MeshConfigController
}

// +kubebuilder:rbac:groups=mesh.consul.hashicorp.com,resources=meshgateway,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=mesh.consul.hashicorp.com,resources=meshgateway/status,verbs=get;update;patch

func (r *MeshGatewayController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger(req.NamespacedName)

	// Fetch the resource being reconciled
	resource := &meshv2beta1.MeshGateway{}
	if err := r.Get(ctx, req.NamespacedName, resource); k8serr.IsNotFound(err) {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	} else if err != nil {
		logger.Error(err, "retrieving resource")
		return ctrl.Result{}, err
	}

	// Call hooks
	if !resource.GetDeletionTimestamp().IsZero() {
		logger.Info("deletion event")

		if err := r.onDelete(ctx, req, resource); err != nil {
			return ctrl.Result{}, err
		}
	} else {
		if err := r.onCreateUpdate(ctx, req, resource); err != nil {
			return ctrl.Result{}, err
		}
	}

	return r.MeshConfigController.ReconcileEntry(ctx, r, req, &meshv2beta1.MeshGateway{})
}

func (r *MeshGatewayController) Logger(name types.NamespacedName) logr.Logger {
	return r.Log.WithValues("request", name)
}

func (r *MeshGatewayController) UpdateStatus(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	return r.Status().Update(ctx, obj, opts...)
}

func (r *MeshGatewayController) SetupWithManager(mgr ctrl.Manager) error {
	return setupWithManager(mgr, &meshv2beta1.MeshGateway{}, r)
}

func (r *MeshGatewayController) onCreateUpdate(ctx context.Context, req ctrl.Request, resource *meshv2beta1.MeshGateway) error {
	builder := gateways.NewMeshGatewayBuilder(resource)

	ifNew := func(ctx context.Context, object client.Object) error {
		return r.Create(ctx, object)
	}

	ifExists := func(ctx context.Context, object client.Object) error {
		return r.Update(ctx, object)
	}

	err := r.opIfNewOrOwned(ctx, resource, &corev1.ServiceAccount{}, builder.ServiceAccount(), ifNew, ifExists)
	if err != nil {
		return err
	}

	// TODO NET-6392 NET-6393 NET-6395

	return nil
}

func (r *MeshGatewayController) onDelete(ctx context.Context, req ctrl.Request, resource *meshv2beta1.MeshGateway) error {
	builder := gateways.NewMeshGatewayBuilder(resource)

	ifExists := func(ctx context.Context, object client.Object) error {
		return r.Delete(ctx, object)
	}

	err := r.opIfNewOrOwned(ctx, resource, &corev1.ServiceAccount{}, builder.ServiceAccount(), nil, ifExists)
	if err != nil {
		return err
	}

	// TODO NET-6392 NET-6393 NET-6395

	return nil
}

type op func(context.Context, client.Object) error

// opIfNewOrOwned runs a given op function to create, update, or delete a resource.
// The purpose of opIfNewOrOwned is to ensure that we aren't updating or deleting a
// resource that was not created by us. If this scenario is encountered, we error.
func (r *MeshGatewayController) opIfNewOrOwned(ctx context.Context, gateway *meshv2beta1.MeshGateway, scanTarget, writeSource client.Object, ifNew, ifOwned op) error {
	// Ensure owner reference is always set on objects that we write
	ctrl.SetControllerReference(gateway, writeSource, r.Client.Scheme())

	key := client.ObjectKey{
		Namespace: writeSource.GetNamespace(),
		Name:      writeSource.GetName(),
	}

	exists := false
	if err := r.Get(ctx, key, scanTarget); err != nil {
		// We failed to fetch the object in a way that doesn't tell us about its existence
		if !k8serr.IsNotFound(err) {
			return err
		}
	} else {
		// We successfully fetched the object, so it exists
		exists = true
	}

	// None exists, so we need only create it
	if !exists {
		return ifNew(ctx, writeSource)
	}

	// Ensure the existing object was put there by us so that we don't overwrite random objects
	owned := false
	for _, reference := range scanTarget.GetOwnerReferences() {
		if reference.UID == gateway.GetUID() && reference.Name == gateway.GetName() {
			owned = true
			break
		}
	}
	if !owned {
		return errors.New("existing resource not owned by controller")
	}
	return ifOwned(ctx, writeSource)
}
