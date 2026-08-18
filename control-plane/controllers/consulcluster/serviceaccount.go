// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package consulcluster

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

func (r *ConsulClusterReconciler) ensureServiceAccount(ctx context.Context, cluster *v1alpha1.ConsulCluster) error {
	annotations := map[string]string{}
	for k, v := range cluster.Spec.ServiceAccountAnnotations {
		annotations[k] = v
	}

	var pullSecrets []corev1.LocalObjectReference
	if cluster.Spec.Pod != nil {
		pullSecrets = cluster.Spec.Pod.ImagePullSecrets
	}

	desired := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:            serviceAccountName(cluster),
			Namespace:       cluster.Namespace,
			Labels:          serviceLabels(cluster),
			Annotations:     annotations,
			OwnerReferences: []metav1.OwnerReference{ownerRef(cluster, r.Scheme)},
		},
		ImagePullSecrets: pullSecrets,
	}

	existing := &corev1.ServiceAccount{}
	err := r.Get(ctx, client.ObjectKeyFromObject(desired), existing)
	if k8serrors.IsNotFound(err) {
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}

	patch := client.MergeFrom(existing.DeepCopy())
	existing.Labels = desired.Labels
	existing.Annotations = desired.Annotations
	existing.ImagePullSecrets = desired.ImagePullSecrets
	return r.Patch(ctx, existing, patch)
}

func serviceAccountName(cluster *v1alpha1.ConsulCluster) string {
	return cluster.Name + "-server"
}
