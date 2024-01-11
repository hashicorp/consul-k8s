// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package controllers

import (
	"context"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	multiclusterv2beta1 "github.com/hashicorp/consul-k8s/control-plane/api/multicluster/v2beta1"
	"github.com/hashicorp/consul-k8s/control-plane/gateways"
)

// ExportedServicesController reconciles a MeshGateway object.
type ExportedServicesController struct {
	client.Client
	Log           logr.Logger
	Scheme        *runtime.Scheme
	Controller    *ConsulResourceController
	GatewayConfig gateways.GatewayConfig
}

// +kubebuilder:rbac:groups=multicluster.consul.hashicorp.com,resources=exportedservices,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=multicluster.consul.hashicorp.com,resources=exportedservices/status,verbs=get;update;patch

func (r *ExportedServicesController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return r.Controller.ReconcileResource(ctx, r, req, &multiclusterv2beta1.ExportedServices{})
}

func (r *ExportedServicesController) Logger(name types.NamespacedName) logr.Logger {
	return r.Log.WithValues("request", name)
}

func (r *ExportedServicesController) UpdateStatus(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	return r.Status().Update(ctx, obj, opts...)
}

func (r *ExportedServicesController) SetupWithManager(mgr ctrl.Manager) error {
	return setupWithManager(mgr, &multiclusterv2beta1.ExportedServices{}, r)
}
