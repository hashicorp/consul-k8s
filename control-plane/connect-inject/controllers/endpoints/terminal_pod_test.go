// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package endpoints

import (
	"context"
	"errors"
	"fmt"
	"testing"

	mapset "github.com/deckarep/golang-set"
	logrtest "github.com/go-logr/logr/testr"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil"
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

	// The pod object always remains. The EndpointSlice either still references it or has already dropped it.
	for _, sliceRetained := range []bool{true, false} {
		for name, tt := range cases {
			if !sliceRetained && tt.phase == corev1.PodRunning {
				// A running pod missing from the slice is a plain removal, not what is being tested here.
				continue
			}
			name := fmt.Sprintf("%s (slice retained: %t)", name, sliceRetained)
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
				if !sliceRetained {
					endpoint.Endpoints = nil
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

// Test that a pod that no longer exists, whose stale slice entry shares its IP with a healthy replacement, doesn't
// cause the replacement to be deregistered, and that its own registrations are removed, regardless of the order the
// endpoints are listed in. Terminal pods sharing an IP are covered by TestReconcile_TerminalPodIdentityMatching.
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
			newPod := createServicePod("new", "1.2.3.4", true, true)
			newPod.Status.Phase = corev1.PodRunning
			ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			node := corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}}
			fakeClient := fake.NewClientBuilder().WithScheme(endpointSliceTestScheme(t)).
				WithRuntimeObjects(slice, newPod, &ns, &node).Build()

			testClient := test.TestServerWithMockConnMgrWatcher(t, nil)
			consulClient := testClient.APIClient

			// The terminal pod's instances were registered while it was still running, at the IP the replacement now has.
			for _, svc := range []string{svcName, svcName + "-sidecar-proxy"} {
				_, err := consulClient.Catalog().Register(&api.CatalogRegistration{
					Node:     consulNodeName,
					Address:  consulNodeAddress,
					NodeMeta: map[string]string{metaKeySyntheticNode: "true"},
					Service: &api.AgentService{
						ID:      "old-" + svc,
						Service: svc,
						Address: "1.2.3.4",
						Meta: map[string]string{
							constants.MetaKeyKubeNS:  namespace,
							metaKeyKubeServiceName:   svcName,
							metaKeyManagedBy:         constants.ManagedByValue,
							metaKeySyntheticNode:     "true",
							constants.MetaKeyPodName: "old",
							constants.MetaKeyPodUID:  "uid-old",
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
				// The terminal pod's instance is removed by pod identity even though its address is kept for the replacement.
				require.NotContains(t, ids, "old-"+svc)
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

func TestIsTerminalPodInstance(t *testing.T) {
	namespace := "default"
	podWithPhase := func(name string, uid types.UID, phase corev1.PodPhase) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, UID: uid},
			Status:     corev1.PodStatus{Phase: phase},
		}
	}
	instance := func(name, uid string) *api.CatalogService {
		return &api.CatalogService{ServiceMeta: map[string]string{
			constants.MetaKeyPodName: name, constants.MetaKeyPodUID: uid, constants.MetaKeyKubeNS: namespace,
		}}
	}
	controller := &Controller{
		Client: fake.NewClientBuilder().WithScheme(endpointSliceTestScheme(t)).WithRuntimeObjects(
			podWithPhase("failed", "uid-failed", corev1.PodFailed),
			podWithPhase("succeeded", "uid-succeeded", corev1.PodSucceeded),
			podWithPhase("running", "uid-running", corev1.PodRunning),
		).Build(),
		Log: logrtest.New(t),
	}
	check := func(svc *api.CatalogService) bool {
		terminal, err := controller.isTerminalPodInstance(context.Background(), svc, namespace)
		require.NoError(t, err)
		return terminal
	}

	require.True(t, check(instance("failed", "uid-failed")))
	require.True(t, check(instance("succeeded", "uid-succeeded")))
	require.True(t, check(instance("failed", "")), "instances without a UID match by name")
	require.False(t, check(instance("failed", "uid-other")), "same name, different pod")
	require.False(t, check(instance("running", "uid-running")))
	require.True(t, check(instance("missing", "uid-missing")), "pod no longer exists; its registration is stale")
	require.False(t, check(&api.CatalogService{ServiceMeta: map[string]string{}}), "instance without pod metadata")
}

// seedInstances registers a service and proxy instance for a pod at the given address.
func seedInstances(t *testing.T, consulClient *api.Client, svcName, namespace, podName, podUID, address string) {
	t.Helper()
	for _, svc := range []string{svcName, svcName + "-sidecar-proxy"} {
		_, err := consulClient.Catalog().Register(&api.CatalogRegistration{
			Node:     consulNodeName,
			Address:  consulNodeAddress,
			NodeMeta: map[string]string{metaKeySyntheticNode: "true"},
			Service: &api.AgentService{
				ID:      podName + "-" + svc,
				Service: svc,
				Address: address,
				Meta: map[string]string{
					constants.MetaKeyKubeNS:  namespace,
					metaKeyKubeServiceName:   svcName,
					metaKeyManagedBy:         constants.ManagedByValue,
					metaKeySyntheticNode:     "true",
					constants.MetaKeyPodName: podName,
					constants.MetaKeyPodUID:  podUID,
				},
			},
		}, nil)
		require.NoError(t, err)
	}
}

func instanceIDs(t *testing.T, consulClient *api.Client, svc string) []string {
	t.Helper()
	instances, _, err := consulClient.Catalog().Service(svc, "", nil)
	require.NoError(t, err)
	var ids []string
	for _, instance := range instances {
		ids = append(ids, instance.ServiceID)
	}
	return ids
}

// Test how instances of a terminal pod that shares its IP with a healthy replacement are matched by pod identity.
func TestReconcile_TerminalPodIdentityMatching(t *testing.T) {
	t.Parallel()
	svcName := "service-identity"
	namespace := "default"

	cases := map[string]struct {
		instanceUID   string
		expectOldGone bool
	}{
		"instance UID matches the terminal pod":                    {instanceUID: "uid-old", expectOldGone: true},
		"instance registered without a UID is matched by pod name": {instanceUID: "", expectOldGone: true},
		"instance with a different UID is not the terminal pod's":  {instanceUID: "uid-reborn", expectOldGone: false},
	}

	for name, tt := range cases {
		for _, terminalFirst := range []bool{true, false} {
			for _, graceSecs := range []int{0, 30} {
				testName := fmt.Sprintf("%s (terminal first: %t, grace period: %d)", name, terminalFirst, graceSecs)
				t.Run(testName, func(t *testing.T) {
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
					oldPod.UID = "uid-old"
					oldPod.Status.Phase = corev1.PodFailed
					oldPod.Status.Reason = "Evicted"
					newPod := createServicePod("new", "1.2.3.4", true, true)
					newPod.UID = "uid-new"
					newPod.Status.Phase = corev1.PodRunning
					ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
					node := corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}}
					fakeClient := fake.NewClientBuilder().WithScheme(endpointSliceTestScheme(t)).
						WithRuntimeObjects(slice, oldPod, newPod, &ns, &node).Build()

					testClient := test.TestServerWithMockConnMgrWatcher(t, nil)
					consulClient := testClient.APIClient
					seedInstances(t, consulClient, svcName, namespace, "old", tt.instanceUID, "1.2.3.4")

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
							DefaultEnableProxyLifecycle:       graceSecs > 0,
							DefaultShutdownGracePeriodSeconds: graceSecs,
						},
					}
					resp, err := ep.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: namespace, Name: svcName}})
					require.NoError(t, err)
					require.Zero(t, resp.RequeueAfter, "no graceful shutdown wait for a terminal pod or its replacement")

					for _, svc := range []string{svcName, svcName + "-sidecar-proxy"} {
						ids := instanceIDs(t, consulClient, svc)
						require.Contains(t, ids, "new-"+svc, "replacement must always remain")
						if tt.expectOldGone {
							require.NotContains(t, ids, "old-"+svc)
						} else {
							require.Contains(t, ids, "old-"+svc)
						}
					}
				})
			}
		}
	}
}

