// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package controllers

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

const (
	gatewayClassFinalizer = "gateway-exists-finalizer.consul.hashicorp.com"

	// GatewayClass status fields.
	accepted          = "Accepted"
	invalidParameters = "InvalidParameters"
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
	log := r.Log.WithValues("gatewayClass", req.NamespacedName.Name)
	log.V(1).Info("Reconciling GatewayClass")

	gc := &gwv1beta1.GatewayClass{}

	err := r.Client.Get(ctx, req.NamespacedName, gc)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		log.Error(err, "unable to get GatewayClass")
		return ctrl.Result{}, err
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
			if k8serrors.IsConflict(err) {
				log.V(1).Info("error removing finalizer for gatewayClass, will try to re-reconcile")

				return ctrl.Result{Requeue: true}, nil
			}
			log.Error(err, "unable to remove finalizer")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// We are creating or updating the GatewayClass.
	didUpdate, err := EnsureFinalizer(ctx, r.Client, gc, gatewayClassFinalizer)
	if err != nil {
		if k8serrors.IsConflict(err) {
			log.V(1).Info("error adding finalizer for gatewayClass, will try to re-reconcile")

			return ctrl.Result{Requeue: true}, nil
		}
		log.Error(err, "unable to add finalizer")
		return ctrl.Result{}, err
	}
	if didUpdate {
		// We updated the GatewayClass, requeue to avoid another update.
		return ctrl.Result{}, nil
	}

	didUpdate, err = r.validateParametersRef(ctx, gc, log)
	if didUpdate {
		if err := r.Client.Status().Update(ctx, gc); err != nil {
			if k8serrors.IsConflict(err) {
				log.V(1).Info("error updating status for gatewayClass, will try to re-reconcile")

				return ctrl.Result{Requeue: true}, nil
			}
			log.Error(err, "unable to update status for GatewayClass")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}
	if err != nil {
		log.Error(err, "unable to validate ParametersRef")
	}

	return ctrl.Result{}, err
}

// SetupWithManager registers the controller with the given manager.
func (r *GatewayClassController) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gwv1beta1.GatewayClass{}).
		// Watch for changes to GatewayClassConfig objects.
		Watches(&v1alpha1.GatewayClassConfig{}, r.gatewayClassConfigFieldIndexEventHandler()).
		// Watch for changes to Gateway objects that reference this GatewayClass.
		Watches(&gwv1beta1.Gateway{}, r.gatewayFieldIndexEventHandler()).
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
	if parametersRef != nil {
		if parametersRef.Kind != v1alpha1.GatewayClassConfigKind {
			didUpdate = r.setCondition(gc, metav1.Condition{
				Type:    accepted,
				Status:  metav1.ConditionFalse,
				Reason:  invalidParameters,
				Message: fmt.Sprintf("Incorrect type for parametersRef. Expected GatewayClassConfig, got %q.", parametersRef.Kind),
			})
			return didUpdate, nil
		}

		err = r.Client.Get(ctx, types.NamespacedName{Name: parametersRef.Name}, &v1alpha1.GatewayClassConfig{})
		if k8serrors.IsNotFound(err) {
			didUpdate := r.setCondition(gc, metav1.Condition{
				Type:    accepted,
				Status:  metav1.ConditionFalse,
				Reason:  invalidParameters,
				Message: fmt.Sprintf("GatewayClassConfig not found %q.", parametersRef.Name),
			})
			return didUpdate, nil
		}
		if err != nil {
			log.Error(err, "unable to fetch GatewayClassConfig")
			return false, err
		}
	}

	didUpdate = r.setCondition(gc, metav1.Condition{
		Type:    accepted,
		Status:  metav1.ConditionTrue,
		Reason:  accepted,
		Message: "GatewayClass Accepted",
	})

	return didUpdate, err
}

// setCondition sets the given condition on the given GatewayClass.
func (r *GatewayClassController) setCondition(gc *gwv1beta1.GatewayClass, condition metav1.Condition) (didUpdate bool) {
	condition.LastTransitionTime = metav1.Now()
	condition.ObservedGeneration = gc.GetGeneration()

	// Set the condition if it already exists.
	for i, c := range gc.Status.Conditions {
		if c.Type == condition.Type {
			// The condition already exists and is up to date.
			if equalConditions(condition, c) {
				return false
			}

			gc.Status.Conditions[i] = condition

			return true
		}
	}

	// Append the condition if it does not exist.
	gc.Status.Conditions = append(gc.Status.Conditions, condition)

	return true
}

// gatewayClassConfigFieldIndexEventHandler returns an EventHandler that will enqueue
// reconcile.Requests for GatewayClass objects that reference the GatewayClassConfig
// object that triggered the event.
func (r *GatewayClassController) gatewayClassConfigFieldIndexEventHandler() handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []reconcile.Request {
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
func (r *GatewayClassController) gatewayFieldIndexEventHandler() handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []reconcile.Request {
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
		a.Message == b.Message &&
		a.ObservedGeneration == b.ObservedGeneration
}
