// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package pod

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	mapset "github.com/deckarep/golang-set"
	logrtest "github.com/go-logr/logr/testr"
	"github.com/google/go-cmp/cmp"
	"github.com/hashicorp/consul/api"
	pbcatalog "github.com/hashicorp/consul/proto-public/pbcatalog/v2beta1"
	pbmesh "github.com/hashicorp/consul/proto-public/pbmesh/v2beta1"
	"github.com/hashicorp/consul/proto-public/pbresource"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/anypb"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/metrics"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
)

const (
	// TODO: (v2/nitya) Bring back consulLocalityNodeName once node controller is implemented and assertions for
	// workloads need node names again.
	nodeName         = "test-node"
	localityNodeName = "test-node-w-locality"
	consulNodeName   = "test-node-virtual"
)

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

		// Create the pod controller.
		pc := &Controller{
			Client:              fakeClient,
			Log:                 logrtest.New(t),
			ConsulClientConfig:  testClient.Cfg,
			ConsulServerConnMgr: testClient.Watcher,
			K8sNamespaceConfig: common.K8sNamespaceConfig{
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSetWith(),
			},
			ResourceClient: testClient.ResourceClient,
		}

		err := pc.writeWorkload(context.Background(), *tc.pod)
		require.NoError(t, err)

		req := &pbresource.ReadRequest{
			Id: getWorkloadID(tc.pod.GetName(), metav1.NamespaceDefault, constants.DefaultConsulPartition),
		}
		actualRes, err := testClient.ResourceClient.Read(context.Background(), req)
		require.NoError(t, err)
		require.NotNil(t, actualRes)

		requireEqualID(t, actualRes, tc.pod.GetName(), constants.DefaultConsulNS, constants.DefaultConsulPartition)
		require.NotNil(t, actualRes.GetResource().GetData())

		actualWorkload := &pbcatalog.Workload{}
		err = actualRes.GetResource().GetData().UnmarshalTo(actualWorkload)
		require.NoError(t, err)

		require.True(t, proto.Equal(actualWorkload, tc.expectedWorkload))
	}

	testCases := []testCase{
		{
			name:             "multi-port single-container",
			pod:              createPod("foo", "", true, true),
			expectedWorkload: createWorkload(),
		},
		{
			name: "multi-port multi-container",
			pod:  createPod("foo", "", true, true),
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
				Identity: "foo",
			},
		},
		{
			name: "pod with locality",
			pod:  createPod("foo", "", true, true),
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
				Identity: "foo",
			},
		},
		{
			name: "pod with unnamed ports",
			pod:  createPod("foo", "", true, true),
			podModifier: func(pod *corev1.Pod) {
				pod.Spec.Containers[0].Ports[0].Name = ""
				pod.Spec.Containers[0].Ports[1].Name = ""
			},
			expectedWorkload: &pbcatalog.Workload{
				Addresses: []*pbcatalog.WorkloadAddress{
					{Host: "10.0.0.1", Ports: []string{"cslport-80", "cslport-8080", "mesh"}},
				},
				Ports: map[string]*pbcatalog.WorkloadPort{
					"cslport-80": {
						Port:     80,
						Protocol: pbcatalog.Protocol_PROTOCOL_UNSPECIFIED,
					},
					"cslport-8080": {
						Port:     8080,
						Protocol: pbcatalog.Protocol_PROTOCOL_UNSPECIFIED,
					},
					"mesh": {
						Port:     constants.ProxyDefaultInboundPort,
						Protocol: pbcatalog.Protocol_PROTOCOL_MESH,
					},
				},
				Identity: "foo",
			},
		},
		{
			name: "pod with no ports",
			pod:  createPod("foo", "", true, true),
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

		// Create the pod controller.
		pc := &Controller{
			Client:              fakeClient,
			Log:                 logrtest.New(t),
			ConsulClientConfig:  testClient.Cfg,
			ConsulServerConnMgr: testClient.Watcher,
			K8sNamespaceConfig: common.K8sNamespaceConfig{
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSetWith(),
			},
			ResourceClient: testClient.ResourceClient,
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

		_, err = testClient.ResourceClient.Write(context.Background(), writeReq)
		require.NoError(t, err)
		test.ResourceHasPersisted(t, context.Background(), testClient.ResourceClient, workloadID)

		reconcileReq := types.NamespacedName{
			Namespace: metav1.NamespaceDefault,
			Name:      tc.pod.GetName(),
		}
		err = pc.deleteWorkload(context.Background(), reconcileReq)
		require.NoError(t, err)

		readReq := &pbresource.ReadRequest{
			Id: getWorkloadID(tc.pod.GetName(), metav1.NamespaceDefault, constants.DefaultConsulPartition),
		}
		_, err = testClient.ResourceClient.Read(context.Background(), readReq)
		require.Error(t, err)
		s, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.NotFound, s.Code())
	}

	testCases := []testCase{
		{
			name:             "basic pod delete",
			pod:              createPod("foo", "", true, true),
			existingWorkload: createWorkload(),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			run(t, tc)
		})
	}
}

func TestHealthStatusWrite(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name                 string
		pod                  *corev1.Pod
		podModifier          func(pod *corev1.Pod)
		expectedHealthStatus *pbcatalog.HealthStatus
	}

	run := func(t *testing.T, tc testCase) {
		if tc.podModifier != nil {
			tc.podModifier(tc.pod)
		}

		fakeClient := fake.NewClientBuilder().WithRuntimeObjects().Build()

		// Create test consulServer server.
		testClient := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
			c.Experiments = []string{"resource-apis"}
		})

		// Create the pod controller.
		pc := &Controller{
			Client:              fakeClient,
			Log:                 logrtest.New(t),
			ConsulClientConfig:  testClient.Cfg,
			ConsulServerConnMgr: testClient.Watcher,
			K8sNamespaceConfig: common.K8sNamespaceConfig{
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSetWith(),
			},
			ResourceClient: testClient.ResourceClient,
		}

		// The owner of a resource is validated, so create a dummy workload for the HealthStatus
		workloadData, err := anypb.New(createWorkload())
		require.NoError(t, err)

		workloadID := getWorkloadID(tc.pod.GetName(), metav1.NamespaceDefault, constants.DefaultConsulPartition)
		writeReq := &pbresource.WriteRequest{
			Resource: &pbresource.Resource{
				Id:   workloadID,
				Data: workloadData,
			},
		}
		_, err = testClient.ResourceClient.Write(context.Background(), writeReq)
		require.NoError(t, err)

		// Test writing the pod to a HealthStatus
		err = pc.writeHealthStatus(context.Background(), *tc.pod)
		require.NoError(t, err)

		req := &pbresource.ReadRequest{
			Id: getHealthStatusID(tc.pod.GetName(), metav1.NamespaceDefault, constants.DefaultConsulPartition),
		}
		actualRes, err := testClient.ResourceClient.Read(context.Background(), req)
		require.NoError(t, err)
		require.NotNil(t, actualRes)

		requireEqualID(t, actualRes, tc.pod.GetName(), constants.DefaultConsulNS, constants.DefaultConsulPartition)
		require.NotNil(t, actualRes.GetResource().GetData())

		actualHealthStatus := &pbcatalog.HealthStatus{}
		err = actualRes.GetResource().GetData().UnmarshalTo(actualHealthStatus)
		require.NoError(t, err)

		require.True(t, proto.Equal(actualHealthStatus, tc.expectedHealthStatus))
	}

	testCases := []testCase{
		{
			name:                 "ready pod",
			pod:                  createPod("foo", "", true, true),
			expectedHealthStatus: createPassingHealthStatus(),
		},
		{
			name:                 "not ready pod",
			pod:                  createPod("foo", "", true, false),
			expectedHealthStatus: createCriticalHealthStatus("foo", "default"),
		},
		{
			name: "pod with no condition",
			pod:  createPod("foo", "", true, true),
			podModifier: func(pod *corev1.Pod) {
				pod.Status.Conditions = []corev1.PodCondition{}
			},
			expectedHealthStatus: createCriticalHealthStatus("foo", "default"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			run(t, tc)
		})
	}
}

