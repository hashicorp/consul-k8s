// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

// Package ai contains the controller for the McpServerConfig CRD.
package ai

import (
	"context"
	"time"

	"github.com/go-logr/logr"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

const (
	mcpServerConfigFinalizer = "mcp-server-config-exists-finalizer.consul.hashicorp.com"
)

// McpServerConfigController reconciles McpServerConfig objects.
// It is responsible for:
//   - Adding a finalizer so the resource cannot be hard-deleted while in use.
//   - Updating the Status.Conditions to reflect the current reconcile result.
//   - Removing the finalizer and allowing deletion when requested.
type McpServerConfigController struct {
	client.Client
	Log logr.Logger
}

// +kubebuilder:rbac:groups=consul.hashicorp.com,resources=mcpserverconfigs,verbs=get;list;watch;update
// +kubebuilder:rbac:groups=consul.hashicorp.com,resources=mcpserverconfigs/status,verbs=get;patch;update
// +kubebuilder:rbac:groups=consul.hashicorp.com,resources=mcpserverconfigs/finalizers,verbs=update

func (r *McpServerConfigController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("mcpServerConfig", req.Name)
	log.Info("reconcile started")

	mcp := &v1alpha1.McpServerConfig{}
	if err := r.Client.Get(ctx, req.NamespacedName, mcp); err != nil {
		if k8serrors.IsNotFound(err) {
			log.Info("McpServerConfig not found; must have been deleted, nothing to do")
			return ctrl.Result{}, nil
		}
		log.Error(err, "failed to get McpServerConfig")
		return ctrl.Result{}, err
	}

	log.Info("fetched McpServerConfig",
		"resourceVersion", mcp.ResourceVersion,
		"generation", mcp.Generation,
		"enabled", mcp.Spec.Enabled,
		"interceptorPort", mcp.Spec.Defaults.InterceptorPort,
		"transport", mcp.Spec.Defaults.Transport,
		"path", mcp.Spec.Defaults.Path,
		"protocolVersion", mcp.Spec.Defaults.ProtocolVersion,
	)

	// --- Deletion path ---
	if !mcp.ObjectMeta.DeletionTimestamp.IsZero() {
		log.Info("McpServerConfig is marked for deletion",
			"deletionTimestamp", mcp.ObjectMeta.DeletionTimestamp,
		)
		removed, err := ensureMcpFinalizer(ctx, r.Client, mcp, mcpServerConfigFinalizer, true)
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
	added, err := ensureMcpFinalizer(ctx, r.Client, mcp, mcpServerConfigFinalizer, false)
	if err != nil {
		if k8serrors.IsConflict(err) {
			log.Info("conflict adding finalizer, requeueing")
			return ctrl.Result{Requeue: true}, nil
		}
		log.Error(err, "failed to add finalizer")
		return ctrl.Result{}, err
	}
	if added {
		log.Info("finalizer added", "finalizer", mcpServerConfigFinalizer)
	} else {
		log.V(1).Info("finalizer already present")
	}

	// 2. Sync status.
	log.Info("syncing status conditions", "specEnabled", mcp.Spec.Enabled)
	if err := r.syncMcpStatus(ctx, mcp); err != nil {
		log.Error(err, "failed to sync status conditions")
		return ctrl.Result{RequeueAfter: 10 * time.Second}, err
	}

	log.Info("reconcile complete",
		"enabled", mcp.Spec.Enabled,
		"interceptorPort", mcp.Spec.Defaults.InterceptorPort,
		"transport", mcp.Spec.Defaults.Transport,
		"protocolVersion", mcp.Spec.Defaults.ProtocolVersion,
	)
	return ctrl.Result{}, nil
}

func (r *McpServerConfigController) syncMcpStatus(ctx context.Context, mcp *v1alpha1.McpServerConfig) error {
	log := r.Log.WithValues("mcpServerConfig", mcp.Name)
	now := metav1.Now()

	// Accepted is unconditionally True: the CR is stored and referenceable.
	acceptedMsg := "McpServerConfig has been accepted and the configuration is stored"

	readyStatus := metav1.ConditionTrue
	readyMsg := "McpServerConfig is reconciled; spec.enabled=true, defaults are active"

	if !mcp.Spec.Enabled {
		readyStatus = metav1.ConditionFalse
		readyMsg = "McpServerConfig is reconciled; spec.enabled=false, feature is standing by"
	}

	log.Info("setting status conditions",
		"Accepted", metav1.ConditionTrue,
		"Ready", readyStatus,
	)

	conditions := []metav1.Condition{
		{
			Type:               conditionTypeAccepted,
			Status:             metav1.ConditionTrue,
			ObservedGeneration: mcp.Generation,
			LastTransitionTime: now,
			Reason:             reasonReconciled,
			Message:            acceptedMsg,
		},
		{
			Type:               conditionTypeReady,
			Status:             readyStatus,
			ObservedGeneration: mcp.Generation,
			LastTransitionTime: now,
			Reason:             reasonReconciled,
			Message:            readyMsg,
		},
	}

	patch := client.MergeFrom(mcp.DeepCopy())
	mcp.Status.Conditions = mergeConditions(mcp.Status.Conditions, conditions)
	mcp.Status.LastSyncedTime = &now

	if err := r.Client.Status().Patch(ctx, mcp, patch); err != nil {
		log.Error(err, "failed to patch status")
		return err
	}

	log.Info("status conditions patched successfully", "lastSyncedTime", now)
	return nil
}

func (r *McpServerConfigController) SetupWithManager(mgr ctrl.Manager) error {
	r.Log.Info("registering McpServerConfigController with manager")
	return ctrl.NewControllerManagedBy(mgr).
		Named("mcpserverconfig").
		For(&v1alpha1.McpServerConfig{}).
		Complete(r)
}

// ensureMcpFinalizer adds (remove=false) or removes (remove=true) the given
// finalizer on obj. Returns true if the object was actually mutated.
func ensureMcpFinalizer(ctx context.Context, c client.Client, obj client.Object, finalizer string, remove bool) (bool, error) {
	finalizers := obj.GetFinalizers()
	if remove {
		for i, f := range finalizers {
			if f == finalizer {
				obj.SetFinalizers(append(finalizers[:i], finalizers[i+1:]...))
				return true, c.Update(ctx, obj)
			}
		}
		return false, nil
	}
	for _, f := range finalizers {
		if f == finalizer {
			return false, nil
		}
	}
	obj.SetFinalizers(append(finalizers, finalizer))
	return true, c.Update(ctx, obj)
}
