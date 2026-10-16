// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package endpoints

import (
	"context"
	"testing"

	mapset "github.com/deckarep/golang-set"
	logrtest "github.com/go-logr/logr/testr"
	"github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/lifecycle"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
)

func TestIsPodTerminal(t *testing.T) {
	for phase, expected := range map[corev1.PodPhase]bool{
		corev1.PodPending:   false,
		corev1.PodRunning:   false,
		corev1.PodUnknown:   false,
		corev1.PodSucceeded: true,
		corev1.PodFailed:    true,
	} {
		t.Run(string(phase), func(t *testing.T) {
			require.Equal(t, expected, isPodTerminal(corev1.Pod{Status: corev1.PodStatus{Phase: phase}}))
		})
	}
}

// Test that service instances of pods in a terminal phase (e.g. evicted by the kubelet) are deregistered even
// though the Pod object and its Endpoints entry still exist.
func TestReconcile_TerminalPodIsDeregistered(t *testing.T) {
	t.Parallel()
	svcName := "service-terminal"
	namespace := "default"

	cases := map[string]struct {
		phase              corev1.PodPhase
		reason             string
		shutdownGraceSecs  int
		expectedNumSvcInst int
	}{
		"Failed (evicted) pod is deregistered":                          {phase: corev1.PodFailed, reason: "Evicted"},
		"Succeeded pod is deregistered":                                 {phase: corev1.PodSucceeded},
		"Failed pod is deregistered despite graceful shutdown period":   {phase: corev1.PodFailed, reason: "Evicted", shutdownGraceSecs: 30},
		"Succeeded pod is deregistered despite graceful shutdown":       {phase: corev1.PodSucceeded, shutdownGraceSecs: 30},
		"Running pod stays registered":                                  {phase: corev1.PodRunning, expectedNumSvcInst: 1},
		"Running pod stays registered with graceful shutdown (control)": {phase: corev1.PodRunning, shutdownGraceSecs: 30, expectedNumSvcInst: 1},
	}

	for name, tt := range cases {
		t.Run(name, func(t *testing.T) {
			endpoint := &corev1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Name:      svcName,
					Namespace: namespace,
				},
				Subsets: []corev1.EndpointSubset{{
					Addresses: []corev1.EndpointAddress{{
						IP:        "1.2.3.4",
						TargetRef: &corev1.ObjectReference{Kind: "Pod", Name: "pod1", Namespace: namespace},
					}},
				}},
			}
			pod1 := createServicePod("pod1", "1.2.3.4", true, true)
			pod1.Status.Phase = tt.phase
			pod1.Status.Reason = tt.reason
			ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			node := corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}}
			fakeClient := fake.NewClientBuilder().WithObjects(endpoint, pod1, &ns, &node).Build()

			testClient := test.TestServerWithMockConnMgrWatcher(t, nil)
			consulClient := testClient.APIClient

			for _, svc := range []string{svcName, svcName + "-sidecar-proxy"} {
				_, err := consulClient.Catalog().Register(&api.CatalogRegistration{
					Node:     consulNodeName,
					Address:  consulNodeAddress,
					NodeMeta: map[string]string{metaKeySyntheticNode: "true"},
					Service: &api.AgentService{
						ID:      "pod1-" + svc,
						Service: svc,
						Address: "1.2.3.4",
						Meta: map[string]string{
							constants.MetaKeyKubeNS:  namespace,
							metaKeyKubeServiceName:   svcName,
							metaKeyManagedBy:         constants.ManagedByValue,
							metaKeySyntheticNode:     "true",
							constants.MetaKeyPodName: "pod1",
							constants.MetaKeyPodUID:  "",
						},
					},
				}, nil)
				require.NoError(t, err)
			}

			ep := &Controller{
				Client:                fakeClient,
				Log:                   logrtest.New(t),
				ConsulClientConfig:    testClient.Cfg,
				ConsulServerConnMgr:   testClient.Watcher,
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSetWith(),
				ReleaseName:           "consul",
				ReleaseNamespace:      namespace,
				LifecycleConfig: lifecycle.Config{
					DefaultEnableProxyLifecycle:       tt.shutdownGraceSecs > 0,
					DefaultShutdownGracePeriodSeconds: tt.shutdownGraceSecs,
				},
			}

			namespacedName := types.NamespacedName{Namespace: namespace, Name: svcName}
			resp, err := ep.Reconcile(context.Background(), ctrl.Request{NamespacedName: namespacedName})
			require.NoError(t, err)
			require.False(t, resp.Requeue)
			if tt.phase != corev1.PodRunning {
				// Nothing should be waiting on a graceful shutdown for a pod that already exited.
				require.Zero(t, resp.RequeueAfter)
			}

			for _, svc := range []string{svcName, svcName + "-sidecar-proxy"} {
				instances, _, err := consulClient.Catalog().Service(svc, "", nil)
				require.NoError(t, err)
				require.Len(t, instances, tt.expectedNumSvcInst, svc)
			}
		})
	}
}

func TestRequestsForTerminalPod(t *testing.T) {
	namespace := "default"
	endpoints := func(name, pod string) *corev1.Endpoints {
		return &corev1.Endpoints{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Subsets: []corev1.EndpointSubset{{
				Addresses: []corev1.EndpointAddress{{
					IP:        "1.2.3.4",
					TargetRef: &corev1.ObjectReference{Kind: "Pod", Name: pod, Namespace: namespace},
				}},
			}},
		}
	}
	controller := &Controller{
		Client: fake.NewClientBuilder().
			WithObjects(endpoints("svc-a", "pod1"), endpoints("svc-b", "pod1"), endpoints("svc-c", "other")).
			Build(),
		Log: logrtest.New(t),
	}
	podWithPhase := func(name string, phase corev1.PodPhase) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			Status:     corev1.PodStatus{Phase: phase},
		}
	}

	for _, phase := range []corev1.PodPhase{corev1.PodFailed, corev1.PodSucceeded} {
		t.Run(string(phase), func(t *testing.T) {
			requests := controller.requestsForTerminalPod(context.Background(), podWithPhase("pod1", phase))
			require.ElementsMatch(t, []reconcile.Request{
				{NamespacedName: types.NamespacedName{Namespace: namespace, Name: "svc-a"}},
				{NamespacedName: types.NamespacedName{Namespace: namespace, Name: "svc-b"}},
			}, requests)
		})
	}

	t.Run("non-terminal pods are ignored", func(t *testing.T) {
		for _, phase := range []corev1.PodPhase{corev1.PodPending, corev1.PodRunning, corev1.PodUnknown} {
			require.Empty(t, controller.requestsForTerminalPod(context.Background(), podWithPhase("pod1", phase)))
		}
	})
	t.Run("pod not referenced by any Endpoints", func(t *testing.T) {
		require.Empty(t, controller.requestsForTerminalPod(context.Background(), podWithPhase("unknown", corev1.PodFailed)))
	})
	t.Run("non-pod object", func(t *testing.T) {
		require.Empty(t, controller.requestsForTerminalPod(context.Background(), &corev1.Service{}))
	})
}
