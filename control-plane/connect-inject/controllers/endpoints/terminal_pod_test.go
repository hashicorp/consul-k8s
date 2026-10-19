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
	"k8s.io/apimachinery/pkg/runtime"
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

// Test that service instances of terminal pods are deregistered while their Endpoints entry still exists.
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
			endpoints := &corev1.Endpoints{
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
			k8sObjects := []runtime.Object{endpoints, pod1, &ns, &node}
			fakeClient := fake.NewClientBuilder().WithRuntimeObjects(k8sObjects...).Build()

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
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
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
			WithRuntimeObjects(endpoints("svc-a", "pod1"), endpoints("svc-b", "pod1"), endpoints("svc-c", "other")).
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

func TestMarkForDeregistrationWithSharedIP(t *testing.T) {
	for name, registerFirst := range map[string]bool{"terminal pod first": false, "replacement first": true} {
		t.Run(name, func(t *testing.T) {
			addresses := map[string]bool{}
			if registerFirst {
				addresses["1.2.3.4"] = false
				markForDeregistration(addresses, "1.2.3.4")
			} else {
				markForDeregistration(addresses, "1.2.3.4")
				addresses["1.2.3.4"] = false
			}
			require.False(t, addresses["1.2.3.4"])
		})
	}

	t.Run("lookup error is not overridden", func(t *testing.T) {
		addresses := map[string]bool{"1.2.3.4": false}
		markForDeregistration(addresses, "1.2.3.4")
		require.False(t, addresses["1.2.3.4"])
	})
}

func TestIsTerminalPodInstance(t *testing.T) {
	const namespace = "default"
	pods := []runtime.Object{
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "failed", Namespace: namespace, UID: "uid-failed"},
			Status:     corev1.PodStatus{Phase: corev1.PodFailed},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "running", Namespace: namespace, UID: "uid-running"},
			Status:     corev1.PodStatus{Phase: corev1.PodRunning},
		},
	}
	controller := &Controller{Client: fake.NewClientBuilder().WithRuntimeObjects(pods...).Build()}
	serviceInstance := func(podName, podUID string) *api.CatalogService {
		return &api.CatalogService{ServiceMeta: map[string]string{
			constants.MetaKeyPodName: podName,
			constants.MetaKeyPodUID:  podUID,
			constants.MetaKeyKubeNS:  namespace,
		}}
	}
	check := func(svc *api.CatalogService) bool {
		terminal, err := controller.isTerminalPodInstance(context.Background(), svc, namespace)
		require.NoError(t, err)
		return terminal
	}

	require.True(t, check(serviceInstance("failed", "uid-failed")))
	require.True(t, check(serviceInstance("failed", "")))
	require.False(t, check(serviceInstance("failed", "uid-replaced")))
	require.False(t, check(serviceInstance("running", "uid-running")))
	require.True(t, check(serviceInstance("missing", "uid-missing")))
	require.False(t, check(&api.CatalogService{}))
}

func TestRequestsForTerminalPod_ServiceSelector(t *testing.T) {
	const namespace = "default"
	endpoints := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{Name: "svc-slice", Namespace: namespace},
		Subsets: []corev1.EndpointSubset{{
			Addresses: []corev1.EndpointAddress{{
				TargetRef: &corev1.ObjectReference{Kind: "Pod", Name: "pod1", Namespace: namespace},
			}},
		}},
	}
	services := []runtime.Object{
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "svc-slice", Namespace: namespace},
			Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": "web"}},
		},
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "svc-selector", Namespace: namespace},
			Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": "web"}},
		},
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "svc-other", Namespace: namespace},
			Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": "other"}},
		},
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "svc-no-selector", Namespace: namespace},
		},
	}
	controller := &Controller{
		Client: fake.NewClientBuilder().WithRuntimeObjects(append([]runtime.Object{endpoints}, services...)...).Build(),
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod1",
			Namespace: namespace,
			Labels:    map[string]string{"app": "web"},
		},
		Status: corev1.PodStatus{Phase: corev1.PodFailed},
	}

	require.ElementsMatch(t, []reconcile.Request{
		{NamespacedName: types.NamespacedName{Namespace: namespace, Name: "svc-slice"}},
		{NamespacedName: types.NamespacedName{Namespace: namespace, Name: "svc-selector"}},
	}, controller.requestsForTerminalPod(context.Background(), pod))
}

func TestReconcile_TerminalPodSharingIPWithReplacement(t *testing.T) {
	t.Parallel()

	const (
		serviceName = "service-ip-reuse"
		namespace   = "default"
	)

	for name, tc := range map[string]struct {
		terminalFirst    bool
		terminalIncluded bool
	}{
		"terminal pod first":                 {terminalFirst: true, terminalIncluded: true},
		"terminal pod last":                  {terminalFirst: false, terminalIncluded: true},
		"terminal pod absent from Endpoints": {terminalIncluded: false},
	} {
		t.Run(name, func(t *testing.T) {
			terminalAddress := corev1.EndpointAddress{
				IP:        "1.2.3.4",
				TargetRef: &corev1.ObjectReference{Kind: "Pod", Name: "old", Namespace: namespace},
			}
			replacementAddress := corev1.EndpointAddress{
				IP:        "1.2.3.4",
				TargetRef: &corev1.ObjectReference{Kind: "Pod", Name: "new", Namespace: namespace},
			}
			addresses := []corev1.EndpointAddress{replacementAddress}
			if tc.terminalIncluded {
				addresses = append(addresses, terminalAddress)
			}
			if tc.terminalIncluded && !tc.terminalFirst {
				addresses[0], addresses[1] = addresses[1], addresses[0]
			}
			endpoints := &corev1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{Name: serviceName, Namespace: namespace},
				Subsets: []corev1.EndpointSubset{{
					Addresses: addresses,
				}},
			}
			oldPod := createServicePod("old", "1.2.3.4", true, true)
			oldPod.UID = "old-uid"
			oldPod.Status.Phase = corev1.PodFailed
			newPod := createServicePod("new", "1.2.3.4", true, true)
			newPod.UID = "new-uid"

			fakeClient := fake.NewClientBuilder().WithRuntimeObjects(
				endpoints,
				oldPod,
				newPod,
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}},
				&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}},
			).Build()
			testClient := test.TestServerWithMockConnMgrWatcher(t, nil)
			consulClient := testClient.APIClient

			for _, svc := range []string{serviceName, serviceName + "-sidecar-proxy"} {
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
							metaKeyKubeServiceName:   serviceName,
							metaKeyManagedBy:         constants.ManagedByValue,
							metaKeySyntheticNode:     "true",
							constants.MetaKeyPodName: "old",
							constants.MetaKeyPodUID:  string(oldPod.UID),
						},
					},
				}, nil)
				require.NoError(t, err)
			}

			controller := &Controller{
				Client:                fakeClient,
				Log:                   logrtest.New(t),
				ConsulClientConfig:    testClient.Cfg,
				ConsulServerConnMgr:   testClient.Watcher,
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSetWith(),
				ReleaseName:           "consul",
				ReleaseNamespace:      namespace,
			}
			_, err := controller.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: types.NamespacedName{Namespace: namespace, Name: serviceName},
			})
			require.NoError(t, err)

			for _, svc := range []string{serviceName, serviceName + "-sidecar-proxy"} {
				instances, _, err := consulClient.Catalog().Service(svc, "", nil)
				require.NoError(t, err)
				var ids []string
				for _, instance := range instances {
					ids = append(ids, instance.ServiceID)
				}
				require.Contains(t, ids, "new-"+svc)
				require.NotContains(t, ids, "old-"+svc)
			}
		})
	}
}
