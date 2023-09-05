// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package pod

import (
	"context"
	"testing"

	mapset "github.com/deckarep/golang-set"
	logrtest "github.com/go-logr/logr/testr"
	pbcatalog "github.com/hashicorp/consul/proto-public/pbcatalog/v1alpha1"
	"github.com/hashicorp/consul/proto-public/pbresource"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/metrics"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
)

const (
	nodeName               = "test-node"
	localityNodeName       = "test-node-w-locality"
	consulNodeName         = "test-node-virtual"
	consulLocalityNodeName = "test-node-w-locality-virtual"
	consulNodeAddress      = "127.0.0.1"
)

func TestHasBeenInjected(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		pod      func() corev1.Pod
		expected bool
	}{
		{
			name: "Pod with injected annotation",
			pod: func() corev1.Pod {
				pod1 := createPod("pod1", "1.2.3.4", "foo", true, true)
				return *pod1
			},
			expected: true,
		},
		{
			name: "Pod without injected annotation",
			pod: func() corev1.Pod {
				pod1 := createPod("pod1", "1.2.3.4", "foo", false, true)
				return *pod1
			},
			expected: false,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {

			actual := hasBeenInjected(tt.pod())
			require.Equal(t, tt.expected, actual)
		})
	}
}

func TestParseLocality(t *testing.T) {
	t.Run("no labels", func(t *testing.T) {
		n := corev1.Node{}
		require.Nil(t, parseLocality(n))
	})

	t.Run("zone only", func(t *testing.T) {
		n := corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					corev1.LabelTopologyZone: "us-west-1a",
				},
			},
		}
		require.Nil(t, parseLocality(n))
	})

	t.Run("everything", func(t *testing.T) {
		n := corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					corev1.LabelTopologyRegion: "us-west-1",
					corev1.LabelTopologyZone:   "us-west-1a",
				},
			},
		}
		require.True(t, proto.Equal(&pbcatalog.Locality{Region: "us-west-1", Zone: "us-west-1a"}, parseLocality(n)))
	})
}