func TestProxyConfigurationWrite(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name                       string
		pod                        *corev1.Pod
		podModifier                func(pod *corev1.Pod)
		expectedProxyConfiguration *pbmesh.ProxyConfiguration

		tproxy          bool
		overwriteProbes bool
		metrics         bool
		telemetry       bool
	}

	run := func(t *testing.T, tc testCase) {
		ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
			Name: metav1.NamespaceDefault,
		}}

		nsTproxy := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
			Name: "tproxy-party",
			Labels: map[string]string{
				constants.KeyTransparentProxy: "true",
			},
		}}

		if tc.podModifier != nil {
			tc.podModifier(tc.pod)
		}

		fakeClient := fake.NewClientBuilder().WithRuntimeObjects(&ns, &nsTproxy).Build()

		// Create test consulServer server.
		testClient := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
			c.Experiments = []string{"resource-apis"}
		})

		// Create the pod controller.
		pc := &Controller{
			Client:              fakeClient,
			Log:                 logrtest.New(t),
			ConsulClientConfig:  testClient.Cfg,
			ConsulServerConnMgr: testClient.Watcher,
			K8sNamespaceConfig: common.K8sNamespaceConfig{
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSetWith(),
			},
			EnableTransparentProxy:   tc.tproxy,
			TProxyOverwriteProbes:    tc.overwriteProbes,
			EnableTelemetryCollector: tc.telemetry,
			ResourceClient:           testClient.ResourceClient,
		}

		if tc.metrics {
			pc.MetricsConfig = metrics.Config{
				DefaultEnableMetrics:        true,
				DefaultPrometheusScrapePort: "5678",
			}
		}

		// Test writing the pod to a HealthStatus
		err := pc.writeProxyConfiguration(context.Background(), *tc.pod)
		require.NoError(t, err)

		req := &pbresource.ReadRequest{
			Id: getProxyConfigurationID(tc.pod.GetName(), metav1.NamespaceDefault, constants.DefaultConsulPartition),
		}
		actualRes, err := testClient.ResourceClient.Read(context.Background(), req)

		if tc.expectedProxyConfiguration == nil {
			require.Error(t, err)
			s, ok := status.FromError(err)
			require.True(t, ok)
			require.Equal(t, codes.NotFound, s.Code())
			return
		}

		require.NoError(t, err)
		require.NotNil(t, actualRes)

		requireEqualID(t, actualRes, tc.pod.GetName(), constants.DefaultConsulNS, constants.DefaultConsulPartition)
		require.NotNil(t, actualRes.GetResource().GetData())

		actualProxyConfiguration := &pbmesh.ProxyConfiguration{}
		err = actualRes.GetResource().GetData().UnmarshalTo(actualProxyConfiguration)
		require.NoError(t, err)

		diff := cmp.Diff(actualProxyConfiguration, tc.expectedProxyConfiguration, test.CmpProtoIgnoreOrder()...)
		require.Equal(t, "", diff)
	}

	testCases := []testCase{
		{
			name:                       "no tproxy, no telemetry, no metrics, no probe overwrite",
			pod:                        createPod("foo", "", true, true),
			expectedProxyConfiguration: nil,
		},
		{
			name: "kitchen sink - globally enabled",
			pod:  createPod("foo", "", true, true),
			podModifier: func(pod *corev1.Pod) {
				addProbesAndOriginalPodAnnotation(pod)
			},
			tproxy:          true,
			overwriteProbes: true,
			metrics:         true,
			telemetry:       true,
			expectedProxyConfiguration: &pbmesh.ProxyConfiguration{
				Workloads: &pbcatalog.WorkloadSelector{
					Names: []string{"foo"},
				},
				DynamicConfig: &pbmesh.DynamicConfig{
					Mode: pbmesh.ProxyMode_PROXY_MODE_TRANSPARENT,
					ExposeConfig: &pbmesh.ExposeConfig{
						ExposePaths: []*pbmesh.ExposePath{
							{
								ListenerPort:  20400,
								LocalPathPort: 2001,
								Path:          "/livez",
							},
							{
								ListenerPort:  20300,
								LocalPathPort: 2000,
								Path:          "/readyz",
							},
							{
								ListenerPort:  20500,
								LocalPathPort: 2002,
								Path:          "/startupz",
							},
						},
					},
					TransparentProxy: &pbmesh.TransparentProxy{
						OutboundListenerPort: 15001,
					},
				},
				BootstrapConfig: &pbmesh.BootstrapConfig{
					PrometheusBindAddr:              "0.0.0.0:5678",
					TelemetryCollectorBindSocketDir: DefaultTelemetryBindSocketDir,
				},
			},
		},
		{
			name: "tproxy, metrics, and probe overwrite enabled on pod",
			pod:  createPod("foo", "", true, true),
			podModifier: func(pod *corev1.Pod) {
				pod.Annotations[constants.KeyTransparentProxy] = "true"
				pod.Annotations[constants.AnnotationTransparentProxyOverwriteProbes] = "true"
				pod.Annotations[constants.AnnotationEnableMetrics] = "true"
				pod.Annotations[constants.AnnotationPrometheusScrapePort] = "21234"

				addProbesAndOriginalPodAnnotation(pod)
			},
			expectedProxyConfiguration: &pbmesh.ProxyConfiguration{
				Workloads: &pbcatalog.WorkloadSelector{
					Names: []string{"foo"},
				},
				DynamicConfig: &pbmesh.DynamicConfig{
					Mode: pbmesh.ProxyMode_PROXY_MODE_TRANSPARENT,
					ExposeConfig: &pbmesh.ExposeConfig{
						ExposePaths: []*pbmesh.ExposePath{
							{
								ListenerPort:  20400,
								LocalPathPort: 2001,
								Path:          "/livez",
							},
							{
								ListenerPort:  20300,
								LocalPathPort: 2000,
								Path:          "/readyz",
							},
							{
								ListenerPort:  20500,
								LocalPathPort: 2002,
								Path:          "/startupz",
							},
						},
					},
					TransparentProxy: &pbmesh.TransparentProxy{
						OutboundListenerPort: 15001,
					},
				},
				BootstrapConfig: &pbmesh.BootstrapConfig{
					PrometheusBindAddr: "0.0.0.0:21234",
				},
			},
		},
		{
			name: "tproxy enabled on namespace",
			pod:  createPod("foo", "", true, true),
			podModifier: func(pod *corev1.Pod) {
				pod.Namespace = "tproxy-party"
			},
			expectedProxyConfiguration: &pbmesh.ProxyConfiguration{
				Workloads: &pbcatalog.WorkloadSelector{
					Names: []string{"foo"},
				},
				DynamicConfig: &pbmesh.DynamicConfig{
					Mode: pbmesh.ProxyMode_PROXY_MODE_TRANSPARENT,
					TransparentProxy: &pbmesh.TransparentProxy{
						OutboundListenerPort: 15001,
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			run(t, tc)
		})
	}
}

func requireEqualID(t *testing.T, res *pbresource.ReadResponse, name string, ns string, partition string) {
	require.Equal(t, name, res.GetResource().GetId().GetName())
	require.Equal(t, ns, res.GetResource().GetId().GetTenancy().GetNamespace())
	require.Equal(t, partition, res.GetResource().GetId().GetTenancy().GetPartition())
}

func TestProxyConfigurationDelete(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name                       string
		pod                        *corev1.Pod
		existingProxyConfiguration *pbmesh.ProxyConfiguration
	}

	run := func(t *testing.T, tc testCase) {
		fakeClient := fake.NewClientBuilder().WithRuntimeObjects().Build()

		// Create test consulServer server.
		testClient := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
			c.Experiments = []string{"resource-apis"}
		})

		// Create the pod controller.
		pc := &Controller{
			Client:              fakeClient,
			Log:                 logrtest.New(t),
			ConsulClientConfig:  testClient.Cfg,
			ConsulServerConnMgr: testClient.Watcher,
			K8sNamespaceConfig: common.K8sNamespaceConfig{
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSetWith(),
			},
			ResourceClient: testClient.ResourceClient,
		}

		// Create the existing ProxyConfiguration
		pcData, err := anypb.New(tc.existingProxyConfiguration)
		require.NoError(t, err)

		pcID := getProxyConfigurationID(tc.pod.GetName(), metav1.NamespaceDefault, constants.DefaultConsulPartition)
		writeReq := &pbresource.WriteRequest{
			Resource: &pbresource.Resource{
				Id:   pcID,
				Data: pcData,
			},
		}

		_, err = testClient.ResourceClient.Write(context.Background(), writeReq)
		require.NoError(t, err)
		test.ResourceHasPersisted(t, context.Background(), testClient.ResourceClient, pcID)

		reconcileReq := types.NamespacedName{
			Namespace: metav1.NamespaceDefault,
			Name:      tc.pod.GetName(),
		}
		err = pc.deleteProxyConfiguration(context.Background(), reconcileReq)
		require.NoError(t, err)

		readReq := &pbresource.ReadRequest{
			Id: getProxyConfigurationID(tc.pod.GetName(), metav1.NamespaceDefault, constants.DefaultConsulPartition),
		}
		_, err = testClient.ResourceClient.Read(context.Background(), readReq)
		require.Error(t, err)
		s, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.NotFound, s.Code())
	}

	testCases := []testCase{
		{
			name:                       "proxy configuration delete",
			pod:                        createPod("foo", "", true, true),
			existingProxyConfiguration: createProxyConfiguration("foo", true, pbmesh.ProxyMode_PROXY_MODE_TRANSPARENT),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			run(t, tc)
		})
	}
}

