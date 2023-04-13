package controllers

import (
	"context"
	"k8s.io/apimachinery/pkg/types"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	consulv1alpha1 "github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

// SamenessGroupsController reconciles a SamenessGroups object.
type SamenessGroupsController struct {
	client.Client
	Log                   logr.Logger
	Scheme                *runtime.Scheme
	ConfigEntryController *ConfigEntryController
}

//+kubebuilder:rbac:groups=consul.hashicorp.com,resources=samenessgroups,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=consul.hashicorp.com,resources=samenessgroups/status,verbs=get;update;patch

func (r *SamenessGroupsController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return r.ConfigEntryController.ReconcileEntry(ctx, r, req, &consulv1alpha1.SamenessGroups{})
}

func (r *SamenessGroupsController) Logger(name types.NamespacedName) logr.Logger {
	return r.Log.WithValues("request", name)
}

func (r *SamenessGroupsController) UpdateStatus(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	return r.Status().Update(ctx, obj, opts...)
}

// SetupWithManager sets up the controller with the Manager.
func (r *SamenessGroupsController) SetupWithManager(mgr ctrl.Manager) error {
	return setupWithManager(mgr, &consulv1alpha1.SamenessGroups{}, r)
}
