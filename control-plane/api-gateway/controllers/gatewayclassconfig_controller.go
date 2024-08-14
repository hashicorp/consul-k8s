// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package controllers

import (
	"context"
	"time"

	"github.com/go-logr/logr"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

const (
	gatewayClassConfigFinalizer = "gateway-class-exists-finalizer.consul.hashicorp.com"
)

// The GatewayClassConfigController manages the state of GatewayClassConfigs.
type GatewayClassConfigController struct {
	client.Client

	Log logr.Logger
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.8.3/pkg/reconcile
func (r *GatewayClassConfigController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("gatewayClassConfig", req.NamespacedName.Name)
	log.V(1).Info("Reconciling GatewayClassConfig ")

	gcc := &v1alpha1.GatewayClassConfig{}
	if err := r.Client.Get(ctx, req.NamespacedName, gcc); err != nil {
		if k8serrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		log.Error(err, "failed to get gateway class config")
		return ctrl.Result{}, err
	}

	if !gcc.ObjectMeta.DeletionTimestamp.IsZero() {
		// We have a deletion, ensure we're not in use.
		used, err := gatewayClassConfigInUse(ctx, r.Client, gcc)
		if err != nil {
			log.Error(err, "failed to check if the gateway class config is still in use")
			return ctrl.Result{}, err
		}
		if used {
			log.Info("gateway class config still in use")
			// Requeue as to not block the reconciliation loop.
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
		// gcc is no longer in use.
		if _, err := RemoveFinalizer(ctx, r.Client, gcc, gatewayClassConfigFinalizer); err != nil {
			if k8serrors.IsConflict(err) {
				log.V(1).Info("error removing gateway class config finalizer, will try to re-reconcile")
				return ctrl.Result{Requeue: true}, nil
			}
			log.Error(err, "error removing gateway class config finalizer")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	if _, err := EnsureFinalizer(ctx, r.Client, gcc, gatewayClassConfigFinalizer); err != nil {
		if k8serrors.IsConflict(err) {
			log.V(1).Info("error adding gateway class config finalizer, will try to re-reconcile")

			return ctrl.Result{Requeue: true}, nil
		}
		log.Error(err, "error adding gateway class config finalizer")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// gatewayClassUsesConfig determines whether a given GatewayClass references a
// given GatewayClassConfig. Since these resources are scoped to the cluster,
// namespace is not considered.
func gatewayClassUsesConfig(gc gwv1beta1.GatewayClass, gcc *v1alpha1.GatewayClassConfig) bool {
	parameterRef := gc.Spec.ParametersRef
	return parameterRef != nil &&
		string(parameterRef.Group) == v1alpha1.ConsulHashicorpGroup &&
		parameterRef.Kind == v1alpha1.GatewayClassConfigKind &&
		parameterRef.Name == gcc.Name
}

// GatewayClassConfigInUse determines whether any GatewayClass in the cluster
// references the provided GatewayClassConfig.
func gatewayClassConfigInUse(ctx context.Context, k8sClient client.Client, gcc *v1alpha1.GatewayClassConfig) (bool, error) {
	list := &gwv1beta1.GatewayClassList{}
	if err := k8sClient.List(ctx, list); err != nil {
		return false, err
	}

	for _, gc := range list.Items {
		if gatewayClassUsesConfig(gc, gcc) {
			return true, nil
		}
	}

	return false, nil
}

func (r *GatewayClassConfigController) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.GatewayClassConfig{}).
		// Watch for changes to GatewayClass objects associated with this config for purposes of finalizer removal.
		Watches(&gwv1beta1.GatewayClass{}, r.transformGatewayClassToGatewayClassConfig()).
		Complete(r)
}

func (r *GatewayClassConfigController) transformGatewayClassToGatewayClassConfig() handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []reconcile.Request {
		gc := o.(*gwv1beta1.GatewayClass)

		pr := gc.Spec.ParametersRef
		if pr != nil && pr.Kind == v1alpha1.GatewayClassConfigKind {
			return []reconcile.Request{{
				NamespacedName: types.NamespacedName{
					Name: pr.Name,
				},
			}}
		}

		return nil
	})
}