// TestDestinationsWrite does a subsampling of tests covered in TestProcessUpstreams to make sure things are hooked up
// correctly. For the sake of test speed, more exhaustive testing is performed in TestProcessUpstreams.
func TestDestinationsWrite(t *testing.T) {
	t.Parallel()

	const podName = "pod1"

	cases := []struct {
		name                    string
		pod                     func() *corev1.Pod
		expected                *pbmesh.Destinations
		expErr                  string
		consulNamespacesEnabled bool
		consulPartitionsEnabled bool
	}{
		{
			name: "labeled annotated destination with svc only",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "", true, true)
				pod1.Annotations[constants.AnnotationMeshDestinations] = "destination.port.upstream1.svc:1234"
				return pod1
			},
			expected: &pbmesh.Destinations{
				Workloads: &pbcatalog.WorkloadSelector{
					Names: []string{podName},
				},
				Destinations: []*pbmesh.Destination{
					{
						DestinationRef: &pbresource.Reference{
							Type: pbcatalog.ServiceType,
							Tenancy: &pbresource.Tenancy{
								Partition: constants.GetNormalizedConsulPartition(""),
								Namespace: constants.GetNormalizedConsulNamespace(""),
							},
							Name: "upstream1",
						},
						DestinationPort: "destination",
						Datacenter:      "",
						ListenAddr: &pbmesh.Destination_IpPort{
							IpPort: &pbmesh.IPPortAddress{
								Port: uint32(1234),
								Ip:   consulNodeAddress,
							},
						},
					},
				},
			},
			consulNamespacesEnabled: false,
			consulPartitionsEnabled: false,
		},
		{
			name: "labeled annotated destination with svc, ns, and peer",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "", true, true)
				pod1.Annotations[constants.AnnotationMeshDestinations] = "destination.port.upstream1.svc.ns1.ns.peer1.peer:1234"
				return pod1
			},
			expErr: "error processing destination annotations: destination currently does not support peers: destination.port.upstream1.svc.ns1.ns.peer1.peer:1234",
			// TODO: uncomment this and remove expErr when peers is supported
			//expected: &pbmesh.Destinations{
			//	Workloads: &pbcatalog.WorkloadSelector{
			//		Names: []string{podName},
			//	},
			//	Destinations: []*pbmesh.Destination{
			//		{
			//			DestinationRef: &pbresource.Reference{
			//              Type: pbcatalog.ServiceType,
			//				Tenancy: &pbresource.Tenancy{
			//					Partition: constants.GetNormalizedConsulPartition(""),
			//					Namespace: "ns1",
			//					PeerName:  "peer1",
			//				},
			//				Name: "upstream1",
			//			},
			//			DestinationPort: "destination",
			//			Datacenter:      "",
			//			ListenAddr: &pbmesh.Destination_IpPort{
			//				IpPort: &pbmesh.IPPortAddress{
			//					Port: uint32(1234),
			//                  Ip:   consulNodeAddress,
			//				},
			//			},
			//		},
			//	},
			//},
			consulNamespacesEnabled: true,
			consulPartitionsEnabled: false,
		},
		{
			name: "labeled annotated destination with svc, ns, and partition",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "", true, true)
				pod1.Annotations[constants.AnnotationMeshDestinations] = "destination.port.upstream1.svc.ns1.ns.part1.ap:1234"
				return pod1
			},
			expected: &pbmesh.Destinations{
				Workloads: &pbcatalog.WorkloadSelector{
					Names: []string{podName},
				},
				Destinations: []*pbmesh.Destination{
					{
						DestinationRef: &pbresource.Reference{
							Type: pbcatalog.ServiceType,
							Tenancy: &pbresource.Tenancy{
								Partition: "part1",
								Namespace: "ns1",
							},
							Name: "upstream1",
						},
						DestinationPort: "destination",
						Datacenter:      "",
						ListenAddr: &pbmesh.Destination_IpPort{
							IpPort: &pbmesh.IPPortAddress{
								Port: uint32(1234),
								Ip:   consulNodeAddress,
							},
						},
					},
				},
			},
			consulNamespacesEnabled: true,
			consulPartitionsEnabled: true,
		},
		{
			name: "error labeled annotated destination error: invalid partition/dc/peer",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "", true, true)
				pod1.Annotations[constants.AnnotationMeshDestinations] = "destination.port.upstream1.svc.ns1.ns.part1.err:1234"
				return pod1
			},
			expErr:                  "error processing destination annotations: destination structured incorrectly: destination.port.upstream1.svc.ns1.ns.part1.err:1234",
			consulNamespacesEnabled: true,
			consulPartitionsEnabled: false,
		},
		{
			name: "unlabeled single destination",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "", true, true)
				pod1.Annotations[constants.AnnotationMeshDestinations] = "destination.upstream:1234"
				return pod1
			},
			expected: &pbmesh.Destinations{
				Workloads: &pbcatalog.WorkloadSelector{
					Names: []string{podName},
				},
				Destinations: []*pbmesh.Destination{
					{
						DestinationRef: &pbresource.Reference{
							Type: pbcatalog.ServiceType,
							Tenancy: &pbresource.Tenancy{
								Partition: constants.GetNormalizedConsulPartition(""),
								Namespace: constants.GetNormalizedConsulNamespace(""),
							},
							Name: "upstream",
						},
						DestinationPort: "destination",
						Datacenter:      "",
						ListenAddr: &pbmesh.Destination_IpPort{
							IpPort: &pbmesh.IPPortAddress{
								Port: uint32(1234),
								Ip:   consulNodeAddress,
							},
						},
					},
				},
			},
			consulNamespacesEnabled: false,
			consulPartitionsEnabled: false,
		},
		{
			name: "unlabeled single destination with namespace and partition",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "", true, true)
				pod1.Annotations[constants.AnnotationMeshDestinations] = "destination.upstream.foo.bar:1234"
				return pod1
			},
			expected: &pbmesh.Destinations{
				Workloads: &pbcatalog.WorkloadSelector{
					Names: []string{podName},
				},
				Destinations: []*pbmesh.Destination{
					{
						DestinationRef: &pbresource.Reference{
							Type: pbcatalog.ServiceType,
							Tenancy: &pbresource.Tenancy{
								Partition: "bar",
								Namespace: "foo",
							},
							Name: "upstream",
						},
						DestinationPort: "destination",
						Datacenter:      "",
						ListenAddr: &pbmesh.Destination_IpPort{
							IpPort: &pbmesh.IPPortAddress{
								Port: uint32(1234),
								Ip:   consulNodeAddress,
							},
						},
					},
				},
			},
			consulNamespacesEnabled: true,
			consulPartitionsEnabled: true,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			// Create test consulServer client.
			testClient := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
				c.Experiments = []string{"resource-apis"}
			})

			pc := &Controller{
				Log: logrtest.New(t),
				K8sNamespaceConfig: common.K8sNamespaceConfig{
					AllowK8sNamespacesSet: mapset.NewSetWith("*"),
					DenyK8sNamespacesSet:  mapset.NewSetWith(),
				},
				ConsulTenancyConfig: common.ConsulTenancyConfig{
					EnableConsulNamespaces: tt.consulNamespacesEnabled,
					EnableConsulPartitions: tt.consulPartitionsEnabled,
				},
				ResourceClient: testClient.ResourceClient,
			}

			err := pc.writeDestinations(context.Background(), *tt.pod())

			if tt.expErr != "" {
				require.EqualError(t, err, tt.expErr)
			} else {
				require.NoError(t, err)
				uID := getDestinationsID(tt.pod().Name, metav1.NamespaceDefault, constants.DefaultConsulPartition)
				expectedDestinationMatches(t, context.Background(), testClient.ResourceClient, uID, tt.expected)
			}
		})
	}
}

