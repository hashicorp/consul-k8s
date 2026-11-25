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
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
)

const ambientNetNS = "/var/run/netns/cni-1234"

func createAmbientPod(name, ip, status string) *corev1.Pod {
	pod := createServicePod(name, ip, true, true)
	pod.Labels[constants.KeyAmbientMesh] = constants.LabelAmbientMeshEnabled
	pod.Annotations[constants.KeyAmbientMeshStatus] = status
	if status == constants.AmbientMeshStatusComplete {
		pod.Annotations[constants.AnnotationAmbientNetNS] = ambientNetNS
	}
	pod.Annotations[constants.AnnotationPort] = "8080"
	pod.Annotations[constants.AnnotationUpstreams] = "backend:1234"
	return pod
}

func createAmbientNodeProxyPod(node string, ready bool) *corev1.Pod {
	status := corev1.ConditionFalse
	if ready {
		status = corev1.ConditionTrue
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "consul-ambient-mesh-" + node,
			Namespace: "consul",
			Labels:    map[string]string{"app": "consul", "component": "ambient-mesh"},
		},
		Spec:   corev1.PodSpec{NodeName: node},
		Status: corev1.PodStatus{Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: status}}},
	}
}

func ambientTestObjects(podStatus string, nodeProxyReady bool) []runtime.Object {
	ready := true
	pod := createAmbientPod("pod1", "1.2.3.4", podStatus)
	slice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "web",
			Namespace: "default",
			Labels:    map[string]string{discoveryv1.LabelServiceName: "web"},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
		Endpoints: []discoveryv1.Endpoint{{
			Addresses:  []string{"1.2.3.4"},
			Conditions: discoveryv1.EndpointConditions{Ready: &ready},
			TargetRef:  &corev1.ObjectReference{Kind: "Pod", Name: "pod1", Namespace: "default"},
		}},
	}
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "default"},
		Spec: corev1.ServiceSpec{
			ClusterIP: "10.96.0.20",
			Ports:     []corev1.ServicePort{{Port: 80, TargetPort: intstr.FromInt(8080)}},
		},
	}
	return []runtime.Object{
		pod, slice, svc,
		createAmbientNodeProxyPod(nodeName, nodeProxyReady),
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}},
	}
}

