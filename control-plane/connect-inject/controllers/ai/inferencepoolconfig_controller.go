// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package ai

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

const (
	inferencePoolConfigFinalizer = "inference-pool-config-exists-finalizer.consul.hashicorp.com"

	// conditionTypeParentResolved is set to True once every parentRef has been
	// located in the API server, or False when one or more parents are missing.
	conditionTypeParentResolved = "ParentResolved"

	reasonParentNotFound = "ParentNotFound"
)

// InferencePoolConfigController reconciles InferencePoolConfig objects.
// It is responsible for:
//   - Adding a finalizer so the resource cannot be hard-deleted while in use.
//   - Validating that every parentRef in spec.parentRefs resolves to a live
//     resource and surfacing the result as a ParentResolved status condition.
//   - Updating the Ready condition to reflect whether the pool is active and
//     all parents are resolved.
//   - Removing the finalizer and allowing deletion when requested.
type InferencePoolConfigController struct {
	client.Client
	Log logr.Logger
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

	// --- Deletion path ---
	if !ipc.ObjectMeta.DeletionTimestamp.IsZero() {
		log.Info("InferencePoolConfig is marked for deletion",
			"deletionTimestamp", ipc.ObjectMeta.DeletionTimestamp,
		)
		removed, err := removeFinalizer(ctx, r.Client, ipc, inferencePoolConfigFinalizer)
		if err != nil {
			if k8serrors.IsConflict(err) {
				log.Info("conflict removing finalizer, requeueing")
				return ctrl.Result{Requeue: true}, nil
			}
			log.Error(err, "failed to remove finalizer")
			return ctrl.Result{}, err
		}
		if removed {
			log.Info("finalizer removed, deletion will proceed")
		} else {
			log.Info("finalizer was already absent")
		}
		return ctrl.Result{}, nil
	}

	// --- Normal path ---

	// 1. Ensure finalizer.
	added, err := ensureFinalizer(ctx, r.Client, ipc, inferencePoolConfigFinalizer)
	if err != nil {
		if k8serrors.IsConflict(err) {
			log.Info("conflict adding finalizer, requeueing")
			return ctrl.Result{Requeue: true}, nil
		}
		log.Error(err, "failed to add finalizer")
		return ctrl.Result{}, err
	}
	if added {
		log.Info("finalizer added", "finalizer", inferencePoolConfigFinalizer)
	} else {
		log.V(1).Info("finalizer already present")
	}

	// 2. Resolve parentRefs and sync status.
	log.Info("syncing status conditions",
		"specEnabled", ipc.Spec.Enabled,
		"parentRefsCount", len(ipc.Spec.ParentRefs),
	)
	if err := r.syncPoolStatus(ctx, ipc); err != nil {
		log.Error(err, "failed to sync status conditions")
		return ctrl.Result{RequeueAfter: 10 * time.Second}, err
	}

	log.Info("reconcile complete",
		"enabled", ipc.Spec.Enabled,
		"parentRefsCount", len(ipc.Spec.ParentRefs),
	)
	return ctrl.Result{}, nil
}

// syncPoolStatus resolves every parentRef and writes Accepted, ParentResolved,
// and Ready conditions onto the InferencePoolConfig status sub-resource.
func (r *InferencePoolConfigController) syncPoolStatus(ctx context.Context, ipc *v1alpha1.InferencePoolConfig) error {
	log := r.Log.WithValues("inferencePoolConfig", ipc.Name, "namespace", ipc.Namespace)
	now := metav1.Now()

	// --- Resolve parentRefs ---
	parentResolved := metav1.ConditionTrue
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
				parentResolvedMsg = fmt.Sprintf("parentRef %q (kind=%s, namespace=%s) not found", ref.Name, ref.Kind, ns)
				log.Info("parentRef not found", "name", ref.Name, "kind", ref.Kind, "namespace", ns)
				break
			}
			// Treat transient API errors as a temporary failure — requeue.
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
			Reason:             func() string {
				if parentResolved == metav1.ConditionTrue {
					return reasonReconciled
				}
				return reasonParentNotFound
			}(),
			Message: parentResolvedMsg,
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

func (r *InferencePoolConfigController) SetupWithManager(mgr ctrl.Manager) error {
	r.Log.Info("registering InferencePoolConfigController with manager")
	return ctrl.NewControllerManagedBy(mgr).
		Named("inferencepoolconfig").
		For(&v1alpha1.InferencePoolConfig{}).
		Complete(r)
}
