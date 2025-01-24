// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package resources

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
	"github.com/hashicorp/consul-k8s/control-plane/gateways"
)

// errResourceNotOwned indicates that a resource the controller would have
// updated or deleted does not have an owner reference pointing to the MeshGateway.
var errResourceNotOwned = errors.New("existing resource not owned by controller")

// MeshGatewayController reconciles a MeshGateway object.
type MeshGatewayController struct {
	client.Client
	Log           logr.Logger
	Scheme        *runtime.Scheme
	Controller    *ConsulResourceController
	GatewayConfig gateways.GatewayConfig
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

		if err := onDelete(ctx, req, r.Client, resource); err != nil {
			return ctrl.Result{}, err
		}
	} else {
		// Fetch GatewayClassConfig for the gateway
		gcc, err := getGatewayClassConfigByGatewayClassName(ctx, r.Client, resource.Spec.GatewayClassName)
		if err != nil {
			r.Log.Error(err, "unable to get gatewayclassconfig for gateway: %s gatewayclass: %s", resource.Name, resource.Spec.GatewayClassName)
			return ctrl.Result{}, err
		}

		if err := onCreateUpdate(ctx, r.Client, gatewayConfigs{
			gcc:           gcc,
			gatewayConfig: r.GatewayConfig,
		}, resource, gateways.MeshGatewayAnnotationKind); err != nil {
			return ctrl.Result{}, err
		}
	}

	return r.Controller.ReconcileResource(ctx, r, req, &meshv2beta1.MeshGateway{})
}

func (r *MeshGatewayController) Logger(name types.NamespacedName) logr.Logger {
	return r.Log.WithValues("request", name)
}

func (r *MeshGatewayController) UpdateStatus(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	return r.Status().Update(ctx, obj, opts...)
}

func (r *MeshGatewayController) SetupWithManager(mgr ctrl.Manager) error {
	return setupGatewayControllerWithManager[*meshv2beta1.MeshGatewayList](mgr, &meshv2beta1.MeshGateway{}, r.Client, r, MeshGateway_GatewayClassIndex)
}