func TestWorkloadWrite(t *testing.T) {
	t.Parallel()

	ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name:      metav1.NamespaceDefault,
		Namespace: metav1.NamespaceDefault,
	}}
	node := corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}}
	localityNode := corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name:      localityNodeName,
		Namespace: metav1.NamespaceDefault,
		Labels: map[string]string{
			corev1.LabelTopologyRegion: "us-east1",
			corev1.LabelTopologyZone:   "us-east1-b",
		},
	}}

	type testCase struct {
		name             string
		pod              *corev1.Pod
		podModifier      func(pod *corev1.Pod)
		expectedWorkload *pbcatalog.Workload
	}

	run := func(t *testing.T, tc testCase) {
		if tc.podModifier != nil {
			tc.podModifier(tc.pod)
		}

		k8sObjects := []runtime.Object{
			&ns,
			&node,
			&localityNode,
		}

		fakeClient := fake.NewClientBuilder().WithRuntimeObjects(k8sObjects...).Build()

		// Create test consulServer server.
		testClient := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
			c.Experiments = []string{"resource-apis"}
		})
		resourceClient, err := consul.NewResourceServiceClient(testClient.Watcher)
		require.NoError(t, err)

		// Create the pod controller.
		pc := &Controller{
			Client:                fakeClient,
			Log:                   logrtest.New(t),
			ConsulClientConfig:    testClient.Cfg,
			ConsulServerConnMgr:   testClient.Watcher,
			AllowK8sNamespacesSet: mapset.NewSetWith("*"),
			DenyK8sNamespacesSet:  mapset.NewSetWith(),
			ResourceClient:        resourceClient,
		}

		err = pc.writeWorkload(context.Background(), *tc.pod)
		require.NoError(t, err)

		req := &pbresource.ReadRequest{Id: &pbresource.ID{
			Name: tc.pod.GetName(),
			Type: &pbresource.Type{
				Group:        "catalog",
				GroupVersion: "v1alpha1",
				Kind:         "Workload",
			},
			Tenancy: &pbresource.Tenancy{
				Partition: constants.DefaultConsulPartition,
				Namespace: metav1.NamespaceDefault,
			},
		}}
		actualRes, err := resourceClient.Read(context.Background(), req)
		require.NoError(t, err)
		require.NotNil(t, actualRes)

		require.Equal(t, tc.pod.GetName(), actualRes.GetResource().GetId().GetName())
		require.Equal(t, constants.DefaultConsulNS, actualRes.GetResource().GetId().GetTenancy().GetNamespace())
		require.Equal(t, constants.DefaultConsulPartition, actualRes.GetResource().GetId().GetTenancy().GetPartition())

		require.NotNil(t, actualRes.GetResource().GetData())

		actualWorkload := &pbcatalog.Workload{}
		err = actualRes.GetResource().GetData().UnmarshalTo(actualWorkload)
		require.NoError(t, err)

		require.True(t, proto.Equal(actualWorkload, tc.expectedWorkload))
	}

	testCases := []testCase{
		{
			name: "multi-port single-container",
			pod:  createPod("foo", "10.0.0.1", "foo", true, true),
			expectedWorkload: &pbcatalog.Workload{
				Addresses: []*pbcatalog.WorkloadAddress{
					{Host: "10.0.0.1", Ports: []string{"public", "admin", "mesh"}},
				},
				Ports: map[string]*pbcatalog.WorkloadPort{
					"public": {
						Port:     80,
						Protocol: pbcatalog.Protocol_PROTOCOL_UNSPECIFIED,
					},
					"admin": {
						Port:     8080,
						Protocol: pbcatalog.Protocol_PROTOCOL_UNSPECIFIED,
					},
					"mesh": {
						Port:     constants.ProxyDefaultInboundPort,
						Protocol: pbcatalog.Protocol_PROTOCOL_MESH,
					},
				},
				NodeName: consulNodeName,
				Identity: "foo",
			},
		},
		{
			name: "multi-port multi-container",
			pod:  createPod("foo", "10.0.0.1", "foo", true, true),
			podModifier: func(pod *corev1.Pod) {
				container := corev1.Container{
					Name: "logger",
					Ports: []corev1.ContainerPort{
						{
							Name:          "agent",
							Protocol:      corev1.ProtocolTCP,
							ContainerPort: 6666,
						},
					},
				}
				pod.Spec.Containers = append(pod.Spec.Containers, container)
			},
			expectedWorkload: &pbcatalog.Workload{
				Addresses: []*pbcatalog.WorkloadAddress{
					{Host: "10.0.0.1", Ports: []string{"public", "admin", "agent", "mesh"}},
				},
				Ports: map[string]*pbcatalog.WorkloadPort{
					"public": {
						Port:     80,
						Protocol: pbcatalog.Protocol_PROTOCOL_UNSPECIFIED,
					},
					"admin": {
						Port:     8080,
						Protocol: pbcatalog.Protocol_PROTOCOL_UNSPECIFIED,
					},
					"agent": {
						Port:     6666,
						Protocol: pbcatalog.Protocol_PROTOCOL_UNSPECIFIED,
					},
					"mesh": {
						Port:     constants.ProxyDefaultInboundPort,
						Protocol: pbcatalog.Protocol_PROTOCOL_MESH,
					},
				},
				NodeName: consulNodeName,
				Identity: "foo",
			},
		},
		{
			name: "pod with locality",
			pod:  createPod("foo", "10.0.0.1", "foo", true, true),
			podModifier: func(pod *corev1.Pod) {
				pod.Spec.NodeName = localityNodeName
			},
			expectedWorkload: &pbcatalog.Workload{
				Addresses: []*pbcatalog.WorkloadAddress{
					{Host: "10.0.0.1", Ports: []string{"public", "admin", "mesh"}},
				},
				Ports: map[string]*pbcatalog.WorkloadPort{
					"public": {
						Port:     80,
						Protocol: pbcatalog.Protocol_PROTOCOL_UNSPECIFIED,
					},
					"admin": {
						Port:     8080,
						Protocol: pbcatalog.Protocol_PROTOCOL_UNSPECIFIED,
					},
					"mesh": {
						Port:     constants.ProxyDefaultInboundPort,
						Protocol: pbcatalog.Protocol_PROTOCOL_MESH,
					},
				},
				Locality: &pbcatalog.Locality{
					Region: "us-east1",
					Zone:   "us-east1-b",
				},
				NodeName: consulLocalityNodeName,
				Identity: "foo",
			},
		},
		{
			name: "pod with unnamed ports",
			pod:  createPod("foo", "10.0.0.1", "foo", true, true),
			podModifier: func(pod *corev1.Pod) {
				pod.Spec.Containers[0].Ports[0].Name = ""
				pod.Spec.Containers[0].Ports[1].Name = ""
			},
			expectedWorkload: &pbcatalog.Workload{
				Addresses: []*pbcatalog.WorkloadAddress{
					{Host: "10.0.0.1", Ports: []string{"80", "8080", "mesh"}},
				},
				Ports: map[string]*pbcatalog.WorkloadPort{
					"80": {
						Port:     80,
						Protocol: pbcatalog.Protocol_PROTOCOL_UNSPECIFIED,
					},
					"8080": {
						Port:     8080,
						Protocol: pbcatalog.Protocol_PROTOCOL_UNSPECIFIED,
					},
					"mesh": {
						Port:     constants.ProxyDefaultInboundPort,
						Protocol: pbcatalog.Protocol_PROTOCOL_MESH,
					},
				},
				NodeName: consulNodeName,
				Identity: "foo",
			},
		},
		{
			name: "pod with no ports",
			pod:  createPod("foo", "10.0.0.1", "foo", true, true),
			podModifier: func(pod *corev1.Pod) {
				pod.Spec.Containers[0].Ports = nil
			},
			expectedWorkload: &pbcatalog.Workload{
				Addresses: []*pbcatalog.WorkloadAddress{
					{Host: "10.0.0.1", Ports: []string{"mesh"}},
				},
				Ports: map[string]*pbcatalog.WorkloadPort{
					"mesh": {
						Port:     constants.ProxyDefaultInboundPort,
						Protocol: pbcatalog.Protocol_PROTOCOL_MESH,
					},
				},
				NodeName: consulNodeName,
				Identity: "foo",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			run(t, tc)
		})
	}
}

