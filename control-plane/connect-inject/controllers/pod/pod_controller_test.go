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
	"github.com/hashicorp/consul-k8s/control-plane/consul"
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
		resourceClient, err := consul.NewResourceServiceClient(testClient.Watcher)
		require.NoError(t, err)

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
			ResourceClient: resourceClient,
		}

		err = pc.writeWorkload(context.Background(), *tc.pod)
		require.NoError(t, err)

		req := &pbresource.ReadRequest{
			Id: getWorkloadID(tc.pod.GetName(), metav1.NamespaceDefault, constants.DefaultConsulPartition),
		}
		actualRes, err := resourceClient.Read(context.Background(), req)
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
		resourceClient, err := consul.NewResourceServiceClient(testClient.Watcher)
		require.NoError(t, err)

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
			ResourceClient: resourceClient,
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

		readReq := &pbresource.ReadRequest{
			Id: getWorkloadID(tc.pod.GetName(), metav1.NamespaceDefault, constants.DefaultConsulPartition),
		}
		_, err = resourceClient.Read(context.Background(), readReq)
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
		resourceClient, err := consul.NewResourceServiceClient(testClient.Watcher)
		require.NoError(t, err)

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
			ResourceClient: resourceClient,
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
		_, err = resourceClient.Write(context.Background(), writeReq)
		require.NoError(t, err)

		// Test writing the pod to a HealthStatus
		err = pc.writeHealthStatus(context.Background(), *tc.pod)
		require.NoError(t, err)

		req := &pbresource.ReadRequest{
			Id: getHealthStatusID(tc.pod.GetName(), metav1.NamespaceDefault, constants.DefaultConsulPartition),
		}
		actualRes, err := resourceClient.Read(context.Background(), req)
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
		resourceClient, err := consul.NewResourceServiceClient(testClient.Watcher)
		require.NoError(t, err)

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
			ResourceClient:           resourceClient,
		}

		if tc.metrics {
			pc.MetricsConfig = metrics.Config{
				DefaultEnableMetrics:        true,
				DefaultPrometheusScrapePort: "5678",
			}
		}

		// Test writing the pod to a HealthStatus
		err = pc.writeProxyConfiguration(context.Background(), *tc.pod)
		require.NoError(t, err)

		req := &pbresource.ReadRequest{
			Id: getProxyConfigurationID(tc.pod.GetName(), metav1.NamespaceDefault, constants.DefaultConsulPartition),
		}
		actualRes, err := resourceClient.Read(context.Background(), req)

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
		resourceClient, err := consul.NewResourceServiceClient(testClient.Watcher)
		require.NoError(t, err)

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
			ResourceClient: resourceClient,
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

		_, err = resourceClient.Write(context.Background(), writeReq)
		require.NoError(t, err)
		test.ResourceHasPersisted(t, resourceClient, pcID)

		reconcileReq := types.NamespacedName{
			Namespace: metav1.NamespaceDefault,
			Name:      tc.pod.GetName(),
		}
		err = pc.deleteProxyConfiguration(context.Background(), reconcileReq)
		require.NoError(t, err)

		readReq := &pbresource.ReadRequest{
			Id: getProxyConfigurationID(tc.pod.GetName(), metav1.NamespaceDefault, constants.DefaultConsulPartition),
		}
		_, err = resourceClient.Read(context.Background(), readReq)
		require.Error(t, err)
		s, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.NotFound, s.Code())
	}

	testCases := []testCase{
		{
			name:                       "proxy configuration delete",
			pod:                        createPod("foo", "", true, true),
			existingProxyConfiguration: createProxyConfiguration("foo", pbmesh.ProxyMode_PROXY_MODE_TRANSPARENT),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			run(t, tc)
		})
	}
}

