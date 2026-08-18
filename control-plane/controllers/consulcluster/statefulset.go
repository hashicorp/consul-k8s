// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package consulcluster

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

// templateHashAnnotation records a hash of the pod template the operator last
// wrote. Comparing hashes avoids diffing against a server-defaulted PodSpec,
// which never compares equal to the one we build.
const templateHashAnnotation = "consul.hashicorp.com/template-hash"

func statefulSetName(cluster *v1alpha1.ConsulCluster) string {
	return cluster.Name + "-server"
}

// ensureStatefulSet creates or updates the server StatefulSet and returns the
// live object. Scale up, scale down, pod replacement after node loss, and volume
// reattachment are all the StatefulSet controller's responsibility.
//
// It writes updateStrategy.rollingUpdate.partition only to freeze a rollout it
// is about to start; reconcileRollout owns stepping the partition back down.
func (r *ConsulClusterReconciler) ensureStatefulSet(ctx context.Context, cluster *v1alpha1.ConsulCluster, configChecksum string) (*appsv1.StatefulSet, error) {
	desired, err := buildStatefulSet(cluster, configChecksum)
	if err != nil {
		return nil, err
	}
	if err := ctrl.SetControllerReference(cluster, desired, r.Scheme); err != nil {
		return nil, err
	}

	existing := &appsv1.StatefulSet{}
	err = r.Get(ctx, client.ObjectKeyFromObject(desired), existing)
	switch {
	case k8serrors.IsNotFound(err):
		// Nothing to roll on first create, so the partition stays at 0 and
		// podManagementPolicy: Parallel brings the whole cluster up at once.
		if err := r.Create(ctx, desired); err != nil {
			return nil, fmt.Errorf("creating statefulset: %w", err)
		}
		return desired, nil
	case err != nil:
		return nil, err
	}

	replicas := int32(cluster.Spec.Size)
	templateChanged := existing.Annotations[templateHashAnnotation] != desired.Annotations[templateHashAnnotation]
	replicasChanged := existing.Spec.Replicas == nil || *existing.Spec.Replicas != replicas

	if !templateChanged && !replicasChanged {
		return existing, nil
	}

	// serviceName, selector, podManagementPolicy and volumeClaimTemplates are
	// immutable after creation, so only the fields the API server accepts are
	// patched. Changing storage size or the selector needs a recreate, which is
	// out of scope for the operator to do on its own.
	patch := client.MergeFrom(existing.DeepCopy())
	existing.Spec.Replicas = &replicas
	existing.Spec.PersistentVolumeClaimRetentionPolicy = desired.Spec.PersistentVolumeClaimRetentionPolicy

	if templateChanged {
		existing.Spec.Template = desired.Spec.Template
		if existing.Annotations == nil {
			existing.Annotations = map[string]string{}
		}
		existing.Annotations[templateHashAnnotation] = desired.Annotations[templateHashAnnotation]

		// Freeze the rollout before the new revision lands. Without this the
		// StatefulSet controller starts rolling immediately, gated only on pod
		// Readiness — which says the agent has a leader to report, not that it
		// has rejoined Raft as a voter. reconcileRollout steps the partition
		// down under an autopilot check instead.
		existing.Spec.UpdateStrategy = appsv1.StatefulSetUpdateStrategy{
			Type:          appsv1.RollingUpdateStatefulSetStrategyType,
			RollingUpdate: &appsv1.RollingUpdateStatefulSetStrategy{Partition: &replicas},
		}
	}

	if err := r.Patch(ctx, existing, patch); err != nil {
		return nil, fmt.Errorf("patching statefulset: %w", err)
	}
	return existing, nil
}

func buildStatefulSet(cluster *v1alpha1.ConsulCluster, configChecksum string) (*appsv1.StatefulSet, error) {
	replicas := int32(cluster.Spec.Size)
	template := buildServerPodTemplate(cluster, configChecksum)

	hash, err := hashPodTemplate(&template)
	if err != nil {
		return nil, err
	}

	storageQty := cluster.Spec.Storage
	if storageQty.IsZero() {
		storageQty = resource.MustParse("10Gi")
	}

	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:        statefulSetName(cluster),
			Namespace:   cluster.Namespace,
			Labels:      serverPodLabels(cluster),
			Annotations: map[string]string{templateHashAnnotation: hash},
		},
		Spec: appsv1.StatefulSetSpec{
			ServiceName: headlessServiceName(cluster),
			Replicas:    &replicas,
			// Servers find each other through retry_join against the headless
			// service, which publishes not-ready addresses, so they can all
			// start at once rather than waiting for an ordered bootstrap.
			PodManagementPolicy: appsv1.ParallelPodManagement,
			Selector:            &metav1.LabelSelector{MatchLabels: serverPodLabels(cluster)},
			Template:            template,
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: appsv1.RollingUpdateStatefulSetStrategyType,
			},
			PersistentVolumeClaimRetentionPolicy: pvcRetentionPolicyForSTS(cluster),
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:   dataVolumeName(cluster),
						Labels: serverPodLabels(cluster),
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{corev1.ResourceStorage: storageQty},
						},
						StorageClassName: cluster.Spec.StorageClassName,
					},
				},
			},
		},
	}, nil
}

// pvcRetentionPolicyForSTS maps the CR's policy onto the StatefulSet field.
// Clusters older than 1.27 drop this field, so reconcileDelete still deletes
// PVCs itself when the policy says Delete.
func pvcRetentionPolicyForSTS(cluster *v1alpha1.ConsulCluster) *appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy {
	policy := pvcRetentionPolicy(cluster)
	toSTS := func(p v1alpha1.PVCRetentionPolicyType) appsv1.PersistentVolumeClaimRetentionPolicyType {
		if p == v1alpha1.PVCRetentionPolicyRetain {
			return appsv1.RetainPersistentVolumeClaimRetentionPolicyType
		}
		return appsv1.DeletePersistentVolumeClaimRetentionPolicyType
	}
	return &appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy{
		WhenScaled:  toSTS(policy.WhenScaled),
		WhenDeleted: toSTS(policy.WhenDeleted),
	}
}

func hashPodTemplate(template *corev1.PodTemplateSpec) (string, error) {
	b, err := json.Marshal(template)
	if err != nil {
		return "", fmt.Errorf("hashing pod template: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
