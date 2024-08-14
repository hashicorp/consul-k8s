// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package configentries

import (
	"context"

	"k8s.io/apimachinery/pkg/types"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	consulv1alpha1 "github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

var _ Controller = (*SamenessGroupController)(nil)

// SamenessGroupController reconciles a SamenessGroups object.
type SamenessGroupController struct {
	client.Client
	FinalizerPatcher
	Log                   logr.Logger
	Scheme                *runtime.Scheme
	ConfigEntryController *ConfigEntryController
}

//+kubebuilder:rbac:groups=consul.hashicorp.com,resources=samenessgroups,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=consul.hashicorp.com,resources=samenessgroups/status,verbs=get;update;patch

func (r *SamenessGroupController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return r.ConfigEntryController.ReconcileEntry(ctx, r, req, &consulv1alpha1.SamenessGroup{})
}

func (r *SamenessGroupController) Logger(name types.NamespacedName) logr.Logger {
	return r.Log.WithValues("request", name)
}

func (r *SamenessGroupController) UpdateStatus(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	return r.Status().Update(ctx, obj, opts...)
}

// SetupWithManager sets up the controller with the Manager.
func (r *SamenessGroupController) SetupWithManager(mgr ctrl.Manager) error {
	return setupWithManager(mgr, &consulv1alpha1.SamenessGroup{}, r)
}
