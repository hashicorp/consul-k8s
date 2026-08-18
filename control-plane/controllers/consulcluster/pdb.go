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
	if cluster.Spec.PodDisruptionBudget == nil || !cluster.Spec.PodDisruptionBudget.Enabled {
		return nil
	}

	pdb := &policyv1.PodDisruptionBudget{}
	name := cluster.Name + "-server"
	err := r.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: name}, pdb)
	if err == nil {
		return nil
	}
	if !k8serrors.IsNotFound(err) {
		return err
	}

	maxUnavailable := 1
	if cluster.Spec.PodDisruptionBudget.MaxUnavailable != nil {
		maxUnavailable = *cluster.Spec.PodDisruptionBudget.MaxUnavailable
	}
	maxUnavailableVal := intstr.FromInt(maxUnavailable)

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

	return r.Create(ctx, desired)
}
