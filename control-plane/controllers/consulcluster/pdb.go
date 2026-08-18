// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package consulcluster

import (
	"context"

	policyv1 "k8s.io/api/policy/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

func (r *ConsulClusterReconciler) ensurePodDisruptionBudget(ctx context.Context, cluster *v1alpha1.ConsulCluster) error {
	name := statefulSetName(cluster)

	if cluster.Spec.PodDisruptionBudget == nil || !cluster.Spec.PodDisruptionBudget.Enabled {
		// Remove a budget left behind by a previous spec that enabled it,
		// otherwise it keeps blocking drains after the user turned it off.
		existing := &policyv1.PodDisruptionBudget{}
		err := r.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: name}, existing)
		if k8serrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		return r.Delete(ctx, existing)
	}

	maxUnavailableVal := intstr.FromInt(serverMaxUnavailable(cluster))

	desired := &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       cluster.Namespace,
			Labels:          serviceLabels(cluster),
			OwnerReferences: []metav1.OwnerReference{ownerRef(cluster, r.Scheme)},
		},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MaxUnavailable: &maxUnavailableVal,
			Selector: &metav1.LabelSelector{
				MatchLabels: serverPodLabels(cluster),
			},
		},
	}

	existing := &policyv1.PodDisruptionBudget{}
	err := r.Get(ctx, client.ObjectKeyFromObject(desired), existing)
	if k8serrors.IsNotFound(err) {
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}

	patch := client.MergeFrom(existing.DeepCopy())
	existing.Labels = desired.Labels
	existing.Spec = desired.Spec
	return r.Patch(ctx, existing, patch)
}

// serverMaxUnavailable returns how many servers may be voluntarily disrupted at
// once. A single-server cluster returns 0: there is no redundancy, so any
// eviction is a full outage and a drain should block rather than take the
// datastore down.
func serverMaxUnavailable(cluster *v1alpha1.ConsulCluster) int {
	if cluster.Spec.Size <= 1 {
		return 0
	}
	if cluster.Spec.PodDisruptionBudget != nil && cluster.Spec.PodDisruptionBudget.MaxUnavailable != nil {
		return *cluster.Spec.PodDisruptionBudget.MaxUnavailable
	}
	return 1
}