func TestDestinationsDelete(t *testing.T) {
	t.Parallel()

	const podName = "pod1"

	cases := []struct {
		name                 string
		pod                  func() *corev1.Pod
		existingDestinations *pbmesh.Destinations
		expErr               string
		configEntry          func() api.ConfigEntry
		consulUnavailable    bool
	}{
		{
			name: "labeled annotated destination with svc only",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "", true, true)
				pod1.Annotations[constants.AnnotationMeshDestinations] = "destination.port.upstream1.svc:1234"
				return pod1
			},
			existingDestinations: &pbmesh.Destinations{
				Workloads: &pbcatalog.WorkloadSelector{
					Names: []string{podName},
				},
				Destinations: []*pbmesh.Destination{
					{
						DestinationRef: &pbresource.Reference{
							Type: pbcatalog.ServiceType,
							Tenancy: &pbresource.Tenancy{
								Partition: constants.GetNormalizedConsulPartition(""),
								Namespace: constants.GetNormalizedConsulNamespace(""),
							},
							Name: "upstream1",
						},
						DestinationPort: "destination",
						Datacenter:      "",
						ListenAddr: &pbmesh.Destination_IpPort{
							IpPort: &pbmesh.IPPortAddress{
								Port: uint32(1234),
								Ip:   consulNodeAddress,
							},
						},
					},
				},
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			// Create test consulServer server.
			testClient := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
				c.Experiments = []string{"resource-apis"}
			})

			pc := &Controller{
				Log: logrtest.New(t),
				K8sNamespaceConfig: common.K8sNamespaceConfig{
					AllowK8sNamespacesSet: mapset.NewSetWith("*"),
					DenyK8sNamespacesSet:  mapset.NewSetWith(),
				},
				ResourceClient: testClient.ResourceClient,
			}

			// Load in the upstream for us to delete and check that it's there
			loadResource(t, context.Background(), testClient.ResourceClient, getDestinationsID(tt.pod().Name, constants.DefaultConsulNS, constants.DefaultConsulPartition), tt.existingDestinations, nil)
			uID := getDestinationsID(tt.pod().Name, metav1.NamespaceDefault, constants.DefaultConsulPartition)
			expectedDestinationMatches(t, context.Background(), testClient.ResourceClient, uID, tt.existingDestinations)

			// Delete the upstream
			nn := types.NamespacedName{Name: tt.pod().Name}
			err := pc.deleteDestinations(context.Background(), nn)

			// Verify the upstream has been deleted or that an expected error has been returned
			if tt.expErr != "" {
				require.EqualError(t, err, tt.expErr)
			} else {
				require.NoError(t, err)
				uID := getDestinationsID(tt.pod().Name, metav1.NamespaceDefault, constants.DefaultConsulPartition)
				expectedDestinationMatches(t, context.Background(), testClient.ResourceClient, uID, nil)
			}
		})
	}
}

func TestDeleteACLTokens(t *testing.T) {
	t.Parallel()

	podName := "foo-123"
	serviceName := "foo"

	// Create test consulServer server.
	masterToken := "b78d37c7-0ca7-5f4d-99ee-6d9975ce4586"
	testClient := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
		c.ACL.Enabled = true
		c.ACL.Tokens.InitialManagement = masterToken
		c.Experiments = []string{"resource-apis"}
	})

	test.SetupK8sAuthMethodV2(t, testClient.APIClient, serviceName, metav1.NamespaceDefault)
	token, _, err := testClient.APIClient.ACL().Login(&api.ACLLoginParams{
		AuthMethod:  test.AuthMethod,
		BearerToken: test.ServiceAccountJWTToken,
		Meta: map[string]string{
			"pod":       fmt.Sprintf("%s/%s", metav1.NamespaceDefault, podName),
			"component": "connect-injector",
		},
	}, nil)
	require.NoError(t, err)

	pc := &Controller{
		Log: logrtest.New(t),
		K8sNamespaceConfig: common.K8sNamespaceConfig{
			AllowK8sNamespacesSet: mapset.NewSetWith("*"),
			DenyK8sNamespacesSet:  mapset.NewSetWith(),
		},
		ResourceClient:      testClient.ResourceClient,
		AuthMethod:          test.AuthMethod,
		ConsulClientConfig:  testClient.Cfg,
		ConsulServerConnMgr: testClient.Watcher,
	}

	// Delete the ACL Token
	pod := types.NamespacedName{Name: podName, Namespace: metav1.NamespaceDefault}
	err = pc.deleteACLTokensForPod(testClient.APIClient, pod)
	require.NoError(t, err)

	// Verify the token has been deleted.
	_, _, err = testClient.APIClient.ACL().TokenRead(token.AccessorID, nil)
	require.Contains(t, err.Error(), "ACL not found")
}

