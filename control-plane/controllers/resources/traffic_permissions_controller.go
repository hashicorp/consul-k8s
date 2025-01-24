// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package resources

import (
	"context"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	consulv2beta1 "github.com/hashicorp/consul-k8s/control-plane/api/auth/v2beta1"
)

// TrafficPermissionsController reconciles a TrafficPermissions object.
type TrafficPermissionsController struct {
	client.Client
	Log        logr.Logger
	Scheme     *runtime.Scheme
	Controller *ConsulResourceController
}

// +kubebuilder:rbac:groups=auth.consul.hashicorp.com,resources=trafficpermissions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=auth.consul.hashicorp.com,resources=trafficpermissions/status,verbs=get;update;patch

func (r *TrafficPermissionsController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return r.Controller.ReconcileResource(ctx, r, req, &consulv2beta1.TrafficPermissions{})
}

func (r *TrafficPermissionsController) Logger(name types.NamespacedName) logr.Logger {
	return r.Log.WithValues("request", name)
}

func (r *TrafficPermissionsController) UpdateStatus(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	return r.Status().Update(ctx, obj, opts...)
}

func (r *TrafficPermissionsController) SetupWithManager(mgr ctrl.Manager) error {
	return setupWithManager(mgr, &consulv2beta1.TrafficPermissions{}, r)
}
