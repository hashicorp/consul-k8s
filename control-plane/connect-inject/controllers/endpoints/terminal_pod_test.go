// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package endpoints

import (
	"context"
	"errors"
	"testing"

	mapset "github.com/deckarep/golang-set"
	logrtest "github.com/go-logr/logr/testr"
	"github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
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
// though the Pod object and its EndpointSlice entry still exist.
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
			endpoint := &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      svcName,
					Namespace: namespace,
					Labels:    map[string]string{discoveryv1.LabelServiceName: svcName},
				},
				AddressType: discoveryv1.AddressTypeIPv4,
				Endpoints: []discoveryv1.Endpoint{{
					Conditions: discoveryv1.EndpointConditions{Ready: ptr.To(tt.phase == corev1.PodRunning)},
					Addresses:  []string{"1.2.3.4"},
					TargetRef:  &corev1.ObjectReference{Kind: "Pod", Name: "pod1", Namespace: namespace},
				}},
			}
			pod1 := createServicePod("pod1", "1.2.3.4", true, true)
			pod1.Status.Phase = tt.phase
			pod1.Status.Reason = tt.reason
			ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			node := corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}}
			k8sObjects := []runtime.Object{endpoint, pod1, &ns, &node}
			fakeClient := fake.NewClientBuilder().WithScheme(endpointSliceTestScheme(t)).WithRuntimeObjects(k8sObjects...).Build()

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
	slice := func(name, svc, pod string) *discoveryv1.EndpointSlice {
		return &discoveryv1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				Labels:    map[string]string{discoveryv1.LabelServiceName: svc},
			},
			AddressType: discoveryv1.AddressTypeIPv4,
			Endpoints: []discoveryv1.Endpoint{{
				Addresses: []string{"1.2.3.4"},
				TargetRef: &corev1.ObjectReference{Kind: "Pod", Name: pod, Namespace: namespace},
			}},
		}
	}
	controller := &Controller{
		Client: fake.NewClientBuilder().
			WithScheme(endpointSliceTestScheme(t)).
			WithRuntimeObjects(slice("slice-a", "svc-a", "pod1"), slice("slice-b", "svc-b", "pod1"), slice("slice-c", "svc-c", "other")).
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
	t.Run("pod not referenced by any slice", func(t *testing.T) {
		require.Empty(t, controller.requestsForTerminalPod(context.Background(), podWithPhase("unknown", corev1.PodFailed)))
	})
	t.Run("non-pod object", func(t *testing.T) {
		require.Empty(t, controller.requestsForTerminalPod(context.Background(), &corev1.Service{}))
	})
}

// Test that a terminal pod sharing its IP with a healthy replacement doesn't cause the replacement to be
// deregistered, regardless of the order the endpoints are listed in.
func TestReconcile_TerminalPodSharingIPWithReplacement(t *testing.T) {
	t.Parallel()
	svcName := "service-ip-reuse"
	namespace := "default"

	for name, terminalFirst := range map[string]bool{"terminal pod first": true, "terminal pod last": false} {
		t.Run(name, func(t *testing.T) {
			endpointFor := func(pod string, ready bool) discoveryv1.Endpoint {
				return discoveryv1.Endpoint{
					Conditions: discoveryv1.EndpointConditions{Ready: ptr.To(ready)},
					Addresses:  []string{"1.2.3.4"},
					TargetRef:  &corev1.ObjectReference{Kind: "Pod", Name: pod, Namespace: namespace},
				}
			}
			endpoints := []discoveryv1.Endpoint{endpointFor("old", false), endpointFor("new", true)}
			if !terminalFirst {
				endpoints[0], endpoints[1] = endpoints[1], endpoints[0]
			}
			slice := &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      svcName,
					Namespace: namespace,
					Labels:    map[string]string{discoveryv1.LabelServiceName: svcName},
				},
				AddressType: discoveryv1.AddressTypeIPv4,
				Endpoints:   endpoints,
			}
			oldPod := createServicePod("old", "1.2.3.4", true, true)
			oldPod.Status.Phase = corev1.PodFailed
			oldPod.Status.Reason = "Evicted"
			newPod := createServicePod("new", "1.2.3.4", true, true)
			newPod.Status.Phase = corev1.PodRunning
			ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			node := corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}}
			fakeClient := fake.NewClientBuilder().WithScheme(endpointSliceTestScheme(t)).
				WithRuntimeObjects(slice, oldPod, newPod, &ns, &node).Build()

			testClient := test.TestServerWithMockConnMgrWatcher(t, nil)
			consulClient := testClient.APIClient

			ep := &Controller{
				Client:                fakeClient,
				Log:                   logrtest.New(t),
				ConsulClientConfig:    testClient.Cfg,
				ConsulServerConnMgr:   testClient.Watcher,
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSetWith(),
				ReleaseName:           "consul",
				ReleaseNamespace:      namespace,
			}

			_, err := ep.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: namespace, Name: svcName}})
			require.NoError(t, err)

			// Registration happens before cleanup, so the replacement only survives if it wasn't marked for deregistration.
			for _, svc := range []string{svcName, svcName + "-sidecar-proxy"} {
				instances, _, err := consulClient.Catalog().Service(svc, "", nil)
				require.NoError(t, err)
				var ids []string
				for _, instance := range instances {
					ids = append(ids, instance.ServiceID)
				}
				require.Contains(t, ids, "new-"+svc)
			}
		})
	}
}

