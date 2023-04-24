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

	// GatewayClass status fields.
	invalidParameters = "InvalidParameters"
	accepted          = "Accepted"

	// GatewayClass status condition reasons.
	configurationAccepted = "ConfigurationAccepted"
	configurationInvalid  = "ConfigurationInvalid"
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

	if didUpdate, err := r.validateParametersRef(ctx, gc, log); didUpdate || err != nil {
		return ctrl.Result{}, err
	}

	// Update the status to Accepted=True.
	_, err = r.ensureCondition(ctx, gc, metav1.Condition{
		Type:    accepted,
		Status:  metav1.ConditionTrue,
		Reason:  configurationAccepted,
		Message: "Configuration accepted",
	})

	return ctrl.Result{}, err
}

// SetupWithManager registers the controller with the given manager.
func (r *GatewayClassController) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gwv1beta1.GatewayClass{}).
		// Watch for changes to GatewayClassConfig objects.
		Watches(source.NewKindWithCache(&v1alpha1.GatewayClassConfig{}, mgr.GetCache()), r.gatewayClassConfigFieldIndexEventHandler(ctx)).
		// Watch for changes to Gateway objects that reference this GatewayClass.
		Watches(source.NewKindWithCache(&gwv1beta1.Gateway{}, mgr.GetCache()), r.gatewayFieldIndexEventHandler(ctx)).
		Complete(r)
}

// isGatewayClassInUse returns true if the given GatewayClass is referenced by any Gateway objects.
func (r *GatewayClassController) isGatewayClassInUse(ctx context.Context, gc *gwv1beta1.GatewayClass) (bool, error) {
	list := &gwv1beta1.GatewayList{}
	if err := r.Client.List(ctx, list, &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(Gateway_GatewayClassIndex, gc.Name),
	}); err != nil {
		return false, err
	}

	return len(list.Items) != 0, nil
}

// validateParametersRef validates the ParametersRef field of the given GatewayClass
// if it is set, ensuring that the referenced object is a GatewayClassConfig that exists.
func (r *GatewayClassController) validateParametersRef(ctx context.Context, gc *gwv1beta1.GatewayClass, log logr.Logger) (didUpdate bool, err error) {
	parametersRef := gc.Spec.ParametersRef
	if parametersRef == nil {
		return false, nil
	}

	if parametersRef.Kind != v1alpha1.GatewayClassConfigKind {
		didUpdate, err := r.ensureCondition(ctx, gc, metav1.Condition{
			Type:    invalidParameters,
			Status:  metav1.ConditionTrue,
			Reason:  configurationInvalid,
			Message: fmt.Sprintf("Incorrect type for parametersRef. Expected GatewayClassConfig, got %q.", parametersRef.Kind),
		})
		if err != nil {
			log.Error(err, "unable to update status")
		}
		return didUpdate, err
	}

	err = r.Client.Get(ctx, types.NamespacedName{Name: parametersRef.Name}, &v1alpha1.GatewayClassConfig{})
	if k8serrors.IsNotFound(err) {
		didUpdate, err := r.ensureCondition(ctx, gc, metav1.Condition{
			Type:    invalidParameters,
			Status:  metav1.ConditionTrue,
			Reason:  configurationInvalid,
			Message: fmt.Sprintf("GatewayClassConfig not found %q.", parametersRef.Name),
		})
		if err != nil {
			log.Error(err, "unable to update status")
		}
		return didUpdate, err
	}
	if err != nil {
		log.Error(err, "unable to fetch GatewayClassConfig")
	}
	return false, err
}

// ensureCondition ensures that the given condition is set on the GatewayClass.
func (r *GatewayClassController) ensureCondition(ctx context.Context, gc *gwv1beta1.GatewayClass, condition metav1.Condition) (didUpdate bool, err error) {

	// Update a condition the GatewayClass already has.
	existsAt := -1
	for i, c := range gc.Status.Conditions {
		if condition.Type == c.Type {
			existsAt = i
		}
	}
	if 0 <= existsAt {
		// Don't update if there is no change.
		if equalConditions(condition, gc.Status.Conditions[existsAt]) {
			return false, nil
		}

		// Update and set the time of the update.
		gc.Status.Conditions[existsAt] = condition
		gc.Status.Conditions[existsAt].LastTransitionTime = metav1.Now()

		if err := r.Client.Status().Update(ctx, gc); err != nil {
			return false, err
		}

		return true, nil
	}

	// Add a new condition to the GatewayClass.
	gc.Status.Conditions = append(gc.Status.Conditions, condition)
	gc.Status.Conditions[len(gc.Status.Conditions)-1].LastTransitionTime = metav1.Now()

	if err := r.Client.Status().Update(ctx, gc); err != nil {
		return false, err
	}

	return true, nil

}

// gatewayClassConfigFieldIndexEventHandler returns an EventHandler that will enqueue
// reconcile.Requests for GatewayClass objects that reference the GatewayClassConfig
// object that triggered the event.
func (r *GatewayClassController) gatewayClassConfigFieldIndexEventHandler(ctx context.Context) handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(o client.Object) []reconcile.Request {
		requests := []reconcile.Request{}

		// Get all GatewayClass objects from the field index of the GatewayClassConfig which triggered the event.
		var gcList gwv1beta1.GatewayClassList
		err := r.Client.List(ctx, &gcList, &client.ListOptions{
			FieldSelector: fields.OneTermEqualSelector(GatewayClass_GatewayClassConfigIndex, o.GetName()),
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

// gatewayFieldIndexEventHandler returns an EventHandler that will enqueue
// reconcile.Requests for GatewayClass objects from Gateways which reference the GatewayClass
// when those Gateways are updated.
func (r *GatewayClassController) gatewayFieldIndexEventHandler(ctx context.Context) handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(o client.Object) []reconcile.Request {
		// Get the Gateway object that triggered the event.
		g := o.(*gwv1beta1.Gateway)

		// Return a slice with the single reconcile.Request for the GatewayClass
		// that the Gateway references.
		return []reconcile.Request{
			{
				NamespacedName: types.NamespacedName{
					Name: string(g.Spec.GatewayClassName),
				},
			},
		}
	})
}

func equalConditions(a, b metav1.Condition) bool {
	return a.Type == b.Type &&
		a.Status == b.Status &&
		a.Reason == b.Reason &&
		a.Message == b.Message
}
