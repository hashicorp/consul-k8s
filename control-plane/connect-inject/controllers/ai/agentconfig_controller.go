// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

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
	agentConfigFinalizer = "agent-config-exists-finalizer.consul.hashicorp.com"
)

// AgentConfigController reconciles AgentConfig objects.
type AgentConfigController struct {
	client.Client
	Log logr.Logger
}

// +kubebuilder:rbac:groups=consul.hashicorp.com,resources=agentconfigs,verbs=get;list;watch;update
// +kubebuilder:rbac:groups=consul.hashicorp.com,resources=agentconfigs/status,verbs=get;patch;update
// +kubebuilder:rbac:groups=consul.hashicorp.com,resources=agentconfigs/finalizers,verbs=update

func (r *AgentConfigController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("agentConfig", req.Name)
	log.Info("reconcile started")

	ac := &v1alpha1.AgentConfig{}
	if err := r.Client.Get(ctx, req.NamespacedName, ac); err != nil {
		if k8serrors.IsNotFound(err) {
			log.Info("AgentConfig not found; must have been deleted, nothing to do")
			return ctrl.Result{}, nil
		}
		log.Error(err, "failed to get AgentConfig")
		return ctrl.Result{}, err
	}

	log.Info("fetched AgentConfig",
		"resourceVersion", ac.ResourceVersion,
		"generation", ac.Generation,
		"enabled", ac.Spec.Enabled,
		"interceptorPort", ac.Spec.Defaults.InterceptorPort,
		"mcpPort", ac.Spec.Defaults.McpPort,
		"hitlPort", ac.Spec.Defaults.HITL.Port,
		"hitlApprovalTimeout", ac.Spec.Defaults.HITL.ApprovalTimeout,
	)

	// --- Deletion path ---
	if !ac.ObjectMeta.DeletionTimestamp.IsZero() {
		log.Info("AgentConfig is marked for deletion",
			"deletionTimestamp", ac.ObjectMeta.DeletionTimestamp,
		)
		removed, err := ensureFinalizer(ctx, r.Client, ac, agentConfigFinalizer)
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
	added, err := ensureFinalizer(ctx, r.Client, ac, agentConfigFinalizer)
	if err != nil {
		if k8serrors.IsConflict(err) {
			log.Info("conflict adding finalizer, requeueing")
			return ctrl.Result{Requeue: true}, nil
		}
		log.Error(err, "failed to add finalizer")
		return ctrl.Result{}, err
	}
	if added {
		log.Info("finalizer added", "finalizer", agentConfigFinalizer)
	} else {
		log.V(1).Info("finalizer already present")
	}

	// 2. Sync status.
	log.Info("syncing status conditions", "specEnabled", ac.Spec.Enabled)
	if err := r.syncAgentStatus(ctx, ac); err != nil {
		log.Error(err, "failed to sync status conditions")
		return ctrl.Result{RequeueAfter: 10 * time.Second}, err
	}

	log.Info("reconcile complete",
		"enabled", ac.Spec.Enabled,
		"interceptorPort", ac.Spec.Defaults.InterceptorPort,
		"mcpPort", ac.Spec.Defaults.McpPort,
		"hitlPort", ac.Spec.Defaults.HITL.Port,
	)
	return ctrl.Result{}, nil
}

func (r *AgentConfigController) syncAgentStatus(ctx context.Context, ac *v1alpha1.AgentConfig) error {
	log := r.Log.WithValues("agentConfig", ac.Name)
	now := metav1.Now()

	acceptedMsg := "AgentConfig has been accepted and the configuration is stored"

	readyStatus := metav1.ConditionTrue
	readyMsg := "AgentConfig is reconciled; spec.enabled=true, defaults are active"
	if !ac.Spec.Enabled {
		readyStatus = metav1.ConditionFalse
		readyMsg = "AgentConfig is reconciled; spec.enabled=false, feature is standing by"
	}

	log.Info("setting status conditions",
		"Accepted", metav1.ConditionTrue,
		"Ready", readyStatus,
	)

	conditions := []metav1.Condition{
		{
			Type:               conditionTypeAccepted,
			Status:             metav1.ConditionTrue,
			ObservedGeneration: ac.Generation,
			LastTransitionTime: now,
			Reason:             reasonReconciled,
			Message:            acceptedMsg,
		},
		{
			Type:               conditionTypeReady,
			Status:             readyStatus,
			ObservedGeneration: ac.Generation,
			LastTransitionTime: now,
			Reason:             reasonReconciled,
			Message:            readyMsg,
		},
	}

	patch := client.MergeFrom(ac.DeepCopy())
	ac.Status.Conditions = mergeConditions(ac.Status.Conditions, conditions)
	ac.Status.LastSyncedTime = &now

	if err := r.Client.Status().Patch(ctx, ac, patch); err != nil {
		log.Error(err, "failed to patch status")
		return err
	}

	log.Info("status conditions patched successfully", "lastSyncedTime", now)
	return nil
}

func (r *AgentConfigController) SetupWithManager(mgr ctrl.Manager) error {
	r.Log.Info("registering AgentConfigController with manager")
	return ctrl.NewControllerManagedBy(mgr).
		Named("agentconfig").
		For(&v1alpha1.AgentConfig{}).
		Complete(r)
}