// A stale entry for a pod that no longer exists must not cause a live pod that reuses its IP to be deregistered.
func TestReconcile_DeletedPodEntrySharingIPWithLivePod(t *testing.T) {
	t.Parallel()
	svcName := "service-deleted-ip"
	namespace := "default"

	for name, deletedFirst := range map[string]bool{"deleted pod first": true, "deleted pod last": false} {
		t.Run(name, func(t *testing.T) {
			endpointFor := func(pod string) discoveryv1.Endpoint {
				return discoveryv1.Endpoint{
					Conditions: discoveryv1.EndpointConditions{Ready: ptr.To(true)},
					Addresses:  []string{"1.2.3.4"},
					TargetRef:  &corev1.ObjectReference{Kind: "Pod", Name: pod, Namespace: namespace},
				}
			}
			endpoints := []discoveryv1.Endpoint{endpointFor("gone"), endpointFor("live")}
			if !deletedFirst {
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
			livePod := createServicePod("live", "1.2.3.4", true, true)
			ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			node := corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}}
			fakeClient := fake.NewClientBuilder().WithScheme(endpointSliceTestScheme(t)).
				WithRuntimeObjects(slice, livePod, &ns, &node).Build()

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

			for _, svc := range []string{svcName, svcName + "-sidecar-proxy"} {
				require.Contains(t, instanceIDs(t, consulClient, svc), "live-"+svc)
			}
		})
	}
}

