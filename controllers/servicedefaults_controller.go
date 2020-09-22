package controllers

import (
	"context"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	consulv1alpha1 "github.com/hashicorp/consul-k8s/api/v1alpha1"
)

// ServiceDefaultsController is the controller for ServiceDefaults resources.
type ServiceDefaultsController struct {
	client.Client
	Log                   logr.Logger
	Scheme                *runtime.Scheme
	ConfigEntryController *ConfigEntryController
}

// +kubebuilder:rbac:groups=consul.hashicorp.com,resources=servicedefaults,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=consul.hashicorp.com,resources=servicedefaults/status,verbs=get;update;patch

func (r *ServiceDefaultsController) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	return r.ConfigEntryController.ReconcileEntry(r, req, &consulv1alpha1.ServiceDefaults{})
}

func (r *ServiceDefaultsController) Logger(name types.NamespacedName) logr.Logger {
	return r.Log.WithValues("request", name)
}

func (r *ServiceDefaultsController) UpdateStatus(ctx context.Context, obj runtime.Object, opts ...client.UpdateOption) error {
	return r.Status().Update(ctx, obj, opts...)
}

func (r *ServiceDefaultsController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&consulv1alpha1.ServiceDefaults{}).
		Complete(r)
}