// TestReconcileCreatePod ensures that a new pod reconciliation fans out to create
// the appropriate Consul resources. Translation details from pod to Consul workload are
// tested at the relevant private functions. Any error states that are also tested here.
func TestReconcileCreatePod(t *testing.T) {
	t.Parallel()

	ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name: metav1.NamespaceDefault,
	}}
	node := corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}}

	type testCase struct {
		name      string
		podName   string // This needs to be aligned with the pod created in `k8sObjects`
		namespace string // Defaults to metav1.NamespaceDefault if empty. Should be aligned with the ns in the pod

		k8sObjects                 func() []runtime.Object // testing node is injected separately
		expectedWorkload           *pbcatalog.Workload
		expectedHealthStatus       *pbcatalog.HealthStatus
		expectedProxyConfiguration *pbmesh.ProxyConfiguration
		expectedDestinations       *pbmesh.Destinations

		tproxy          bool
		overwriteProbes bool
		metrics         bool
		telemetry       bool

		requeue bool
		expErr  string
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

		// Create the pod controller.
		pc := &Controller{
			Client:              fakeClient,
			Log:                 logrtest.New(t),
			ConsulClientConfig:  testClient.Cfg,
			ConsulServerConnMgr: testClient.Watcher,
			K8sNamespaceConfig: common.K8sNamespaceConfig{
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSetWith(),
			},
			TProxyOverwriteProbes:    tc.overwriteProbes,
			EnableTransparentProxy:   tc.tproxy,
			EnableTelemetryCollector: tc.telemetry,
		}
		if tc.metrics {
			pc.MetricsConfig = metrics.Config{
				DefaultEnableMetrics:        true,
				DefaultPrometheusScrapePort: "1234",
			}
		}

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
		require.Equal(t, tc.requeue, resp.Requeue)

		wID := getWorkloadID(tc.podName, metav1.NamespaceDefault, constants.DefaultConsulPartition)
		expectedWorkloadMatches(t, context.Background(), testClient.ResourceClient, wID, tc.expectedWorkload)

		hsID := getHealthStatusID(tc.podName, metav1.NamespaceDefault, constants.DefaultConsulPartition)
		expectedHealthStatusMatches(t, context.Background(), testClient.ResourceClient, hsID, tc.expectedHealthStatus)

		pcID := getProxyConfigurationID(tc.podName, metav1.NamespaceDefault, constants.DefaultConsulPartition)
		expectedProxyConfigurationMatches(t, context.Background(), testClient.ResourceClient, pcID, tc.expectedProxyConfiguration)

		uID := getDestinationsID(tc.podName, metav1.NamespaceDefault, constants.DefaultConsulPartition)
		expectedDestinationMatches(t, context.Background(), testClient.ResourceClient, uID, tc.expectedDestinations)
	}

	testCases := []testCase{
		{
			name:    "vanilla new mesh-injected pod",
			podName: "foo",
			k8sObjects: func() []runtime.Object {
				pod := createPod("foo", "", true, true)
				addProbesAndOriginalPodAnnotation(pod)

				return []runtime.Object{pod}
			},
			tproxy:                     true,
			telemetry:                  true,
			metrics:                    true,
			overwriteProbes:            true,
			expectedWorkload:           createWorkload(),
			expectedHealthStatus:       createPassingHealthStatus(),
			expectedProxyConfiguration: createProxyConfiguration("foo", true, pbmesh.ProxyMode_PROXY_MODE_TRANSPARENT),
		},
		{
			name:    "vanilla new gateway pod (not mesh-injected)",
			podName: "foo",
			k8sObjects: func() []runtime.Object {
				pod := createPod("foo", "", false, true)
				pod.Annotations[constants.AnnotationGatewayKind] = "mesh-gateway"
				pod.Annotations[constants.AnnotationMeshInject] = "false"
				pod.Annotations[constants.AnnotationTransparentProxyOverwriteProbes] = "false"

				return []runtime.Object{pod}
			},
			tproxy:                     true,
			telemetry:                  true,
			metrics:                    true,
			overwriteProbes:            true,
			expectedWorkload:           createWorkload(),
			expectedHealthStatus:       createPassingHealthStatus(),
			expectedProxyConfiguration: createProxyConfiguration("foo", false, pbmesh.ProxyMode_PROXY_MODE_TRANSPARENT),
		},
		{
			name:      "pod in ignored namespace",
			podName:   "foo",
			namespace: metav1.NamespaceSystem,
			k8sObjects: func() []runtime.Object {
				pod := createPod("foo", "", true, true)
				pod.ObjectMeta.Namespace = metav1.NamespaceSystem
				return []runtime.Object{pod}
			},
		},
		{
			name:    "unhealthy new pod",
			podName: "foo",
			k8sObjects: func() []runtime.Object {
				pod := createPod("foo", "", true, false)
				return []runtime.Object{pod}
			},
			expectedWorkload:     createWorkload(),
			expectedHealthStatus: createCriticalHealthStatus("foo", "default"),
		},
		{
			name:    "return error - pod has no original pod annotation",
			podName: "foo",
			k8sObjects: func() []runtime.Object {
				pod := createPod("foo", "", true, false)
				return []runtime.Object{pod}
			},
			tproxy:               true,
			overwriteProbes:      true,
			expectedWorkload:     createWorkload(),
			expectedHealthStatus: createCriticalHealthStatus("foo", "default"),
			expErr:               "1 error occurred:\n\t* failed to get expose config: failed to get original pod spec: unexpected end of JSON input\n\n",
		},
		{
			name:    "pod has not been injected",
			podName: "foo",
			k8sObjects: func() []runtime.Object {
				pod := createPod("foo", "", false, true)
				return []runtime.Object{pod}
			},
		},
		{
			name:    "pod with annotations",
			podName: "foo",
			k8sObjects: func() []runtime.Object {
				pod := createPod("foo", "", true, true)
				addProbesAndOriginalPodAnnotation(pod)
				pod.Annotations[constants.AnnotationMeshDestinations] = "destination.port.mySVC.svc:24601"
				return []runtime.Object{pod}
			},
			tproxy:                     false,
			telemetry:                  true,
			metrics:                    true,
			overwriteProbes:            true,
			expectedWorkload:           createWorkload(),
			expectedHealthStatus:       createPassingHealthStatus(),
			expectedProxyConfiguration: createProxyConfiguration("foo", true, pbmesh.ProxyMode_PROXY_MODE_DEFAULT),
			expectedDestinations:       createDestinations(),
		},
		{
			name:    "pod w/o IP",
			podName: "foo",
			k8sObjects: func() []runtime.Object {
				pod := createPod("foo", "", true, true)
				pod.Status.PodIP = ""
				return []runtime.Object{pod}
			},
			requeue: true,
		},
		// TODO: make sure multi-error accumulates errors
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			run(t, tc)
		})
	}
}