func TestReconcile_AmbientPod(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		podStatus        string
		nodeProxyReady   bool
		expectRegistered bool
		expectProxyCheck string
	}{
		"registers once CNI is complete": {
			podStatus:        constants.AmbientMeshStatusComplete,
			nodeProxyReady:   true,
			expectRegistered: true,
			expectProxyCheck: api.HealthPassing,
		},
		"node proxy not ready makes the proxy critical": {
			podStatus:        constants.AmbientMeshStatusComplete,
			nodeProxyReady:   false,
			expectRegistered: true,
			expectProxyCheck: api.HealthCritical,
		},
		"waits for CNI": {
			podStatus:      constants.AmbientMeshStatusWaiting,
			nodeProxyReady: true,
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().
				WithScheme(endpointSliceTestScheme(t)).
				WithRuntimeObjects(ambientTestObjects(c.podStatus, c.nodeProxyReady)...).
				Build()
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
				ReleaseNamespace:      "consul",
				// Sidecar tproxy settings must not leak into ambient registrations.
				EnableTransparentProxy: true,
				TProxyOverwriteProbes:  true,
				Ambient: AmbientConfig{
					Enabled:            true,
					InboundPort:        15006,
					OutboundPort:       15001,
					NodeProxySelector:  labels.SelectorFromSet(labels.Set{"app": "consul", "component": "ambient-mesh"}),
					NodeProxyNamespace: "consul",
				},
			}

			resp, err := ep.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: types.NamespacedName{Namespace: "default", Name: "web"},
			})
			require.NoError(t, err)

			proxies, _, err := consulClient.Catalog().Service("web-sidecar-proxy", "", nil)
			require.NoError(t, err)
			if !c.expectRegistered {
				require.Empty(t, proxies)
				require.NotZero(t, resp.RequeueAfter, "pods waiting for CNI must be retried")
				return
			}

			services, _, err := consulClient.Catalog().Service("web", "", nil)
			require.NoError(t, err)
			require.Len(t, services, 1)
			require.Equal(t, consulNodeName, services[0].Node)
			require.Equal(t, 8080, services[0].ServicePort)
			require.NotContains(t, services[0].ServiceMeta, constants.MetaKeyAmbientMesh)
			require.Equal(t, api.ServiceAddress{Address: "10.96.0.20", Port: 80}, services[0].ServiceTaggedAddresses["virtual"])

			require.Len(t, proxies, 1)
			proxy := proxies[0]
			require.Equal(t, consulNodeName, proxy.Node)
			require.Equal(t, "pod1-web-sidecar-proxy", proxy.ServiceID)
			require.Equal(t, "1.2.3.4", proxy.ServiceAddress)
			require.Equal(t, 15006, proxy.ServicePort)
			require.Equal(t, "true", proxy.ServiceMeta[constants.MetaKeyAmbientMesh])
			require.Equal(t, nodeName, proxy.ServiceMeta[constants.MetaKeyAmbientNode])
			require.Equal(t, ambientNetNS, proxy.ServiceMeta[constants.MetaKeyAmbientNetNS])
			require.Equal(t, api.ServiceAddress{Address: "10.96.0.20", Port: 80}, proxy.ServiceTaggedAddresses["virtual"])
			require.Equal(t, api.ProxyModeTransparent, proxy.ServiceProxy.Mode)
			require.Equal(t, 15001, proxy.ServiceProxy.TransparentProxy.OutboundListenerPort)
			require.Equal(t, "web", proxy.ServiceProxy.DestinationServiceName)
			require.Equal(t, 8080, proxy.ServiceProxy.LocalServicePort)
			require.Empty(t, proxy.ServiceProxy.Expose.Paths)
			require.Len(t, proxy.ServiceProxy.Upstreams, 1)
			require.Equal(t, "backend", proxy.ServiceProxy.Upstreams[0].DestinationName)

			checks, _, err := consulClient.Health().Checks("web-sidecar-proxy", nil)
			require.NoError(t, err)
			var nodeProxyCheck *api.HealthCheck
			for _, chk := range checks {
				if chk.Name == constants.AmbientNodeProxyCheckName {
					nodeProxyCheck = chk
				}
			}
			require.NotNil(t, nodeProxyCheck)
			require.Equal(t, "pod1-web-sidecar-proxy/ambient-node-proxy", nodeProxyCheck.CheckID)
			require.Equal(t, c.expectProxyCheck, nodeProxyCheck.Status)
		})
	}
}

func TestRequestsForAmbientNodeProxyPod(t *testing.T) {
	t.Parallel()

	objs := ambientTestObjects(constants.AmbientMeshStatusComplete, true)
	otherNode := createAmbientPod("pod2", "1.2.3.5", constants.AmbientMeshStatusComplete)
	otherNode.Spec.NodeName = "other-node"
	objs = append(objs, otherNode)
	fakeClient := fake.NewClientBuilder().WithScheme(endpointSliceTestScheme(t)).WithRuntimeObjects(objs...).Build()

	ep := &Controller{
		Client: fakeClient,
		Log:    logrtest.New(t),
		Ambient: AmbientConfig{
			Enabled:            true,
			NodeProxySelector:  labels.SelectorFromSet(labels.Set{"component": "ambient-mesh"}),
			NodeProxyNamespace: "consul",
		},
	}

	reqs := ep.requestsForAmbientNodeProxyPod(context.Background(), createAmbientNodeProxyPod(nodeName, false))
	require.Len(t, reqs, 1)
	require.Equal(t, types.NamespacedName{Namespace: "default", Name: "web"}, reqs[0].NamespacedName)

	// Pods that are not node proxies are ignored.
	require.Empty(t, ep.requestsForAmbientNodeProxyPod(context.Background(), createServicePod("x", "1.1.1.1", false, false)))
}