func TestWorkloadDelete(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name             string
		pod              *corev1.Pod
		existingWorkload *pbcatalog.Workload
	}

	run := func(t *testing.T, tc testCase) {
		fakeClient := fake.NewClientBuilder().WithRuntimeObjects().Build()

		// Create test consulServer server.
		testClient := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
			c.Experiments = []string{"resource-apis"}
		})
		resourceClient, err := consul.NewResourceServiceClient(testClient.Watcher)
		require.NoError(t, err)

		// Create the pod controller.
		pc := &Controller{
			Client:                fakeClient,
			Log:                   logrtest.New(t),
			ConsulClientConfig:    testClient.Cfg,
			ConsulServerConnMgr:   testClient.Watcher,
			AllowK8sNamespacesSet: mapset.NewSetWith("*"),
			DenyK8sNamespacesSet:  mapset.NewSetWith(),
			ResourceClient:        resourceClient,
		}

		workload, err := anypb.New(tc.existingWorkload)
		require.NoError(t, err)

		workloadID := getWorkloadID(tc.pod.GetName(), metav1.NamespaceDefault, constants.DefaultConsulPartition)
		writeReq := &pbresource.WriteRequest{
			Resource: &pbresource.Resource{
				Id:   workloadID,
				Data: workload,
			},
		}

		_, err = resourceClient.Write(context.Background(), writeReq)
		require.NoError(t, err)
		test.ResourceHasPersisted(t, resourceClient, workloadID)

		reconcileReq := types.NamespacedName{
			Namespace: metav1.NamespaceDefault,
			Name:      tc.pod.GetName(),
		}
		err = pc.deleteWorkload(context.Background(), reconcileReq)
		require.NoError(t, err)

		readReq := &pbresource.ReadRequest{Id: &pbresource.ID{
			Name: tc.pod.GetName(),
			Type: &pbresource.Type{
				Group:        "catalog",
				GroupVersion: "v1alpha1",
				Kind:         "Workload",
			},
			Tenancy: &pbresource.Tenancy{
				Partition: constants.DefaultConsulPartition,
				Namespace: metav1.NamespaceDefault,
			},
		}}
		_, err = resourceClient.Read(context.Background(), readReq)
		require.Error(t, err)
		s, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.NotFound, s.Code())
	}

	testCases := []testCase{
		{
			name: "basic pod delete",
			pod:  createPod("foo", "10.0.0.1", "foo", true, true),
			existingWorkload: &pbcatalog.Workload{
				Addresses: []*pbcatalog.WorkloadAddress{
					{Host: "10.0.0.1", Ports: []string{"public", "admin", "mesh"}},
				},
				Ports: map[string]*pbcatalog.WorkloadPort{
					"public": {
						Port:     80,
						Protocol: pbcatalog.Protocol_PROTOCOL_UNSPECIFIED,
					},
					"admin": {
						Port:     8080,
						Protocol: pbcatalog.Protocol_PROTOCOL_UNSPECIFIED,
					},
					"mesh": {
						Port:     constants.ProxyDefaultInboundPort,
						Protocol: pbcatalog.Protocol_PROTOCOL_MESH,
					},
				},
				NodeName: consulNodeName,
				Identity: "foo",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			run(t, tc)
		})
	}
}