// TestReconcileUpdatePod test updating a Pod object when there is already matching resources in Consul.
// Updates are unlikely because of the immutable behaviors of pods as members of deployment/statefulset,
// but theoretically it is possible to update annotations and labels in-place. Most likely this will be
// from a change in health status.
func TestReconcileUpdatePod(t *testing.T) {
	t.Parallel()

	ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name: metav1.NamespaceDefault,
	}}
	node := corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}}

	type testCase struct {
		name      string
		podName   string // This needs to be aligned with the pod created in `k8sObjects`
		namespace string // Defaults to metav1.NamespaceDefault if empty. Should be aligned with the ns in the pod

		k8sObjects func() []runtime.Object // testing node is injected separately

		existingWorkload           *pbcatalog.Workload
		existingHealthStatus       *pbcatalog.HealthStatus
		existingProxyConfiguration *pbmesh.ProxyConfiguration
		existingDestinations       *pbmesh.Destinations

		expectedWorkload           *pbcatalog.Workload
		expectedHealthStatus       *pbcatalog.HealthStatus
		expectedProxyConfiguration *pbmesh.ProxyConfiguration
		expectedDestinations       *pbmesh.Destinations

		tproxy          bool
		overwriteProbes bool
		metrics         bool
		telemetry       bool

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

		// Create the pod controller.
		pc := &Controller{
			Client:              fakeClient,
			Log:                 logrtest.New(t),
			ConsulClientConfig:  testClient.Cfg,
			ConsulServerConnMgr: testClient.Watcher,
			K8sNamespaceConfig: common.K8sNamespaceConfig{
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSetWith(),
			},
			TProxyOverwriteProbes:    tc.overwriteProbes,
			EnableTransparentProxy:   tc.tproxy,
			EnableTelemetryCollector: tc.telemetry,
		}
		if tc.metrics {
			pc.MetricsConfig = metrics.Config{
				DefaultEnableMetrics:        true,
				DefaultPrometheusScrapePort: "1234",
			}
		}

		namespace := tc.namespace
		if namespace == "" {
			namespace = metav1.NamespaceDefault
		}

		workloadID := getWorkloadID(tc.podName, constants.DefaultConsulNS, constants.DefaultConsulPartition)
		loadResource(t, context.Background(), testClient.ResourceClient, workloadID, tc.existingWorkload, nil)
		loadResource(t, context.Background(), testClient.ResourceClient, getHealthStatusID(tc.podName, constants.DefaultConsulNS, constants.DefaultConsulPartition), tc.existingHealthStatus, workloadID)
		loadResource(t, context.Background(), testClient.ResourceClient, getProxyConfigurationID(tc.podName, constants.DefaultConsulNS, constants.DefaultConsulPartition), tc.existingProxyConfiguration, nil)
		loadResource(t, context.Background(), testClient.ResourceClient, getDestinationsID(tc.podName, constants.DefaultConsulNS, constants.DefaultConsulPartition), tc.existingDestinations, nil)

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

		wID := getWorkloadID(tc.podName, metav1.NamespaceDefault, constants.DefaultConsulPartition)
		expectedWorkloadMatches(t, context.Background(), testClient.ResourceClient, wID, tc.expectedWorkload)

		hsID := getHealthStatusID(tc.podName, metav1.NamespaceDefault, constants.DefaultConsulPartition)
		expectedHealthStatusMatches(t, context.Background(), testClient.ResourceClient, hsID, tc.expectedHealthStatus)

		pcID := getProxyConfigurationID(tc.podName, metav1.NamespaceDefault, constants.DefaultConsulPartition)
		expectedProxyConfigurationMatches(t, context.Background(), testClient.ResourceClient, pcID, tc.expectedProxyConfiguration)

		uID := getDestinationsID(tc.podName, metav1.NamespaceDefault, constants.DefaultConsulPartition)
		expectedDestinationMatches(t, context.Background(), testClient.ResourceClient, uID, tc.expectedDestinations)
	}

	testCases := []testCase{
		{
			name:    "pod update ports",
			podName: "foo",
			k8sObjects: func() []runtime.Object {
				pod := createPod("foo", "", true, true)
				return []runtime.Object{pod}
			},
			existingHealthStatus: createPassingHealthStatus(),
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
			expectedWorkload:     createWorkload(),
			expectedHealthStatus: createPassingHealthStatus(),
		},
		{
			name:    "pod healthy to unhealthy",
			podName: "foo",
			k8sObjects: func() []runtime.Object {
				pod := createPod("foo", "", true, false)
				return []runtime.Object{pod}
			},
			existingWorkload:     createWorkload(),
			existingHealthStatus: createPassingHealthStatus(),
			expectedWorkload:     createWorkload(),
			expectedHealthStatus: createCriticalHealthStatus("foo", "default"),
		},
		{
			name:    "add metrics, tproxy and probe overwrite to pod",
			podName: "foo",
			k8sObjects: func() []runtime.Object {
				pod := createPod("foo", "", true, true)
				pod.Annotations[constants.KeyTransparentProxy] = "true"
				pod.Annotations[constants.AnnotationTransparentProxyOverwriteProbes] = "true"
				pod.Annotations[constants.AnnotationEnableMetrics] = "true"
				pod.Annotations[constants.AnnotationPrometheusScrapePort] = "21234"
				addProbesAndOriginalPodAnnotation(pod)

				return []runtime.Object{pod}
			},
			existingWorkload:     createWorkload(),
			existingHealthStatus: createPassingHealthStatus(),
			expectedWorkload:     createWorkload(),
			expectedHealthStatus: createPassingHealthStatus(),
			expectedProxyConfiguration: &pbmesh.ProxyConfiguration{
				Workloads: &pbcatalog.WorkloadSelector{
					Names: []string{"foo"},
				},
				DynamicConfig: &pbmesh.DynamicConfig{
					Mode: pbmesh.ProxyMode_PROXY_MODE_TRANSPARENT,
					ExposeConfig: &pbmesh.ExposeConfig{
						ExposePaths: []*pbmesh.ExposePath{
							{
								ListenerPort:  20400,
								LocalPathPort: 2001,
								Path:          "/livez",
							},
							{
								ListenerPort:  20300,
								LocalPathPort: 2000,
								Path:          "/readyz",
							},
							{
								ListenerPort:  20500,
								LocalPathPort: 2002,
								Path:          "/startupz",
							},
						},
					},
					TransparentProxy: &pbmesh.TransparentProxy{
						OutboundListenerPort: 15001,
					},
				},
				BootstrapConfig: &pbmesh.BootstrapConfig{
					PrometheusBindAddr: "0.0.0.0:21234",
				},
			},
		},
		{
			name:    "pod update explicit destination",
			podName: "foo",
			k8sObjects: func() []runtime.Object {
				pod := createPod("foo", "", true, true)
				pod.Annotations[constants.AnnotationMeshDestinations] = "destination.port.mySVC.svc:24601"
				return []runtime.Object{pod}
			},
			existingWorkload:     createWorkload(),
			existingHealthStatus: createPassingHealthStatus(),
			existingDestinations: &pbmesh.Destinations{
				Workloads: &pbcatalog.WorkloadSelector{
					Names: []string{"foo"},
				},
				Destinations: []*pbmesh.Destination{
					{
						DestinationRef: &pbresource.Reference{
							Type: pbcatalog.ServiceType,
							Tenancy: &pbresource.Tenancy{
								Partition: "ap1",
								Namespace: "ns1",
							},
							Name: "mySVC3",
						},
						DestinationPort: "destination2",
						Datacenter:      "",
						ListenAddr: &pbmesh.Destination_IpPort{
							IpPort: &pbmesh.IPPortAddress{
								Port: uint32(1234),
								Ip:   consulNodeAddress,
							},
						},
					},
				},
			},
			expectedWorkload:     createWorkload(),
			expectedHealthStatus: createPassingHealthStatus(),
			expectedDestinations: createDestinations(),
		},
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

		existingWorkload           *pbcatalog.Workload
		existingHealthStatus       *pbcatalog.HealthStatus
		existingProxyConfiguration *pbmesh.ProxyConfiguration
		existingDestinations       *pbmesh.Destinations

		expectedWorkload           *pbcatalog.Workload
		expectedHealthStatus       *pbcatalog.HealthStatus
		expectedProxyConfiguration *pbmesh.ProxyConfiguration
		expectedDestinations       *pbmesh.Destinations

		aclsEnabled bool

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
		masterToken := "b78d37c7-0ca7-5f4d-99ee-6d9975ce4586"

		testClient := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
			if tc.aclsEnabled {
				c.ACL.Enabled = true
				c.ACL.Tokens.InitialManagement = masterToken
			}
			c.Experiments = []string{"resource-apis"}
		})

		ctx := context.Background()
		if tc.aclsEnabled {
			ctx = metadata.AppendToOutgoingContext(context.Background(), "x-consul-token", masterToken)
		}

		// Wait for the default partition to be created
		require.Eventually(t, func() bool {
			_, _, err := testClient.APIClient.Partitions().Read(ctx, constants.DefaultConsulPartition, nil)
			return err == nil
		}, 5*time.Second, 500*time.Millisecond)

		// Create the pod controller.
		pc := &Controller{
			Client:              fakeClient,
			Log:                 logrtest.New(t),
			ConsulClientConfig:  testClient.Cfg,
			ConsulServerConnMgr: testClient.Watcher,
			K8sNamespaceConfig: common.K8sNamespaceConfig{
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSetWith(),
			},
		}
		if tc.aclsEnabled {
			pc.AuthMethod = test.AuthMethod
		}

		namespace := tc.namespace
		if namespace == "" {
			namespace = metav1.NamespaceDefault
		}

		workloadID := getWorkloadID(tc.podName, constants.DefaultConsulNS, constants.DefaultConsulPartition)
		loadResource(t, ctx, testClient.ResourceClient, workloadID, tc.existingWorkload, nil)
		loadResource(t, ctx, testClient.ResourceClient, getHealthStatusID(tc.podName, constants.DefaultConsulNS, constants.DefaultConsulPartition), tc.existingHealthStatus, workloadID)
		loadResource(t, ctx, testClient.ResourceClient, getProxyConfigurationID(tc.podName, constants.DefaultConsulNS, constants.DefaultConsulPartition), tc.existingProxyConfiguration, nil)
		loadResource(t, ctx, testClient.ResourceClient, getDestinationsID(tc.podName, constants.DefaultConsulNS, constants.DefaultConsulPartition), tc.existingDestinations, nil)

		var token *api.ACLToken
		var err error
		if tc.aclsEnabled {
			test.SetupK8sAuthMethodV2(t, testClient.APIClient, tc.podName, metav1.NamespaceDefault) //podName is a standin for the service name
			token, _, err = testClient.APIClient.ACL().Login(&api.ACLLoginParams{
				AuthMethod:  test.AuthMethod,
				BearerToken: test.ServiceAccountJWTToken,
				Meta: map[string]string{
					"pod":       fmt.Sprintf("%s/%s", metav1.NamespaceDefault, tc.podName),
					"component": "connect-injector",
				},
			}, nil)
			require.NoError(t, err)

			// We create another junk token here just to make sure it doesn't interfere with cleaning up the
			// previous "real" token that has metadata.
			_, _, err = testClient.APIClient.ACL().Login(&api.ACLLoginParams{
				AuthMethod:  test.AuthMethod,
				BearerToken: test.ServiceAccountJWTToken,
			}, nil)
			require.NoError(t, err)
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

		wID := getWorkloadID(tc.podName, metav1.NamespaceDefault, constants.DefaultConsulPartition)
		expectedWorkloadMatches(t, ctx, testClient.ResourceClient, wID, tc.expectedWorkload)

		hsID := getHealthStatusID(tc.podName, metav1.NamespaceDefault, constants.DefaultConsulPartition)
		expectedHealthStatusMatches(t, ctx, testClient.ResourceClient, hsID, tc.expectedHealthStatus)

		pcID := getProxyConfigurationID(tc.podName, metav1.NamespaceDefault, constants.DefaultConsulPartition)
		expectedProxyConfigurationMatches(t, ctx, testClient.ResourceClient, pcID, tc.expectedProxyConfiguration)

		uID := getDestinationsID(tc.podName, metav1.NamespaceDefault, constants.DefaultConsulPartition)
		expectedDestinationMatches(t, ctx, testClient.ResourceClient, uID, tc.expectedDestinations)

		if tc.aclsEnabled {
			_, _, err = testClient.APIClient.ACL().TokenRead(token.AccessorID, nil)
			require.Contains(t, err.Error(), "ACL not found")
		}

	}

	testCases := []testCase{
		{
			name:                       "vanilla delete pod",
			podName:                    "foo",
			existingWorkload:           createWorkload(),
			existingHealthStatus:       createPassingHealthStatus(),
			existingProxyConfiguration: createProxyConfiguration("foo", true, pbmesh.ProxyMode_PROXY_MODE_TRANSPARENT),
		},
		{
			name:                       "annotated delete pod",
			podName:                    "foo",
			existingWorkload:           createWorkload(),
			existingHealthStatus:       createPassingHealthStatus(),
			existingProxyConfiguration: createProxyConfiguration("foo", true, pbmesh.ProxyMode_PROXY_MODE_DEFAULT),
			existingDestinations:       createDestinations(),
		},
		{
			name:                       "delete pod w/ acls",
			podName:                    "foo",
			existingWorkload:           createWorkload(),
			existingHealthStatus:       createPassingHealthStatus(),
			existingProxyConfiguration: createProxyConfiguration("foo", true, pbmesh.ProxyMode_PROXY_MODE_TRANSPARENT),
			aclsEnabled:                true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			run(t, tc)
		})
	}
}

