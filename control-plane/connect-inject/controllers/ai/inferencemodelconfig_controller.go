// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

// Package ai contains the controller for the InferenceModelConfig CRD.
package ai

import (
	"context"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

const (
	// inferenceModelConfigFinalizer protects an InferenceModelConfig from deletion
	// while it is still actively governing inference interceptor injection.
	inferenceModelConfigFinalizer = "inference-model-config-exists-finalizer.consul.hashicorp.com"

	// condition type constants written to InferenceModelConfigStatus.Conditions.
	conditionTypeAccepted = "Accepted"
	conditionTypeReady    = "Ready"

	// condition reason constants.
	reasonReconciled = "Reconciled"
)

// InferenceModelConfigController reconciles InferenceModelConfig objects.
// It is responsible for:
//   - Adding a finalizer so the resource cannot be hard-deleted while in use.
//   - Updating the Status.Conditions to reflect the current reconcile result.
//   - Removing the finalizer and allowing deletion when the resource is no
//     longer enabled.
type InferenceModelConfigController struct {
	client.Client
	Log      logr.Logger
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=consul.hashicorp.com,resources=inferencemodelconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=consul.hashicorp.com,resources=inferencemodelconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=consul.hashicorp.com,resources=inferencemodelconfigs/finalizers,verbs=update

// Reconcile is the main reconciliation loop. It is called whenever an
// InferenceModelConfig is created, updated, or deleted, or whenever a
// watch event fires on an object the controller owns.
func (r *InferenceModelConfigController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("inferenceModelConfig", req.Name)
	log.Info("reconcile started")

	imc := &v1alpha1.InferenceModelConfig{}
	if err := r.Client.Get(ctx, req.NamespacedName, imc); err != nil {
		if k8serrors.IsNotFound(err) {
			log.Info("InferenceModelConfig not found; must have been deleted, nothing to do")
			return ctrl.Result{}, nil
		}
		log.Error(err, "failed to get InferenceModelConfig")
		return ctrl.Result{}, err
	}

	log.Info("fetched InferenceModelConfig",
		"resourceVersion", imc.ResourceVersion,
		"generation", imc.Generation,
		"enabled", imc.Spec.Enabled,
		"interceptorPort", imc.Spec.Defaults.InterceptorPort,
		"inferenceProtocol", imc.Spec.Defaults.InferenceProtocol,
		"inferencePath", imc.Spec.Defaults.InferencePath,
	)

	// 1. Normal path: object is not being deleted — ensure finalizer exists.
	if imc.DeletionTimestamp.IsZero() {
		if !controllerutil.ContainsFinalizer(imc, inferenceModelConfigFinalizer) {
			controllerutil.AddFinalizer(imc, inferenceModelConfigFinalizer)
			if err := r.Client.Update(ctx, imc); err != nil {
				if k8serrors.IsConflict(err) {
					log.Info("conflict adding finalizer, requeueing")
					return ctrl.Result{Requeue: true}, nil
				}
				log.Error(err, "failed to add finalizer")
				r.Recorder.Eventf(imc, corev1.EventTypeWarning, eventReasonFinalizerAdded, "failed to add finalizer: %v", err)
				return ctrl.Result{}, err
			}
			log.Info("finalizer added", "finalizer", inferenceModelConfigFinalizer)
			r.Recorder.Event(imc, corev1.EventTypeNormal, eventReasonFinalizerAdded, "finalizer added successfully")
			// Requeue so we operate on the updated object next pass.
			return ctrl.Result{Requeue: true}, nil
		}
	}

	// 2. Deletion path: object is being deleted — run cleanup and remove finalizer.
	if !imc.DeletionTimestamp.IsZero() {
		log.Info("InferenceModelConfig is marked for deletion",
			"deletionTimestamp", imc.DeletionTimestamp,
		)
		if controllerutil.ContainsFinalizer(imc, inferenceModelConfigFinalizer) {
			controllerutil.RemoveFinalizer(imc, inferenceModelConfigFinalizer)
			if err := r.Client.Update(ctx, imc); err != nil {
				if k8serrors.IsConflict(err) {
					log.Info("conflict removing finalizer, requeueing")
					return ctrl.Result{Requeue: true}, nil
				}
				log.Error(err, "failed to remove finalizer")
				r.Recorder.Eventf(imc, corev1.EventTypeWarning, eventReasonFinalizerRemoved, "failed to remove finalizer: %v", err)
				return ctrl.Result{}, err
			}
			log.Info("finalizer removed, deletion will proceed")
			r.Recorder.Event(imc, corev1.EventTypeNormal, eventReasonFinalizerRemoved, "finalizer removed, deletion will proceed")
		}
		return ctrl.Result{}, nil
	}

	// 3. Sync status.
	log.Info("syncing status conditions", "specEnabled", imc.Spec.Enabled)
	if err := r.syncStatus(ctx, imc); err != nil {
		log.Error(err, "failed to sync status conditions")
		r.Recorder.Eventf(imc, corev1.EventTypeWarning, eventReasonSyncFailed, "failed to sync status: %v", err)
		return ctrl.Result{RequeueAfter: 10 * time.Second}, err
	}

	log.Info("reconcile complete",
		"enabled", imc.Spec.Enabled,
		"interceptorPort", imc.Spec.Defaults.InterceptorPort,
		"inferenceProtocol", imc.Spec.Defaults.InferenceProtocol,
	)
	r.Recorder.Event(imc, corev1.EventTypeNormal, eventReasonSynced, "InferenceModelConfig synced successfully")
	return ctrl.Result{}, nil
}

// syncStatus writes Accepted / Ready conditions onto the InferenceModelConfig
// status sub-resource reflecting whether the feature is currently active.
//
// Accepted is always True — the object is a valid configuration store and can
// be referenced regardless of spec.enabled. Ready reflects whether the feature
// is currently active (spec.enabled=true) or standing by (spec.enabled=false).
func (r *InferenceModelConfigController) syncStatus(ctx context.Context, imc *v1alpha1.InferenceModelConfig) error {
	log := r.Log.WithValues("inferenceModelConfig", imc.Name)
	now := metav1.Now()

	// Accepted is unconditionally True: the CR is syntactically valid, stored,
	// and referenceable as a configuration source whether or not the feature gate
	// (spec.enabled) is on.
	acceptedMsg := "InferenceModelConfig has been accepted and the configuration is stored"

	readyStatus := metav1.ConditionTrue
	readyMsg := "InferenceModelConfig is reconciled; spec.enabled=true, defaults are active"

	if !imc.Spec.Enabled {
		readyStatus = metav1.ConditionFalse
		readyMsg = "InferenceModelConfig is reconciled; spec.enabled=false, feature is standing by"
	}

	log.Info("setting status conditions",
		"Accepted", metav1.ConditionTrue,
		"Ready", readyStatus,
	)

	conditions := []metav1.Condition{
		{
			Type:               conditionTypeAccepted,
			Status:             metav1.ConditionTrue,
			ObservedGeneration: imc.Generation,
			LastTransitionTime: now,
			Reason:             reasonReconciled,
			Message:            acceptedMsg,
		},
		{
			Type:               conditionTypeReady,
			Status:             readyStatus,
			ObservedGeneration: imc.Generation,
			LastTransitionTime: now,
			Reason:             reasonReconciled,
			Message:            readyMsg,
		},
	}

	patch := client.MergeFrom(imc.DeepCopy())
	imc.Status.Conditions = mergeConditions(imc.Status.Conditions, conditions)
	imc.Status.LastSyncedTime = &now

	if err := r.Client.Status().Patch(ctx, imc, patch); err != nil {
		log.Error(err, "failed to patch status")
		return err
	}

	log.Info("status conditions patched successfully", "lastSyncedTime", now)
	return nil
}

// SetupWithManager registers the controller with the controller-runtime manager
// and declares the resources it watches.
func (r *InferenceModelConfigController) SetupWithManager(mgr ctrl.Manager) error {
	r.Log.Info("registering InferenceModelConfigController with manager")
	return ctrl.NewControllerManagedBy(mgr).
		Named("inferencemodelconfig").
		For(&v1alpha1.InferenceModelConfig{}).
		Complete(r)
}

// mergeConditions upserts newConditions into existing, preserving
// LastTransitionTime when the Status has not changed.
func mergeConditions(existing, newConditions []metav1.Condition) []metav1.Condition {
	result := make([]metav1.Condition, 0, len(existing))
	// Copy conditions that are NOT being updated.
	for _, e := range existing {
		found := false
		for _, n := range newConditions {
			if e.Type == n.Type {
				found = true
				break
			}
		}
		if !found {
			result = append(result, e)
		}
	}
	// Upsert new conditions, keeping LastTransitionTime stable when Status is unchanged.
	for _, n := range newConditions {
		for _, e := range existing {
			if e.Type == n.Type && e.Status == n.Status {
				n.LastTransitionTime = e.LastTransitionTime
				break
			}
		}
		result = append(result, n)
	}
	return result
}

