package controller

import (
	"context"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	consulv1alpha1 "github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

// ExportedServicesController reconciles a ExportedServices object
type ExportedServicesController struct {
	client.Client
	Log                   logr.Logger
	Scheme                *runtime.Scheme
	ConfigEntryController *ConfigEntryController
}

// +kubebuilder:rbac:groups=consul.hashicorp.com,resources=exportedservices,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=consul.hashicorp.com,resources=exportedservices/status,verbs=get;update;patch

func (r *ExportedServicesController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return r.ConfigEntryController.ReconcileEntry(ctx, r, req, &consulv1alpha1.ExportedServices{})
}

func (r *ExportedServicesController) Logger(name types.NamespacedName) logr.Logger {
	return r.Log.WithValues("request", name)
}

func (r *ExportedServicesController) UpdateStatus(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	return r.Status().Update(ctx, obj, opts...)
}

func (r *ExportedServicesController) SetupWithManager(mgr ctrl.Manager) error {
	return setupWithManager(mgr, &consulv1alpha1.ExportedServices{}, r)
}
