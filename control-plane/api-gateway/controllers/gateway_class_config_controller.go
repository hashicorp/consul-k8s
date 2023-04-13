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

type Controller struct {
	client.Client

	Log logr.Logger
}

// Reconcile reads the state of an Endpoints object for a Kubernetes Service and reconciles Consul services which
// correspond to the Kubernetes Service. These events are driven by changes to the Pods backing the Kube service.
func (r *Controller) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Log.Info("Reconcile the Gateway Class Config Controller", "name", req.Name, "namespace", req.Namespace)

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
		// we have a deletion, ensure we're not in use
		used, err := r.GatewayClassConfigInUse(ctx, gcc)
		if err != nil {
			r.Log.Error(err, "failed to check if the gateway class config is still in use")
			return ctrl.Result{}, err
		}
		if used {
			r.Log.Info("gateway class config still in use")
			// requeue as to not block the reconciliation loop
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
		// gcc is no longer in use
		if _, err := r.RemoveFinalizer(ctx, gcc, gatewayClassConfigFinalizer); err != nil {
			r.Log.Error(err, "error removing gateway class config finalizer")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	if _, err := r.EnsureFinalizer(ctx, gcc, gatewayClassConfigFinalizer); err != nil {
		r.Log.Error(err, "error adding gateway class config finalizer")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *Controller) EnsureFinalizer(ctx context.Context, object client.Object, finalizer string) (bool, error) {
	finalizers := object.GetFinalizers()
	for _, f := range finalizers {
		if f == finalizer {
			return false, nil
		}
	}
	object.SetFinalizers(append(finalizers, finalizer))
	if err := r.Update(ctx, object); err != nil {
		return false, err
	}
	return true, nil
}

// RemoveFinalizer ensures that the given finalizer is removed from the passed object
// it returns a boolean saying whether a finalizer was removed, and any
// potential errors
func (r *Controller) RemoveFinalizer(ctx context.Context, object client.Object, finalizer string) (bool, error) {
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
		if err := r.Update(ctx, object); err != nil {
			return false, err
		}
	}
	return found, nil
}

// gatewayClassUsesConfig determines whether a given GatewayClass references a
// given GatewayClassConfig. Since these resources are scoped to the cluster,
// namespace is not considered.
func gatewayClassUsesConfig(gc gwv1beta1.GatewayClass, gcc *v1alpha1.GatewayClassConfig) bool {
	paramaterRef := gc.Spec.ParametersRef
	return paramaterRef != nil &&
		string(paramaterRef.Group) == v1alpha1.ConsulHashicorpGroup &&
		paramaterRef.Kind == v1alpha1.GatewayClassConfigKind &&
		paramaterRef.Name == gcc.Name
}

// GatewayClassConfigInUse determines whether any GatewayClass in the cluster
// references the provided GatewayClassConfig.
func (g *Controller) GatewayClassConfigInUse(ctx context.Context, gcc *v1alpha1.GatewayClassConfig) (bool, error) {
	list := &gwv1beta1.GatewayClassList{}
	if err := g.List(ctx, list); err != nil {
		return false, err
	}

	for _, gc := range list.Items {
		if gatewayClassUsesConfig(gc, gcc) {
			return true, nil
		}
	}

	return false, nil
}

func (r *Controller) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.GatewayClassConfig{}).
		Complete(r)
}