// TestUpstreamsWrite does a subsampling of tests covered in TestProcessUpstreams to make sure things are hooked up
// correctly. For the sake of test speed, more exhaustive testing is performed in TestProcessUpstreams.
func TestUpstreamsWrite(t *testing.T) {
	t.Parallel()

	const podName = "pod1"

	cases := []struct {
		name                    string
		pod                     func() *corev1.Pod
		expected                *pbmesh.Upstreams
		expErr                  string
		consulNamespacesEnabled bool
		consulPartitionsEnabled bool
	}{
		{
			name: "labeled annotated upstream with svc only",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "", true, true)
				pod1.Annotations[constants.AnnotationMeshDestinations] = "myPort.port.upstream1.svc:1234"
				return pod1
			},
			expected: &pbmesh.Upstreams{
				Workloads: &pbcatalog.WorkloadSelector{
					Names: []string{podName},
				},
				Upstreams: []*pbmesh.Upstream{
					{
						DestinationRef: &pbresource.Reference{
							Type: pbcatalog.ServiceType,
							Tenancy: &pbresource.Tenancy{
								Partition: getDefaultConsulPartition(""),
								Namespace: getDefaultConsulNamespace(""),
								PeerName:  getDefaultConsulPeer(""),
							},
							Name: "upstream1",
						},
						DestinationPort: "myPort",
						Datacenter:      "",
						ListenAddr: &pbmesh.Upstream_IpPort{
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
			name: "labeled annotated upstream with svc, ns, and peer",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "", true, true)
				pod1.Annotations[constants.AnnotationMeshDestinations] = "myPort.port.upstream1.svc.ns1.ns.peer1.peer:1234"
				return pod1
			},
			expErr: "error processing upstream annotations: upstream currently does not support peers: myPort.port.upstream1.svc.ns1.ns.peer1.peer:1234",
			// TODO: uncomment this and remove expErr when peers is supported
			//expected: &pbmesh.Upstreams{
			//	Workloads: &pbcatalog.WorkloadSelector{
			//		Names: []string{podName},
			//	},
			//	Upstreams: []*pbmesh.Upstream{
			//		{
			//			DestinationRef: &pbresource.Reference{
			//              Type: pbcatalog.ServiceType,
			//				Tenancy: &pbresource.Tenancy{
			//					Partition: getDefaultConsulPartition(""),
			//					Namespace: "ns1",
			//					PeerName:  "peer1",
			//				},
			//				Name: "upstream1",
			//			},
			//			DestinationPort: "myPort",
			//			Datacenter:      "",
			//			ListenAddr: &pbmesh.Upstream_IpPort{
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
			name: "labeled annotated upstream with svc, ns, and partition",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "", true, true)
				pod1.Annotations[constants.AnnotationMeshDestinations] = "myPort.port.upstream1.svc.ns1.ns.part1.ap:1234"
				return pod1
			},
			expected: &pbmesh.Upstreams{
				Workloads: &pbcatalog.WorkloadSelector{
					Names: []string{podName},
				},
				Upstreams: []*pbmesh.Upstream{
					{
						DestinationRef: &pbresource.Reference{
							Type: pbcatalog.ServiceType,
							Tenancy: &pbresource.Tenancy{
								Partition: "part1",
								Namespace: "ns1",
								PeerName:  getDefaultConsulPeer(""),
							},
							Name: "upstream1",
						},
						DestinationPort: "myPort",
						Datacenter:      "",
						ListenAddr: &pbmesh.Upstream_IpPort{
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
			name: "error labeled annotated upstream error: invalid partition/dc/peer",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "", true, true)
				pod1.Annotations[constants.AnnotationMeshDestinations] = "myPort.port.upstream1.svc.ns1.ns.part1.err:1234"
				return pod1
			},
			expErr:                  "error processing upstream annotations: upstream structured incorrectly: myPort.port.upstream1.svc.ns1.ns.part1.err:1234",
			consulNamespacesEnabled: true,
			consulPartitionsEnabled: false,
		},
		{
			name: "unlabeled single upstream",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "", true, true)
				pod1.Annotations[constants.AnnotationMeshDestinations] = "myPort.upstream:1234"
				return pod1
			},
			expected: &pbmesh.Upstreams{
				Workloads: &pbcatalog.WorkloadSelector{
					Names: []string{podName},
				},
				Upstreams: []*pbmesh.Upstream{
					{
						DestinationRef: &pbresource.Reference{
							Type: pbcatalog.ServiceType,
							Tenancy: &pbresource.Tenancy{
								Partition: getDefaultConsulPartition(""),
								Namespace: getDefaultConsulNamespace(""),
								PeerName:  getDefaultConsulPeer(""),
							},
							Name: "upstream",
						},
						DestinationPort: "myPort",
						Datacenter:      "",
						ListenAddr: &pbmesh.Upstream_IpPort{
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
			name: "unlabeled single upstream with namespace and partition",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "", true, true)
				pod1.Annotations[constants.AnnotationMeshDestinations] = "myPort.upstream.foo.bar:1234"
				return pod1
			},
			expected: &pbmesh.Upstreams{
				Workloads: &pbcatalog.WorkloadSelector{
					Names: []string{podName},
				},
				Upstreams: []*pbmesh.Upstream{
					{
						DestinationRef: &pbresource.Reference{
							Type: pbcatalog.ServiceType,
							Tenancy: &pbresource.Tenancy{
								Partition: "bar",
								Namespace: "foo",
								PeerName:  getDefaultConsulPeer(""),
							},
							Name: "upstream",
						},
						DestinationPort: "myPort",
						Datacenter:      "",
						ListenAddr: &pbmesh.Upstream_IpPort{
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
			resourceClient, err := consul.NewResourceServiceClient(testClient.Watcher)
			require.NoError(t, err)

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
				ResourceClient: resourceClient,
			}

			err = pc.writeUpstreams(context.Background(), *tt.pod())

			if tt.expErr != "" {
				require.EqualError(t, err, tt.expErr)
			} else {
				require.NoError(t, err)
				uID := getUpstreamsID(tt.pod().Name, metav1.NamespaceDefault, constants.DefaultConsulPartition)
				expectedUpstreamMatches(t, resourceClient, uID, tt.expected)
			}
		})
	}
}

func TestProcessUpstreams(t *testing.T) {
	t.Parallel()

	const podName = "pod1"

	cases := []struct {
		name                    string
		pod                     func() *corev1.Pod
		expected                *pbmesh.Upstreams
		expErr                  string
		configEntry             func() api.ConfigEntry
		consulUnavailable       bool
		consulNamespacesEnabled bool
		consulPartitionsEnabled bool
	}{
		{
			name: "labeled annotated upstream with svc only",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "", true, true)
				pod1.Annotations[constants.AnnotationMeshDestinations] = "myPort.port.upstream1.svc:1234"
				return pod1
			},
			expected: &pbmesh.Upstreams{
				Workloads: &pbcatalog.WorkloadSelector{
					Names: []string{podName},
				},
				Upstreams: []*pbmesh.Upstream{
					{
						DestinationRef: &pbresource.Reference{
							Type: pbcatalog.ServiceType,
							Tenancy: &pbresource.Tenancy{
								Partition: getDefaultConsulPartition(""),
								Namespace: getDefaultConsulNamespace(""),
								PeerName:  getDefaultConsulPeer(""),
							},
							Name: "upstream1",
						},
						DestinationPort: "myPort",
						Datacenter:      "",
						ListenAddr: &pbmesh.Upstream_IpPort{
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
			name: "labeled annotated upstream with svc and dc",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "", true, true)
				pod1.Annotations[constants.AnnotationMeshDestinations] = "myPort.port.upstream1.svc.dc1.dc:1234"
				return pod1
			},
			expErr: "upstream currently does not support datacenters: myPort.port.upstream1.svc.dc1.dc:1234",
			// TODO: uncomment this and remove expErr when datacenters is supported
			//expected: &pbmesh.Upstreams{
			//	Workloads: &pbcatalog.WorkloadSelector{
			//		Names: []string{podName},
			//	},
			//	Upstreams: []*pbmesh.Upstream{
			//		{
			//			DestinationRef: &pbresource.Reference{
			//				Type: pbcatalog.ServiceType,
			//				Tenancy: &pbresource.Tenancy{
			//					Partition: getDefaultConsulPartition(""),
			//					Namespace: getDefaultConsulNamespace(""),
			//					PeerName: getDefaultConsulPeer(""),
			//				},
			//				Name: "upstream1",
			//			},
			//			DestinationPort: "myPort",
			//			Datacenter:      "dc1",
			//			ListenAddr: &pbmesh.Upstream_IpPort{
			//				IpPort: &pbmesh.IPPortAddress{
			//					Port: uint32(1234),
			//                  Ip:   consulNodeAddress,
			//				},
			//			},
			//		},
			//	},
			//},
			consulNamespacesEnabled: false,
			consulPartitionsEnabled: false,
		},
		{
			name: "labeled annotated upstream with svc and peer",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "", true, true)
				pod1.Annotations[constants.AnnotationMeshDestinations] = "myPort.port.upstream1.svc.peer1.peer:1234"
				return pod1
			},
			expErr: "upstream currently does not support peers: myPort.port.upstream1.svc.peer1.peer:1234",
			// TODO: uncomment this and remove expErr when peers is supported
			//expected: &pbmesh.Upstreams{
			//	Workloads: &pbcatalog.WorkloadSelector{
			//		Names: []string{podName},
			//	},
			//	Upstreams: []*pbmesh.Upstream{
			//		{
			//			DestinationRef: &pbresource.Reference{
			//              Type: pbcatalog.ServiceType,
			//				Tenancy: &pbresource.Tenancy{
			//					Partition: getDefaultConsulPartition(""),
			//					Namespace: getDefaultConsulNamespace(""),
			//					PeerName:  "peer1",
			//				},
			//				Name: "upstream1",
			//			},
			//			DestinationPort: "myPort",
			//			Datacenter:      "",
			//			ListenAddr: &pbmesh.Upstream_IpPort{
			//				IpPort: &pbmesh.IPPortAddress{
			//					Port: uint32(1234),
			//                  Ip:   consulNodeAddress,
			//				},
			//			},
			//		},
			//	},
			//},
			consulNamespacesEnabled: false,
			consulPartitionsEnabled: false,
		},
		{
			name: "labeled annotated upstream with svc, ns, and peer",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "", true, true)
				pod1.Annotations[constants.AnnotationMeshDestinations] = "myPort.port.upstream1.svc.ns1.ns.peer1.peer:1234"
				return pod1
			},
			expErr: "upstream currently does not support peers: myPort.port.upstream1.svc.ns1.ns.peer1.peer:1234",
			// TODO: uncomment this and remove expErr when peers is supported
			//expected: &pbmesh.Upstreams{
			//	Workloads: &pbcatalog.WorkloadSelector{
			//		Names: []string{podName},
			//	},
			//	Upstreams: []*pbmesh.Upstream{
			//		{
			//			DestinationRef: &pbresource.Reference{
			// 			    Type: pbcatalog.ServiceType,
			//				Tenancy: &pbresource.Tenancy{
			//					Partition: getDefaultConsulPartition(""),
			//					Namespace: "ns1",
			//					PeerName:  "peer1",
			//				},
			//				Name: "upstream1",
			//			},
			//			DestinationPort: "myPort",
			//			Datacenter:      "",
			//			ListenAddr: &pbmesh.Upstream_IpPort{
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
			name: "labeled annotated upstream with svc, ns, and partition",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "", true, true)
				pod1.Annotations[constants.AnnotationMeshDestinations] = "myPort.port.upstream1.svc.ns1.ns.part1.ap:1234"
				return pod1
			},
			expected: &pbmesh.Upstreams{
				Workloads: &pbcatalog.WorkloadSelector{
					Names: []string{podName},
				},
				Upstreams: []*pbmesh.Upstream{
					{
						DestinationRef: &pbresource.Reference{
							Type: pbcatalog.ServiceType,
							Tenancy: &pbresource.Tenancy{
								Partition: "part1",
								Namespace: "ns1",
								PeerName:  getDefaultConsulPeer(""),
							},
							Name: "upstream1",
						},
						DestinationPort: "myPort",
						Datacenter:      "",
						ListenAddr: &pbmesh.Upstream_IpPort{
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
			name: "labeled annotated upstream with svc, ns, and dc",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "", true, true)
				pod1.Annotations[constants.AnnotationMeshDestinations] = "myPort.port.upstream1.svc.ns1.ns.dc1.dc:1234"
				return pod1
			},
			expErr: "upstream currently does not support datacenters: myPort.port.upstream1.svc.ns1.ns.dc1.dc:1234",
			// TODO: uncomment this and remove expErr when datacenters is supported
			//expected: &pbmesh.Upstreams{
			//	Workloads: &pbcatalog.WorkloadSelector{
			//		Names: []string{podName},
			//	},
			//	Upstreams: []*pbmesh.Upstream{
			//		{
			//			DestinationRef: &pbresource.Reference{
			//              Type: pbcatalog.ServiceType,
			//				Tenancy: &pbresource.Tenancy{
			//					Partition: getDefaultConsulPartition(""),
			//					Namespace: "ns1",
			//					PeerName: getDefaultConsulPeer(""),
			//				},
			//				Name: "upstream1",
			//			},
			//			DestinationPort: "myPort",
			//			Datacenter:      "dc1",
			//			ListenAddr: &pbmesh.Upstream_IpPort{
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
			name: "labeled multiple annotated upstreams",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "", true, true)
				pod1.Annotations[constants.AnnotationMeshDestinations] = "myPort.port.upstream1.svc.ns1.ns:1234, myPort2.port.upstream2.svc:2234, myPort4.port.upstream4.svc.ns1.ns.ap1.ap:4234"
				return pod1
			},
			expected: &pbmesh.Upstreams{
				Workloads: &pbcatalog.WorkloadSelector{
					Names: []string{podName},
				},
				Upstreams: []*pbmesh.Upstream{
					{
						DestinationRef: &pbresource.Reference{
							Type: pbcatalog.ServiceType,
							Tenancy: &pbresource.Tenancy{
								Partition: getDefaultConsulPartition(""),
								Namespace: "ns1",
								PeerName:  getDefaultConsulPeer(""),
							},
							Name: "upstream1",
						},
						DestinationPort: "myPort",
						Datacenter:      "",
						ListenAddr: &pbmesh.Upstream_IpPort{
							IpPort: &pbmesh.IPPortAddress{
								Port: uint32(1234),
								Ip:   consulNodeAddress,
							},
						},
					},
					{
						DestinationRef: &pbresource.Reference{
							Type: pbcatalog.ServiceType,
							Tenancy: &pbresource.Tenancy{
								Partition: getDefaultConsulPartition(""),
								Namespace: getDefaultConsulNamespace(""),
								PeerName:  getDefaultConsulPeer(""),
							},
							Name: "upstream2",
						},
						DestinationPort: "myPort2",
						Datacenter:      "",
						ListenAddr: &pbmesh.Upstream_IpPort{
							IpPort: &pbmesh.IPPortAddress{
								Port: uint32(2234),
								Ip:   consulNodeAddress,
							},
						},
					},
					{
						DestinationRef: &pbresource.Reference{
							Type: pbcatalog.ServiceType,
							Tenancy: &pbresource.Tenancy{
								Partition: "ap1",
								Namespace: "ns1",
								PeerName:  getDefaultConsulPeer(""),
							},
							Name: "upstream4",
						},
						DestinationPort: "myPort4",
						Datacenter:      "",
						ListenAddr: &pbmesh.Upstream_IpPort{
							IpPort: &pbmesh.IPPortAddress{
								Port: uint32(4234),
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
			name: "labeled multiple annotated upstreams with dcs and peers",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "", true, true)
				pod1.Annotations[constants.AnnotationMeshDestinations] = "myPort.port.upstream1.svc.ns1.ns.dc1.dc:1234, myPort2.port.upstream2.svc:2234, myPort3.port.upstream3.svc.ns1.ns:3234, myPort4.port.upstream4.svc.ns1.ns.peer1.peer:4234"
				return pod1
			},
			expErr: "upstream currently does not support datacenters: myPort.port.upstream1.svc.ns1.ns.dc1.dc:1234",
			// TODO: uncomment this and remove expErr when datacenters is supported
			//expected: &pbmesh.Upstreams{
			//	Workloads: &pbcatalog.WorkloadSelector{
			//		Names: []string{podName},
			//	},
			//	Upstreams: []*pbmesh.Upstream{
			//		{
			//			DestinationRef: &pbresource.Reference{
			//              Type: pbcatalog.ServiceType,
			//				Tenancy: &pbresource.Tenancy{
			//					Partition: getDefaultConsulPartition(""),
			//					Namespace: "ns1",
			//					PeerName: getDefaultConsulPeer(""),
			//				},
			//				Name: "upstream1",
			//			},
			//			DestinationPort: "myPort",
			//			Datacenter:      "dc1",
			//			ListenAddr: &pbmesh.Upstream_IpPort{
			//				IpPort: &pbmesh.IPPortAddress{
			//					Port: uint32(1234),
			//                  Ip:   consulNodeAddress,
			//				},
			//			},
			//		},
			//		{
			//			DestinationRef: &pbresource.Reference{
			//              Type: pbcatalog.ServiceType,
			//				Tenancy: &pbresource.Tenancy{
			//					Partition: getDefaultConsulPartition(""),
			//					Namespace: getDefaultConsulNamespace(""),
			//					PeerName: getDefaultConsulPeer(""),
			//				},
			//				Name: "upstream2",
			//			},
			//			DestinationPort: "myPort2",
			//			Datacenter:      "",
			//			ListenAddr: &pbmesh.Upstream_IpPort{
			//				IpPort: &pbmesh.IPPortAddress{
			//					Port: uint32(2234),
			//                  Ip:   consulNodeAddress,
			//				},
			//			},
			//		},
			//		{
			//			DestinationRef: &pbresource.Reference{
			//              Type: pbcatalog.ServiceType,
			//				Tenancy: &pbresource.Tenancy{
			//					Partition: getDefaultConsulPartition(""),
			//					Namespace: "ns1",
			//					PeerName: getDefaultConsulPeer(""),
			//				},
			//				Name: "upstream3",
			//			},
			//			DestinationPort: "myPort3",
			//			Datacenter:      "",
			//			ListenAddr: &pbmesh.Upstream_IpPort{
			//				IpPort: &pbmesh.IPPortAddress{
			//					Port: uint32(3234),
			//                  Ip:   consulNodeAddress,
			//				},
			//			},
			//		},
			//		{
			//			DestinationRef: &pbresource.Reference{
			//              Type: pbcatalog.ServiceType,
			//				Tenancy: &pbresource.Tenancy{
			//					Partition: getDefaultConsulPartition(""),
			//					Namespace: "ns1",
			//					PeerName:  "peer1",
			//				},
			//				Name: "upstream4",
			//			},
			//			DestinationPort: "myPort4",
			//			Datacenter:      "",
			//			ListenAddr: &pbmesh.Upstream_IpPort{
			//				IpPort: &pbmesh.IPPortAddress{
			//					Port: uint32(4234),
			//                  Ip:   consulNodeAddress,
			//				},
			//			},
			//		},
			//	},
			//},
			consulNamespacesEnabled: true,
			consulPartitionsEnabled: true,
		},
		{
			name: "error labeled annotated upstream error: invalid partition/dc/peer",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "", true, true)
				pod1.Annotations[constants.AnnotationMeshDestinations] = "myPort.port.upstream1.svc.ns1.ns.part1.err:1234"
				return pod1
			},
			expErr:                  "upstream structured incorrectly: myPort.port.upstream1.svc.ns1.ns.part1.err:1234",
			consulNamespacesEnabled: true,
			consulPartitionsEnabled: false,
		},
		{
			name: "error labeled annotated upstream with svc and peer, needs ns before peer if namespaces enabled",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "", true, true)
				pod1.Annotations[constants.AnnotationMeshDestinations] = "myPort.port.upstream1.svc.peer1.peer:1234"
				return pod1
			},
			expErr:                  "upstream structured incorrectly: myPort.port.upstream1.svc.peer1.peer:1234",
			consulNamespacesEnabled: true,
			consulPartitionsEnabled: false,
		},
		{
			name: "error labeled annotated upstream error: invalid namespace",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "", true, true)
				pod1.Annotations[constants.AnnotationMeshDestinations] = "myPort.port.upstream1.svc.ns1.err:1234"
				return pod1
			},
			expErr:                  "upstream structured incorrectly: myPort.port.upstream1.svc.ns1.err:1234",
			consulNamespacesEnabled: true,
			consulPartitionsEnabled: false,
		},
		{
			name: "error labeled annotated upstream error: invalid number of pieces in the address",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "", true, true)
				pod1.Annotations[constants.AnnotationMeshDestinations] = "myPort.port.upstream1.svc.err:1234"
				return pod1
			},
			expErr:                  "upstream structured incorrectly: myPort.port.upstream1.svc.err:1234",
			consulNamespacesEnabled: true,
			consulPartitionsEnabled: false,
		},
		{
			name: "error labeled annotated upstream error: invalid peer",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "", true, true)
				pod1.Annotations[constants.AnnotationMeshDestinations] = "myPort.port.upstream1.svc.peer1.err:1234"
				return pod1
			},
			expErr:                  "upstream structured incorrectly: myPort.port.upstream1.svc.peer1.err:1234",
			consulNamespacesEnabled: false,
			consulPartitionsEnabled: false,
		},
		{
			name: "error labeled annotated upstream error: invalid number of pieces in the address without namespaces and partitions",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "", true, true)
				pod1.Annotations[constants.AnnotationMeshDestinations] = "myPort.port.upstream1.svc.err:1234"
				return pod1
			},
			expErr:                  "upstream structured incorrectly: myPort.port.upstream1.svc.err:1234",
			consulNamespacesEnabled: false,
			consulPartitionsEnabled: false,
		},
		{
			name: "error labeled annotated upstream error: both peer and partition provided",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "", true, true)
				pod1.Annotations[constants.AnnotationMeshDestinations] = "myPort.port.upstream1.svc.ns1.ns.part1.partition.peer1.peer:1234"
				return pod1
			},
			expErr:                  "upstream structured incorrectly: myPort.port.upstream1.svc.ns1.ns.part1.partition.peer1.peer:1234",
			consulNamespacesEnabled: true,
			consulPartitionsEnabled: true,
		},
		{
			name: "error labeled annotated upstream error: both peer and dc provided",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "", true, true)
				pod1.Annotations[constants.AnnotationMeshDestinations] = "myPort.port.upstream1.svc.ns1.ns.peer1.peer.dc1.dc:1234"
				return pod1
			},
			expErr:                  "upstream structured incorrectly: myPort.port.upstream1.svc.ns1.ns.peer1.peer.dc1.dc:1234",
			consulNamespacesEnabled: true,
			consulPartitionsEnabled: false,
		},
		{
			name: "error labeled annotated upstream error: both dc and partition provided",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "", true, true)
				pod1.Annotations[constants.AnnotationMeshDestinations] = "myPort.port.upstream1.svc.ns1.ns.part1.partition.dc1.dc:1234"
				return pod1
			},
			expErr:                  "upstream structured incorrectly: myPort.port.upstream1.svc.ns1.ns.part1.partition.dc1.dc:1234",
			consulNamespacesEnabled: true,
			consulPartitionsEnabled: true,
		},
		{
			name: "error labeled annotated upstream error: wrong ordering for port and svc with namespace partition enabled",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "", true, true)
				pod1.Annotations[constants.AnnotationMeshDestinations] = "upstream1.svc.myPort.port.ns1.ns.part1.partition.dc1.dc:1234"
				return pod1
			},
			expErr:                  "upstream structured incorrectly: upstream1.svc.myPort.port.ns1.ns.part1.partition.dc1.dc:1234",
			consulNamespacesEnabled: true,
			consulPartitionsEnabled: true,
		},
		{
			name: "error labeled annotated upstream error: wrong ordering for port and svc with namespace partition disabled",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "", true, true)
				pod1.Annotations[constants.AnnotationMeshDestinations] = "upstream1.svc.myPort.port:1234"
				return pod1
			},
			expErr:                  "upstream structured incorrectly: upstream1.svc.myPort.port:1234",
			consulNamespacesEnabled: false,
			consulPartitionsEnabled: false,
		},
		{
			name: "error labeled annotated upstream error: incorrect key name namespace partition enabled",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "", true, true)
				pod1.Annotations[constants.AnnotationMeshDestinations] = "myPort.portage.upstream1.svc.ns1.ns.part1.partition.dc1.dc:1234"
				return pod1
			},
			expErr:                  "upstream structured incorrectly: myPort.portage.upstream1.svc.ns1.ns.part1.partition.dc1.dc:1234",
			consulNamespacesEnabled: true,
			consulPartitionsEnabled: true,
		},
		{
			name: "error labeled annotated upstream error: incorrect key name namespace partition disabled",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "", true, true)
				pod1.Annotations[constants.AnnotationMeshDestinations] = "myPort.portage.upstream1.svc:1234"
				return pod1
			},
			expErr:                  "upstream structured incorrectly: myPort.portage.upstream1.svc:1234",
			consulNamespacesEnabled: false,
			consulPartitionsEnabled: false,
		},
		{
			name: "unlabeled and labeled multiple annotated upstreams",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "", true, true)
				pod1.Annotations[constants.AnnotationMeshDestinations] = "myPort.port.upstream1.svc.ns1.ns:1234, myPort2.upstream2:2234, myPort4.port.upstream4.svc.ns1.ns.ap1.ap:4234"
				return pod1
			},
			expected: &pbmesh.Upstreams{
				Workloads: &pbcatalog.WorkloadSelector{
					Names: []string{podName},
				},
				Upstreams: []*pbmesh.Upstream{
					{
						DestinationRef: &pbresource.Reference{
							Type: pbcatalog.ServiceType,
							Tenancy: &pbresource.Tenancy{
								Partition: getDefaultConsulPartition(""),
								Namespace: "ns1",
								PeerName:  getDefaultConsulPeer(""),
							},
							Name: "upstream1",
						},
						DestinationPort: "myPort",
						Datacenter:      "",
						ListenAddr: &pbmesh.Upstream_IpPort{
							IpPort: &pbmesh.IPPortAddress{
								Port: uint32(1234),
								Ip:   consulNodeAddress,
							},
						},
					},
					{
						DestinationRef: &pbresource.Reference{
							Type: pbcatalog.ServiceType,
							Tenancy: &pbresource.Tenancy{
								Partition: getDefaultConsulPartition(""),
								Namespace: getDefaultConsulNamespace(""),
								PeerName:  getDefaultConsulPeer(""),
							},
							Name: "upstream2",
						},
						DestinationPort: "myPort2",
						Datacenter:      "",
						ListenAddr: &pbmesh.Upstream_IpPort{
							IpPort: &pbmesh.IPPortAddress{
								Port: uint32(2234),
								Ip:   consulNodeAddress,
							},
						},
					},
					{
						DestinationRef: &pbresource.Reference{
							Type: pbcatalog.ServiceType,
							Tenancy: &pbresource.Tenancy{
								Partition: "ap1",
								Namespace: "ns1",
								PeerName:  getDefaultConsulPeer(""),
							},
							Name: "upstream4",
						},
						DestinationPort: "myPort4",
						Datacenter:      "",
						ListenAddr: &pbmesh.Upstream_IpPort{
							IpPort: &pbmesh.IPPortAddress{
								Port: uint32(4234),
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
			name: "unlabeled single upstream",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "", true, true)
				pod1.Annotations[constants.AnnotationMeshDestinations] = "myPort.upstream:1234"
				return pod1
			},
			expected: &pbmesh.Upstreams{
				Workloads: &pbcatalog.WorkloadSelector{
					Names: []string{podName},
				},
				Upstreams: []*pbmesh.Upstream{
					{
						DestinationRef: &pbresource.Reference{
							Type: pbcatalog.ServiceType,
							Tenancy: &pbresource.Tenancy{
								Partition: getDefaultConsulPartition(""),
								Namespace: getDefaultConsulNamespace(""),
								PeerName:  getDefaultConsulPeer(""),
							},
							Name: "upstream",
						},
						DestinationPort: "myPort",
						Datacenter:      "",
						ListenAddr: &pbmesh.Upstream_IpPort{
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
			name: "unlabeled single upstream with namespace",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "", true, true)
				pod1.Annotations[constants.AnnotationMeshDestinations] = "myPort.upstream.foo:1234"
				return pod1
			},
			expected: &pbmesh.Upstreams{
				Workloads: &pbcatalog.WorkloadSelector{
					Names: []string{podName},
				},
				Upstreams: []*pbmesh.Upstream{
					{
						DestinationRef: &pbresource.Reference{
							Type: pbcatalog.ServiceType,
							Tenancy: &pbresource.Tenancy{
								Partition: getDefaultConsulPartition(""),
								Namespace: "foo",
								PeerName:  getDefaultConsulPeer(""),
							},
							Name: "upstream",
						},
						DestinationPort: "myPort",
						Datacenter:      "",
						ListenAddr: &pbmesh.Upstream_IpPort{
							IpPort: &pbmesh.IPPortAddress{
								Port: uint32(1234),
								Ip:   consulNodeAddress,
							},
						},
					},
				},
			},
			consulNamespacesEnabled: true,
			consulPartitionsEnabled: false,
		},
		{
			name: "unlabeled single upstream with namespace and partition",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "", true, true)
				pod1.Annotations[constants.AnnotationMeshDestinations] = "myPort.upstream.foo.bar:1234"
				return pod1
			},
			expected: &pbmesh.Upstreams{
				Workloads: &pbcatalog.WorkloadSelector{
					Names: []string{podName},
				},
				Upstreams: []*pbmesh.Upstream{
					{
						DestinationRef: &pbresource.Reference{
							Type: pbcatalog.ServiceType,
							Tenancy: &pbresource.Tenancy{
								Partition: "bar",
								Namespace: "foo",
								PeerName:  getDefaultConsulPeer(""),
							},
							Name: "upstream",
						},
						DestinationPort: "myPort",
						Datacenter:      "",
						ListenAddr: &pbmesh.Upstream_IpPort{
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
			name: "unlabeled multiple upstreams",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "", true, true)
				pod1.Annotations[constants.AnnotationMeshDestinations] = "myPort.upstream1:1234, myPort2.upstream2:2234"
				return pod1
			},
			expected: &pbmesh.Upstreams{
				Workloads: &pbcatalog.WorkloadSelector{
					Names: []string{podName},
				},
				Upstreams: []*pbmesh.Upstream{
					{
						DestinationRef: &pbresource.Reference{
							Type: pbcatalog.ServiceType,
							Tenancy: &pbresource.Tenancy{
								Partition: getDefaultConsulPartition(""),
								Namespace: getDefaultConsulNamespace(""),
								PeerName:  getDefaultConsulPeer(""),
							},
							Name: "upstream1",
						},
						DestinationPort: "myPort",
						Datacenter:      "",
						ListenAddr: &pbmesh.Upstream_IpPort{
							IpPort: &pbmesh.IPPortAddress{
								Port: uint32(1234),
								Ip:   consulNodeAddress,
							},
						},
					},
					{
						DestinationRef: &pbresource.Reference{
							Type: pbcatalog.ServiceType,
							Tenancy: &pbresource.Tenancy{
								Partition: getDefaultConsulPartition(""),
								Namespace: getDefaultConsulNamespace(""),
								PeerName:  getDefaultConsulPeer(""),
							},
							Name: "upstream2",
						},
						DestinationPort: "myPort2",
						Datacenter:      "",
						ListenAddr: &pbmesh.Upstream_IpPort{
							IpPort: &pbmesh.IPPortAddress{
								Port: uint32(2234),
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
			name: "unlabeled multiple upstreams with consul namespaces, partitions and datacenters",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "", true, true)
				pod1.Annotations[constants.AnnotationMeshDestinations] = "myPort.upstream1:1234, myPort2.upstream2.bar:2234, myPort3.upstream3.foo.baz:3234:dc2"
				return pod1
			},
			configEntry: func() api.ConfigEntry {
				ce, _ := api.MakeConfigEntry(api.ProxyDefaults, "global")
				pd := ce.(*api.ProxyConfigEntry)
				pd.MeshGateway.Mode = "remote"
				return pd
			},
			expErr: "upstream currently does not support datacenters:  myPort3.upstream3.foo.baz:3234:dc2",
			// TODO: uncomment this and remove expErr when datacenters is supported
			//expected: &pbmesh.Upstreams{
			//	Workloads: &pbcatalog.WorkloadSelector{
			//		Names: []string{podName},
			//	},
			//	Upstreams: []*pbmesh.Upstream{
			//		{
			//			DestinationRef: &pbresource.Reference{
			//              Type: pbcatalog.ServiceType,
			//				Tenancy: &pbresource.Tenancy{
			//					Partition: getDefaultConsulPartition(""),
			//					Namespace: getDefaultConsulNamespace(""),
			//					PeerName: getDefaultConsulPeer(""),
			//				},
			//				Name: "upstream1",
			//			},
			//			DestinationPort: "myPort",
			//			Datacenter:      "",
			//			ListenAddr: &pbmesh.Upstream_IpPort{
			//				IpPort: &pbmesh.IPPortAddress{
			//					Port: uint32(1234),
			//                  Ip:   consulNodeAddress,
			//				},
			//			},
			//		},
			//		{
			//			DestinationRef: &pbresource.Reference{
			//              Type: pbcatalog.ServiceType,
			//				Tenancy: &pbresource.Tenancy{
			//					Partition: getDefaultConsulPartition(""),
			//					Namespace: "bar",
			//					PeerName: getDefaultConsulPeer(""),
			//				},
			//				Name: "upstream2",
			//			},
			//			DestinationPort: "myPort2",
			//			Datacenter:      "",
			//			ListenAddr: &pbmesh.Upstream_IpPort{
			//				IpPort: &pbmesh.IPPortAddress{
			//					Port: uint32(2234),
			//                  Ip:   consulNodeAddress,
			//				},
			//			},
			//		},
			//		{
			//			DestinationRef: &pbresource.Reference{
			//              Type: pbcatalog.ServiceType,
			//				Tenancy: &pbresource.Tenancy{
			//					Partition: "baz",
			//					Namespace: "foo",
			//					PeerName: getDefaultConsulPeer(""),
			//				},
			//				Name: "upstream3",
			//			},
			//			DestinationPort: "myPort3",
			//			Datacenter:      "dc2",
			//			ListenAddr: &pbmesh.Upstream_IpPort{
			//				IpPort: &pbmesh.IPPortAddress{
			//					Port: uint32(3234),
			//                  Ip:   consulNodeAddress,
			//				},
			//			},
			//		},
			//	},
			//},
			consulNamespacesEnabled: true,
			consulPartitionsEnabled: true,
		},
		{
			name: "unlabeled multiple upstreams with consul namespaces and datacenters",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "", true, true)
				pod1.Annotations[constants.AnnotationMeshDestinations] = "myPort.upstream1:1234, myPort2.upstream2.bar:2234, myPort3.upstream3.foo:3234:dc2"
				return pod1
			},
			configEntry: func() api.ConfigEntry {
				ce, _ := api.MakeConfigEntry(api.ProxyDefaults, "global")
				pd := ce.(*api.ProxyConfigEntry)
				pd.MeshGateway.Mode = "remote"
				return pd
			},
			expErr: "upstream currently does not support datacenters:  myPort3.upstream3.foo:3234:dc2",
			// TODO: uncomment this and remove expErr when datacenters is supported
			//expected: &pbmesh.Upstreams{
			//	Workloads: &pbcatalog.WorkloadSelector{
			//		Names: []string{podName},
			//	},
			//	Upstreams: []*pbmesh.Upstream{
			//		{
			//			DestinationRef: &pbresource.Reference{
			//              Type: pbcatalog.ServiceType,
			//				Tenancy: &pbresource.Tenancy{
			//					Partition: getDefaultConsulPartition(""),
			//					Namespace: getDefaultConsulNamespace(""),
			//					PeerName: getDefaultConsulPeer(""),
			//				},
			//				Name: "upstream1",
			//			},
			//			DestinationPort: "myPort",
			//			Datacenter:      "",
			//			ListenAddr: &pbmesh.Upstream_IpPort{
			//				IpPort: &pbmesh.IPPortAddress{
			//					Port: uint32(1234),
			//                  Ip:   consulNodeAddress,
			//				},
			//			},
			//		},
			//		{
			//			DestinationRef: &pbresource.Reference{
			//              Type: pbcatalog.ServiceType,
			//				Tenancy: &pbresource.Tenancy{
			//					Partition: getDefaultConsulPartition(""),
			//					Namespace: "bar",
			//					PeerName: getDefaultConsulPeer(""),
			//				},
			//				Name: "upstream2",
			//			},
			//			DestinationPort: "myPort2",
			//			Datacenter:      "",
			//			ListenAddr: &pbmesh.Upstream_IpPort{
			//				IpPort: &pbmesh.IPPortAddress{
			//					Port: uint32(2234),
			//                  Ip:   consulNodeAddress,
			//				},
			//			},
			//		},
			//		{
			//			DestinationRef: &pbresource.Reference{
			//              Type: pbcatalog.ServiceType,
			//				Tenancy: &pbresource.Tenancy{
			//					Partition: getDefaultConsulPartition(""),
			//					Namespace: "foo",
			//					PeerName: getDefaultConsulPeer(""),
			//				},
			//				Name: "upstream3",
			//			},
			//			DestinationPort: "myPort3",
			//			Datacenter:      "dc2",
			//			ListenAddr: &pbmesh.Upstream_IpPort{
			//				IpPort: &pbmesh.IPPortAddress{
			//					Port: uint32(3234),
			//                  Ip:   consulNodeAddress,
			//				},
			//			},
			//		},
			//	},
			//},
			consulNamespacesEnabled: true,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
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
			}

			upstreams, err := pc.processUpstreams(*tt.pod())
			if tt.expErr != "" {
				require.EqualError(t, err, tt.expErr)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expected, upstreams)

				if diff := cmp.Diff(tt.expected, upstreams, protocmp.Transform()); diff != "" {
					t.Errorf("unexpected difference:\n%v", diff)
				}
			}
		})
	}
}