// createPod creates a multi-port pod as a base for tests. If `namespace` is empty,
// the default Kube namespace will be used.
func createPod(name, namespace string, inject, ready bool) *corev1.Pod {
	if namespace == "" {
		namespace = metav1.NamespaceDefault
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    map[string]string{},
			Annotations: map[string]string{
				constants.AnnotationConsulK8sVersion: "1.3.0",
			},
		},
		Status: corev1.PodStatus{
			PodIP:  "10.0.0.1",
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
					ReadinessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Path: "/readyz",
								Port: intstr.FromInt(2000),
							},
						},
					},
					LivenessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Path: "/livez",
								Port: intstr.FromInt(2001),
							},
						},
					},
					StartupProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Path: "/startupz",
								Port: intstr.FromInt(2002),
							},
						},
					},
				},
			},
			NodeName:           nodeName,
			ServiceAccountName: name,
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

// createWorkload creates a workload that matches the pod from createPod.
func createWorkload() *pbcatalog.Workload {
	return &pbcatalog.Workload{
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
		Identity: "foo",
	}
}

// createPassingHealthStatus creates a passing HealthStatus that matches the pod from createPod.
func createPassingHealthStatus() *pbcatalog.HealthStatus {
	return &pbcatalog.HealthStatus{
		Type:        constants.ConsulKubernetesCheckType,
		Status:      pbcatalog.Health_HEALTH_PASSING,
		Output:      constants.KubernetesSuccessReasonMsg,
		Description: constants.ConsulKubernetesCheckName,
	}
}

// createCriticalHealthStatus creates a failing HealthStatus that matches the pod from createPod.
func createCriticalHealthStatus(name string, namespace string) *pbcatalog.HealthStatus {
	return &pbcatalog.HealthStatus{
		Type:        constants.ConsulKubernetesCheckType,
		Status:      pbcatalog.Health_HEALTH_CRITICAL,
		Output:      fmt.Sprintf("Pod \"%s/%s\" is not ready", namespace, name),
		Description: constants.ConsulKubernetesCheckName,
	}
}