// Test that a terminal pod without consul-dataplane, registered on a node-local Consul client, is not deregistered
// (the catalog cleanup can't reach it) but has its health check marked critical, even if its endpoint is still ready.
func TestReconcile_TerminalLegacyPodHealthCheckIsCritical(t *testing.T) {
	t.Parallel()
	svcName := "service-legacy-terminal"
	namespace := "default"

	for _, phase := range []corev1.PodPhase{corev1.PodFailed, corev1.PodSucceeded} {
		for _, endpointReady := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s pod, endpoint ready: %t", phase, endpointReady), func(t *testing.T) {
				pod1 := createServicePod("pod1", "1.2.3.4", true, true)
				pod1.Status.HostIP = "127.0.0.1"
				pod1.Status.Phase = phase
				pod1.Annotations[constants.LegacyAnnotationConsulK8sVersion] = "0.99.0" // Less than 1.0.0, so no consul-dataplane.
				endpoint := &discoveryv1.EndpointSlice{
					ObjectMeta: metav1.ObjectMeta{
						Name:      svcName,
						Namespace: namespace,
						Labels:    map[string]string{discoveryv1.LabelServiceName: svcName},
					},
					AddressType: discoveryv1.AddressTypeIPv4,
					Endpoints: []discoveryv1.Endpoint{{
						Conditions: discoveryv1.EndpointConditions{Ready: ptr.To(endpointReady)},
						Addresses:  []string{"1.2.3.4"},
						TargetRef:  &corev1.ObjectReference{Kind: "Pod", Name: "pod1", Namespace: namespace},
					}},
				}
				ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
				fakeClient := fake.NewClientBuilder().WithScheme(endpointSliceTestScheme(t)).WithRuntimeObjects(pod1, endpoint, &ns).Build()

				testClient := test.TestServerWithMockConnMgrWatcher(t, nil)

				var consulClientHttpPort int
				consulClientAgent, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
					c.Server = false
					c.Bootstrap = false
					consulClientHttpPort = c.Ports.HTTP
				})
				require.NoError(t, err)
				consulClientAgent.JoinLAN(t, testClient.TestServer.LANAddr)
				consulClientAgent.WaitForSerfCheck(t)

				consulClient, err := api.NewClient(&api.Config{Address: consulClientAgent.HTTPAddr})
				require.NoError(t, err)

				serviceID := "pod1-" + svcName
				require.NoError(t, consulClient.Agent().ServiceRegister(&api.AgentServiceRegistration{
					ID:      serviceID,
					Name:    svcName,
					Port:    80,
					Address: "1.2.3.4",
					Check: &api.AgentServiceCheck{
						CheckID: "default/" + serviceID + "/kubernetes-health-check",
						TTL:     "100000h",
						Name:    "Kubernetes Health Check",
						Status:  api.HealthPassing,
					},
				}))
				require.NoError(t, consulClient.Agent().ServiceRegister(&api.AgentServiceRegistration{
					Kind:    api.ServiceKindConnectProxy,
					ID:      serviceID + "-sidecar-proxy",
					Name:    svcName + "-sidecar-proxy",
					Port:    20000,
					Address: "1.2.3.4",
					Proxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: svcName,
						DestinationServiceID:   serviceID,
					},
				}))

				ep := &Controller{
					Client:                fakeClient,
					Log:                   logrtest.New(t),
					ConsulClientConfig:    testClient.Cfg,
					ConsulServerConnMgr:   testClient.Watcher,
					AllowK8sNamespacesSet: mapset.NewSetWith("*"),
					DenyK8sNamespacesSet:  mapset.NewSetWith(),
					ReleaseName:           "consul",
					ReleaseNamespace:      namespace,
					consulClientHttpPort:  consulClientHttpPort,
				}
				_, err = ep.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: namespace, Name: svcName}})
				require.NoError(t, err)

				checks, err := consulClient.Agent().ChecksWithFilter(fmt.Sprintf("ServiceID == %q", serviceID))
				require.NoError(t, err)
				require.Len(t, checks, 1)
				for _, check := range checks {
					require.Equal(t, api.HealthCritical, check.Status)
				}

				// The registration on the node-local client is not removed.
				services, err := consulClient.Agent().Services()
				require.NoError(t, err)
				require.Contains(t, services, serviceID)
			})
		}
	}
}