// TODO
// func TestHealthStatusWrite(t *testing.T)

// TODO
// func TestHealthStatusDelete(t *testing.T)

// TODO
// func TestUpstreamsWrite(t *testing.T)

// TODO
// func TestUpstreamsDelete(t *testing.T)

// TODO
// func TestDeleteACLTokens(t *testing.T)

// TestReconcileCreatePod ensures that a new pod reconciliation fans out to create
// the appropriate Consul resources. Translation details from pod to Consul workload are
// tested at the relevant private functions. Any error states that are also tested here.
func TestReconcileCreatePod(t *testing.T) {
	t.Parallel()

	ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name:      metav1.NamespaceDefault,
		Namespace: metav1.NamespaceDefault,
	}}
	node := corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}}

	type testCase struct {
		name      string
		podName   string // This needs to be aligned with the pod created in `k8sObjects`
		namespace string // Defaults to metav1.NamespaceDefault if empty. Should be aligned with the ns in the pod

		k8sObjects       func() []runtime.Object // testing node is injected separately
		expectedWorkload *pbcatalog.Workload
		//expectedHealthStatus       *pbcatalog.HealthStatus
		//expectedProxyConfiguration *pbmesh.ProxyConfiguration
		//expectedUpstreams          *pbmesh.Upstreams

		metricsEnabled   bool
		telemetryEnabled bool

		expErr string
	}

	run := func(t *testing.T, tc testCase) {
		k8sObjects := []runtime.Object{
			&ns,
			&node,
		}
		if tc.k8sObjects != nil {
			k8sObjects = append(k8sObjects, tc.k8sObjects()...)
		}

		fakeClient := fake.NewClientBuilder().WithRuntimeObjects(k8sObjects...).Build()

		// Create test consulServer server.
		testClient := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
			c.Experiments = []string{"resource-apis"}
		})
		resourceClient, err := consul.NewResourceServiceClient(testClient.Watcher)
		require.NoError(t, err)

		// Create the pod controller.
		pc := &Controller{
			Client:                fakeClient,
			Log:                   logrtest.New(t),
			ConsulClientConfig:    testClient.Cfg,
			ConsulServerConnMgr:   testClient.Watcher,
			AllowK8sNamespacesSet: mapset.NewSetWith("*"),
			DenyK8sNamespacesSet:  mapset.NewSetWith(),
		}
		if tc.metricsEnabled {
			pc.MetricsConfig = metrics.Config{
				DefaultEnableMetrics: true,
				EnableGatewayMetrics: true,
			}
		}
		pc.EnableTelemetryCollector = tc.telemetryEnabled

		namespace := tc.namespace
		if namespace == "" {
			namespace = metav1.NamespaceDefault
		}

		namespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      tc.podName,
		}

		resp, err := pc.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: namespacedName,
		})
		if tc.expErr != "" {
			require.EqualError(t, err, tc.expErr)
		} else {
			require.NoError(t, err)
		}
		require.False(t, resp.Requeue)

		expectedWorkloadMatches(t, resourceClient, tc.podName, tc.expectedWorkload)
		// TODO(dans): compare the following to expected values
		// expectedHealthStatus
		// expectedProxyConfiguration
		// expectedUpstreams
	}

	testCases := []testCase{
		{
			name:    "vanilla new pod",
			podName: "foo",
			k8sObjects: func() []runtime.Object {
				pod := createPod("foo", "10.0.0.1", "foo", true, true)
				return []runtime.Object{pod}
			},
			expectedWorkload: &pbcatalog.Workload{
				Addresses: []*pbcatalog.WorkloadAddress{
					{Host: "10.0.0.1", Ports: []string{"public", "admin", "mesh"}},
				},
				Ports: map[string]*pbcatalog.WorkloadPort{
					"public": {
						Port:     80,
						Protocol: pbcatalog.Protocol_PROTOCOL_UNSPECIFIED,
					},
					"admin": {
						Port:     8080,
						Protocol: pbcatalog.Protocol_PROTOCOL_UNSPECIFIED,
					},
					"mesh": {
						Port:     constants.ProxyDefaultInboundPort,
						Protocol: pbcatalog.Protocol_PROTOCOL_MESH,
					},
				},
				NodeName: consulNodeName,
				Identity: "foo",
			},
		},
		{
			name:      "pod in ignored namespace",
			podName:   "foo",
			namespace: metav1.NamespaceSystem,
			k8sObjects: func() []runtime.Object {
				pod := createPod("foo", "10.0.0.1", "foo", true, true)
				pod.ObjectMeta.Namespace = metav1.NamespaceSystem
				return []runtime.Object{pod}
			},
		},
		// TODO(dans): NotHealthyPod
		// TODO(dans): tproxy + Metrics + Telemetry
		// TODO: explicit upstreams
		// TODO: at least one error cases
		// TODO: make sure multi-error accumulates errors
		// TODO: injection annotation added
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			run(t, tc)
		})
	}
}

