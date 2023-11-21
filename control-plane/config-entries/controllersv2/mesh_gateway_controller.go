// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package controllersv2

import (
	"context"
	"errors"

	"github.com/go-logr/logr"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	meshv2beta1 "github.com/hashicorp/consul-k8s/control-plane/api/mesh/v2beta1"
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
	// TODO NET-6392 NET-6393 NET-6394 NET-6395
	return errors.New("onCreateUpdate not implemented")
}

func (r *MeshGatewayController) onDelete(ctx context.Context, req ctrl.Request, resource *meshv2beta1.MeshGateway) error {
	// TODO NET-6392 NET-6393 NET-6394 NET-6395
	return errors.New("onDelete not implemented")
}
