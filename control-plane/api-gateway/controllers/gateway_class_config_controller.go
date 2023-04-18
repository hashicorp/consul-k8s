// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package controllers

import (
	"context"
	"time"

	"github.com/go-logr/logr"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

const (
	gatewayClassConfigFinalizer = "gateway-class-exists-finalizer.consul.hashicorp.com"
)

// The GatewayClassConfigController manages the state of GatewayClassConfigs
type GatewayClassConfigController struct {
	client.Client

	Log logr.Logger
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.8.3/pkg/reconcile
func (r *GatewayClassConfigController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Log.Info("Reconciling the Gateway Class Config GatewayClassConfigController", "name", req.Name)

	gcc := &v1alpha1.GatewayClassConfig{}
	if err := r.Client.Get(ctx, req.NamespacedName, gcc); err != nil {
		if k8serrors.IsNotFound(err) {
			r.Log.Info("gateway class not found")
			return ctrl.Result{}, nil
		}
		r.Log.Error(err, "failed to get gateway class config")
		return ctrl.Result{}, err
	}

	if !gcc.ObjectMeta.DeletionTimestamp.IsZero() {
		// We have a deletion, ensure we're not in use.
		used, err := gatewayClassConfigInUse(ctx, r.Client, gcc)
		if err != nil {
			r.Log.Error(err, "failed to check if the gateway class config is still in use")
			return ctrl.Result{}, err
		}
		if used {
			r.Log.Info("gateway class config still in use")
			// Requeue as to not block the reconciliation loop.
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
		// gcc is no longer in use.
		if _, err := removeFinalizer(ctx, r.Client, gcc, gatewayClassConfigFinalizer); err != nil {
			r.Log.Error(err, "error removing gateway class config finalizer")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	if _, err := ensureFinalizer(ctx, r.Client, gcc, gatewayClassConfigFinalizer); err != nil {
		r.Log.Error(err, "error adding gateway class config finalizer")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// EnsureFinalizer ensures that the given finalizer is added to the passed
// object if it does not already exist on the object
// it returns a boolean saying whether a finalizer was added, and any
// potential errors.
func ensureFinalizer(ctx context.Context, k8sClient client.Client, object client.Object, finalizer string) (bool, error) {
	finalizers := object.GetFinalizers()
	for _, f := range finalizers {
		if f == finalizer {
			return false, nil
		}
	}
	object.SetFinalizers(append(finalizers, finalizer))
	if err := k8sClient.Update(ctx, object); err != nil {
		return false, err
	}
	return true, nil
}

// RemoveFinalizer ensures that the given finalizer is removed from the passed object
// it returns a boolean saying whether a finalizer was removed, and any
// potential errors.
func removeFinalizer(ctx context.Context, k8sClient client.Client, object client.Object, finalizer string) (bool, error) {
	finalizers := []string{}
	found := false
	for _, f := range object.GetFinalizers() {
		if f == finalizer {
			found = true
			continue
		}
		finalizers = append(finalizers, f)
	}
	if found {
		object.SetFinalizers(finalizers)
		if err := k8sClient.Update(ctx, object); err != nil {
			return false, err
		}
	}
	return found, nil
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

func (r *GatewayClassConfigController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.GatewayClassConfig{}).
		Complete(r)
}