// TestReconcileUpdatePod test updating a Pod object when there is already matching resources in Consul.
func TestReconcileUpdatePod(t *testing.T) {
	t.Parallel()

	ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name:      metav1.NamespaceDefault,
		Namespace: metav1.NamespaceDefault,
	}}
	node := corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}}

	type testCase struct {
		name      string
		podName   string // This needs to be aligned with the pod created in `k8sObjects`
		namespace string // Defaults to metav1.NamespaceDefault if empty. Should be aligned with the ns in the pod

		k8sObjects func() []runtime.Object // testing node is injected separately

		existingWorkload *pbcatalog.Workload
		//existingHealthStatus       *pbcatalog.HealthStatus
		//existingProxyConfiguration *pbmesh.ProxyConfiguration
		//existingUpstreams          *pbmesh.Upstreams

		expectedWorkload *pbcatalog.Workload
		//expectedHealthStatus       *pbcatalog.HealthStatus
		//expectedProxyConfiguration *pbmesh.ProxyConfiguration
		//expectedUpstreams          *pbmesh.Upstreams

		metricsEnabled   bool
		telemetryEnabled bool

		expErr string
	}

	run := func(t *testing.T, tc testCase) {
		k8sObjects := []runtime.Object{
			&ns,
			&node,
		}
		if tc.k8sObjects != nil {
			k8sObjects = append(k8sObjects, tc.k8sObjects()...)
		}

		fakeClient := fake.NewClientBuilder().WithRuntimeObjects(k8sObjects...).Build()

		// Create test consulServer server.
		testClient := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
			c.Experiments = []string{"resource-apis"}
		})
		resourceClient, err := consul.NewResourceServiceClient(testClient.Watcher)
		require.NoError(t, err)

		// Create the pod controller.
		pc := &Controller{
			Client:                fakeClient,
			Log:                   logrtest.New(t),
			ConsulClientConfig:    testClient.Cfg,
			ConsulServerConnMgr:   testClient.Watcher,
			AllowK8sNamespacesSet: mapset.NewSetWith("*"),
			DenyK8sNamespacesSet:  mapset.NewSetWith(),
		}
		if tc.metricsEnabled {
			pc.MetricsConfig = metrics.Config{
				DefaultEnableMetrics: true,
				EnableGatewayMetrics: true,
			}
		}
		pc.EnableTelemetryCollector = tc.telemetryEnabled

		namespace := tc.namespace
		if namespace == "" {
			namespace = metav1.NamespaceDefault
		}

		loadResource(
			t,
			resourceClient,
			getWorkloadID(tc.podName, constants.DefaultConsulNS, constants.DefaultConsulPartition),
			tc.existingWorkload,
		)

		// TODO(dans): load the existing resources
		// loadHealthStatus
		// loadProxyConfiguration
		// loadUpstreams

		namespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      tc.podName,
		}

		resp, err := pc.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: namespacedName,
		})
		if tc.expErr != "" {
			require.EqualError(t, err, tc.expErr)
		} else {
			require.NoError(t, err)
		}
		require.False(t, resp.Requeue)

		expectedWorkloadMatches(t, resourceClient, tc.podName, tc.expectedWorkload)
		// TODO(dans): compare the following to expected values
		// expectedHealthStatus
		// expectedProxyConfiguration
		// expectedUpstreams
	}

	testCases := []testCase{
		{
			name:    "pod update ports",
			podName: "foo",
			k8sObjects: func() []runtime.Object {
				pod := createPod("foo", "10.0.0.1", "foo", true, true)
				return []runtime.Object{pod}
			},
			existingWorkload: &pbcatalog.Workload{
				Addresses: []*pbcatalog.WorkloadAddress{
					{Host: "10.0.0.1", Ports: []string{"public", "mesh"}},
				},
				Ports: map[string]*pbcatalog.WorkloadPort{
					"public": {
						Port:     80,
						Protocol: pbcatalog.Protocol_PROTOCOL_UNSPECIFIED,
					},
					"mesh": {
						Port:     constants.ProxyDefaultInboundPort,
						Protocol: pbcatalog.Protocol_PROTOCOL_MESH,
					},
				},
				NodeName: consulNodeName,
				Identity: "foo",
			},
			expectedWorkload: &pbcatalog.Workload{
				Addresses: []*pbcatalog.WorkloadAddress{
					{Host: "10.0.0.1", Ports: []string{"public", "admin", "mesh"}},
				},
				Ports: map[string]*pbcatalog.WorkloadPort{
					"public": {
						Port:     80,
						Protocol: pbcatalog.Protocol_PROTOCOL_UNSPECIFIED,
					},
					"admin": {
						Port:     8080,
						Protocol: pbcatalog.Protocol_PROTOCOL_UNSPECIFIED,
					},
					"mesh": {
						Port:     constants.ProxyDefaultInboundPort,
						Protocol: pbcatalog.Protocol_PROTOCOL_MESH,
					},
				},
				NodeName: consulNodeName,
				Identity: "foo",
			},
		},
		// TODO(dans): Pod Health to Unhealthy
		// TODO(dans): update tproxy + Metrics + Telemetry
		// TODO: update explicit upstreams
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			run(t, tc)
		})
	}
}

