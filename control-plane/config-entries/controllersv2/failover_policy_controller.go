// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package controllersv2

import (
	"context"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	catalogv2beta1 "github.com/hashicorp/consul-k8s/control-plane/api/catalog/v2beta1"
)

// FailoverPolicyController reconciles a FailoverPolicy object.
type FailoverPolicyController struct {
	client.Client
	Log                  logr.Logger
	Scheme               *runtime.Scheme
	MeshConfigController *MeshConfigController
}

// +kubebuilder:rbac:groups=catalog.consul.hashicorp.com,resources=failoverpolicy,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=catalog.consul.hashicorp.com,resources=failoverpolicy/status,verbs=get;update;patch

func (r *FailoverPolicyController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return r.MeshConfigController.ReconcileEntry(ctx, r, req, &catalogv2beta1.FailoverPolicy{})
}

func (r *FailoverPolicyController) Logger(name types.NamespacedName) logr.Logger {
	return r.Log.WithValues("request", name)
}

func (r *FailoverPolicyController) UpdateStatus(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	return r.Status().Update(ctx, obj, opts...)
}

func (r *FailoverPolicyController) SetupWithManager(mgr ctrl.Manager) error {
	return setupWithManager(mgr, &catalogv2beta1.FailoverPolicy{}, r)
}
