// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package consulcluster

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	capi "github.com/hashicorp/consul/api"
	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

// reconcileRollout advances a partitioned rolling update one ordinal at a time,
// highest first, and only when autopilot reports the cluster can afford to lose
// another server. It returns true once every pod is on the current revision.
//
// The StatefulSet controller's own RollingUpdate already serializes pods and
// waits for Readiness between them. The partition adds the check Readiness
// cannot make: a Consul server passes its probe as soon as it can name a
// leader, which can be true while it is still replaying Raft and before it has
// been promoted back to a voter.
func (r *ConsulClusterReconciler) reconcileRollout(
	ctx context.Context,
	log logr.Logger,
	cluster *v1alpha1.ConsulCluster,
	sts *appsv1.StatefulSet,
) (bool, error) {
	replicas := int32(cluster.Spec.Size)
	partition := currentPartition(sts, replicas)

	// CurrentRevision catches up to UpdateRevision only after every replica has
	// been updated, which makes this the authoritative "rollout finished" signal.
	if sts.Status.UpdateRevision == sts.Status.CurrentRevision && partition == 0 {
		return true, nil
	}

	// Everything is on the new revision. Drop the partition so the next template
	// change starts from a clean state.
	if sts.Status.UpdatedReplicas >= replicas {
		if partition != 0 {
			return false, r.setPartition(ctx, sts, 0)
		}
		return true, nil
	}

	// Pods at ordinals at or above the partition should already have been
	// updated. Wait for the one in flight to come back before touching another.
	expectedUpdated := replicas - partition
	if sts.Status.UpdatedReplicas < expectedUpdated || sts.Status.ReadyReplicas < replicas {
		log.Info("waiting for in-flight pod",
			"partition", partition,
			"updated", sts.Status.UpdatedReplicas,
			"ready", sts.Status.ReadyReplicas,
			"replicas", replicas)
		return false, nil
	}

	// Kubernetes says the cluster is whole. Ask Consul whether it agrees before
	// taking the next server down.
	tolerant, err := r.raftCanTolerateFailure(ctx, cluster)
	if err != nil {
		log.Info("could not read autopilot health, holding rollout", "err", err)
		return false, nil
	}
	if !tolerant {
		log.Info("raft has no failure tolerance, holding rollout", "partition", partition)
		return false, nil
	}

	log.Info("advancing rollout", "from", partition, "to", partition-1)
	return false, r.setPartition(ctx, sts, partition-1)
}

func currentPartition(sts *appsv1.StatefulSet, replicas int32) int32 {
	if sts.Spec.UpdateStrategy.RollingUpdate == nil || sts.Spec.UpdateStrategy.RollingUpdate.Partition == nil {
		return 0
	}
	if p := *sts.Spec.UpdateStrategy.RollingUpdate.Partition; p <= replicas {
		return p
	}
	return replicas
}

func (r *ConsulClusterReconciler) setPartition(ctx context.Context, sts *appsv1.StatefulSet, partition int32) error {
	patch := client.MergeFrom(sts.DeepCopy())
	sts.Spec.UpdateStrategy = appsv1.StatefulSetUpdateStrategy{
		Type:          appsv1.RollingUpdateStatefulSetStrategyType,
		RollingUpdate: &appsv1.RollingUpdateStatefulSetStrategy{Partition: &partition},
	}
	if err := r.Patch(ctx, sts, patch); err != nil {
		return fmt.Errorf("setting rollout partition to %d: %w", partition, err)
	}
	return nil
}

// raftCanTolerateFailure reports whether the cluster can lose one more server
// without losing quorum. A single-server cluster can never tolerate a failure,
// so it is allowed through explicitly — there is no availability to protect.
func (r *ConsulClusterReconciler) raftCanTolerateFailure(ctx context.Context, cluster *v1alpha1.ConsulCluster) (bool, error) {
	if cluster.Spec.Size <= 1 {
		return true, nil
	}

	consulClient, err := r.consulClientForCluster(ctx, cluster)
	if err != nil {
		return false, err
	}

	health, err := consulClient.Operator().AutopilotServerHealth(&capi.QueryOptions{})
	if err != nil {
		return false, fmt.Errorf("reading autopilot health: %w", err)
	}

	// Healthy on its own is not enough: it can be true on a cluster that is one
	// server away from losing quorum. FailureTolerance is the number of servers
	// that could still be lost without an outage.
	return health.Healthy &&
		health.FailureTolerance >= 1 &&
		len(health.Servers) >= cluster.Spec.Size, nil
}