// Tests deleting a Pod object, with and without matching Consul resources.
func TestReconcileDeletePod(t *testing.T) {
	t.Parallel()

	ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name:      metav1.NamespaceDefault,
		Namespace: metav1.NamespaceDefault,
	}}
	node := corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}}

	type testCase struct {
		name      string
		podName   string // This needs to be aligned with the pod created in `k8sObjects`
		namespace string // Defaults to metav1.NamespaceDefault if empty. Should be aligned with the ns in the pod

		k8sObjects func() []runtime.Object // testing node is injected separately

		existingWorkload *pbcatalog.Workload
		//existingHealthStatus       *pbcatalog.HealthStatus
		//existingProxyConfiguration *pbmesh.ProxyConfiguration
		//existingUpstreams          *pbmesh.Upstreams

		expectedWorkload *pbcatalog.Workload
		//expectedHealthStatus       *pbcatalog.HealthStatus
		//expectedProxyConfiguration *pbmesh.ProxyConfiguration
		//expectedUpstreams          *pbmesh.Upstreams

		aclsEnabled      bool
		metricsEnabled   bool
		telemetryEnabled bool

		expErr string
	}

	run := func(t *testing.T, tc testCase) {
		k8sObjects := []runtime.Object{
			&ns,
			&node,
		}
		if tc.k8sObjects != nil {
			k8sObjects = append(k8sObjects, tc.k8sObjects()...)
		}

		fakeClient := fake.NewClientBuilder().WithRuntimeObjects(k8sObjects...).Build()

		// Create test consulServer server.
		testClient := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
			c.Experiments = []string{"resource-apis"}
		})
		resourceClient, err := consul.NewResourceServiceClient(testClient.Watcher)
		require.NoError(t, err)

		// Create the pod controller.
		pc := &Controller{
			Client:                fakeClient,
			Log:                   logrtest.New(t),
			ConsulClientConfig:    testClient.Cfg,
			ConsulServerConnMgr:   testClient.Watcher,
			AllowK8sNamespacesSet: mapset.NewSetWith("*"),
			DenyK8sNamespacesSet:  mapset.NewSetWith(),
		}
		if tc.metricsEnabled {
			pc.MetricsConfig = metrics.Config{
				DefaultEnableMetrics: true,
				EnableGatewayMetrics: true,
			}
		}
		if tc.aclsEnabled {
			pc.AuthMethod = test.AuthMethod
		}
		pc.EnableTelemetryCollector = tc.telemetryEnabled

		namespace := tc.namespace
		if namespace == "" {
			namespace = metav1.NamespaceDefault
		}

		loadResource(
			t,
			resourceClient,
			getWorkloadID(tc.podName, constants.DefaultConsulNS, constants.DefaultConsulPartition),
			tc.existingWorkload,
		)

		// TODO(dans): load the existing resources
		// loadHealthStatus
		// loadProxyConfiguration
		// loadUpstreams

		namespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      tc.podName,
		}

		resp, err := pc.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: namespacedName,
		})
		if tc.expErr != "" {
			require.EqualError(t, err, tc.expErr)
		} else {
			require.NoError(t, err)
		}
		require.False(t, resp.Requeue)

		expectedWorkloadMatches(t, resourceClient, tc.podName, tc.expectedWorkload)
		// TODO(dans): compare the following to expected values
		// expectedHealthStatus
		// expectedProxyConfiguration
		// expectedUpstreams
	}

	testCases := []testCase{
		{
			name:    "vanilla delete pod",
			podName: "foo",
			existingWorkload: &pbcatalog.Workload{
				Addresses: []*pbcatalog.WorkloadAddress{
					{Host: "10.0.0.1", Ports: []string{"public", "admin", "mesh"}},
				},
				Ports: map[string]*pbcatalog.WorkloadPort{
					"public": {
						Port:     80,
						Protocol: pbcatalog.Protocol_PROTOCOL_UNSPECIFIED,
					},
					"admin": {
						Port:     8080,
						Protocol: pbcatalog.Protocol_PROTOCOL_UNSPECIFIED,
					},
					"mesh": {
						Port:     constants.ProxyDefaultInboundPort,
						Protocol: pbcatalog.Protocol_PROTOCOL_MESH,
					},
				},
				NodeName: consulNodeName,
				Identity: "foo",
			},
		},
		// TODO: enable ACLs and make sure they are deleted
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			run(t, tc)
		})
	}
}

