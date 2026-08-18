// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package consulcluster

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/go-logr/logr"
	capi "github.com/hashicorp/consul/api"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
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
	requeueAfterPending   = 5 * time.Second
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
		Owns(&corev1.Pod{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&policyv1.PodDisruptionBudget{}).
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
		if err := r.updateStatus(ctx, cluster, v1alpha1.ConsulClusterPhaseRunning, 0, nil, ""); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: requeueAfterSafetyNet}, nil
	}

	if err := r.ensureHeadlessService(ctx, cluster); err != nil {
		return ctrl.Result{}, fmt.Errorf("ensuring headless service: %w", err)
	}
	if err := r.ensureClientService(ctx, cluster); err != nil {
		return ctrl.Result{}, fmt.Errorf("ensuring client service: %w", err)
	}
	if err := r.ensureExposeService(ctx, cluster); err != nil {
		return ctrl.Result{}, fmt.Errorf("ensuring expose service: %w", err)
	}
	if err := r.ensureConfigMap(ctx, cluster); err != nil {
		return ctrl.Result{}, fmt.Errorf("ensuring config map: %w", err)
	}
	if err := r.ensureServiceAccount(ctx, cluster); err != nil {
		return ctrl.Result{}, fmt.Errorf("ensuring service account: %w", err)
	}
	if err := r.ensurePodDisruptionBudget(ctx, cluster); err != nil {
		return ctrl.Result{}, fmt.Errorf("ensuring pod disruption budget: %w", err)
	}

	podList := &corev1.PodList{}
	if err := r.List(ctx, podList,
		client.InNamespace(cluster.Namespace),
		client.MatchingLabels(serverPodLabels(cluster)),
	); err != nil {
		return ctrl.Result{}, fmt.Errorf("listing pods: %w", err)
	}

	var running, pending, terminating []*corev1.Pod
	for i := range podList.Items {
		pod := &podList.Items[i]
		if pod.DeletionTimestamp != nil {
			terminating = append(terminating, pod)
		} else if pod.Status.Phase == corev1.PodPending || pod.Status.Phase == "" {
			pending = append(pending, pod)
		} else {
			running = append(running, pod)
		}
	}

	log.Info("pod state", "running", len(running), "pending", len(pending), "terminating", len(terminating))

	if len(pending) > 0 || len(terminating) > 0 {
		if err := r.updateStatus(ctx, cluster, v1alpha1.ConsulClusterPhaseCreating, countReady(running), memberNames(running), ""); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: requeueAfterPending}, nil
	}

	active := len(running)

	if active < cluster.Spec.Size {
		log.Info("scaling up", "current", active, "desired", cluster.Spec.Size)
		if err := r.createServerPod(ctx, cluster); err != nil {
			return ctrl.Result{}, fmt.Errorf("creating server pod: %w", err)
		}
		if err := r.updateStatus(ctx, cluster, v1alpha1.ConsulClusterPhaseCreating, countReady(running), memberNames(running), ""); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: requeueAfterPending}, nil
	}

	if active > cluster.Spec.Size {
		log.Info("scaling down", "current", active, "desired", cluster.Spec.Size)
		if err := r.deleteOneServerPod(ctx, log, cluster, running); err != nil {
			return ctrl.Result{}, fmt.Errorf("deleting server pod: %w", err)
		}
		if err := r.updateStatus(ctx, cluster, v1alpha1.ConsulClusterPhaseRunning, countReady(running), memberNames(running), ""); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: requeueAfterPending}, nil
	}

	if len(running) > 0 {
		if err := r.removeDeadRaftPeers(ctx, log, cluster, running); err != nil {
			log.Info("dead raft peer cleanup encountered an error, will retry", "err", err)
		}
	}

	desiredImage := consulImage(cluster)
	for _, pod := range running {
		if pod.Labels[labelVersion] != cluster.Spec.Version {
			log.Info("upgrading pod", "pod", pod.Name, "from", pod.Labels[labelVersion], "to", cluster.Spec.Version)
			if err := r.upgradeOnePod(ctx, cluster, pod, desiredImage); err != nil {
				return ctrl.Result{}, fmt.Errorf("upgrading pod %s: %w", pod.Name, err)
			}
			if err := r.updateStatus(ctx, cluster, v1alpha1.ConsulClusterPhaseUpgrading, countReady(running), memberNames(running), cluster.Spec.Version); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: requeueAfterPending}, nil
		}
	}

	phase := v1alpha1.ConsulClusterPhaseRunning
	if countReady(running) == 0 && cluster.Spec.Size > 0 {
		phase = v1alpha1.ConsulClusterPhaseCreating
	}
	if err := r.updateStatus(ctx, cluster, phase, countReady(running), memberNames(running), cluster.Spec.Version); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: requeueAfterSafetyNet}, nil
}

