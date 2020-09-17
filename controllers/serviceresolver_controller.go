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

// ServiceResolverReconciler reconciles a ServiceResolver object
type ServiceResolverReconciler struct {
	client.Client
	Log                   logr.Logger
	Scheme                *runtime.Scheme
	ConfigEntryReconciler *ConfigEntryReconciler
}

// +kubebuilder:rbac:groups=consul.hashicorp.com,resources=serviceresolvers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=consul.hashicorp.com,resources=serviceresolvers/status,verbs=get;update;patch

func (r *ServiceResolverReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	var svcResolver consulv1alpha1.ServiceResolver
	return r.ConfigEntryReconciler.Reconcile(r, req, &svcResolver)
}

func (r *ServiceResolverReconciler) Logger(name types.NamespacedName) logr.Logger {
	return r.Log.WithValues("controller", "serviceresolver", "request", name)
}

func (r *ServiceResolverReconciler) UpdateStatus(ctx context.Context, obj runtime.Object, opts ...client.UpdateOption) error {
	return r.Status().Update(ctx, obj, opts...)
}

func (r *ServiceResolverReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&consulv1alpha1.ServiceResolver{}).
		Complete(r)
}
