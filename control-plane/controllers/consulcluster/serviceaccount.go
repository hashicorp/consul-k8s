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
	sa := &corev1.ServiceAccount{}
	name := serviceAccountName(cluster)
	err := r.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: name}, sa)
	if err == nil {
		return nil
	}
	if !k8serrors.IsNotFound(err) {
		return err
	}

	annotations := map[string]string{}
	for k, v := range cluster.Spec.ServiceAccountAnnotations {
		annotations[k] = v
	}

	desired := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       cluster.Namespace,
			Labels:          serviceLabels(cluster),
			Annotations:     annotations,
			OwnerReferences: []metav1.OwnerReference{ownerRef(cluster, r.Scheme)},
		},
	}

	return r.Create(ctx, desired)
}

func serviceAccountName(cluster *v1alpha1.ConsulCluster) string {
	return cluster.Name + "-server"
}