func TestUpstreamsDelete(t *testing.T) {
	t.Parallel()

	const podName = "pod1"

	cases := []struct {
		name              string
		pod               func() *corev1.Pod
		existingUpstreams *pbmesh.Upstreams
		expErr            string
		configEntry       func() api.ConfigEntry
		consulUnavailable bool
	}{
		{
			name: "labeled annotated upstream with svc only",
			pod: func() *corev1.Pod {
				pod1 := createPod(podName, "", true, true)
				pod1.Annotations[constants.AnnotationMeshDestinations] = "myPort.port.upstream1.svc:1234"
				return pod1
			},
			existingUpstreams: &pbmesh.Upstreams{
				Workloads: &pbcatalog.WorkloadSelector{
					Names: []string{podName},
				},
				Upstreams: []*pbmesh.Upstream{
					{
						DestinationRef: &pbresource.Reference{
							Type: pbcatalog.ServiceType,
							Tenancy: &pbresource.Tenancy{
								Partition: getDefaultConsulPartition(""),
								Namespace: getDefaultConsulNamespace(""),
								PeerName:  getDefaultConsulPeer(""),
							},
							Name: "upstream1",
						},
						DestinationPort: "myPort",
						Datacenter:      "",
						ListenAddr: &pbmesh.Upstream_IpPort{
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
			resourceClient, err := consul.NewResourceServiceClient(testClient.Watcher)
			require.NoError(t, err)

			pc := &Controller{
				Log: logrtest.New(t),
				K8sNamespaceConfig: common.K8sNamespaceConfig{
					AllowK8sNamespacesSet: mapset.NewSetWith("*"),
					DenyK8sNamespacesSet:  mapset.NewSetWith(),
				},
				ResourceClient: resourceClient,
			}

			// Load in the upstream for us to delete and check that it's there
			loadResource(t,
				resourceClient,
				getUpstreamsID(tt.pod().Name, constants.DefaultConsulNS, constants.DefaultConsulPartition),
				tt.existingUpstreams,
				nil)
			uID := getUpstreamsID(tt.pod().Name, metav1.NamespaceDefault, constants.DefaultConsulPartition)
			expectedUpstreamMatches(t, resourceClient, uID, tt.existingUpstreams)

			// Delete the upstream
			nn := types.NamespacedName{Name: tt.pod().Name}
			err = pc.deleteUpstreams(context.Background(), nn)

			// Verify the upstream has been deleted or that an expected error has been returned
			if tt.expErr != "" {
				require.EqualError(t, err, tt.expErr)
			} else {
				require.NoError(t, err)
				uID := getUpstreamsID(tt.pod().Name, metav1.NamespaceDefault, constants.DefaultConsulPartition)
				expectedUpstreamMatches(t, resourceClient, uID, nil)
			}
		})
	}
}

// TODO
// func TestDeleteACLTokens(t *testing.T)

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
		expectedUpstreams          *pbmesh.Upstreams

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
		resourceClient, err := consul.NewResourceServiceClient(testClient.Watcher)
		require.NoError(t, err)

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
		require.False(t, resp.Requeue)

		wID := getWorkloadID(tc.podName, metav1.NamespaceDefault, constants.DefaultConsulPartition)
		expectedWorkloadMatches(t, resourceClient, wID, tc.expectedWorkload)

		hsID := getHealthStatusID(tc.podName, metav1.NamespaceDefault, constants.DefaultConsulPartition)
		expectedHealthStatusMatches(t, resourceClient, hsID, tc.expectedHealthStatus)

		pcID := getProxyConfigurationID(tc.podName, metav1.NamespaceDefault, constants.DefaultConsulPartition)
		expectedProxyConfigurationMatches(t, resourceClient, pcID, tc.expectedProxyConfiguration)

		uID := getUpstreamsID(tc.podName, metav1.NamespaceDefault, constants.DefaultConsulPartition)
		expectedUpstreamMatches(t, resourceClient, uID, tc.expectedUpstreams)
	}

	testCases := []testCase{
		{
			name:    "vanilla new pod",
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
			expectedProxyConfiguration: createProxyConfiguration("foo", pbmesh.ProxyMode_PROXY_MODE_TRANSPARENT),
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
				pod.Annotations[constants.AnnotationMeshDestinations] = "myPort.port.mySVC.svc:24601"
				return []runtime.Object{pod}
			},
			tproxy:                     false,
			telemetry:                  true,
			metrics:                    true,
			overwriteProbes:            true,
			expectedWorkload:           createWorkload(),
			expectedHealthStatus:       createPassingHealthStatus(),
			expectedProxyConfiguration: createProxyConfiguration("foo", pbmesh.ProxyMode_PROXY_MODE_DEFAULT),
			expectedUpstreams:          createUpstreams(),
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
		existingUpstreams          *pbmesh.Upstreams

		expectedWorkload           *pbcatalog.Workload
		expectedHealthStatus       *pbcatalog.HealthStatus
		expectedProxyConfiguration *pbmesh.ProxyConfiguration
		expectedUpstreams          *pbmesh.Upstreams

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
		resourceClient, err := consul.NewResourceServiceClient(testClient.Watcher)
		require.NoError(t, err)

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
		loadResource(t, resourceClient, workloadID, tc.existingWorkload, nil)
		loadResource(
			t,
			resourceClient,
			getHealthStatusID(tc.podName, constants.DefaultConsulNS, constants.DefaultConsulPartition),
			tc.existingHealthStatus,
			workloadID)
		loadResource(t,
			resourceClient,
			getProxyConfigurationID(tc.podName, constants.DefaultConsulNS, constants.DefaultConsulPartition),
			tc.existingProxyConfiguration,
			nil)
		loadResource(t,
			resourceClient,
			getUpstreamsID(tc.podName, constants.DefaultConsulNS, constants.DefaultConsulPartition),
			tc.existingUpstreams,
			nil)

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
		expectedWorkloadMatches(t, resourceClient, wID, tc.expectedWorkload)

		hsID := getHealthStatusID(tc.podName, metav1.NamespaceDefault, constants.DefaultConsulPartition)
		expectedHealthStatusMatches(t, resourceClient, hsID, tc.expectedHealthStatus)

		pcID := getProxyConfigurationID(tc.podName, metav1.NamespaceDefault, constants.DefaultConsulPartition)
		expectedProxyConfigurationMatches(t, resourceClient, pcID, tc.expectedProxyConfiguration)

		uID := getUpstreamsID(tc.podName, metav1.NamespaceDefault, constants.DefaultConsulPartition)
		expectedUpstreamMatches(t, resourceClient, uID, tc.expectedUpstreams)
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
				},
				BootstrapConfig: &pbmesh.BootstrapConfig{
					PrometheusBindAddr: "0.0.0.0:21234",
				},
			},
		},
		{
			name:    "pod update explicit upstreams",
			podName: "foo",
			k8sObjects: func() []runtime.Object {
				pod := createPod("foo", "", true, true)
				pod.Annotations[constants.AnnotationMeshDestinations] = "myPort.port.mySVC.svc:24601"
				return []runtime.Object{pod}
			},
			existingWorkload:     createWorkload(),
			existingHealthStatus: createPassingHealthStatus(),
			existingUpstreams: &pbmesh.Upstreams{
				Workloads: &pbcatalog.WorkloadSelector{
					Names: []string{"foo"},
				},
				Upstreams: []*pbmesh.Upstream{
					{
						DestinationRef: &pbresource.Reference{
							Type: pbcatalog.ServiceType,
							Tenancy: &pbresource.Tenancy{
								Partition: "ap1",
								Namespace: "ns1",
								PeerName:  getDefaultConsulPeer(""),
							},
							Name: "mySVC3",
						},
						DestinationPort: "myPort2",
						Datacenter:      "",
						ListenAddr: &pbmesh.Upstream_IpPort{
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
			expectedUpstreams:    createUpstreams(),
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
		existingUpstreams          *pbmesh.Upstreams

		expectedWorkload           *pbcatalog.Workload
		expectedHealthStatus       *pbcatalog.HealthStatus
		expectedProxyConfiguration *pbmesh.ProxyConfiguration
		expectedUpstreams          *pbmesh.Upstreams

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
		testClient := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
			c.Experiments = []string{"resource-apis"}
		})
		resourceClient, err := consul.NewResourceServiceClient(testClient.Watcher)
		require.NoError(t, err)

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
		loadResource(t, resourceClient, workloadID, tc.existingWorkload, nil)
		loadResource(
			t,
			resourceClient,
			getHealthStatusID(tc.podName, constants.DefaultConsulNS, constants.DefaultConsulPartition),
			tc.existingHealthStatus,
			workloadID)
		loadResource(
			t,
			resourceClient,
			getProxyConfigurationID(tc.podName, constants.DefaultConsulNS, constants.DefaultConsulPartition),
			tc.existingProxyConfiguration,
			nil)
		loadResource(
			t,
			resourceClient,
			getUpstreamsID(tc.podName, constants.DefaultConsulNS, constants.DefaultConsulPartition),
			tc.existingUpstreams,
			nil)

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
		expectedWorkloadMatches(t, resourceClient, wID, tc.expectedWorkload)

		hsID := getHealthStatusID(tc.podName, metav1.NamespaceDefault, constants.DefaultConsulPartition)
		expectedHealthStatusMatches(t, resourceClient, hsID, tc.expectedHealthStatus)

		pcID := getProxyConfigurationID(tc.podName, metav1.NamespaceDefault, constants.DefaultConsulPartition)
		expectedProxyConfigurationMatches(t, resourceClient, pcID, tc.expectedProxyConfiguration)

		uID := getUpstreamsID(tc.podName, metav1.NamespaceDefault, constants.DefaultConsulPartition)
		expectedUpstreamMatches(t, resourceClient, uID, tc.expectedUpstreams)
	}

	testCases := []testCase{
		{
			name:                       "vanilla delete pod",
			podName:                    "foo",
			existingWorkload:           createWorkload(),
			existingHealthStatus:       createPassingHealthStatus(),
			existingProxyConfiguration: createProxyConfiguration("foo", pbmesh.ProxyMode_PROXY_MODE_TRANSPARENT),
		},
		{
			name:                       "annotated delete pod",
			podName:                    "foo",
			existingWorkload:           createWorkload(),
			existingHealthStatus:       createPassingHealthStatus(),
			existingProxyConfiguration: createProxyConfiguration("foo", pbmesh.ProxyMode_PROXY_MODE_DEFAULT),
			existingUpstreams:          createUpstreams(),
		},
		// TODO: enable ACLs and make sure they are deleted
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
func createProxyConfiguration(podName string, mode pbmesh.ProxyMode) *pbmesh.ProxyConfiguration {
	return &pbmesh.ProxyConfiguration{
		Workloads: &pbcatalog.WorkloadSelector{
			Names: []string{podName},
		},
		DynamicConfig: &pbmesh.DynamicConfig{
			Mode: mode,
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
		},
		BootstrapConfig: &pbmesh.BootstrapConfig{
			PrometheusBindAddr:              "0.0.0.0:1234",
			TelemetryCollectorBindSocketDir: DefaultTelemetryBindSocketDir,
		},
	}
}

// createCriticalHealthStatus creates a failing HealthStatus that matches the pod from createPod.
func createUpstreams() *pbmesh.Upstreams {
	return &pbmesh.Upstreams{
		Workloads: &pbcatalog.WorkloadSelector{
			Names: []string{"foo"},
		},
		Upstreams: []*pbmesh.Upstream{
			{
				DestinationRef: &pbresource.Reference{
					Type: pbcatalog.ServiceType,
					Tenancy: &pbresource.Tenancy{
						Partition: getDefaultConsulPartition(""),
						Namespace: getDefaultConsulNamespace(""),
						PeerName:  getDefaultConsulPeer(""),
					},
					Name: "mySVC",
				},
				DestinationPort: "myPort",
				Datacenter:      "",
				ListenAddr: &pbmesh.Upstream_IpPort{
					IpPort: &pbmesh.IPPortAddress{
						Port: uint32(24601),
						Ip:   consulNodeAddress,
					},
				},
			},
		},
	}
}

func expectedWorkloadMatches(t *testing.T, client pbresource.ResourceServiceClient, id *pbresource.ID, expectedWorkload *pbcatalog.Workload) {
	req := &pbresource.ReadRequest{Id: id}

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

	requireEqualResourceID(t, id, res.GetResource().GetId())

	require.NotNil(t, res.GetResource().GetData())

	actualWorkload := &pbcatalog.Workload{}
	err = res.GetResource().GetData().UnmarshalTo(actualWorkload)
	require.NoError(t, err)

	diff := cmp.Diff(expectedWorkload, actualWorkload, test.CmpProtoIgnoreOrder()...)
	require.Equal(t, "", diff, "Workloads do not match")
}

func expectedHealthStatusMatches(t *testing.T, client pbresource.ResourceServiceClient, id *pbresource.ID, expectedHealthStatus *pbcatalog.HealthStatus) {
	req := &pbresource.ReadRequest{Id: id}

	res, err := client.Read(context.Background(), req)

	if expectedHealthStatus == nil {
		// Because HealthStatus is asynchronously garbage-collected, we can retry to make sure it gets cleaned up.
		require.Eventually(t, func() bool {
			_, err := client.Read(context.Background(), req)
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

func expectedProxyConfigurationMatches(t *testing.T, client pbresource.ResourceServiceClient, id *pbresource.ID, expectedProxyConfiguration *pbmesh.ProxyConfiguration) {
	req := &pbresource.ReadRequest{Id: id}

	res, err := client.Read(context.Background(), req)

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

func expectedUpstreamMatches(t *testing.T, client pbresource.ResourceServiceClient, id *pbresource.ID, expectedUpstreams *pbmesh.Upstreams) {
	req := &pbresource.ReadRequest{Id: id}
	res, err := client.Read(context.Background(), req)

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

	actualUpstreams := &pbmesh.Upstreams{}
	err = res.GetResource().GetData().UnmarshalTo(actualUpstreams)
	require.NoError(t, err)

	require.True(t, proto.Equal(actualUpstreams, expectedUpstreams))
}

func loadResource(t *testing.T, client pbresource.ResourceServiceClient, id *pbresource.ID, proto proto.Message, owner *pbresource.ID) {
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
	_, err = client.Write(context.Background(), req)
	require.NoError(t, err)
	test.ResourceHasPersisted(t, client, id)
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
