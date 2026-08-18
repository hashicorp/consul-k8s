// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package consulcluster

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/go-logr/logr"
	capi "github.com/hashicorp/consul/api"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

const (
	consulHTTPPort   = 8500
	consulHTTPSPort  = 8501
	consulAPITimeout = 10 * time.Second
)

// consulClientForCluster returns a client pointed at any ready server, honouring
// the cluster's TLS and ACL settings. Without both, the operator's Raft and
// autopilot calls fail on exactly the configurations that matter in production.
func (r *ConsulClusterReconciler) consulClientForCluster(ctx context.Context, cluster *v1alpha1.ConsulCluster) (*capi.Client, error) {
	pods, err := r.readyServerPods(ctx, cluster)
	if err != nil {
		return nil, err
	}
	if len(pods) == 0 {
		return nil, fmt.Errorf("no ready server pods for cluster %s/%s", cluster.Namespace, cluster.Name)
	}
	return r.consulClientForPod(ctx, cluster, pods[0])
}

func (r *ConsulClusterReconciler) consulClientForPod(ctx context.Context, cluster *v1alpha1.ConsulCluster, pod *corev1.Pod) (*capi.Client, error) {
	if pod.Status.PodIP == "" {
		return nil, fmt.Errorf("pod %s has no IP yet", pod.Name)
	}

	cfg := capi.DefaultConfig()
	cfg.HttpClient = &http.Client{Timeout: consulAPITimeout}

	if tlsEnabled(cluster) {
		cfg.Scheme = "https"
		cfg.Address = net.JoinHostPort(pod.Status.PodIP, fmt.Sprintf("%d", consulHTTPSPort))

		caPEM, err := r.readSecretKey(ctx, cluster.Namespace, cluster.Spec.TLS.CASecretName, "tls.crt")
		if err != nil {
			return nil, fmt.Errorf("reading CA certificate for consul client: %w", err)
		}
		cfg.TLSConfig = capi.TLSConfig{
			CAPem: caPEM,
			// The server certificate is issued for the cluster's DNS name, not
			// for the pod IP being dialed, so pin the expected name rather than
			// disabling verification.
			Address: fmt.Sprintf("server.%s.%s", datacenterName(cluster), consulDomain(cluster)),
		}
	} else {
		cfg.Scheme = "http"
		cfg.Address = net.JoinHostPort(pod.Status.PodIP, fmt.Sprintf("%d", consulHTTPPort))
	}

	if cluster.Spec.ACLs != nil && cluster.Spec.ACLs.Token != nil {
		token, err := r.readSecretKey(ctx, cluster.Namespace,
			cluster.Spec.ACLs.Token.SecretName,
			cluster.Spec.ACLs.Token.SecretKey)
		if err != nil {
			return nil, fmt.Errorf("reading ACL token for consul client: %w", err)
		}
		cfg.Token = string(token)
	}

	return capi.NewClient(cfg)
}

func (r *ConsulClusterReconciler) readSecretKey(ctx context.Context, namespace, name, key string) ([]byte, error) {
	if name == "" {
		return nil, fmt.Errorf("secret name is empty")
	}
	secret := &corev1.Secret{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, secret); err != nil {
		return nil, err
	}
	value, ok := secret.Data[key]
	if !ok {
		return nil, fmt.Errorf("secret %s/%s has no key %q", namespace, name, key)
	}
	return value, nil
}

// reapDeadRaftPeers removes Raft peers whose addresses no longer match a live
// server pod. Pods leave gossip cleanly via leave_on_terminate and the preStop
// hook, but a node failure kills the agent without either, and the peer stays in
// the Raft configuration counting against quorum until something removes it.
func (r *ConsulClusterReconciler) reapDeadRaftPeers(ctx context.Context, log logr.Logger, cluster *v1alpha1.ConsulCluster) error {
	pods, err := r.readyServerPods(ctx, cluster)
	if err != nil {
		return err
	}
	if len(pods) == 0 {
		return nil
	}

	// Only reap once the cluster is at its desired size. Reaping mid-rollout
	// would remove the peer belonging to the pod currently being restarted.
	if len(pods) < cluster.Spec.Size {
		return nil
	}

	consulClient, err := r.consulClientForPod(ctx, cluster, pods[0])
	if err != nil {
		return err
	}

	raftCfg, err := consulClient.Operator().RaftGetConfiguration(&capi.QueryOptions{})
	if err != nil {
		return fmt.Errorf("getting raft configuration: %w", err)
	}

	liveIPs := make(map[string]bool, len(pods))
	for _, pod := range pods {
		liveIPs[pod.Status.PodIP] = true
	}

	for _, srv := range raftCfg.Servers {
		if srv.Leader {
			continue
		}
		host, _, err := net.SplitHostPort(srv.Address)
		if err != nil {
			continue
		}
		if liveIPs[host] {
			continue
		}
		log.Info("removing dead raft peer", "address", srv.Address, "id", srv.ID)
		if err := consulClient.Operator().RaftRemovePeerByAddress(srv.Address, &capi.WriteOptions{}); err != nil {
			return fmt.Errorf("removing dead raft peer %s: %w", srv.Address, err)
		}
	}
	return nil
}
