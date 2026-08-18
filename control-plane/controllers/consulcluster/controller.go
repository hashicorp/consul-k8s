// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package consulcluster

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

const (
	finalizerName = "consulcluster.consul.hashicorp.com/finalizer"

	labelApp       = "app"
	labelComponent = "component"
	labelCluster   = "consul.hashicorp.com/cluster"
	labelVersion   = "consul.hashicorp.com/version"

	labelAppValue       = "consul"
	labelComponentValue = "server"

	requeueAfterSafetyNet = 30 * time.Second

	// requeueAfterRollout is the poll interval while a rollout is in flight.
	// StatefulSet and Pod events wake the reconciler too; this covers the
	// autopilot health check, which nothing in Kubernetes notifies us about.
	requeueAfterRollout = 10 * time.Second
)

// ConsulClusterReconciler reconciles ConsulCluster objects.
type ConsulClusterReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// SetupWithManager registers the reconciler with the controller-runtime manager.
func (r *ConsulClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.ConsulCluster{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&policyv1.PodDisruptionBudget{}).
		// Pods belong to the StatefulSet rather than to us, but watching them
		// means a server going unready wakes the dead-peer reaper promptly.
		Owns(&corev1.Pod{}).
		Complete(r)
}

// Reconcile is the main reconcile loop for ConsulCluster.
func (r *ConsulClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("consulcluster", req.NamespacedName)

	cluster := &v1alpha1.ConsulCluster{}
	if err := r.Get(ctx, req.NamespacedName, cluster); err != nil {
		if k8serrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if cluster.DeletionTimestamp != nil {
		return r.reconcileDelete(ctx, log, cluster)
	}

	if !controllerutil.ContainsFinalizer(cluster, finalizerName) {
		patch := client.MergeFrom(cluster.DeepCopy())
		controllerutil.AddFinalizer(cluster, finalizerName)
		if err := r.Patch(ctx, cluster, patch); err != nil {
			return ctrl.Result{}, fmt.Errorf("adding finalizer: %w", err)
		}
		return ctrl.Result{}, nil
	}

	if cluster.Spec.Paused {
		log.Info("cluster is paused, skipping reconciliation")
		return ctrl.Result{RequeueAfter: requeueAfterSafetyNet}, nil
	}

	// The UI (client) and expose Services remain Helm-owned (ui-service.yaml,
	// expose-servers-service.yaml) — Helm is still the delivery mechanism for
	// cross-cutting objects, so the operator only owns the headless Service,
	// which the StatefulSet requires to exist.
	if err := r.ensureHeadlessService(ctx, cluster); err != nil {
		return ctrl.Result{}, fmt.Errorf("ensuring headless service: %w", err)
	}
	configChecksum, err := r.ensureConfigMap(ctx, cluster)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("ensuring config map: %w", err)
	}
	if err := r.ensureServiceAccount(ctx, cluster); err != nil {
		return ctrl.Result{}, fmt.Errorf("ensuring service account: %w", err)
	}
	if err := r.ensurePodDisruptionBudget(ctx, cluster); err != nil {
		return ctrl.Result{}, fmt.Errorf("ensuring pod disruption budget: %w", err)
	}

	sts, err := r.ensureStatefulSet(ctx, cluster, configChecksum)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("ensuring statefulset: %w", err)
	}

	rolloutDone, err := r.reconcileRollout(ctx, log, cluster, sts)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("reconciling rollout: %w", err)
	}

	// Best effort: a transient Consul API error must not fail the reconcile,
	// because the cluster is otherwise converged.
	if err := r.reapDeadRaftPeers(ctx, log, cluster); err != nil {
		log.Info("dead raft peer cleanup failed, will retry", "err", err)
	}

	if err := r.updateStatus(ctx, cluster, sts, rolloutDone); err != nil {
		return ctrl.Result{}, err
	}

	if !rolloutDone {
		return ctrl.Result{RequeueAfter: requeueAfterRollout}, nil
	}
	return ctrl.Result{RequeueAfter: requeueAfterSafetyNet}, nil
}

func (r *ConsulClusterReconciler) reconcileDelete(ctx context.Context, log logr.Logger, cluster *v1alpha1.ConsulCluster) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(cluster, finalizerName) {
		return ctrl.Result{}, nil
	}

	log.Info("deleting ConsulCluster", "name", cluster.Name)

	// Garbage collection removes the StatefulSet along with its pods, and the
	// StatefulSet's own retention policy handles the PVCs. That policy is only
	// honoured from Kubernetes 1.27 onward, so delete the claims here too when
	// the cluster asks for it. Deleting an already-deleted PVC is a no-op.
	if pvcRetentionPolicy(cluster).WhenDeleted == v1alpha1.PVCRetentionPolicyDelete {
		if err := r.deleteAllPVCs(ctx, cluster); err != nil {
			return ctrl.Result{}, fmt.Errorf("deleting PVCs on cluster delete: %w", err)
		}
	}

	patch := client.MergeFrom(cluster.DeepCopy())
	controllerutil.RemoveFinalizer(cluster, finalizerName)
	if err := r.Patch(ctx, cluster, patch); err != nil {
		return ctrl.Result{}, fmt.Errorf("removing finalizer: %w", err)
	}
	return ctrl.Result{}, nil
}

