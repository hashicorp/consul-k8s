// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package connect

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hashicorp/consul-k8s/acceptance/framework/connhelper"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
)

// Test that the endpoints controller deregisters service instances of pods that were evicted by the kubelet.
// Evicted pods stay in the Failed phase in the Kubernetes API, so without this they remain registered in Consul
// and keep receiving (or failing over) traffic.
func TestConnectInject_EvictedPodIsDeregistered(t *testing.T) {
	cfg := suite.Config()

	if !cfg.EnableEnterprise {
		t.Skipf("skipping this test because -enable-enterprise is not set")
	}

	cfg.SkipWhenOpenshiftAndCNI(t)

	for _, tc := range []struct {
		secure      bool
		gracePeriod string
	}{
		{secure: false, gracePeriod: "0"},
		{secure: true, gracePeriod: "0"},
		// With a graceful shutdown period, the controller used to keep the evicted instance as critical.
		{secure: false, gracePeriod: "5"},
		{secure: true, gracePeriod: "5"},
	} {
		name := fmt.Sprintf("secure: %t, gracePeriodSeconds: %s", tc.secure, tc.gracePeriod)
		t.Run(name, func(t *testing.T) {
			ctx := suite.Environment().DefaultContext(t)

			connHelper := connhelper.ConnectHelper{
				ClusterKind: consul.Helm,
				Secure:      tc.secure,
				ReleaseName: helpers.RandomName(),
				Ctx:         ctx,
				Cfg:         cfg,
				HelmValues: map[string]string{
					helmGracePeriodSecondsKey: tc.gracePeriod,
				},
			}

			connHelper.Setup(t)
			connHelper.Install(t)
			connHelper.DeployClientAndServer(t)

			opts := connHelper.KubectlOptsForApp(t)
			ns := opts.Namespace

			if tc.secure {
				connHelper.CreateIntention(t, connhelper.IntentionOpts{})
			}

			// Run two replicas with a tight ephemeral-storage limit so one can be evicted by filling its disk.
			logger.Log(t, "scaling static-server to 2 replicas with a 30Mi ephemeral-storage limit")
			k8s.RunKubectl(t, opts, "patch", "deploy/"+connhelper.StaticServerName, "--type=strategic", "-p",
				`{"spec":{"replicas":2,"template":{"spec":{"containers":[{"name":"static-server","resources":{"limits":{"ephemeral-storage":"30Mi"}}}]}}}}`)
			k8s.RunKubectl(t, opts, "rollout", "status", "deploy/"+connhelper.StaticServerName, "--timeout=3m")

			serverPods := func(r *retry.R) []corev1.Pod {
				pods, err := ctx.KubernetesClient(r).CoreV1().Pods(ns).List(context.Background(),
					metav1.ListOptions{LabelSelector: "app=" + connhelper.StaticServerName})
				require.NoError(r, err)
				var running []corev1.Pod
				for _, p := range pods.Items {
					if p.Status.Phase == corev1.PodRunning && p.DeletionTimestamp == nil {
						running = append(running, p)
					}
				}
				return running
			}

			var victim string
			retry.RunWith(&retry.Timer{Timeout: 2 * time.Minute, Wait: 5 * time.Second}, t, func(r *retry.R) {
				pods := serverPods(r)
				require.Len(r, pods, 2)
				victim = pods[0].Name
			})

			logger.Logf(t, "waiting for both static-server instances to be registered, victim pod is %q", victim)
			retry.Run(t, func(r *retry.R) {
				instances, _, err := connHelper.ConsulClient.Catalog().Service(connhelper.StaticServerName, "", nil)
				require.NoError(r, err)
				require.Len(r, instances, 2)
			})

			// Exceed the ephemeral-storage limit so the kubelet evicts the pod.
			logger.Logf(t, "filling ephemeral storage of pod %q", victim)
			_, _ = k8s.RunKubectlAndGetOutputE(t, opts, "exec", victim, "-c", connhelper.StaticServerName, "--",
				"dd", "if=/dev/zero", "of=/tmp/fill", "bs=1M", "count=80")

			logger.Logf(t, "waiting for pod %q to be evicted", victim)
			retry.RunWith(&retry.Timer{Timeout: 5 * time.Minute, Wait: 5 * time.Second}, t, func(r *retry.R) {
				pod, err := ctx.KubernetesClient(r).CoreV1().Pods(ns).Get(context.Background(), victim, metav1.GetOptions{})
				require.NoError(r, err)
				require.Equal(r, corev1.PodFailed, pod.Status.Phase)
				require.Equal(r, "Evicted", pod.Status.Reason)
			})

			// The evicted pod object still exists, yet its instances must be removed from Consul.
			logger.Log(t, "ensuring the evicted pod is deregistered from Consul while the Pod object still exists")
			retry.RunWith(&retry.Timer{Timeout: 1 * time.Minute, Wait: 2 * time.Second}, t, func(r *retry.R) {
				for _, svc := range []string{connhelper.StaticServerName, connhelper.StaticServerName + "-sidecar-proxy"} {
					instances, _, err := connHelper.ConsulClient.Catalog().Service(svc, "", nil)
					require.NoError(r, err)
					for _, instance := range instances {
						if strings.Contains(instance.ServiceID, victim) {
							r.Errorf("%s is still registered for evicted pod %s", instance.ServiceID, victim)
						}
					}
				}
			})

			// Remaining healthy replicas keep serving.
			connHelper.TestConnectionSuccess(t, connhelper.ConnHelperOpts{})
		})
	}
}