func TestMarkForDeregistration(t *testing.T) {
	cases := map[string]struct {
		initial  map[string]bool
		expected map[string]bool
	}{
		"unseen IP is marked for deregistration": {
			initial:  map[string]bool{},
			expected: map[string]bool{"1.2.3.4": true},
		},
		"IP already marked for deregistration stays marked": {
			initial:  map[string]bool{"1.2.3.4": true},
			expected: map[string]bool{"1.2.3.4": true},
		},
		"IP marked to keep is not overwritten": {
			initial:  map[string]bool{"1.2.3.4": false},
			expected: map[string]bool{"1.2.3.4": false},
		},
		"other IPs are unaffected": {
			initial:  map[string]bool{"5.6.7.8": false},
			expected: map[string]bool{"5.6.7.8": false, "1.2.3.4": true},
		},
	}
	for name, tt := range cases {
		t.Run(name, func(t *testing.T) {
			markForDeregistration(tt.initial, "1.2.3.4")
			require.Equal(t, tt.expected, tt.initial)
		})
	}
}

// A pod whose lookup fails with a non-NotFound error is kept (not deregistered), and a stale terminal entry
// sharing its IP must not flip that decision.
func TestReconcile_PodGetErrorIsNotOverriddenByTerminalPodAtSameIP(t *testing.T) {
	t.Parallel()
	svcName := "service-get-error"
	namespace := "default"

	endpointFor := func(pod string) discoveryv1.Endpoint {
		return discoveryv1.Endpoint{
			Conditions: discoveryv1.EndpointConditions{Ready: ptr.To(true)},
			Addresses:  []string{"1.2.3.4"},
			TargetRef:  &corev1.ObjectReference{Kind: "Pod", Name: pod, Namespace: namespace},
		}
	}
	for name, errFirst := range map[string]bool{"errored pod first": true, "errored pod last": false} {
		t.Run(name, func(t *testing.T) {
			endpoints := []discoveryv1.Endpoint{endpointFor("flaky"), endpointFor("old")}
			if !errFirst {
				endpoints[0], endpoints[1] = endpoints[1], endpoints[0]
			}
			slice := &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      svcName,
					Namespace: namespace,
					Labels:    map[string]string{discoveryv1.LabelServiceName: svcName},
				},
				AddressType: discoveryv1.AddressTypeIPv4,
				Endpoints:   endpoints,
			}
			oldPod := createServicePod("old", "1.2.3.4", true, true)
			oldPod.Status.Phase = corev1.PodFailed
			ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			node := corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}}
			fakeClient := fake.NewClientBuilder().WithScheme(endpointSliceTestScheme(t)).
				WithRuntimeObjects(slice, oldPod, &ns, &node).
				WithInterceptorFuncs(interceptor.Funcs{
					Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						if key.Name == "flaky" {
							return errors.New("API blip")
						}
						return c.Get(ctx, key, obj, opts...)
					},
				}).Build()

			testClient := test.TestServerWithMockConnMgrWatcher(t, nil)
			consulClient := testClient.APIClient

			_, err := consulClient.Catalog().Register(&api.CatalogRegistration{
				Node:     consulNodeName,
				Address:  consulNodeAddress,
				NodeMeta: map[string]string{metaKeySyntheticNode: "true"},
				Service: &api.AgentService{
					ID:      "flaky-" + svcName,
					Service: svcName,
					Address: "1.2.3.4",
					Meta: map[string]string{
						constants.MetaKeyKubeNS:  namespace,
						metaKeyKubeServiceName:   svcName,
						metaKeyManagedBy:         constants.ManagedByValue,
						metaKeySyntheticNode:     "true",
						constants.MetaKeyPodName: "flaky",
						constants.MetaKeyPodUID:  "",
					},
				},
			}, nil)
			require.NoError(t, err)

			ep := &Controller{
				Client:                fakeClient,
				Log:                   logrtest.New(t),
				ConsulClientConfig:    testClient.Cfg,
				ConsulServerConnMgr:   testClient.Watcher,
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSetWith(),
				ReleaseName:           "consul",
				ReleaseNamespace:      namespace,
			}
			// The reconcile returns the lookup error, but must not deregister the instance at the shared IP.
			_, err = ep.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: namespace, Name: svcName}})
			require.Error(t, err)

			instances, _, err := consulClient.Catalog().Service(svcName, "", nil)
			require.NoError(t, err)
			require.Len(t, instances, 1)
			require.Equal(t, "flaky-"+svcName, instances[0].ServiceID)
		})
	}
}