func (r *ConsulClusterReconciler) deleteAllPVCs(ctx context.Context, cluster *v1alpha1.ConsulCluster) error {
	pvcList := &corev1.PersistentVolumeClaimList{}
	if err := r.List(ctx, pvcList,
		client.InNamespace(cluster.Namespace),
		client.MatchingLabels(serverPodLabels(cluster)),
	); err != nil {
		return err
	}
	for i := range pvcList.Items {
		pvc := &pvcList.Items[i]
		if err := r.Delete(ctx, pvc); err != nil && !k8serrors.IsNotFound(err) {
			return fmt.Errorf("deleting PVC %s: %w", pvc.Name, err)
		}
	}
	return nil
}

// readyServerPods returns the ready, non-terminating server pods for a cluster.
func (r *ConsulClusterReconciler) readyServerPods(ctx context.Context, cluster *v1alpha1.ConsulCluster) ([]*corev1.Pod, error) {
	podList := &corev1.PodList{}
	if err := r.List(ctx, podList,
		client.InNamespace(cluster.Namespace),
		client.MatchingLabels(serverPodLabels(cluster)),
	); err != nil {
		return nil, fmt.Errorf("listing server pods: %w", err)
	}

	var ready []*corev1.Pod
	for i := range podList.Items {
		pod := &podList.Items[i]
		if pod.DeletionTimestamp == nil && isPodReady(pod) && pod.Status.PodIP != "" {
			ready = append(ready, pod)
		}
	}
	return ready, nil
}

// updateStatus derives status from the StatefulSet rather than from the
// reconciler's own bookkeeping.
func (r *ConsulClusterReconciler) updateStatus(
	ctx context.Context,
	cluster *v1alpha1.ConsulCluster,
	sts *appsv1.StatefulSet,
	rolloutDone bool,
) error {
	phase := v1alpha1.ConsulClusterPhaseCreating
	switch {
	case !rolloutDone:
		phase = v1alpha1.ConsulClusterPhaseUpgrading
	case sts.Status.ReadyReplicas >= int32(cluster.Spec.Size):
		phase = v1alpha1.ConsulClusterPhaseRunning
	}

	// StatefulSet pod names are deterministic, so the member list no longer
	// depends on querying the pods.
	members := make([]string, 0, cluster.Spec.Size)
	for i := 0; i < cluster.Spec.Size; i++ {
		members = append(members, fmt.Sprintf("%s-%d", statefulSetName(cluster), i))
	}

	patch := client.MergeFrom(cluster.DeepCopy())
	cluster.Status.Phase = phase
	cluster.Status.ReadyCount = int(sts.Status.ReadyReplicas)
	cluster.Status.Members = members
	if rolloutDone {
		cluster.Status.CurrentVersion = cluster.Spec.Version
	}
	return r.Status().Patch(ctx, cluster, patch)
}

func pvcRetentionPolicy(cluster *v1alpha1.ConsulCluster) v1alpha1.ConsulClusterPVCRetentionPolicy {
	if cluster.Spec.PersistentVolumeClaimRetentionPolicy != nil {
		return *cluster.Spec.PersistentVolumeClaimRetentionPolicy
	}
	return v1alpha1.ConsulClusterPVCRetentionPolicy{
		WhenScaled:  v1alpha1.PVCRetentionPolicyDelete,
		WhenDeleted: v1alpha1.PVCRetentionPolicyDelete,
	}
}

func serverPodLabels(cluster *v1alpha1.ConsulCluster) map[string]string {
	labels := map[string]string{
		labelApp:       labelAppValue,
		labelComponent: labelComponentValue,
		labelCluster:   cluster.Name,
	}
	for k, v := range cluster.Labels {
		if _, exists := labels[k]; !exists {
			labels[k] = v
		}
	}
	return labels
}

func isPodReady(pod *corev1.Pod) bool {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func consulImage(cluster *v1alpha1.ConsulCluster) string {
	if cluster.Spec.Image != "" {
		return cluster.Spec.Image
	}
	return fmt.Sprintf("hashicorp/consul:%s", cluster.Spec.Version)
}

func datacenterName(cluster *v1alpha1.ConsulCluster) string {
	if cluster.Spec.DatacenterName != "" {
		return cluster.Spec.DatacenterName
	}
	return "dc1"
}

// consulDomain is Consul's own DNS domain, not the Kubernetes cluster domain.
// It defaults to "consul", which is what every *.service.consul lookup depends
// on.
func consulDomain(cluster *v1alpha1.ConsulCluster) string {
	if cluster.Spec.Domain != "" {
		return cluster.Spec.Domain
	}
	return "consul"
}

func headlessServiceName(cluster *v1alpha1.ConsulCluster) string {
	return cluster.Name + "-server"
}

func configMapName(cluster *v1alpha1.ConsulCluster) string {
	return cluster.Name + "-server-config"
}

func ownerRef(cluster *v1alpha1.ConsulCluster, _ *runtime.Scheme) metav1.OwnerReference {
	gvk := v1alpha1.GroupVersion.WithKind("ConsulCluster")
	return metav1.OwnerReference{
		APIVersion:         gvk.GroupVersion().String(),
		Kind:               gvk.Kind,
		Name:               cluster.Name,
		UID:                cluster.UID,
		Controller:         boolPtr(true),
		BlockOwnerDeletion: boolPtr(true),
	}
}

func boolPtr(b bool) *bool { return &b }
