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

// PartitionExportsController reconciles a PartitionExports object
type PartitionExportsController struct {
	client.Client
	Log                   logr.Logger
	Scheme                *runtime.Scheme
	ConfigEntryController *ConfigEntryController
}

// +kubebuilder:rbac:groups=consul.hashicorp.com,resources=partitionexports,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=consul.hashicorp.com,resources=partitionexports/status,verbs=get;update;patch

func (r *PartitionExportsController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return r.ConfigEntryController.ReconcileEntry(ctx, r, req, &consulv1alpha1.PartitionExports{})
}

func (r *PartitionExportsController) Logger(name types.NamespacedName) logr.Logger {
	return r.Log.WithValues("request", name)
}

func (r *PartitionExportsController) UpdateStatus(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	return r.Status().Update(ctx, obj, opts...)
}

func (r *PartitionExportsController) SetupWithManager(mgr ctrl.Manager) error {
	return setupWithManager(mgr, &consulv1alpha1.PartitionExports{}, r)
}