// createProxyConfiguration creates a proxyConfiguration that matches the pod from createPod,
// assuming that metrics, telemetry, and overwrite probes are enabled separately.
func createProxyConfiguration(podName string, overwriteProbes bool, mode pbmesh.ProxyMode) *pbmesh.ProxyConfiguration {
	mesh := &pbmesh.ProxyConfiguration{
		Workloads: &pbcatalog.WorkloadSelector{
			Names: []string{podName},
		},
		DynamicConfig: &pbmesh.DynamicConfig{
			Mode:         mode,
			ExposeConfig: nil,
		},
		BootstrapConfig: &pbmesh.BootstrapConfig{
			PrometheusBindAddr:              "0.0.0.0:1234",
			TelemetryCollectorBindSocketDir: DefaultTelemetryBindSocketDir,
		},
	}

	if overwriteProbes {
		mesh.DynamicConfig.ExposeConfig = &pbmesh.ExposeConfig{
			ExposePaths: []*pbmesh.ExposePath{
				{
					ListenerPort:  20400,
					LocalPathPort: 2001,
					Path:          "/livez",
				},
				{
					ListenerPort:  20300,
					LocalPathPort: 2000,
					Path:          "/readyz",
				},
				{
					ListenerPort:  20500,
					LocalPathPort: 2002,
					Path:          "/startupz",
				},
			},
		}
	}

	if mode == pbmesh.ProxyMode_PROXY_MODE_TRANSPARENT {
		mesh.DynamicConfig.TransparentProxy = &pbmesh.TransparentProxy{
			OutboundListenerPort: 15001,
		}
	}

	return mesh
}

// createCriticalHealthStatus creates a failing HealthStatus that matches the pod from createPod.
func createDestinations() *pbmesh.Destinations {
	return &pbmesh.Destinations{
		Workloads: &pbcatalog.WorkloadSelector{
			Names: []string{"foo"},
		},
		Destinations: []*pbmesh.Destination{
			{
				DestinationRef: &pbresource.Reference{
					Type: pbcatalog.ServiceType,
					Tenancy: &pbresource.Tenancy{
						Partition: constants.GetNormalizedConsulPartition(""),
						Namespace: constants.GetNormalizedConsulNamespace(""),
					},
					Name: "mySVC",
				},
				DestinationPort: "destination",
				Datacenter:      "",
				ListenAddr: &pbmesh.Destination_IpPort{
					IpPort: &pbmesh.IPPortAddress{
						Port: uint32(24601),
						Ip:   consulNodeAddress,
					},
				},
			},
		},
	}
}

func expectedWorkloadMatches(t *testing.T, ctx context.Context, client pbresource.ResourceServiceClient, id *pbresource.ID, expectedWorkload *pbcatalog.Workload) {
	req := &pbresource.ReadRequest{Id: id}

	res, err := client.Read(ctx, req)

	if expectedWorkload == nil {
		require.Error(t, err)
		s, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.NotFound, s.Code())
		return
	}

	require.NoError(t, err)
	require.NotNil(t, res)

	requireEqualResourceID(t, id, res.GetResource().GetId())

	require.NotNil(t, res.GetResource().GetData())

	actualWorkload := &pbcatalog.Workload{}
	err = res.GetResource().GetData().UnmarshalTo(actualWorkload)
	require.NoError(t, err)

	diff := cmp.Diff(expectedWorkload, actualWorkload, test.CmpProtoIgnoreOrder()...)
	require.Equal(t, "", diff, "Workloads do not match")
}

func expectedHealthStatusMatches(t *testing.T, ctx context.Context, client pbresource.ResourceServiceClient, id *pbresource.ID, expectedHealthStatus *pbcatalog.HealthStatus) {
	req := &pbresource.ReadRequest{Id: id}

	res, err := client.Read(ctx, req)

	if expectedHealthStatus == nil {
		// Because HealthStatus is asynchronously garbage-collected, we can retry to make sure it gets cleaned up.
		require.Eventually(t, func() bool {
			_, err := client.Read(ctx, req)
			s, ok := status.FromError(err)
			return ok && codes.NotFound == s.Code()
		}, 3*time.Second, 500*time.Millisecond)
		return
	}

	require.NoError(t, err)
	require.NotNil(t, res)

	requireEqualResourceID(t, id, res.GetResource().GetId())

	require.NotNil(t, res.GetResource().GetData())

	actualHealthStatus := &pbcatalog.HealthStatus{}
	err = res.GetResource().GetData().UnmarshalTo(actualHealthStatus)
	require.NoError(t, err)

	diff := cmp.Diff(expectedHealthStatus, actualHealthStatus, test.CmpProtoIgnoreOrder()...)
	require.Equal(t, "", diff, "HealthStatuses do not match")
}

func expectedProxyConfigurationMatches(t *testing.T, ctx context.Context, client pbresource.ResourceServiceClient, id *pbresource.ID, expectedProxyConfiguration *pbmesh.ProxyConfiguration) {
	req := &pbresource.ReadRequest{Id: id}

	res, err := client.Read(ctx, req)

	if expectedProxyConfiguration == nil {
		require.Error(t, err)
		s, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.NotFound, s.Code())
		return
	}

	require.NoError(t, err)
	require.NotNil(t, res)

	requireEqualResourceID(t, id, res.GetResource().GetId())

	require.NotNil(t, res.GetResource().GetData())

	actualProxyConfiguration := &pbmesh.ProxyConfiguration{}
	err = res.GetResource().GetData().UnmarshalTo(actualProxyConfiguration)
	require.NoError(t, err)

	diff := cmp.Diff(expectedProxyConfiguration, actualProxyConfiguration, test.CmpProtoIgnoreOrder()...)
	require.Equal(t, "", diff, "ProxyConfigurations do not match")
}

func expectedDestinationMatches(t *testing.T, ctx context.Context, client pbresource.ResourceServiceClient, id *pbresource.ID, expectedUpstreams *pbmesh.Destinations) {
	req := &pbresource.ReadRequest{Id: id}
	res, err := client.Read(ctx, req)

	if expectedUpstreams == nil {
		require.Error(t, err)
		s, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.NotFound, s.Code())
		return
	}

	require.NoError(t, err)
	require.NotNil(t, res)

	requireEqualResourceID(t, id, res.GetResource().GetId())

	require.NotNil(t, res.GetResource().GetData())

	actualUpstreams := &pbmesh.Destinations{}
	err = res.GetResource().GetData().UnmarshalTo(actualUpstreams)
	require.NoError(t, err)

	require.True(t, proto.Equal(actualUpstreams, expectedUpstreams))
}

func loadResource(t *testing.T, ctx context.Context, client pbresource.ResourceServiceClient, id *pbresource.ID, proto proto.Message, owner *pbresource.ID) {
	if id == nil || !proto.ProtoReflect().IsValid() {
		return
	}

	data, err := anypb.New(proto)
	require.NoError(t, err)

	resource := &pbresource.Resource{
		Id:    id,
		Data:  data,
		Owner: owner,
	}

	req := &pbresource.WriteRequest{Resource: resource}
	_, err = client.Write(ctx, req)
	require.NoError(t, err)
	test.ResourceHasPersisted(t, ctx, client, id)
}

func addProbesAndOriginalPodAnnotation(pod *corev1.Pod) {
	podBytes, _ := json.Marshal(pod)
	pod.Annotations[constants.AnnotationOriginalPod] = string(podBytes)

	// Fake the probe changes that would be added by the mesh webhook
	pod.Spec.Containers[0].ReadinessProbe.HTTPGet.Port = intstr.FromInt(20300)
	pod.Spec.Containers[0].LivenessProbe.HTTPGet.Port = intstr.FromInt(20400)
	pod.Spec.Containers[0].StartupProbe.HTTPGet.Port = intstr.FromInt(20500)
}

func requireEqualResourceID(t *testing.T, expected, actual *pbresource.ID) {
	opts := []cmp.Option{
		protocmp.IgnoreFields(&pbresource.ID{}, "uid"),
	}
	opts = append(opts, test.CmpProtoIgnoreOrder()...)
	diff := cmp.Diff(expected, actual, opts...)
	require.Equal(t, "", diff, "resource IDs do not match")
}