// Test that a terminal pod is cleaned up even when Kubernetes has already dropped it from the EndpointSlice and a
// healthy replacement has reused its IP, so reconciliation has no slice entry to learn the old pod's identity from.
func TestReconcile_TerminalPodAbsentFromSliceWithReplacementAtSameIP(t *testing.T) {
	t.Parallel()
	svcName := "service-absent-from-slice"
	namespace := "default"

	for _, graceSecs := range []int{0, 30} {
		t.Run(fmt.Sprintf("grace period: %d", graceSecs), func(t *testing.T) {
			// Only the replacement is in the slice, at the IP the old pod used to have.
			slice := &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      svcName,
					Namespace: namespace,
					Labels:    map[string]string{discoveryv1.LabelServiceName: svcName},
				},
				AddressType: discoveryv1.AddressTypeIPv4,
				Endpoints: []discoveryv1.Endpoint{{
					Conditions: discoveryv1.EndpointConditions{Ready: ptr.To(true)},
					Addresses:  []string{"1.2.3.4"},
					TargetRef:  &corev1.ObjectReference{Kind: "Pod", Name: "new", Namespace: namespace},
				}},
			}
			oldPod := createServicePod("old", "1.2.3.4", true, true)
			oldPod.UID = "uid-old"
			oldPod.Status.Phase = corev1.PodFailed
			oldPod.Status.Reason = "Evicted"
			newPod := createServicePod("new", "1.2.3.4", true, true)
			newPod.UID = "uid-new"
			ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			node := corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}}
			fakeClient := fake.NewClientBuilder().WithScheme(endpointSliceTestScheme(t)).
				WithRuntimeObjects(slice, oldPod, newPod, &ns, &node).Build()

			testClient := test.TestServerWithMockConnMgrWatcher(t, nil)
			consulClient := testClient.APIClient
			seedInstances(t, consulClient, svcName, namespace, "old", "uid-old", "1.2.3.4")

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
					DefaultEnableProxyLifecycle:       graceSecs > 0,
					DefaultShutdownGracePeriodSeconds: graceSecs,
				},
			}
			resp, err := ep.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: namespace, Name: svcName}})
			require.NoError(t, err)
			require.Zero(t, resp.RequeueAfter)

			for _, svc := range []string{svcName, svcName + "-sidecar-proxy"} {
				ids := instanceIDs(t, consulClient, svc)
				require.Contains(t, ids, "new-"+svc, "replacement must remain")
				require.NotContains(t, ids, "old-"+svc, "terminal pod's registrations must be removed")
			}
		})
	}
}

