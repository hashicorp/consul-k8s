// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

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
	agentConfigFinalizer = "agent-config-exists-finalizer.consul.hashicorp.com"

	// Event reasons.
	eventReasonFinalizerAdded   = "FinalizerAdded"
	eventReasonFinalizerRemoved = "FinalizerRemoved"
	eventReasonSyncFailed       = "SyncFailed"
	eventReasonSynced           = "Synced"
)

// AgentConfigController reconciles AgentConfig objects.
type AgentConfigController struct {
	client.Client
	Log      logr.Logger
	Recorder record.EventRecorder
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

	// 1. Normal path: object is not being deleted — ensure finalizer exists.

	if ac.DeletionTimestamp.IsZero() {
		if !controllerutil.ContainsFinalizer(ac, agentConfigFinalizer) {
			controllerutil.AddFinalizer(ac, agentConfigFinalizer)
			if err := r.Client.Update(ctx, ac); err != nil {
				if k8serrors.IsConflict(err) {
					log.Info("conflict adding finalizer, requeueing")
					return ctrl.Result{Requeue: true}, nil
				}
				log.Error(err, "failed to add finalizer")
				r.Recorder.Eventf(ac, corev1.EventTypeWarning, eventReasonFinalizerAdded, "failed to add finalizer: %v", err)
				return ctrl.Result{}, err
			}
			log.Info("finalizer added", "finalizer", agentConfigFinalizer)
			r.Recorder.Event(ac, corev1.EventTypeNormal, eventReasonFinalizerAdded, "finalizer added successfully")
			// Requeue so we operate on the updated object next pass.
			return ctrl.Result{Requeue: true}, nil
		}
	}

	// 2. Deletion path: object is being deleted — run cleanup and remove finalizer.
	if !ac.DeletionTimestamp.IsZero() {
		log.Info("AgentConfig is marked for deletion",
			"deletionTimestamp", ac.DeletionTimestamp,
		)
		if controllerutil.ContainsFinalizer(ac, agentConfigFinalizer) {
			controllerutil.RemoveFinalizer(ac, agentConfigFinalizer)
			if err := r.Client.Update(ctx, ac); err != nil {
				if k8serrors.IsConflict(err) {
					log.Info("conflict removing finalizer, requeueing")
					return ctrl.Result{Requeue: true}, nil
				}
				log.Error(err, "failed to remove finalizer")
				r.Recorder.Eventf(ac, corev1.EventTypeWarning, eventReasonFinalizerRemoved, "failed to remove finalizer: %v", err)
				return ctrl.Result{}, err
			}
			log.Info("finalizer removed, deletion will proceed")
			r.Recorder.Event(ac, corev1.EventTypeNormal, eventReasonFinalizerRemoved, "finalizer removed, deletion will proceed")
		}
		return ctrl.Result{}, nil
	}

	// 3. Sync status.
	log.Info("syncing status conditions", "specEnabled", ac.Spec.Enabled)
	if err := r.syncAgentStatus(ctx, ac); err != nil {
		log.Error(err, "failed to sync status conditions")
		r.Recorder.Eventf(ac, corev1.EventTypeWarning, eventReasonSyncFailed, "failed to sync status: %v", err)
		return ctrl.Result{RequeueAfter: 10 * time.Second}, err
	}

	log.Info("reconcile complete",
		"enabled", ac.Spec.Enabled,
		"interceptorPort", ac.Spec.Defaults.InterceptorPort,
		"mcpPort", ac.Spec.Defaults.McpPort,
		"hitlPort", ac.Spec.Defaults.HITL.Port,
	)
	r.Recorder.Event(ac, corev1.EventTypeNormal, eventReasonSynced, "AgentConfig synced successfully")
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
