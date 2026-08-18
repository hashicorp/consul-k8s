// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package consulcluster

import (
	"context"
	"fmt"
	"net/http"
	"time"

	capi "github.com/hashicorp/consul/api"
	corev1 "k8s.io/api/core/v1"
)

const (
	consulHTTPPort     = 8500
	consulAPITimeout   = 10 * time.Second
	raftRemoveTimeout  = 30 * time.Second
	gossipLeaveTimeout = 30 * time.Second

	agentMemberAlive = 1
)

// consulClientForPod returns a Consul API client pointed at a specific pod IP.
func consulClientForPod(pod *corev1.Pod) (*capi.Client, error) {
	if pod.Status.PodIP == "" {
		return nil, fmt.Errorf("pod %s has no IP yet", pod.Name)
	}
	cfg := capi.DefaultConfig()
	cfg.Address = fmt.Sprintf("%s:%d", pod.Status.PodIP, consulHTTPPort)
	cfg.HttpClient = &http.Client{Timeout: consulAPITimeout}
	return capi.NewClient(cfg)
}

// removeDeadRaftPeer calls the Consul operator API on a live peer to forcibly
// remove a peer that has left the cluster without a clean Raft departure.
// address must be in "<ip>:8300" form.
func removeDeadRaftPeer(ctx context.Context, livePod *corev1.Pod, deadAddress string) error {
	client, err := consulClientForPod(livePod)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, raftRemoveTimeout)
	defer cancel()

	raftCfg, err := client.Operator().RaftGetConfiguration(&capi.QueryOptions{})
	if err != nil {
		return fmt.Errorf("getting raft configuration: %w", err)
	}

	for _, srv := range raftCfg.Servers {
		if srv.Address == deadAddress {
			if err := client.Operator().RaftRemovePeerByAddress(deadAddress, &capi.WriteOptions{}); err != nil {
				return fmt.Errorf("removing raft peer %s: %w", deadAddress, err)
			}
			return nil
		}
	}
	return nil
}

// waitForGossipLeave polls the gossip member list on a live peer until the
// named node no longer appears as alive, or the timeout is reached.
func waitForGossipLeave(ctx context.Context, livePod *corev1.Pod, nodeName string) error {
	client, err := consulClientForPod(livePod)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, gossipLeaveTimeout)
	defer cancel()

	for {
		members, err := client.Agent().Members(false)
		if err != nil {
			return fmt.Errorf("listing gossip members: %w", err)
		}

		alive := false
		for _, m := range members {
			if m.Name == nodeName && m.Status == agentMemberAlive {
				alive = true
				break
			}
		}
		if !alive {
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for node %s to leave gossip pool", nodeName)
		case <-time.After(2 * time.Second):
		}
	}
}
