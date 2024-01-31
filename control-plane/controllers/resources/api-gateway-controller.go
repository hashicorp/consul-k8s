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

	meshv2beta1 "github.com/hashicorp/consul-k8s/control-plane/api/mesh/v2beta1"
)

// APIGatewayController reconciles a APIGateway object.
type APIGatewayController struct {
	client.Client
	Log        logr.Logger
	Scheme     *runtime.Scheme
	Controller *ConsulResourceController
}

// +kubebuilder:rbac:groups=mesh.consul.hashicorp.com,resources=tcproute,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=mesh.consul.hashicorp.com,resources=tcproute/status,verbs=get;update;patch

func (r *APIGatewayController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Logger(req.NamespacedName).Info("Reconciling APIGateway")
	return r.Controller.ReconcileResource(ctx, r, req, &meshv2beta1.APIGateway{})
}

func (r *APIGatewayController) Logger(name types.NamespacedName) logr.Logger {
	return r.Log.WithValues("request", name)
}

func (r *APIGatewayController) UpdateStatus(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	return r.Status().Update(ctx, obj, opts...)
}

func (r *APIGatewayController) SetupWithManager(mgr ctrl.Manager) error {
	return setupWithManager(mgr, &meshv2beta1.APIGateway{}, r)
}