func (r *ConsulClusterReconciler) reconcileDelete(ctx context.Context, log logr.Logger, cluster *v1alpha1.ConsulCluster) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(cluster, finalizerName) {
		return ctrl.Result{}, nil
	}

	log.Info("deleting ConsulCluster", "name", cluster.Name)

	podList := &corev1.PodList{}
	if err := r.List(ctx, podList,
		client.InNamespace(cluster.Namespace),
		client.MatchingLabels(serverPodLabels(cluster)),
	); err != nil {
		return ctrl.Result{}, fmt.Errorf("listing server pods for deletion: %w", err)
	}
	for i := range podList.Items {
		pod := &podList.Items[i]
		if err := r.Delete(ctx, pod); err != nil && !k8serrors.IsNotFound(err) {
			return ctrl.Result{}, fmt.Errorf("deleting pod %s: %w", pod.Name, err)
		}
	}

	retentionPolicy := pvcRetentionPolicy(cluster)
	if retentionPolicy.WhenDeleted == v1alpha1.PVCRetentionPolicyDelete {
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

func (r *ConsulClusterReconciler) deleteOneServerPod(ctx context.Context, log logr.Logger, cluster *v1alpha1.ConsulCluster, running []*corev1.Pod) error {
	pod := running[len(running)-1]

	remainingPeers := running[:len(running)-1]

	if err := r.Delete(ctx, pod); err != nil && !k8serrors.IsNotFound(err) {
		return fmt.Errorf("deleting pod %s: %w", pod.Name, err)
	}

	if len(remainingPeers) > 0 {
		if err := waitForGossipLeave(ctx, remainingPeers[0], pod.Name); err != nil {
			log.Info("gossip leave wait timed out, continuing", "pod", pod.Name, "err", err)
		}
	}

	retentionPolicy := pvcRetentionPolicy(cluster)
	if retentionPolicy.WhenScaled == v1alpha1.PVCRetentionPolicyDelete {
		pvc := &corev1.PersistentVolumeClaim{}
		pvcName := types.NamespacedName{Namespace: cluster.Namespace, Name: pvcNameForPod(pod.Name)}
		if err := r.Get(ctx, pvcName, pvc); err == nil {
			if delErr := r.Delete(ctx, pvc); delErr != nil && !k8serrors.IsNotFound(delErr) {
				return fmt.Errorf("deleting PVC %s: %w", pvc.Name, delErr)
			}
		}
	}

	return nil
}

func (r *ConsulClusterReconciler) removeDeadRaftPeers(ctx context.Context, log logr.Logger, cluster *v1alpha1.ConsulCluster, running []*corev1.Pod) error {
	livePod := running[0]
	consulClient, err := consulClientForPod(livePod)
	if err != nil {
		return err
	}

	raftCfg, err := consulClient.Operator().RaftGetConfiguration(&capi.QueryOptions{})
	if err != nil {
		return fmt.Errorf("getting raft configuration: %w", err)
	}

	runningIPs := map[string]bool{}
	for _, pod := range running {
		runningIPs[pod.Status.PodIP] = true
	}

	for _, srv := range raftCfg.Servers {
		if srv.Leader {
			continue
		}
		ip, _, err := splitHostPort(srv.Address)
		if err != nil {
			continue
		}
		if !runningIPs[ip] {
			log.Info("removing dead raft peer", "address", srv.Address, "id", srv.ID)
			if err := consulClient.Operator().RaftRemovePeerByAddress(srv.Address, &capi.WriteOptions{}); err != nil {
				return fmt.Errorf("removing dead raft peer %s: %w", srv.Address, err)
			}
		}
	}
	return nil
}

func splitHostPort(address string) (string, string, error) {
	host, port, err := net.SplitHostPort(address)
	return host, port, err
}

func (r *ConsulClusterReconciler) upgradeOnePod(ctx context.Context, cluster *v1alpha1.ConsulCluster, pod *corev1.Pod, desiredImage string) error {
	patch := client.MergeFrom(pod.DeepCopy())
	for i, c := range pod.Spec.Containers {
		if c.Name == "consul" {
			pod.Spec.Containers[i].Image = desiredImage
			break
		}
	}
	if pod.Labels == nil {
		pod.Labels = map[string]string{}
	}
	pod.Labels[labelVersion] = cluster.Spec.Version
	return r.Patch(ctx, pod, patch)
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

func (r *ConsulClusterReconciler) updateStatus(ctx context.Context, cluster *v1alpha1.ConsulCluster, phase v1alpha1.ConsulClusterPhase, readyCount int, members []string, version string) error {
	patch := client.MergeFrom(cluster.DeepCopy())
	cluster.Status.Phase = phase
	cluster.Status.ReadyCount = readyCount
	cluster.Status.Members = members
	if version != "" {
		cluster.Status.CurrentVersion = version
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

func pvcNameForPod(podName string) string {
	return "data-" + podName
}

func countReady(pods []*corev1.Pod) int {
	count := 0
	for _, pod := range pods {
		if isPodReady(pod) {
			count++
		}
	}
	return count
}

func isPodReady(pod *corev1.Pod) bool {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func memberNames(pods []*corev1.Pod) []string {
	names := make([]string, 0, len(pods))
	for _, pod := range pods {
		names = append(names, pod.Name)
	}
	return names
}

func consulImage(cluster *v1alpha1.ConsulCluster) string {
	if cluster.Spec.Image != "" {
		return cluster.Spec.Image
	}
	return fmt.Sprintf("hashicorp/consul:%s", cluster.Spec.Version)
}

func headlessServiceName(cluster *v1alpha1.ConsulCluster) string {
	return cluster.Name + "-server"
}

func clientServiceName(cluster *v1alpha1.ConsulCluster) string {
	return cluster.Name + "-ui"
}

func configMapName(cluster *v1alpha1.ConsulCluster) string {
	return cluster.Name + "-server-config"
}

func ownerRef(cluster *v1alpha1.ConsulCluster, scheme *runtime.Scheme) metav1.OwnerReference {
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
