// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package ai

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

const (
	inferencePoolConfigFinalizer = "inference-pool-config-exists-finalizer.consul.hashicorp.com"

	// conditionTypeParentResolved is set to True once every parentRef has been
	// located in the API server, or False when one or more parents are missing.
	conditionTypeParentResolved = "ParentResolved"

	reasonParentNotFound    = "ParentNotFound"
	reasonParentCRDNotFound = "ParentCRDNotFound"
)

// InferencePoolConfigController reconciles InferencePoolConfig objects.
// It is responsible for:
//   - Adding a finalizer so the resource cannot be hard-deleted while in use.
//   - Validating that every parentRef in spec.parentRefs resolves to a live
//     resource and surfacing the result as a ParentResolved status condition.
//   - Updating the Ready condition to reflect whether the pool is active and
//     all parents are resolved.
//   - Removing the finalizer and allowing deletion when requested.
//
// NOTE: ownerReference writes and InferenceGateway Watches will be added once
// the InferenceGateway CRD and operator are introduced.
type InferencePoolConfigController struct {
	client.Client
	Log      logr.Logger
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=consul.hashicorp.com,resources=inferencepoolconfigs,verbs=get;list;watch;update
// +kubebuilder:rbac:groups=consul.hashicorp.com,resources=inferencepoolconfigs/status,verbs=get;patch;update
// +kubebuilder:rbac:groups=consul.hashicorp.com,resources=inferencepoolconfigs/finalizers,verbs=update

func (r *InferencePoolConfigController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("inferencePoolConfig", req.NamespacedName)
	log.Info("reconcile started")

	ipc := &v1alpha1.InferencePoolConfig{}
	if err := r.Client.Get(ctx, req.NamespacedName, ipc); err != nil {
		if k8serrors.IsNotFound(err) {
			log.Info("InferencePoolConfig not found; must have been deleted, nothing to do")
			return ctrl.Result{}, nil
		}
		log.Error(err, "failed to get InferencePoolConfig")
		return ctrl.Result{}, err
	}

	log.Info("fetched InferencePoolConfig",
		"resourceVersion", ipc.ResourceVersion,
		"generation", ipc.Generation,
		"enabled", ipc.Spec.Enabled,
		"parentRefsCount", len(ipc.Spec.ParentRefs),
	)

	// 1. Normal path: object is not being deleted — ensure finalizer exists.
	if ipc.DeletionTimestamp.IsZero() {
		if !controllerutil.ContainsFinalizer(ipc, inferencePoolConfigFinalizer) {
			controllerutil.AddFinalizer(ipc, inferencePoolConfigFinalizer)
			if err := r.Client.Update(ctx, ipc); err != nil {
				if k8serrors.IsConflict(err) {
					log.Info("conflict adding finalizer, requeueing")
					return ctrl.Result{Requeue: true}, nil
				}
				log.Error(err, "failed to add finalizer")
				r.Recorder.Eventf(ipc, corev1.EventTypeWarning, eventReasonFinalizerAdded, "failed to add finalizer: %v", err)
				return ctrl.Result{}, err
			}
			log.Info("finalizer added", "finalizer", inferencePoolConfigFinalizer)
			r.Recorder.Event(ipc, corev1.EventTypeNormal, eventReasonFinalizerAdded, "finalizer added successfully")
			// Requeue so we operate on the updated object next pass.
			return ctrl.Result{Requeue: true}, nil
		}
	}

	// 2. Deletion path: object is being deleted — run cleanup and remove finalizer.
	if !ipc.DeletionTimestamp.IsZero() {
		log.Info("InferencePoolConfig is marked for deletion",
			"deletionTimestamp", ipc.DeletionTimestamp,
		)
		if controllerutil.ContainsFinalizer(ipc, inferencePoolConfigFinalizer) {
			controllerutil.RemoveFinalizer(ipc, inferencePoolConfigFinalizer)
			if err := r.Client.Update(ctx, ipc); err != nil {
				if k8serrors.IsConflict(err) {
					log.Info("conflict removing finalizer, requeueing")
					return ctrl.Result{Requeue: true}, nil
				}
				log.Error(err, "failed to remove finalizer")
				r.Recorder.Eventf(ipc, corev1.EventTypeWarning, eventReasonFinalizerRemoved, "failed to remove finalizer: %v", err)
				return ctrl.Result{}, err
			}
			log.Info("finalizer removed, deletion will proceed")
			r.Recorder.Event(ipc, corev1.EventTypeNormal, eventReasonFinalizerRemoved, "finalizer removed, deletion will proceed")
		}
		return ctrl.Result{}, nil
	}

	// 3. Resolve parentRefs and sync status.
	log.Info("syncing status conditions",
		"specEnabled", ipc.Spec.Enabled,
		"parentRefsCount", len(ipc.Spec.ParentRefs),
	)
	if err := r.syncPoolStatus(ctx, ipc); err != nil {
		log.Error(err, "failed to sync status conditions")
		r.Recorder.Eventf(ipc, corev1.EventTypeWarning, eventReasonSyncFailed, "failed to sync status: %v", err)
		return ctrl.Result{RequeueAfter: 10 * time.Second}, err
	}

	log.Info("reconcile complete",
		"enabled", ipc.Spec.Enabled,
		"parentRefsCount", len(ipc.Spec.ParentRefs),
	)
	r.Recorder.Event(ipc, corev1.EventTypeNormal, eventReasonSynced, "InferencePoolConfig synced successfully")
	return ctrl.Result{}, nil
}

// syncPoolStatus resolves every parentRef and writes Accepted, ParentResolved,
// and Ready conditions onto the InferencePoolConfig status sub-resource.
func (r *InferencePoolConfigController) syncPoolStatus(ctx context.Context, ipc *v1alpha1.InferencePoolConfig) error {
	log := r.Log.WithValues("inferencePoolConfig", ipc.Name, "namespace", ipc.Namespace)
	now := metav1.Now()

	// --- Resolve parentRefs ---
	parentResolved := metav1.ConditionTrue
	parentResolvedReason := reasonReconciled
	parentResolvedMsg := fmt.Sprintf("all %d parentRef(s) resolved successfully", len(ipc.Spec.ParentRefs))

	for _, ref := range ipc.Spec.ParentRefs {
		ns := ref.Namespace
		if ns == "" {
			ns = "default"
		}

		// Use Unstructured so the controller is decoupled from parent Go types
		// and works with any Kind registered in the cluster. The fake client
		// in tests resolves Unstructured objects by GVK + namespace/name.
		// Group is always consul.hashicorp.com — parentRefs only target
		// resources in the same API group.
		parentObj := &unstructured.Unstructured{}
		parentObj.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   v1alpha1.ConsulHashicorpGroup,
			Version: "v1alpha1",
			Kind:    ref.Kind,
		})

		key := types.NamespacedName{Name: ref.Name, Namespace: ns}
		if err := r.Client.Get(ctx, key, parentObj); err != nil {
			if k8serrors.IsNotFound(err) {
				parentResolved = metav1.ConditionFalse
				parentResolvedReason = reasonParentNotFound
				parentResolvedMsg = fmt.Sprintf("parentRef %q (kind=%s, namespace=%s) not found", ref.Name, ref.Kind, ns)
				log.Info("parentRef not found", "name", ref.Name, "kind", ref.Kind, "namespace", ns)
				break
			}
			if isParentCRDAbsent(err) {
				// The CRD for the referenced Kind is not installed in this cluster
				// yet (e.g. InferenceGateway). Treat this as a soft not-found so
				// the pool is marked ParentResolved=False without an error log or
				// requeue storm.
				parentResolved = metav1.ConditionFalse
				parentResolvedReason = reasonParentCRDNotFound
				parentResolvedMsg = fmt.Sprintf("CRD for parentRef kind %q is not installed in this cluster", ref.Kind)
				log.Info("parentRef CRD not installed, marking ParentResolved=False",
					"name", ref.Name, "kind", ref.Kind, "namespace", ns)
				break
			}
			// Genuine transient API error — requeue.
			return fmt.Errorf("looking up parentRef %q: %w", ref.Name, err)
		}
		log.V(1).Info("parentRef resolved", "name", ref.Name, "kind", ref.Kind, "namespace", ns)
	}

	// --- Compute Ready ---
	readyStatus := metav1.ConditionTrue
	readyMsg := "InferencePoolConfig is reconciled; spec.enabled=true and all parentRefs are resolved"
	if !ipc.Spec.Enabled {
		readyStatus = metav1.ConditionFalse
		readyMsg = "InferencePoolConfig is reconciled; spec.enabled=false, pool is standing by"
	} else if parentResolved == metav1.ConditionFalse {
		readyStatus = metav1.ConditionFalse
		readyMsg = "InferencePoolConfig is not ready; one or more parentRefs could not be resolved"
	}

	log.Info("setting status conditions",
		"Accepted", metav1.ConditionTrue,
		"ParentResolved", parentResolved,
		"Ready", readyStatus,
	)

	conditions := []metav1.Condition{
		{
			Type:               conditionTypeAccepted,
			Status:             metav1.ConditionTrue,
			ObservedGeneration: ipc.Generation,
			LastTransitionTime: now,
			Reason:             reasonReconciled,
			Message:            "InferencePoolConfig has been accepted and the configuration is stored",
		},
		{
			Type:               conditionTypeParentResolved,
			Status:             parentResolved,
			ObservedGeneration: ipc.Generation,
			LastTransitionTime: now,
			Reason:             parentResolvedReason,
			Message:            parentResolvedMsg,
		},
		{
			Type:               conditionTypeReady,
			Status:             readyStatus,
			ObservedGeneration: ipc.Generation,
			LastTransitionTime: now,
			Reason:             reasonReconciled,
			Message:            readyMsg,
		},
	}

	patch := client.MergeFrom(ipc.DeepCopy())
	ipc.Status.Conditions = mergeConditions(ipc.Status.Conditions, conditions)
	ipc.Status.LastSyncedTime = &now

	if err := r.Client.Status().Patch(ctx, ipc, patch); err != nil {
		log.Error(err, "failed to patch status")
		return err
	}

	log.Info("status conditions patched successfully", "lastSyncedTime", now)
	return nil
}

// isParentCRDAbsent returns true when err indicates the API server does not
// know the resource kind at all — meaning the CRD has not been installed yet.
// This covers both "no matches for kind" (meta.NoMatchError) and
// "no kind is registered" (runtime.IsNotRegisteredError), neither of which is
// exported, so we match on the error message text.
func isParentCRDAbsent(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "no matches for kind") ||
		strings.Contains(msg, "no kind is registered") ||
		strings.Contains(msg, "is not registered")
}

func (r *InferencePoolConfigController) SetupWithManager(mgr ctrl.Manager) error {
	r.Log.Info("registering InferencePoolConfigController with manager")
	return ctrl.NewControllerManagedBy(mgr).
		Named("inferencepoolconfig").
		For(&v1alpha1.InferencePoolConfig{}).
		Complete(r)
}
