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

// GatewayClassConfigController reconciles a GatewayClassConfig object.
type GatewayClassConfigController struct {
	client.Client
	Log        logr.Logger
	Scheme     *runtime.Scheme
	Controller *ConsulResourceController
}

// +kubebuilder:rbac:groups=mesh.consul.hashicorp.com,resources=gatewayclassconfig,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=mesh.consul.hashicorp.com,resources=gatewayclassconfig/status,verbs=get;update;patch

func (r *GatewayClassConfigController) Reconcile(_ context.Context, _ ctrl.Request) (ctrl.Result, error) {
	// GatewayClassConfig is not synced into Consul because Consul has no use for it.
	// Consul is only aware of the resource for the sake of Kubernetes CRD generation.
	return ctrl.Result{}, nil
}

func (r *GatewayClassConfigController) Logger(name types.NamespacedName) logr.Logger {
	return r.Log.WithValues("request", name)
}

func (r *GatewayClassConfigController) UpdateStatus(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	return r.Status().Update(ctx, obj, opts...)
}

func (r *GatewayClassConfigController) SetupWithManager(mgr ctrl.Manager) error {
	return setupWithManager(mgr, &meshv2beta1.GatewayClassConfig{}, r)
}