func createPod(name, ip string, identity string, inject bool, ready bool) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: metav1.NamespaceDefault,
			Labels:    map[string]string{},
			Annotations: map[string]string{
				constants.AnnotationConsulK8sVersion: "1.3.0",
			},
		},
		Status: corev1.PodStatus{
			PodIP:  ip,
			HostIP: consulNodeAddress,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "web",
					Ports: []corev1.ContainerPort{
						{
							Name:          "public",
							Protocol:      corev1.ProtocolTCP,
							ContainerPort: 80,
						},
						{
							Name:          "admin",
							Protocol:      corev1.ProtocolTCP,
							ContainerPort: 8080,
						},
					},
				},
			},
			NodeName:           nodeName,
			ServiceAccountName: identity,
		},
	}
	if ready {
		pod.Status.Conditions = []corev1.PodCondition{
			{
				Type:   corev1.PodReady,
				Status: corev1.ConditionTrue,
			},
		}
	} else {
		pod.Status.Conditions = []corev1.PodCondition{
			{
				Type:   corev1.PodReady,
				Status: corev1.ConditionFalse,
			},
		}
	}

	if inject {
		pod.Labels[constants.KeyMeshInjectStatus] = constants.Injected
		pod.Annotations[constants.KeyMeshInjectStatus] = constants.Injected
	}
	return pod
}

