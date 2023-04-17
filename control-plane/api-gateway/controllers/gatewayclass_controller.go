package controllers

import (
	"context"
	"time"

	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

const (
	GatewayClassFinalizer      = "gateway-exists-finalizer.networking.x-k8s.io"
	GatewayClassControllerName = "hashicorp.com/consul-api-gateway-controller"
)

// GatewayClassReconciler reconciles a GatewayClass object.
// The GatewayClass is responsible for defining the behavior of API gateways
// which reference the given class.
type GatewayClassReconciler struct {
	ControllerName string
	Log            logr.Logger
	client.Client
}

// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gatewayclasses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gatewayclasses/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gatewayclasses/finalizers,verbs=update

// Reconcile handles the reconciliation loop for GatewayClass objects.
func (r *GatewayClassReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("gatewayClass", req.NamespacedName)

	gc := &gwv1beta1.GatewayClass{}

	err := r.Client.Get(ctx, req.NamespacedName, gc)
	if err != nil {
		log.Error(err, "unable to get GatewayClass")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if string(gc.Spec.ControllerName) != r.ControllerName {
		// This GatewayClass is not for this controller.
		return ctrl.Result{}, nil
	}

	if !gc.ObjectMeta.DeletionTimestamp.IsZero() {
		// We have a deletion request. Ensure we are not in use.
		used, err := r.isGatewayClassInUse(ctx, gc)
		if err != nil {
			log.Error(err, "unable to check if GatewayClass is in use")
			return ctrl.Result{}, err
		}
		if used {
			// Requeue after 10 seconds to check again.
			log.Info("GatewayClass is in use, cannot delete")
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
		// Remove our finalizer.
		if _, err := RemoveFinalizer(ctx, r.Client, gc, GatewayClassFinalizer); err != nil {
			log.Error(err, "unable to remove finalizer")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// We are creating or updating the GatewayClass.
	didUpdate, err := EnsureFinalizer(ctx, r.Client, gc, GatewayClassFinalizer)
	if err != nil {
		log.Error(err, "unable to add finalizer")
		return ctrl.Result{}, err
	}
	if didUpdate {
		// We updated the GatewayClass, requeue to avoid another update.
		return ctrl.Result{Requeue: true}, nil
	}

	return ctrl.Result{}, nil
}

func (r *GatewayClassReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gwv1beta1.GatewayClass{}).
		Complete(r)
}

func (r *GatewayClassReconciler) isGatewayClassInUse(ctx context.Context, gc *gwv1beta1.GatewayClass) (bool, error) {
	list := &gwv1beta1.GatewayList{}
	if err := r.Client.List(ctx, list); err != nil {
		return false, err
	}

	for _, gw := range list.Items {
		if string(gw.Spec.GatewayClassName) == gc.Name {
			return true, nil
		}
	}

	return false, nil
}
