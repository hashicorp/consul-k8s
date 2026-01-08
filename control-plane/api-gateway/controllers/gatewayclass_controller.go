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

	gwv1beta1exp "sigs.k8s.io/gateway-api-exp/apis/v1beta1"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

//
// ─────────────────────────────────────────────────────────────
//  Constants & types
// ─────────────────────────────────────────────────────────────
//

type MigrationMode string

const (
	PreferBeta MigrationMode = "PreferBeta"
	PreferExp  MigrationMode = "PreferExp"

	ConditionAccepted = "Accepted"
	ReasonInvalid     = "InvalidParameters"
	ReasonSuperseded  = "Superseded"
)

//
// ─────────────────────────────────────────────────────────────
//  Controller-owned model
// ─────────────────────────────────────────────────────────────
//

type GatewayClassParametersRef struct {
	Group string
	Kind  string
	Name  string
}

//
// ─────────────────────────────────────────────────────────────
//  Adapter interface
// ─────────────────────────────────────────────────────────────
//

type GatewayClassAdapter interface {
	GetName() string
	GetControllerName() string
	GetGeneration() int64
	GetParametersRef() *GatewayClassParametersRef
	SetCondition(cond metav1.Condition) bool
	GetObject() client.Object
}

//
// ─────────────────────────────────────────────────────────────
//  v1beta1 adapter
// ─────────────────────────────────────────────────────────────
//

type GatewayClassV1Beta1Adapter struct {
	*gwv1beta1.GatewayClass
}

func (a *GatewayClassV1Beta1Adapter) GetName() string {
	return a.Name
}
func (a *GatewayClassV1Beta1Adapter) GetControllerName() string {
	return string(a.Spec.ControllerName)
}
func (a *GatewayClassV1Beta1Adapter) GetGeneration() int64 {
	return a.Generation
}
func (a *GatewayClassV1Beta1Adapter) GetParametersRef() *GatewayClassParametersRef {
	ref := a.Spec.ParametersRef
	if ref == nil {
		return nil
	}
	return &GatewayClassParametersRef{
		Group: string(ref.Group),
		Kind:  string(ref.Kind),
		Name:  string(ref.Name),
	}
}
func (a *GatewayClassV1Beta1Adapter) GetObject() client.Object {
	return a.GatewayClass
}
func (a *GatewayClassV1Beta1Adapter) SetCondition(cond metav1.Condition) bool {
	cond.ObservedGeneration = a.Generation
	return setOrUpdateCondition(&a.Status.Conditions, cond)
}

//
// ─────────────────────────────────────────────────────────────
//  v1beta1exp adapter
// ─────────────────────────────────────────────────────────────
//

type GatewayClassV1Beta1ExpAdapter struct {
	*gwv1beta1exp.GatewayClass
}

func (a *GatewayClassV1Beta1ExpAdapter) GetName() string {
	return a.Name
}
func (a *GatewayClassV1Beta1ExpAdapter) GetControllerName() string {
	return string(a.Spec.ControllerName)
}
func (a *GatewayClassV1Beta1ExpAdapter) GetGeneration() int64 {
	return a.Generation
}
func (a *GatewayClassV1Beta1ExpAdapter) GetParametersRef() *GatewayClassParametersRef {
	ref := a.Spec.ParametersRef
	if ref == nil {
		return nil
	}
	return &GatewayClassParametersRef{
		Group: string(ref.Group),
		Kind:  string(ref.Kind),
		Name:  string(ref.Name),
	}
}
func (a *GatewayClassV1Beta1ExpAdapter) GetObject() client.Object {
	return a.GatewayClass
}
func (a *GatewayClassV1Beta1ExpAdapter) SetCondition(cond metav1.Condition) bool {
	cond.ObservedGeneration = a.Generation
	return setOrUpdateCondition(&a.Status.Conditions, cond)
}

//
// ─────────────────────────────────────────────────────────────
//  Authority resolution
// ─────────────────────────────────────────────────────────────
//