func expectedWorkloadMatches(t *testing.T, client pbresource.ResourceServiceClient, name string, expectedWorkload *pbcatalog.Workload) {
	req := &pbresource.ReadRequest{Id: getWorkloadID(name, metav1.NamespaceDefault, constants.DefaultConsulPartition)}

	res, err := client.Read(context.Background(), req)

	if expectedWorkload == nil {
		require.Error(t, err)
		s, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.NotFound, s.Code())
		return
	}

	require.NoError(t, err)
	require.NotNil(t, res)

	require.Equal(t, name, res.GetResource().GetId().GetName())
	require.Equal(t, constants.DefaultConsulNS, res.GetResource().GetId().GetTenancy().GetNamespace())
	require.Equal(t, constants.DefaultConsulPartition, res.GetResource().GetId().GetTenancy().GetPartition())

	require.NotNil(t, res.GetResource().GetData())

	actualWorkload := &pbcatalog.Workload{}
	err = res.GetResource().GetData().UnmarshalTo(actualWorkload)
	require.NoError(t, err)

	require.True(t, proto.Equal(actualWorkload, expectedWorkload))
}

func loadResource(t *testing.T, client pbresource.ResourceServiceClient, id *pbresource.ID, proto proto.Message) {
	if id == nil || proto == nil {
		return
	}

	data, err := anypb.New(proto)
	require.NoError(t, err)

	resource := &pbresource.Resource{
		Id:   id,
		Data: data,
	}

	req := &pbresource.WriteRequest{Resource: resource}
	_, err = client.Write(context.Background(), req)
	require.NoError(t, err)
	test.ResourceHasPersisted(t, client, id)
}