// Test that a terminal pod is mapped to its Services through Service selectors even when no EndpointSlice references it.
func TestRequestsForTerminalPod_ServiceSelector(t *testing.T) {
	namespace := "default"
	service := func(name string, selector map[string]string) *corev1.Service {
		return &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			Spec:       corev1.ServiceSpec{Selector: selector},
		}
	}
	// svc-slice is already mapped through a slice and must not be enqueued twice.
	slice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "slice",
			Namespace: namespace,
			Labels:    map[string]string{discoveryv1.LabelServiceName: "svc-slice"},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
		Endpoints: []discoveryv1.Endpoint{{
			Addresses: []string{"1.2.3.4"},
			TargetRef: &corev1.ObjectReference{Kind: "Pod", Name: "pod1", Namespace: namespace},
		}},
	}
	controller := &Controller{
		Client: fake.NewClientBuilder().WithScheme(endpointSliceTestScheme(t)).WithRuntimeObjects(
			slice,
			service("svc-slice", map[string]string{"app": "web"}),
			service("svc-selector", map[string]string{"app": "web"}),
			service("svc-other-app", map[string]string{"app": "other"}),
			service("svc-no-selector", nil),
		).Build(),
		Log: logrtest.New(t),
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod1", Namespace: namespace, Labels: map[string]string{"app": "web"}},
		Status:     corev1.PodStatus{Phase: corev1.PodFailed},
	}

	require.ElementsMatch(t, []reconcile.Request{
		{NamespacedName: types.NamespacedName{Namespace: namespace, Name: "svc-slice"}},
		{NamespacedName: types.NamespacedName{Namespace: namespace, Name: "svc-selector"}},
	}, controller.requestsForTerminalPod(context.Background(), pod))

	t.Run("pod not referenced by any slice is still mapped by selector", func(t *testing.T) {
		unreferenced := pod.DeepCopy()
		unreferenced.Name = "unreferenced"
		require.ElementsMatch(t, []reconcile.Request{
			{NamespacedName: types.NamespacedName{Namespace: namespace, Name: "svc-slice"}},
			{NamespacedName: types.NamespacedName{Namespace: namespace, Name: "svc-selector"}},
		}, controller.requestsForTerminalPod(context.Background(), unreferenced))
	})
	t.Run("running pods are ignored", func(t *testing.T) {
		running := pod.DeepCopy()
		running.Status.Phase = corev1.PodRunning
		require.Empty(t, controller.requestsForTerminalPod(context.Background(), running))
	})
}

// Test that a failed pod lookup during metadata-based cleanup keeps the registrations and returns an error, so the
// reconcile is retried instead of leaving the stale instances behind with no further trigger.
func TestReconcile_TerminalPodLookupErrorIsReturnedForRetry(t *testing.T) {
	t.Parallel()
	svcName := "service-lookup-error"
	namespace := "default"

	// Only the replacement is in the slice; the old pod is found solely through the instance metadata.
	slice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcName,
			Namespace: namespace,
			Labels:    map[string]string{discoveryv1.LabelServiceName: svcName},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
		Endpoints: []discoveryv1.Endpoint{{
			Conditions: discoveryv1.EndpointConditions{Ready: ptr.To(true)},
			Addresses:  []string{"1.2.3.4"},
			TargetRef:  &corev1.ObjectReference{Kind: "Pod", Name: "new", Namespace: namespace},
		}},
	}
	oldPod := createServicePod("old", "1.2.3.4", true, true)
	oldPod.UID = "uid-old"
	oldPod.Status.Phase = corev1.PodFailed
	newPod := createServicePod("new", "1.2.3.4", true, true)
	newPod.UID = "uid-new"
	ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
	node := corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}}

	failOldPodLookup := true
	fakeClient := fake.NewClientBuilder().WithScheme(endpointSliceTestScheme(t)).
		WithRuntimeObjects(slice, oldPod, newPod, &ns, &node).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if failOldPodLookup && key.Name == "old" {
					return errors.New("API blip")
				}
				return c.Get(ctx, key, obj, opts...)
			},
		}).Build()

	testClient := test.TestServerWithMockConnMgrWatcher(t, nil)
	consulClient := testClient.APIClient
	seedInstances(t, consulClient, svcName, namespace, "old", "uid-old", "1.2.3.4")

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
	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: namespace, Name: svcName}}

	_, err := ep.Reconcile(context.Background(), req)
	require.Error(t, err)
	require.ErrorContains(t, err, "API blip")
	for _, svc := range []string{svcName, svcName + "-sidecar-proxy"} {
		ids := instanceIDs(t, consulClient, svc)
		require.Contains(t, ids, "old-"+svc, "registrations are preserved while the lookup fails")
		require.Contains(t, ids, "new-"+svc)
	}

	// The retry succeeds once the lookup recovers.
	failOldPodLookup = false
	_, err = ep.Reconcile(context.Background(), req)
	require.NoError(t, err)
	for _, svc := range []string{svcName, svcName + "-sidecar-proxy"} {
		ids := instanceIDs(t, consulClient, svc)
		require.NotContains(t, ids, "old-"+svc)
		require.Contains(t, ids, "new-"+svc)
	}
}