func ResolveAuthoritativeGatewayClass(
	beta *gwv1beta1.GatewayClass,
	exp *gwv1beta1exp.GatewayClass,
	mode MigrationMode,
) GatewayClassAdapter {

	if mode == PreferExp && exp != nil {
		return &GatewayClassV1Beta1ExpAdapter{exp}
	}
	if beta != nil {
		return &GatewayClassV1Beta1Adapter{beta}
	}
	if exp != nil {
		return &GatewayClassV1Beta1ExpAdapter{exp}
	}
	return nil
}

//
// ─────────────────────────────────────────────────────────────
//  Validation (replaces validateParametersRef)
// ─────────────────────────────────────────────────────────────
//

func ValidateGatewayClass(
	ctx context.Context,
	c client.Client,
	adapter GatewayClassAdapter,
	controllerName string,
) (bool, string, error) {

	if adapter.GetControllerName() != controllerName {
		return false, "Not owned by this controller", nil
	}

	ref := adapter.GetParametersRef()
	if ref == nil {
		return true, "Accepted", nil
	}

	if ref.Kind != v1alpha1.GatewayClassConfigKind {
		return false, "Invalid ParametersRef kind", nil
	}

	if ref.Group != v1alpha1.GroupVersion.Group {
		return false, "Invalid ParametersRef group", nil
	}

	err := c.Get(ctx, types.NamespacedName{Name: ref.Name}, &v1alpha1.GatewayClassConfig{})
	if k8serrors.IsNotFound(err) {
		return false, "GatewayClassConfig not found", nil
	}

	return true, "Accepted", err
}

//
// ─────────────────────────────────────────────────────────────
//  Condition helper (replaces setCondition + equalConditions)
// ─────────────────────────────────────────────────────────────
//

func setOrUpdateCondition(conds *[]metav1.Condition, cond metav1.Condition) bool {
	cond.LastTransitionTime = metav1.Now()

	for i, existing := range *conds {
		if existing.Type != cond.Type {
			continue
		}
		if existing.Status == cond.Status &&
			existing.Reason == cond.Reason &&
			existing.Message == cond.Message &&
			existing.ObservedGeneration == cond.ObservedGeneration {
			return false
		}
		(*conds)[i] = cond
		return true
	}

	*conds = append(*conds, cond)
	return true
}

//
// ─────────────────────────────────────────────────────────────
//  Controller
// ─────────────────────────────────────────────────────────────
//

type GatewayClassController struct {
	client.Client
	Log            logr.Logger
	ControllerName string
	MigrationMode  MigrationMode
}

//
// ─────────────────────────────────────────────────────────────
//  Lifecycle helper (FROM OLD CODE – REQUIRED)
// ─────────────────────────────────────────────────────────────
//

// isGatewayClassInUse returns true if any Gateway references this GatewayClass
func (r *GatewayClassController) isGatewayClassInUse(
	ctx context.Context,
	gcName string,
) (bool, error) {

	var gateways gwv1beta1.GatewayList
	if err := r.List(ctx, &gateways, &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(
			Gateway_GatewayClassIndex,
			gcName,
		),
	}); err != nil {
		return false, err
	}

	return len(gateways.Items) > 0, nil
}

//
// ─────────────────────────────────────────────────────────────
//  Reconcile
// ─────────────────────────────────────────────────────────────
//

