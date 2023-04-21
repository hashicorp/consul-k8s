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
	GatewayClassControllerName = "hashicorp.com/consul-api-gateway-controller"

	gatewayClassFinalizer = "gateway-exists-finalizer.consul.hashicorp.com"
	invalidParameters     = "InvalidParameters"
)

// GatewayClassController reconciles a GatewayClass object.
// The GatewayClass is responsible for defining the behavior of API gateways
// which reference the given class.
type GatewayClassController struct {
	ControllerName string
	Log            logr.Logger
	client.Client
}

// Reconcile handles the reconciliation loop for GatewayClass objects.
func (r *GatewayClassController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("gatewayClass", req.NamespacedName)

	gc := &gwv1beta1.GatewayClass{}

	err := r.Client.Get(ctx, req.NamespacedName, gc)
	if err != nil {
		log.Error(err, "unable to get GatewayClass")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if string(gc.Spec.ControllerName) != r.ControllerName {
		// This GatewayClass is not for this controller.
		_, err := RemoveFinalizer(ctx, r.Client, gc, gatewayClassFinalizer)
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
		if _, err := RemoveFinalizer(ctx, r.Client, gc, gatewayClassFinalizer); err != nil {
			log.Error(err, "unable to remove finalizer")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// We are creating or updating the GatewayClass.
	didUpdate, err := EnsureFinalizer(ctx, r.Client, gc, gatewayClassFinalizer)
	if err != nil {
		log.Error(err, "unable to add finalizer")
		return ctrl.Result{}, err
	}
	if didUpdate {
		// We updated the GatewayClass, requeue to avoid another update.
		return ctrl.Result{}, nil
	}

	if err := r.validateParametersRef(ctx, gc, log); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *GatewayClassController) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gwv1beta1.GatewayClass{}).
		// Watch for changes to GatewayClassConfig objects.
		Watches(source.NewKindWithCache(&v1alpha1.GatewayClassConfig{}, mgr.GetCache()), r.gatewayClassConfigFieldIndexEventHandler(ctx)).
		// Watch for changes to Gateway objects that reference this GatewayClass.
		Watches(source.NewKindWithCache(&gwv1beta1.Gateway{}, mgr.GetCache()), r.gatewayFieldIndexEventHandler(ctx)).
		Complete(r)
}

func (r *GatewayClassController) isGatewayClassInUse(ctx context.Context, gc *gwv1beta1.GatewayClass) (bool, error) {
	list := &gwv1beta1.GatewayList{}
	if err := r.Client.List(ctx, list, &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(GatewayClassFieldIndex, gc.Name),
	}); err != nil {
		return false, err
	}

	return len(list.Items) != 0, nil
}

func (r *GatewayClassController) validateParametersRef(ctx context.Context, gc *gwv1beta1.GatewayClass, log logr.Logger) error {
	parametersRef := gc.Spec.ParametersRef
	if parametersRef == nil {
		return nil
	}

	if parametersRef.Kind != v1alpha1.GatewayClassConfigKind {
		_, err := r.ensureStatus(ctx, gc, invalidParameters, fmt.Sprintf("Incorrect type for parametersRef. Expected GatewayClassConfig, got %q.", parametersRef.Kind), "IncorrectGatewayKind")
		if err != nil {
			log.Error(err, "unable to update status")
		}
		return err
	}

	err := r.Client.Get(ctx, types.NamespacedName{Name: parametersRef.Name}, &v1alpha1.GatewayClassConfig{})
	if k8serrors.IsNotFound(err) {
		_, err := r.ensureStatus(ctx, gc, invalidParameters, fmt.Sprintf("GatewayClassConfig not found %q.", parametersRef.Name), "GatewayClassConfigNotFound")
		if err != nil {
			log.Error(err, "unable to update status")
		}
		return err
	}
	if err != nil {
		log.Error(err, "unable to fetch GatewayClassConfig")
		return err
	}
	return nil
}

func (r *GatewayClassController) ensureStatus(ctx context.Context, gc *gwv1beta1.GatewayClass, key, status, reason string) (didUpdate bool, err error) {
	conditionStatus := metav1.ConditionStatus(status)

	for _, condition := range gc.Status.Conditions {
		if condition.Type == key {
			if condition.Status != conditionStatus || condition.Reason != reason {
				// We need to update the status and/or reason.
				condition.Status = conditionStatus
				condition.Reason = reason
				condition.LastTransitionTime = metav1.Now()
			}

			if err := r.Client.Status().Update(ctx, gc); err != nil {
				return false, err
			}

			return true, nil
		}
	}
	return false, nil
}

func (r *GatewayClassController) gatewayClassConfigFieldIndexEventHandler(ctx context.Context) handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(o client.Object) []reconcile.Request {
		requests := []reconcile.Request{}

		// Get all GatewayClass objects from the field index.
		var gcList gwv1beta1.GatewayClassList
		err := r.Client.List(ctx, &gcList, &client.ListOptions{
			FieldSelector: fields.OneTermEqualSelector(GatewayClassConfigFieldIndex, o.GetName()),
		})
		if err != nil {
			r.Log.Error(err, "unable to list gateway classes")
		}

		// Create a reconcile request for each GatewayClass.
		for _, gc := range gcList.Items {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name: gc.Name,
				},
			})
		}

		return requests
	})
}

func (r *GatewayClassController) gatewayFieldIndexEventHandler(ctx context.Context) handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(o client.Object) []reconcile.Request {
		g := o.(*gwv1beta1.Gateway)
		return []reconcile.Request{
			{
				NamespacedName: types.NamespacedName{
					Name: string(g.Spec.GatewayClassName),
				},
			},
		}
	})
}
