package controllers

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

const (
	GatewayClassFinalizer      = "gateway-exists-finalizer.consul.hashicorp.com"
	GatewayClassControllerName = "hashicorp.com/consul-api-gateway-controller"

	InvalidParameters = "InvalidParameters"

	gatewayClassConfigFieldIndex = "__gatewayclassconfig"
	gatewayClassFieldIndex       = "__gatewayclass"
)

// GatewayClassReconciler reconciles a GatewayClass object.
// The GatewayClass is responsible for defining the behavior of API gateways
// which reference the given class.
type GatewayClassReconciler struct {
	ControllerName string
	Log            logr.Logger
	client.Client
}

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
		_, err := RemoveFinalizer(ctx, r.Client, gc, GatewayClassFinalizer)
		if err != nil {
			log.Error(err, "unable to remove finalizer")
		}

		return ctrl.Result{}, err
	}

	if !gc.ObjectMeta.DeletionTimestamp.IsZero() {
		// We have a deletion request. Ensure we are not in use.
		used, err := r.isGatewayClassInUse(ctx, gc)
		if err != nil {
			log.Error(err, "unable to check if GatewayClass is in use")
			return ctrl.Result{}, err
		}
		if used {
			log.Info("GatewayClass is in use, cannot delete")
			return ctrl.Result{}, nil
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
		return ctrl.Result{}, nil
	}

	// Check that the parametersRef is valid.
	pr := gc.Spec.ParametersRef
	if pr != nil {
		if pr.Kind != v1alpha1.GatewayClassConfigKind {
			_, err := r.ensureStatus(ctx, gc, InvalidParameters, fmt.Sprintf("Incorrect type for parametersRef. Expected GatewayClassConfig, got %q.", pr.Kind))
			if err != nil {
				log.Error(err, "unable to update status")
			}
			return ctrl.Result{}, err
		}

		err := r.Client.Get(ctx, types.NamespacedName{Name: pr.Name}, &v1alpha1.GatewayClassConfig{})
		if k8serrors.IsNotFound(err) {
			_, err := r.ensureStatus(ctx, gc, InvalidParameters, fmt.Sprintf("GatewayClassConfig not found %q.", pr.Name))
			if err != nil {
				log.Error(err, "unable to update status")
			}
			return ctrl.Result{}, err
		}
		if err != nil {
			log.Error(err, "unable to fetch GatewayClassConfig")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *GatewayClassReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {

	if err := mgr.GetFieldIndexer().IndexField(ctx, &gwv1beta1.GatewayClass{}, gatewayClassConfigFieldIndex, func(o client.Object) []string {
		gc := o.(*gwv1beta1.GatewayClass)

		pr := gc.Spec.ParametersRef
		if pr != nil && pr.Kind == v1alpha1.GatewayClassConfigKind {
			return []string{pr.Name}
		}

		return []string{}
	}); err != nil {
		return err
	}

	if err := mgr.GetFieldIndexer().IndexField(ctx, &gwv1beta1.Gateway{}, gatewayClassFieldIndex, func(o client.Object) []string {
		g := o.(*gwv1beta1.Gateway)
		return []string{string(g.Spec.GatewayClassName)}
	}); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&gwv1beta1.GatewayClass{}).
		// Watch for changes to GatewayClassConfig objects.
		Watches(source.NewKindWithCache(&v1alpha1.GatewayClassConfig{}, mgr.GetCache()), handler.EnqueueRequestsFromMapFunc(
			func(o client.Object) []reconcile.Request {
				requests := []reconcile.Request{}

				var gcList gwv1beta1.GatewayClassList
				err := r.Client.List(ctx, &gcList, &client.ListOptions{
					FieldSelector: fields.OneTermEqualSelector(gatewayClassConfigFieldIndex, o.GetName()),
				})
				if err != nil {
					r.Log.Error(err, "unable to list gateway classes")
				}

				for _, gc := range gcList.Items {
					requests = append(requests, reconcile.Request{
						NamespacedName: types.NamespacedName{
							Name: gc.Name,
						},
					})
				}

				return requests
			})).
		// Watch for changes to Gateways that reference this GatewayClass.
		Watches(source.NewKindWithCache(&gwv1beta1.Gateway{}, mgr.GetCache()), handler.EnqueueRequestsFromMapFunc(
			func(o client.Object) []reconcile.Request {
				g := o.(*gwv1beta1.Gateway)
				return []reconcile.Request{
					{
						NamespacedName: types.NamespacedName{
							Name: string(g.Spec.GatewayClassName),
						},
					},
				}
			})).
		Complete(r)
}

func (r *GatewayClassReconciler) isGatewayClassInUse(ctx context.Context, gc *gwv1beta1.GatewayClass) (bool, error) {
	list := &gwv1beta1.GatewayList{}
	if err := r.Client.List(ctx, list, &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(gatewayClassFieldIndex, gc.Name),
	}); err != nil {
		return false, err
	}

	return len(list.Items) != 0, nil
}

func (r *GatewayClassReconciler) ensureStatus(ctx context.Context, gc *gwv1beta1.GatewayClass, key, status string) (didUpdate bool, err error) {
	conditionStatus := metav1.ConditionStatus(status)

	for _, condition := range gc.Status.Conditions {
		if condition.Type == key {
			if condition.Status == conditionStatus {
				// We already have the correct status.
				return false, nil
			}
			condition.Status = conditionStatus
			condition.LastTransitionTime = metav1.Now()

			if err := r.Client.Status().Update(ctx, gc); err != nil {
				return false, err
			}

			return true, nil
		}
	}
	return false, nil
}