func (r *GatewayClassController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {

	log := r.Log.WithValues("gatewayClass", req.Name)

	var beta *gwv1beta1.GatewayClass
	var exp *gwv1beta1exp.GatewayClass

	// Fetch beta
	b := &gwv1beta1.GatewayClass{}
	if err := r.Get(ctx, req.NamespacedName, b); err == nil {
		beta = b
	} else if !k8serrors.IsNotFound(err) {
		return ctrl.Result{}, err
	}

	// Fetch exp
	e := &gwv1beta1exp.GatewayClass{}
	if err := r.Get(ctx, req.NamespacedName, e); err == nil {
		exp = e
	} else if !k8serrors.IsNotFound(err) {
		return ctrl.Result{}, err
	}

	adapter := ResolveAuthoritativeGatewayClass(beta, exp, r.MigrationMode)
	if adapter == nil {
		return ctrl.Result{}, nil
	}

	if !adapter.GetObject().GetDeletionTimestamp().IsZero() {
		inUse, err := r.isGatewayClassInUse(ctx, adapter.GetName())
		if err != nil {
			return ctrl.Result{}, err
		}
		if inUse {
			log.Info("GatewayClass is in use, blocking deletion")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, nil
	}

	if beta != nil && exp != nil && r.MigrationMode == PreferExp {
		legacy := &GatewayClassV1Beta1Adapter{beta}
		legacyCond := metav1.Condition{
			Type:    ConditionAccepted,
			Status:  metav1.ConditionFalse,
			Reason:  ReasonSuperseded,
			Message: "Superseded by v1beta1exp GatewayClass",
		}
		if legacy.SetCondition(legacyCond) {
			_ = r.Status().Update(ctx, legacy.GetObject())
		}
	}

	// ── Validation ──
	accepted, msg, err := ValidateGatewayClass(ctx, r.Client, adapter, r.ControllerName)
	if err != nil {
		return ctrl.Result{}, err
	}

	cond := metav1.Condition{
		Type:               ConditionAccepted,
		Status:             metav1.ConditionFalse,
		Reason:             ReasonInvalid,
		Message:            msg,
		ObservedGeneration: adapter.GetGeneration(),
	}
	if accepted {
		cond.Status = metav1.ConditionTrue
		cond.Reason = ConditionAccepted
	}

	if adapter.SetCondition(cond) {
		if err := r.Status().Update(ctx, adapter.GetObject()); err != nil {
			return ctrl.Result{}, err
		}
	}

	log.Info(
		"Reconciled GatewayClass",
		"accepted", accepted,
		"api", fmt.Sprintf("%T", adapter),
	)

	return ctrl.Result{}, nil
}

//
// ─────────────────────────────────────────────────────────────
//  Watches (FROM OLD CODE – REQUIRED)

func (r *GatewayClassController) gatewayClassConfigFieldIndexEventHandler() handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []reconcile.Request {

		var gcs gwv1beta1.GatewayClassList
		if err := r.List(ctx, &gcs, &client.ListOptions{
			FieldSelector: fields.OneTermEqualSelector(
				GatewayClass_GatewayClassConfigIndex,
				o.GetName(),
			),
		}); err != nil {
			r.Log.Error(err, "unable to list GatewayClasses")
			return nil
		}

		var reqs []reconcile.Request
		for _, gc := range gcs.Items {
			reqs = append(reqs, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: gc.Name},
			})
		}
		return reqs
	})
}

func (r *GatewayClassController) gatewayFieldIndexEventHandler() handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []reconcile.Request {
		gw := o.(*gwv1beta1.Gateway)
		return []reconcile.Request{{
			NamespacedName: types.NamespacedName{
				Name: string(gw.Spec.GatewayClassName),
			},
		}}
	})
}

//
// ─────────────────────────────────────────────────────────────
//  Setup
// ─────────────────────────────────────────────────────────────
//

func (r *GatewayClassController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gwv1beta1.GatewayClass{}).
		Watches(&gwv1beta1exp.GatewayClass{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []reconcile.Request {
				return []reconcile.Request{{
					NamespacedName: types.NamespacedName{Name: o.GetName()},
				}}
			}),
		).
		Watches(&v1alpha1.GatewayClassConfig{}, r.gatewayClassConfigFieldIndexEventHandler()).
		Watches(&gwv1beta1.Gateway{}, r.gatewayFieldIndexEventHandler()).
		Complete(r)
}
