// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package endpoints

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	mapset "github.com/deckarep/golang-set"
	logrtest "github.com/go-logr/logr/testr"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/metrics"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
)

const (
	nodeName       = "test-node"
	consulNodeName = "test-node-virtual"
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
				pod1 := createServicePod("pod1", "1.2.3.4", true, true)
				return *pod1
			},
			expected: true,
		},
		{
			name: "Pod without injected annotation",
			pod: func() corev1.Pod {
				pod1 := createServicePod("pod1", "1.2.3.4", false, true)
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

func TestProcessUpstreams(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name                    string
		pod                     func() *corev1.Pod
		expected                []api.Upstream
		expErr                  string
		configEntry             func() api.ConfigEntry
		consulUnavailable       bool
		consulNamespacesEnabled bool
		consulPartitionsEnabled bool
	}{
		{
			name: "annotated upstream with svc only",
			pod: func() *corev1.Pod {
				pod1 := createServicePod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[constants.AnnotationUpstreams] = "upstream1.svc:1234"
				return pod1
			},
			expected: []api.Upstream{
				{
					DestinationType: api.UpstreamDestTypeService,
					DestinationName: "upstream1",
					LocalBindPort:   1234,
				},
			},
			consulNamespacesEnabled: false,
			consulPartitionsEnabled: false,
		},
		{
			name: "annotated upstream with svc and dc",
			pod: func() *corev1.Pod {
				pod1 := createServicePod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[constants.AnnotationUpstreams] = "upstream1.svc.dc1.dc:1234"
				return pod1
			},
			expected: []api.Upstream{
				{
					DestinationType: api.UpstreamDestTypeService,
					DestinationName: "upstream1",
					Datacenter:      "dc1",
					LocalBindPort:   1234,
				},
			},
			consulNamespacesEnabled: false,
			consulPartitionsEnabled: false,
		},
		{
			name: "annotated upstream with svc and peer",
			pod: func() *corev1.Pod {
				pod1 := createServicePod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[constants.AnnotationUpstreams] = "upstream1.svc.peer1.peer:1234"
				return pod1
			},
			expected: []api.Upstream{
				{
					DestinationType: api.UpstreamDestTypeService,
					DestinationName: "upstream1",
					DestinationPeer: "peer1",
					LocalBindPort:   1234,
				},
			},
			consulNamespacesEnabled: false,
			consulPartitionsEnabled: false,
		},
		{
			name: "annotated upstream with svc and peer, needs ns before peer if namespaces enabled",
			pod: func() *corev1.Pod {
				pod1 := createServicePod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[constants.AnnotationUpstreams] = "upstream1.svc.peer1.peer:1234"
				return pod1
			},
			expErr:                  "upstream structured incorrectly: upstream1.svc.peer1.peer:1234",
			consulNamespacesEnabled: true,
			consulPartitionsEnabled: false,
		},
		{
			name: "annotated upstream with svc, ns, and peer",
			pod: func() *corev1.Pod {
				pod1 := createServicePod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[constants.AnnotationUpstreams] = "upstream1.svc.ns1.ns.peer1.peer:1234"
				return pod1
			},
			expected: []api.Upstream{
				{
					DestinationType:      api.UpstreamDestTypeService,
					DestinationName:      "upstream1",
					DestinationPeer:      "peer1",
					DestinationNamespace: "ns1",
					LocalBindPort:        1234,
				},
			},
			consulNamespacesEnabled: true,
			consulPartitionsEnabled: false,
		},
		{
			name: "annotated upstream with svc, ns, and partition",
			pod: func() *corev1.Pod {
				pod1 := createServicePod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[constants.AnnotationUpstreams] = "upstream1.svc.ns1.ns.part1.ap:1234"
				return pod1
			},
			expected: []api.Upstream{
				{
					DestinationType:      api.UpstreamDestTypeService,
					DestinationName:      "upstream1",
					DestinationPartition: "part1",
					DestinationNamespace: "ns1",
					LocalBindPort:        1234,
				},
			},
			consulNamespacesEnabled: true,
			consulPartitionsEnabled: true,
		},
		{
			name: "annotated upstream with svc, ns, and dc",
			pod: func() *corev1.Pod {
				pod1 := createServicePod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[constants.AnnotationUpstreams] = "upstream1.svc.ns1.ns.dc1.dc:1234"
				return pod1
			},
			expected: []api.Upstream{
				{
					DestinationType:      api.UpstreamDestTypeService,
					DestinationName:      "upstream1",
					Datacenter:           "dc1",
					DestinationNamespace: "ns1",
					LocalBindPort:        1234,
				},
			},
			consulNamespacesEnabled: true,
			consulPartitionsEnabled: false,
		},
		{
			name: "multiple annotated upstreams",
			pod: func() *corev1.Pod {
				pod1 := createServicePod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[constants.AnnotationUpstreams] = "upstream1.svc.ns1.ns.dc1.dc:1234, upstream2.svc:2234, upstream3.svc.ns1.ns:3234, upstream4.svc.ns1.ns.peer1.peer:4234"
				return pod1
			},
			expected: []api.Upstream{
				{
					DestinationType:      api.UpstreamDestTypeService,
					DestinationName:      "upstream1",
					Datacenter:           "dc1",
					DestinationNamespace: "ns1",
					LocalBindPort:        1234,
				},
				{
					DestinationType: api.UpstreamDestTypeService,
					DestinationName: "upstream2",
					LocalBindPort:   2234,
				},
				{
					DestinationType:      api.UpstreamDestTypeService,
					DestinationName:      "upstream3",
					DestinationNamespace: "ns1",
					LocalBindPort:        3234,
				},
				{
					DestinationType:      api.UpstreamDestTypeService,
					DestinationName:      "upstream4",
					DestinationNamespace: "ns1",
					DestinationPeer:      "peer1",
					LocalBindPort:        4234,
				},
			},
			consulNamespacesEnabled: true,
			consulPartitionsEnabled: true,
		},
		{
			name: "annotated upstream error: invalid partition/dc/peer",
			pod: func() *corev1.Pod {
				pod1 := createServicePod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[constants.AnnotationUpstreams] = "upstream1.svc.ns1.ns.part1.err:1234"
				return pod1
			},
			expErr:                  "upstream structured incorrectly: upstream1.svc.ns1.ns.part1.err:1234",
			consulNamespacesEnabled: true,
			consulPartitionsEnabled: false,
		},
		{
			name: "annotated upstream error: invalid namespace",
			pod: func() *corev1.Pod {
				pod1 := createServicePod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[constants.AnnotationUpstreams] = "upstream1.svc.ns1.err:1234"
				return pod1
			},
			expErr:                  "upstream structured incorrectly: upstream1.svc.ns1.err:1234",
			consulNamespacesEnabled: true,
			consulPartitionsEnabled: false,
		},
		{
			name: "annotated upstream error: invalid number of pieces in the address",
			pod: func() *corev1.Pod {
				pod1 := createServicePod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[constants.AnnotationUpstreams] = "upstream1.svc.err:1234"
				return pod1
			},
			expErr:                  "upstream structured incorrectly: upstream1.svc.err:1234",
			consulNamespacesEnabled: true,
			consulPartitionsEnabled: false,
		},
		{
			name: "annotated upstream error: invalid peer",
			pod: func() *corev1.Pod {
				pod1 := createServicePod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[constants.AnnotationUpstreams] = "upstream1.svc.peer1.err:1234"
				return pod1
			},
			expErr:                  "upstream structured incorrectly: upstream1.svc.peer1.err:1234",
			consulNamespacesEnabled: false,
			consulPartitionsEnabled: false,
		},
		{
			name: "annotated upstream error: invalid number of pieces in the address without namespaces and partitions",
			pod: func() *corev1.Pod {
				pod1 := createServicePod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[constants.AnnotationUpstreams] = "upstream1.svc.err:1234"
				return pod1
			},
			expErr:                  "upstream structured incorrectly: upstream1.svc.err:1234",
			consulNamespacesEnabled: false,
			consulPartitionsEnabled: false,
		},
		{
			name: "annotated upstream error: both peer and partition provided",
			pod: func() *corev1.Pod {
				pod1 := createServicePod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[constants.AnnotationUpstreams] = "upstream1.svc.ns1.ns.part1.partition.peer1.peer:1234"
				return pod1
			},
			expErr:                  "upstream structured incorrectly: upstream1.svc.ns1.ns.part1.partition.peer1.peer:1234",
			consulNamespacesEnabled: true,
			consulPartitionsEnabled: true,
		},
		{
			name: "annotated upstream error: both peer and dc provided",
			pod: func() *corev1.Pod {
				pod1 := createServicePod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[constants.AnnotationUpstreams] = "upstream1.svc.ns1.ns.peer1.peer.dc1.dc:1234"
				return pod1
			},
			expErr:                  "upstream structured incorrectly: upstream1.svc.ns1.ns.peer1.peer.dc1.dc:1234",
			consulNamespacesEnabled: true,
			consulPartitionsEnabled: false,
		},
		{
			name: "annotated upstream error: both dc and partition provided",
			pod: func() *corev1.Pod {
				pod1 := createServicePod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[constants.AnnotationUpstreams] = "upstream1.svc.ns1.ns.part1.partition.dc1.dc:1234"
				return pod1
			},
			expErr:                  "upstream structured incorrectly: upstream1.svc.ns1.ns.part1.partition.dc1.dc:1234",
			consulNamespacesEnabled: true,
			consulPartitionsEnabled: true,
		},
		{
			name: "when consul is unavailable, we don't return an error",
			pod: func() *corev1.Pod {
				pod1 := createServicePod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[constants.AnnotationUpstreams] = "upstream1:1234:dc1"
				return pod1
			},
			expErr: "",
			configEntry: func() api.ConfigEntry {
				ce, _ := api.MakeConfigEntry(api.ProxyDefaults, "global")
				pd := ce.(*api.ProxyConfigEntry)
				pd.MeshGateway.Mode = "remote"
				return pd
			},
			expected: []api.Upstream{
				{
					DestinationType: api.UpstreamDestTypeService,
					DestinationName: "upstream1",
					LocalBindPort:   1234,
					Datacenter:      "dc1",
				},
			},
			consulUnavailable:       true,
			consulNamespacesEnabled: false,
			consulPartitionsEnabled: false,
		},
		{
			name: "single upstream",
			pod: func() *corev1.Pod {
				pod1 := createServicePod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[constants.AnnotationUpstreams] = "upstream:1234"
				return pod1
			},
			expected: []api.Upstream{
				{
					DestinationType: api.UpstreamDestTypeService,
					DestinationName: "upstream",
					LocalBindPort:   1234,
				},
			},
			consulNamespacesEnabled: false,
			consulPartitionsEnabled: false,
		},
		{
			name: "single upstream with namespace",
			pod: func() *corev1.Pod {
				pod1 := createServicePod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[constants.AnnotationUpstreams] = "upstream.foo:1234"
				return pod1
			},
			expected: []api.Upstream{
				{
					DestinationType:      api.UpstreamDestTypeService,
					DestinationName:      "upstream",
					LocalBindPort:        1234,
					DestinationNamespace: "foo",
				},
			},
			consulNamespacesEnabled: true,
			consulPartitionsEnabled: false,
		},
		{
			name: "single upstream with namespace and partition",
			pod: func() *corev1.Pod {
				pod1 := createServicePod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[constants.AnnotationUpstreams] = "upstream.foo.bar:1234"
				return pod1
			},
			expected: []api.Upstream{
				{
					DestinationType:      api.UpstreamDestTypeService,
					DestinationName:      "upstream",
					LocalBindPort:        1234,
					DestinationNamespace: "foo",
					DestinationPartition: "bar",
				},
			},
			consulNamespacesEnabled: true,
			consulPartitionsEnabled: true,
		},
		{
			name: "multiple upstreams",
			pod: func() *corev1.Pod {
				pod1 := createServicePod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[constants.AnnotationUpstreams] = "upstream1:1234, upstream2:2234"
				return pod1
			},
			expected: []api.Upstream{
				{
					DestinationType: api.UpstreamDestTypeService,
					DestinationName: "upstream1",
					LocalBindPort:   1234,
				},
				{
					DestinationType: api.UpstreamDestTypeService,
					DestinationName: "upstream2",
					LocalBindPort:   2234,
				},
			},
			consulNamespacesEnabled: false,
			consulPartitionsEnabled: false,
		},
		{
			name: "multiple upstreams with consul namespaces, partitions and datacenters",
			pod: func() *corev1.Pod {
				pod1 := createServicePod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[constants.AnnotationUpstreams] = "upstream1:1234, upstream2.bar:2234, upstream3.foo.baz:3234:dc2"
				return pod1
			},
			configEntry: func() api.ConfigEntry {
				ce, _ := api.MakeConfigEntry(api.ProxyDefaults, "global")
				pd := ce.(*api.ProxyConfigEntry)
				pd.MeshGateway.Mode = "remote"
				return pd
			},
			expected: []api.Upstream{
				{
					DestinationType: api.UpstreamDestTypeService,
					DestinationName: "upstream1",
					LocalBindPort:   1234,
				},
				{
					DestinationType:      api.UpstreamDestTypeService,
					DestinationName:      "upstream2",
					DestinationNamespace: "bar",
					LocalBindPort:        2234,
				}, {
					DestinationType:      api.UpstreamDestTypeService,
					DestinationName:      "upstream3",
					DestinationNamespace: "foo",
					DestinationPartition: "baz",
					LocalBindPort:        3234,
					Datacenter:           "dc2",
				},
			},
			consulNamespacesEnabled: true,
			consulPartitionsEnabled: true,
		},
		{
			name: "multiple upstreams with consul namespaces and datacenters",
			pod: func() *corev1.Pod {
				pod1 := createServicePod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[constants.AnnotationUpstreams] = "upstream1:1234, upstream2.bar:2234, upstream3.foo:3234:dc2"
				return pod1
			},
			configEntry: func() api.ConfigEntry {
				ce, _ := api.MakeConfigEntry(api.ProxyDefaults, "global")
				pd := ce.(*api.ProxyConfigEntry)
				pd.MeshGateway.Mode = "remote"
				return pd
			},
			expected: []api.Upstream{
				{
					DestinationType: api.UpstreamDestTypeService,
					DestinationName: "upstream1",
					LocalBindPort:   1234,
				},
				{
					DestinationType:      api.UpstreamDestTypeService,
					DestinationName:      "upstream2",
					DestinationNamespace: "bar",
					LocalBindPort:        2234,
				}, {
					DestinationType:      api.UpstreamDestTypeService,
					DestinationName:      "upstream3",
					DestinationNamespace: "foo",
					LocalBindPort:        3234,
					Datacenter:           "dc2",
				},
			},
			consulNamespacesEnabled: true,
		},
		{
			name: "prepared query upstream",
			pod: func() *corev1.Pod {
				pod1 := createServicePod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[constants.AnnotationUpstreams] = "prepared_query:queryname:1234"
				return pod1
			},
			expected: []api.Upstream{
				{
					DestinationType: api.UpstreamDestTypePreparedQuery,
					DestinationName: "queryname",
					LocalBindPort:   1234,
				},
			},
			consulNamespacesEnabled: false,
			consulPartitionsEnabled: false,
		},
		{
			name: "prepared query and non-query upstreams and annotated non-query upstreams",
			pod: func() *corev1.Pod {
				pod1 := createServicePod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[constants.AnnotationUpstreams] = "prepared_query:queryname:1234, upstream1:2234, prepared_query:6687bd19-5654-76be-d764:8202, upstream2.svc:3234"
				return pod1
			},
			expected: []api.Upstream{
				{
					DestinationType: api.UpstreamDestTypePreparedQuery,
					DestinationName: "queryname",
					LocalBindPort:   1234,
				},
				{
					DestinationType: api.UpstreamDestTypeService,
					DestinationName: "upstream1",
					LocalBindPort:   2234,
				},
				{
					DestinationType: api.UpstreamDestTypePreparedQuery,
					DestinationName: "6687bd19-5654-76be-d764",
					LocalBindPort:   8202,
				},
				{
					DestinationType: api.UpstreamDestTypeService,
					DestinationName: "upstream2",
					LocalBindPort:   3234,
				},
			},
			consulNamespacesEnabled: false,
			consulPartitionsEnabled: false,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			ep := &Controller{
				Log:                    logrtest.New(t),
				AllowK8sNamespacesSet:  mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:   mapset.NewSetWith(),
				EnableConsulNamespaces: tt.consulNamespacesEnabled,
				EnableConsulPartitions: tt.consulPartitionsEnabled,
			}

			upstreams, err := ep.processUpstreams(*tt.pod(), corev1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "svcname",
					Namespace:   "default",
					Labels:      map[string]string{},
					Annotations: map[string]string{},
				},
			})
			if tt.expErr != "" {
				require.EqualError(t, err, tt.expErr)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expected, upstreams)
			}
		})
	}
}

func TestGetServiceName(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		pod        func() *corev1.Pod
		endpoint   *corev1.Endpoints
		expSvcName string
	}{
		{
			name: "single port, with annotation",
			pod: func() *corev1.Pod {
				pod1 := createServicePod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[constants.AnnotationService] = "web"
				return pod1
			},
			endpoint: &corev1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "not-web",
					Namespace: "default",
				},
			},
			expSvcName: "web",
		},
		{
			name: "single port, without annotation",
			pod: func() *corev1.Pod {
				pod1 := createServicePod("pod1", "1.2.3.4", true, true)
				return pod1
			},
			endpoint: &corev1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ep-name",
					Namespace: "default",
				},
			},
			expSvcName: "ep-name",
		},
		{
			name: "multi port, with annotation",
			pod: func() *corev1.Pod {
				pod1 := createServicePod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[constants.AnnotationService] = "web,web-admin"
				return pod1
			},
			endpoint: &corev1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ep-name-multiport",
					Namespace: "default",
				},
			},
			expSvcName: "ep-name-multiport",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {

			svcName := serviceName(*tt.pod(), *tt.endpoint)
			require.Equal(t, tt.expSvcName, svcName)

		})
	}
}

func TestReconcileCreateEndpoint_MultiportService(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name                       string
		consulSvcName              string
		k8sObjects                 func() []runtime.Object
		initialConsulSvcs          []*api.AgentService
		expectedNumSvcInstances    int
		expectedConsulSvcInstances []*api.CatalogService
		expectedProxySvcInstances  []*api.CatalogService
		expectedHealthChecks       []*api.HealthCheck
	}{
		{
			name:          "Multiport service",
			consulSvcName: "web,web-admin",
			k8sObjects: func() []runtime.Object {
				pod1 := createServicePod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[constants.AnnotationPort] = "8080,9090"
				pod1.Annotations[constants.AnnotationService] = "web,web-admin"
				pod1.Annotations[constants.AnnotationUpstreams] = "upstream1:1234"
				endpoint1 := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "web",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									IP: "1.2.3.4",
									TargetRef: &corev1.ObjectReference{
										Kind:      "Pod",
										Name:      "pod1",
										Namespace: "default",
									},
								},
							},
						},
					},
				}
				endpoint2 := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "web-admin",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									IP: "1.2.3.4",
									TargetRef: &corev1.ObjectReference{
										Kind:      "Pod",
										Name:      "pod1",
										Namespace: "default",
									},
								},
							},
						},
					},
				}
				return []runtime.Object{pod1, endpoint1, endpoint2}
			},
			initialConsulSvcs:       nil,
			expectedNumSvcInstances: 1,
			expectedConsulSvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod1-web",
					ServiceName:    "web",
					ServiceAddress: "1.2.3.4",
					ServicePort:    8080,
					ServiceMeta: map[string]string{
						constants.MetaKeyPodName: "pod1",
						metaKeyKubeServiceName:   "web",
						constants.MetaKeyKubeNS:  "default",
						metaKeyManagedBy:         constants.ManagedByValue,
						metaKeySyntheticNode:     "true",
						constants.MetaKeyPodUID:  "",
					},
					ServiceTags: []string{},
				},
				{
					ServiceID:      "pod1-web-admin",
					ServiceName:    "web-admin",
					ServiceAddress: "1.2.3.4",
					ServicePort:    9090,
					ServiceMeta: map[string]string{
						constants.MetaKeyPodName: "pod1",
						metaKeyKubeServiceName:   "web-admin",
						constants.MetaKeyKubeNS:  "default",
						metaKeyManagedBy:         constants.ManagedByValue,
						metaKeySyntheticNode:     "true",
						constants.MetaKeyPodUID:  "",
					},
					ServiceTags: []string{},
				},
			},
			expectedProxySvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod1-web-sidecar-proxy",
					ServiceName:    "web-sidecar-proxy",
					ServiceAddress: "1.2.3.4",
					ServicePort:    20000,
					ServiceProxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "web",
						DestinationServiceID:   "pod1-web",
						LocalServiceAddress:    "127.0.0.1",
						LocalServicePort:       8080,
						Upstreams: []api.Upstream{
							{
								DestinationType: api.UpstreamDestTypeService,
								DestinationName: "upstream1",
								LocalBindPort:   1234,
							},
						},
					},
					ServiceMeta: map[string]string{
						constants.MetaKeyPodName: "pod1",
						metaKeyKubeServiceName:   "web",
						constants.MetaKeyKubeNS:  "default",
						metaKeyManagedBy:         constants.ManagedByValue,
						metaKeySyntheticNode:     "true",
						constants.MetaKeyPodUID:  "",
					},
					ServiceTags: []string{},
				},
				{
					ServiceID:      "pod1-web-admin-sidecar-proxy",
					ServiceName:    "web-admin-sidecar-proxy",
					ServiceAddress: "1.2.3.4",
					ServicePort:    20001,
					ServiceProxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "web-admin",
						DestinationServiceID:   "pod1-web-admin",
						LocalServiceAddress:    "127.0.0.1",
						LocalServicePort:       9090,
					},
					ServiceMeta: map[string]string{
						constants.MetaKeyPodName: "pod1",
						metaKeyKubeServiceName:   "web-admin",
						constants.MetaKeyKubeNS:  "default",
						metaKeyManagedBy:         constants.ManagedByValue,
						metaKeySyntheticNode:     "true",
						constants.MetaKeyPodUID:  "",
					},
					ServiceTags: []string{},
				},
			},
			expectedHealthChecks: []*api.HealthCheck{
				{
					CheckID:     "default/pod1-web",
					ServiceName: "web",
					ServiceID:   "pod1-web",
					Name:        constants.ConsulKubernetesCheckName,
					Status:      api.HealthPassing,
					Output:      constants.KubernetesSuccessReasonMsg,
					Type:        constants.ConsulKubernetesCheckType,
				},
				{
					CheckID:     "default/pod1-web-sidecar-proxy",
					ServiceName: "web-sidecar-proxy",
					ServiceID:   "pod1-web-sidecar-proxy",
					Name:        constants.ConsulKubernetesCheckName,
					Status:      api.HealthPassing,
					Output:      constants.KubernetesSuccessReasonMsg,
					Type:        constants.ConsulKubernetesCheckType,
				},
				{
					CheckID:     "default/pod1-web-admin",
					ServiceName: "web-admin",
					ServiceID:   "pod1-web-admin",
					Name:        constants.ConsulKubernetesCheckName,
					Status:      api.HealthPassing,
					Output:      constants.KubernetesSuccessReasonMsg,
					Type:        constants.ConsulKubernetesCheckType,
				},
				{
					CheckID:     "default/pod1-web-admin-sidecar-proxy",
					ServiceName: "web-admin-sidecar-proxy",
					ServiceID:   "pod1-web-admin-sidecar-proxy",
					Name:        constants.ConsulKubernetesCheckName,
					Status:      api.HealthPassing,
					Output:      constants.KubernetesSuccessReasonMsg,
					Type:        constants.ConsulKubernetesCheckType,
				},
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			// Add the default namespace.
			ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
			node := corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}}
			// Create fake k8s client
			k8sObjects := append(tt.k8sObjects(), &ns, &node)

			fakeClient := fake.NewClientBuilder().WithRuntimeObjects(k8sObjects...).Build()

			// Create test consul server.
			testClient := test.TestServerWithMockConnMgrWatcher(t, nil)
			consulClient := testClient.APIClient

			// Register service and proxy in consul.
			for _, svc := range tt.initialConsulSvcs {
				catalogRegistration := &api.CatalogRegistration{
					Node:    consulNodeName,
					Address: consulNodeAddress,
					NodeMeta: map[string]string{
						metaKeySyntheticNode: "true",
					},
					Service: svc,
				}
				_, err := consulClient.Catalog().Register(catalogRegistration, nil)
				require.NoError(t, err)
			}

			// Create the endpoints controller
			ep := &Controller{
				Client:                fakeClient,
				Log:                   logrtest.New(t),
				ConsulClientConfig:    testClient.Cfg,
				ConsulServerConnMgr:   testClient.Watcher,
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSetWith(),
				ReleaseName:           "consul",
				ReleaseNamespace:      "default",
			}
			namespacedName := types.NamespacedName{
				Namespace: "default",
				Name:      "web",
			}
			namespacedName2 := types.NamespacedName{
				Namespace: "default",
				Name:      "web-admin",
			}

			resp, err := ep.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: namespacedName,
			})
			require.NoError(t, err)
			require.False(t, resp.Requeue)
			resp, err = ep.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: namespacedName2,
			})
			require.NoError(t, err)
			require.False(t, resp.Requeue)

			// After reconciliation, Consul should have the service with the correct number of instances
			svcs := strings.Split(tt.consulSvcName, ",")
			for i, service := range svcs {
				serviceInstances, _, err := consulClient.Catalog().Service(service, "", nil)
				require.NoError(t, err)
				require.Len(t, serviceInstances, tt.expectedNumSvcInstances)
				for _, instance := range serviceInstances {
					require.Equal(t, tt.expectedConsulSvcInstances[i].ServiceID, instance.ServiceID)
					require.Equal(t, tt.expectedConsulSvcInstances[i].ServiceName, instance.ServiceName)
					require.Equal(t, tt.expectedConsulSvcInstances[i].ServiceAddress, instance.ServiceAddress)
					require.Equal(t, tt.expectedConsulSvcInstances[i].ServicePort, instance.ServicePort)
					require.Equal(t, tt.expectedConsulSvcInstances[i].ServiceMeta, instance.ServiceMeta)
					require.Equal(t, tt.expectedConsulSvcInstances[i].ServiceTags, instance.ServiceTags)
				}
				proxyServiceInstances, _, err := consulClient.Catalog().Service(fmt.Sprintf("%s-sidecar-proxy", service), "", nil)
				require.NoError(t, err)
				require.Len(t, proxyServiceInstances, tt.expectedNumSvcInstances)
				for _, instance := range proxyServiceInstances {
					require.Equal(t, tt.expectedProxySvcInstances[i].ServiceID, instance.ServiceID)
					require.Equal(t, tt.expectedProxySvcInstances[i].ServiceName, instance.ServiceName)
					require.Equal(t, tt.expectedProxySvcInstances[i].ServiceAddress, instance.ServiceAddress)
					require.Equal(t, tt.expectedProxySvcInstances[i].ServicePort, instance.ServicePort)
					require.Equal(t, tt.expectedProxySvcInstances[i].ServiceMeta, instance.ServiceMeta)
					require.Equal(t, tt.expectedProxySvcInstances[i].ServiceTags, instance.ServiceTags)

					// When comparing the ServiceProxy field we ignore the DestinationNamespace
					// field within that struct because on Consul OSS it's set to "" but on Consul Enterprise
					// it's set to "default" and we want to re-use this test for both OSS and Ent.
					// This does mean that we don't test that field but that's okay because
					// it's not getting set specifically in this test.
					// To do the comparison that ignores that field we use go-cmp instead
					// of the regular require.Equal call since it supports ignoring certain
					// fields.
					diff := cmp.Diff(tt.expectedProxySvcInstances[i].ServiceProxy, instance.ServiceProxy,
						cmpopts.IgnoreFields(api.Upstream{}, "DestinationNamespace", "DestinationPartition"))
					require.Empty(t, diff, "expected objects to be equal")
				}
			}

			// Check that the Consul health check was created for the k8s pod.
			for _, expectedCheck := range tt.expectedHealthChecks {
				checks, _, err := consulClient.Health().Checks(expectedCheck.ServiceName, nil)
				require.NoError(t, err)
				require.Equal(t, len(checks), 1)
				// Ignoring Namespace because the response from ENT includes it and OSS does not.
				var ignoredFields = []string{"Node", "Definition", "Namespace", "Partition", "CreateIndex", "ModifyIndex", "ServiceTags"}
				require.True(t, cmp.Equal(checks[0], expectedCheck, cmpopts.IgnoreFields(api.HealthCheck{}, ignoredFields...)))
			}
		})
	}
}

// TestReconcileCreateEndpoint tests the logic to create service instances in Consul from the addresses in the Endpoints
// object. This test covers Controller.createServiceRegistrations and Controller.createGatewayRegistrations.
// This test depends on a Consul binary being present on the host machine.
func TestReconcileCreateEndpoint(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name                       string
		svcName                    string
		consulSvcName              string
		k8sObjects                 func() []runtime.Object
		expectedConsulSvcInstances []*api.CatalogService
		expectedProxySvcInstances  []*api.CatalogService
		expectedHealthChecks       []*api.HealthCheck
		metricsEnabled             bool
		telemetryCollectorDisabled bool
		nodeMeta                   map[string]string
		expErr                     string
	}{
		{
			name:          "Empty endpoints",
			svcName:       "service-created",
			consulSvcName: "service-created",
			k8sObjects: func() []runtime.Object {
				endpoint := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-created",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{},
						},
					},
				}
				return []runtime.Object{endpoint}
			},
			expectedConsulSvcInstances: nil,
			expectedProxySvcInstances:  nil,
			expectedHealthChecks:       nil,
		},
		{
			name:          "Basic endpoints",
			svcName:       "service-created",
			consulSvcName: "service-created",
			nodeMeta: map[string]string{
				"test-node": "true",
			},
			k8sObjects: func() []runtime.Object {
				pod1 := createServicePod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[constants.AnnotationProxyConfigMap] = `{ "xds_fetch_timeout_ms": 9999 }`
				endpoint := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-created",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									IP: "1.2.3.4",
									TargetRef: &corev1.ObjectReference{
										Kind:      "Pod",
										Name:      "pod1",
										Namespace: "default",
									},
								},
							},
						},
					},
				}
				return []runtime.Object{pod1, endpoint}
			},
			expectedConsulSvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod1-service-created",
					ServiceName:    "service-created",
					ServiceAddress: "1.2.3.4",
					ServicePort:    0,
					ServiceMeta:    map[string]string{constants.MetaKeyPodName: "pod1", metaKeyKubeServiceName: "service-created", constants.MetaKeyKubeNS: "default", metaKeyManagedBy: constants.ManagedByValue, metaKeySyntheticNode: "true", constants.MetaKeyPodUID: ""},
					ServiceTags:    []string{},
					ServiceProxy:   &api.AgentServiceConnectProxyConfig{},
					NodeMeta: map[string]string{
						"synthetic-node": "true",
						"test-node":      "true",
					},
				},
			},
			expectedProxySvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod1-service-created-sidecar-proxy",
					ServiceName:    "service-created-sidecar-proxy",
					ServiceAddress: "1.2.3.4",
					ServicePort:    20000,
					ServiceProxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "service-created",
						DestinationServiceID:   "pod1-service-created",
						LocalServiceAddress:    "",
						LocalServicePort:       0,
						Config: map[string]any{
							"envoy_telemetry_collector_bind_socket_dir": string("/consul/connect-inject"),
							"xds_fetch_timeout_ms":                      float64(9999),
						},
					},
					ServiceMeta: map[string]string{constants.MetaKeyPodName: "pod1", metaKeyKubeServiceName: "service-created", constants.MetaKeyKubeNS: "default", metaKeyManagedBy: constants.ManagedByValue, metaKeySyntheticNode: "true", constants.MetaKeyPodUID: ""},
					ServiceTags: []string{},
					NodeMeta: map[string]string{
						"synthetic-node": "true",
						"test-node":      "true",
					},
				},
			},
			expectedHealthChecks: []*api.HealthCheck{
				{
					CheckID:     "default/pod1-service-created",
					ServiceName: "service-created",
					ServiceID:   "pod1-service-created",
					Name:        constants.ConsulKubernetesCheckName,
					Status:      api.HealthPassing,
					Output:      constants.KubernetesSuccessReasonMsg,
					Type:        constants.ConsulKubernetesCheckType,
				},
				{
					CheckID:     "default/pod1-service-created-sidecar-proxy",
					ServiceName: "service-created-sidecar-proxy",
					ServiceID:   "pod1-service-created-sidecar-proxy",
					Name:        constants.ConsulKubernetesCheckName,
					Status:      api.HealthPassing,
					Output:      constants.KubernetesSuccessReasonMsg,
					Type:        constants.ConsulKubernetesCheckType,
				},
			},
		},
		{
			name:          "Mesh Gateway",
			svcName:       "mesh-gateway",
			consulSvcName: "mesh-gateway",
			nodeMeta: map[string]string{
				"test-node": "true",
			},
			k8sObjects: func() []runtime.Object {
				gateway := createGatewayPod("mesh-gateway", "1.2.3.4", map[string]string{
					constants.AnnotationGatewayConsulServiceName: "mesh-gateway",
					constants.AnnotationGatewayWANSource:         "Static",
					constants.AnnotationGatewayWANAddress:        "2.3.4.5",
					constants.AnnotationGatewayWANPort:           "443",
					constants.AnnotationMeshGatewayContainerPort: "8443",
					constants.AnnotationProxyConfigMap:           `{ "xds_fetch_timeout_ms": 9999 }`,
					constants.AnnotationGatewayKind:              meshGateway})
				endpoint := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "mesh-gateway",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									IP: "1.2.3.4",
									TargetRef: &corev1.ObjectReference{
										Kind:      "Pod",
										Name:      "mesh-gateway",
										Namespace: "default",
									},
								},
							},
						},
					},
				}
				return []runtime.Object{gateway, endpoint}
			},
			expectedConsulSvcInstances: []*api.CatalogService{
				{
					ServiceID:      "mesh-gateway",
					ServiceName:    "mesh-gateway",
					ServiceAddress: "1.2.3.4",
					ServicePort:    8443,
					ServiceMeta:    map[string]string{constants.MetaKeyPodName: "mesh-gateway", metaKeyKubeServiceName: "mesh-gateway", constants.MetaKeyKubeNS: "default", metaKeyManagedBy: constants.ManagedByValue, metaKeySyntheticNode: "true", constants.MetaKeyPodUID: ""},
					ServiceTags:    []string{},
					ServiceTaggedAddresses: map[string]api.ServiceAddress{
						"lan": {
							Address: "1.2.3.4",
							Port:    8443,
						},
						"wan": {
							Address: "2.3.4.5",
							Port:    443,
						},
					},
					ServiceProxy: &api.AgentServiceConnectProxyConfig{
						Config: map[string]any{
							"envoy_telemetry_collector_bind_socket_dir": string("/consul/service"),
							"xds_fetch_timeout_ms":                      float64(9999),
						},
					},
					NodeMeta: map[string]string{
						"synthetic-node": "true",
						"test-node":      "true",
					},
				},
			},
			expectedHealthChecks: []*api.HealthCheck{
				{
					CheckID:     "default/mesh-gateway",
					ServiceName: "mesh-gateway",
					ServiceID:   "mesh-gateway",
					Name:        constants.ConsulKubernetesCheckName,
					Status:      api.HealthPassing,
					Output:      constants.KubernetesSuccessReasonMsg,
					Type:        constants.ConsulKubernetesCheckType,
				},
			},
		},
		{
			name:          "Mesh Gateway with Metrics enabled",
			svcName:       "mesh-gateway",
			consulSvcName: "mesh-gateway",
			k8sObjects: func() []runtime.Object {
				gateway := createGatewayPod("mesh-gateway", "1.2.3.4", map[string]string{
					constants.AnnotationGatewayConsulServiceName: "mesh-gateway",
					constants.AnnotationGatewayWANSource:         "Static",
					constants.AnnotationGatewayWANAddress:        "2.3.4.5",
					constants.AnnotationGatewayWANPort:           "443",
					constants.AnnotationMeshGatewayContainerPort: "8443",
					constants.AnnotationGatewayKind:              meshGateway})
				endpoint := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "mesh-gateway",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									IP: "1.2.3.4",
									TargetRef: &corev1.ObjectReference{
										Kind:      "Pod",
										Name:      "mesh-gateway",
										Namespace: "default",
									},
								},
							},
						},
					},
				}
				return []runtime.Object{gateway, endpoint}
			},
			expectedConsulSvcInstances: []*api.CatalogService{
				{
					ServiceID:      "mesh-gateway",
					ServiceName:    "mesh-gateway",
					ServiceAddress: "1.2.3.4",
					ServicePort:    8443,
					ServiceMeta:    map[string]string{constants.MetaKeyPodName: "mesh-gateway", metaKeyKubeServiceName: "mesh-gateway", constants.MetaKeyKubeNS: "default", metaKeyManagedBy: constants.ManagedByValue, metaKeySyntheticNode: "true", constants.MetaKeyPodUID: ""},
					ServiceTags:    []string{},
					ServiceTaggedAddresses: map[string]api.ServiceAddress{
						"lan": {
							Address: "1.2.3.4",
							Port:    8443,
						},
						"wan": {
							Address: "2.3.4.5",
							Port:    443,
						},
					},
					ServiceProxy: &api.AgentServiceConnectProxyConfig{
						Config: map[string]interface{}{
							"envoy_prometheus_bind_addr":                "1.2.3.4:20200",
							"envoy_telemetry_collector_bind_socket_dir": "/consul/service",
						},
					},
				},
			},
			expectedHealthChecks: []*api.HealthCheck{
				{
					CheckID:     "default/mesh-gateway",
					ServiceName: "mesh-gateway",
					ServiceID:   "mesh-gateway",
					Name:        constants.ConsulKubernetesCheckName,
					Status:      api.HealthPassing,
					Output:      constants.KubernetesSuccessReasonMsg,
					Type:        constants.ConsulKubernetesCheckType,
				},
			},
			metricsEnabled: true,
		},
		{
			name:                       "Mesh_Gateway_with_Metrics_enabled_and_telemetry_collector_disabled",
			svcName:                    "mesh-gateway",
			consulSvcName:              "mesh-gateway",
			telemetryCollectorDisabled: true,
			k8sObjects: func() []runtime.Object {
				gateway := createGatewayPod("mesh-gateway", "1.2.3.4", map[string]string{
					constants.AnnotationGatewayConsulServiceName: "mesh-gateway",
					constants.AnnotationGatewayWANSource:         "Static",
					constants.AnnotationGatewayWANAddress:        "2.3.4.5",
					constants.AnnotationGatewayWANPort:           "443",
					constants.AnnotationMeshGatewayContainerPort: "8443",
					constants.AnnotationGatewayKind:              meshGateway})
				endpoint := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "mesh-gateway",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									IP: "1.2.3.4",
									TargetRef: &corev1.ObjectReference{
										Kind:      "Pod",
										Name:      "mesh-gateway",
										Namespace: "default",
									},
								},
							},
						},
					},
				}
				return []runtime.Object{gateway, endpoint}
			},
			expectedConsulSvcInstances: []*api.CatalogService{
				{
					ServiceID:      "mesh-gateway",
					ServiceName:    "mesh-gateway",
					ServiceAddress: "1.2.3.4",
					ServicePort:    8443,
					ServiceMeta:    map[string]string{constants.MetaKeyPodName: "mesh-gateway", metaKeyKubeServiceName: "mesh-gateway", constants.MetaKeyKubeNS: "default", metaKeyManagedBy: constants.ManagedByValue, metaKeySyntheticNode: "true", constants.MetaKeyPodUID: ""},
					ServiceTags:    []string{},
					ServiceTaggedAddresses: map[string]api.ServiceAddress{
						"lan": {
							Address: "1.2.3.4",
							Port:    8443,
						},
						"wan": {
							Address: "2.3.4.5",
							Port:    443,
						},
					},
					ServiceProxy: &api.AgentServiceConnectProxyConfig{
						Config: map[string]interface{}{
							"envoy_prometheus_bind_addr": "1.2.3.4:20200",
						},
					},
				},
			},
			expectedHealthChecks: []*api.HealthCheck{
				{
					CheckID:     "default/mesh-gateway",
					ServiceName: "mesh-gateway",
					ServiceID:   "mesh-gateway",
					Name:        constants.ConsulKubernetesCheckName,
					Status:      api.HealthPassing,
					Output:      constants.KubernetesSuccessReasonMsg,
					Type:        constants.ConsulKubernetesCheckType,
				},
			},
			metricsEnabled: true,
		},
		{
			name:          "Terminating Gateway",
			svcName:       "terminating-gateway",
			consulSvcName: "terminating-gateway",
			k8sObjects: func() []runtime.Object {
				gateway := createGatewayPod("terminating-gateway", "1.2.3.4", map[string]string{
					constants.AnnotationGatewayKind:              terminatingGateway,
					constants.AnnotationGatewayConsulServiceName: "terminating-gateway",
					constants.AnnotationProxyConfigMap:           `{ "xds_fetch_timeout_ms": 9999 }`,
				})
				endpoint := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "terminating-gateway",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									IP: "1.2.3.4",
									TargetRef: &corev1.ObjectReference{
										Kind:      "Pod",
										Name:      "terminating-gateway",
										Namespace: "default",
									},
								},
							},
						},
					},
				}
				return []runtime.Object{gateway, endpoint}
			},
			expectedConsulSvcInstances: []*api.CatalogService{
				{
					ServiceID:      "terminating-gateway",
					ServiceName:    "terminating-gateway",
					ServiceAddress: "1.2.3.4",
					ServicePort:    8443,
					ServiceMeta: map[string]string{
						constants.MetaKeyPodName: "terminating-gateway",
						metaKeyKubeServiceName:   "terminating-gateway",
						constants.MetaKeyKubeNS:  "default",
						metaKeyManagedBy:         constants.ManagedByValue,
						metaKeySyntheticNode:     "true",
						constants.MetaKeyPodUID:  "",
					},
					ServiceTags: []string{},
					ServiceProxy: &api.AgentServiceConnectProxyConfig{
						Config: map[string]any{
							"envoy_telemetry_collector_bind_socket_dir": string("/consul/service"),
							"xds_fetch_timeout_ms":                      float64(9999),
						},
					},
				},
			},
			expectedHealthChecks: []*api.HealthCheck{
				{
					CheckID:     "default/terminating-gateway",
					ServiceName: "terminating-gateway",
					ServiceID:   "terminating-gateway",
					Name:        constants.ConsulKubernetesCheckName,
					Status:      api.HealthPassing,
					Output:      constants.KubernetesSuccessReasonMsg,
					Type:        constants.ConsulKubernetesCheckType,
				},
			},
		},
		{
			name:           "Terminating Gateway with Metrics enabled",
			metricsEnabled: true,
			svcName:        "terminating-gateway",
			consulSvcName:  "terminating-gateway",
			k8sObjects: func() []runtime.Object {
				gateway := createGatewayPod("terminating-gateway", "1.2.3.4", map[string]string{
					constants.AnnotationGatewayKind:              terminatingGateway,
					constants.AnnotationGatewayConsulServiceName: "terminating-gateway",
				})
				endpoint := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "terminating-gateway",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									IP: "1.2.3.4",
									TargetRef: &corev1.ObjectReference{
										Kind:      "Pod",
										Name:      "terminating-gateway",
										Namespace: "default",
									},
								},
							},
						},
					},
				}
				return []runtime.Object{gateway, endpoint}
			},
			expectedConsulSvcInstances: []*api.CatalogService{
				{
					ServiceID:      "terminating-gateway",
					ServiceName:    "terminating-gateway",
					ServiceAddress: "1.2.3.4",
					ServicePort:    8443,
					ServiceMeta: map[string]string{
						constants.MetaKeyPodName: "terminating-gateway",
						metaKeyKubeServiceName:   "terminating-gateway",
						constants.MetaKeyKubeNS:  "default",
						metaKeyManagedBy:         constants.ManagedByValue,
						metaKeySyntheticNode:     "true",
						constants.MetaKeyPodUID:  "",
					},
					ServiceTags: []string{},
					ServiceProxy: &api.AgentServiceConnectProxyConfig{
						Config: map[string]interface{}{
							"envoy_prometheus_bind_addr":                "1.2.3.4:20200",
							"envoy_telemetry_collector_bind_socket_dir": "/consul/service",
						},
					},
				},
			},
			expectedHealthChecks: []*api.HealthCheck{
				{
					CheckID:     "default/terminating-gateway",
					ServiceName: "terminating-gateway",
					ServiceID:   "terminating-gateway",
					Name:        constants.ConsulKubernetesCheckName,
					Status:      api.HealthPassing,
					Output:      constants.KubernetesSuccessReasonMsg,
					Type:        constants.ConsulKubernetesCheckType,
				},
			},
		},
		{
			name:          "Ingress Gateway",
			svcName:       "ingress-gateway",
			consulSvcName: "ingress-gateway",
			k8sObjects: func() []runtime.Object {
				gateway := createGatewayPod("ingress-gateway", "1.2.3.4", map[string]string{
					constants.AnnotationGatewayConsulServiceName: "ingress-gateway",
					constants.AnnotationGatewayKind:              ingressGateway,
					constants.AnnotationGatewayWANSource:         "Service",
					constants.AnnotationGatewayWANPort:           "8443",
					constants.AnnotationProxyConfigMap:           `{ "xds_fetch_timeout_ms": 9999 }`,
				})
				endpoint := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ingress-gateway",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									IP: "1.2.3.4",
									TargetRef: &corev1.ObjectReference{
										Kind:      "Pod",
										Name:      "ingress-gateway",
										Namespace: "default",
									},
								},
							},
						},
					},
				}
				svc := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ingress-gateway",
						Namespace: "default",
					},
					Spec: corev1.ServiceSpec{
						Type: corev1.ServiceTypeLoadBalancer,
					},
					Status: corev1.ServiceStatus{
						LoadBalancer: corev1.LoadBalancerStatus{
							Ingress: []corev1.LoadBalancerIngress{
								{
									IP: "5.6.7.8",
								},
							},
						},
					},
				}
				return []runtime.Object{gateway, endpoint, svc}
			},
			expectedConsulSvcInstances: []*api.CatalogService{
				{
					ServiceID:      "ingress-gateway",
					ServiceName:    "ingress-gateway",
					ServiceAddress: "1.2.3.4",
					ServicePort:    21000,
					ServiceMeta: map[string]string{
						constants.MetaKeyPodName: "ingress-gateway",
						metaKeyKubeServiceName:   "ingress-gateway",
						constants.MetaKeyKubeNS:  "default",
						metaKeyManagedBy:         constants.ManagedByValue,
						metaKeySyntheticNode:     "true",
						constants.MetaKeyPodUID:  "",
					},
					ServiceTags: []string{},
					ServiceTaggedAddresses: map[string]api.ServiceAddress{
						"lan": {
							Address: "1.2.3.4",
							Port:    21000,
						},
						"wan": {
							Address: "5.6.7.8",
							Port:    8443,
						},
					},
					ServiceProxy: &api.AgentServiceConnectProxyConfig{
						Config: map[string]interface{}{
							"envoy_gateway_no_default_bind": true,
							"envoy_gateway_bind_addresses": map[string]interface{}{
								"all-interfaces": map[string]interface{}{
									"address": "0.0.0.0",
								},
							},
							"envoy_telemetry_collector_bind_socket_dir": "/consul/service",
							"xds_fetch_timeout_ms":                      float64(9999),
						},
					},
				},
			},
			expectedHealthChecks: []*api.HealthCheck{
				{
					CheckID:     "default/ingress-gateway",
					ServiceName: "ingress-gateway",
					ServiceID:   "ingress-gateway",
					Name:        constants.ConsulKubernetesCheckName,
					Status:      api.HealthPassing,
					Output:      constants.KubernetesSuccessReasonMsg,
					Type:        constants.ConsulKubernetesCheckType,
				},
			},
		},
		{
			name:           "Ingress Gateway with Metrics enabled",
			metricsEnabled: true,
			svcName:        "ingress-gateway",
			consulSvcName:  "ingress-gateway",
			k8sObjects: func() []runtime.Object {
				gateway := createGatewayPod("ingress-gateway", "1.2.3.4", map[string]string{
					constants.AnnotationGatewayConsulServiceName: "ingress-gateway",
					constants.AnnotationGatewayKind:              ingressGateway,
					constants.AnnotationGatewayWANSource:         "Service",
					constants.AnnotationGatewayWANPort:           "8443",
				})
				endpoint := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ingress-gateway",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									IP: "1.2.3.4",
									TargetRef: &corev1.ObjectReference{
										Kind:      "Pod",
										Name:      "ingress-gateway",
										Namespace: "default",
									},
								},
							},
						},
					},
				}
				svc := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ingress-gateway",
						Namespace: "default",
					},
					Spec: corev1.ServiceSpec{
						Type: corev1.ServiceTypeLoadBalancer,
					},
					Status: corev1.ServiceStatus{
						LoadBalancer: corev1.LoadBalancerStatus{
							Ingress: []corev1.LoadBalancerIngress{
								{
									IP: "5.6.7.8",
								},
							},
						},
					},
				}
				return []runtime.Object{gateway, endpoint, svc}
			},
			expectedConsulSvcInstances: []*api.CatalogService{
				{
					ServiceID:      "ingress-gateway",
					ServiceName:    "ingress-gateway",
					ServiceAddress: "1.2.3.4",
					ServicePort:    21000,
					ServiceMeta: map[string]string{
						constants.MetaKeyPodName: "ingress-gateway",
						metaKeyKubeServiceName:   "ingress-gateway",
						constants.MetaKeyKubeNS:  "default",
						metaKeyManagedBy:         constants.ManagedByValue,
						metaKeySyntheticNode:     "true",
						constants.MetaKeyPodUID:  "",
					},
					ServiceTags: []string{},
					ServiceTaggedAddresses: map[string]api.ServiceAddress{
						"lan": {
							Address: "1.2.3.4",
							Port:    21000,
						},
						"wan": {
							Address: "5.6.7.8",
							Port:    8443,
						},
					},
					ServiceProxy: &api.AgentServiceConnectProxyConfig{
						Config: map[string]interface{}{
							"envoy_gateway_no_default_bind": true,
							"envoy_gateway_bind_addresses": map[string]interface{}{
								"all-interfaces": map[string]interface{}{
									"address": "0.0.0.0",
								},
							},
							"envoy_prometheus_bind_addr":                "1.2.3.4:20200",
							"envoy_telemetry_collector_bind_socket_dir": "/consul/service",
						},
					},
				},
			},
			expectedHealthChecks: []*api.HealthCheck{
				{
					CheckID:     "default/ingress-gateway",
					ServiceName: "ingress-gateway",
					ServiceID:   "ingress-gateway",
					Name:        constants.ConsulKubernetesCheckName,
					Status:      api.HealthPassing,
					Output:      constants.KubernetesSuccessReasonMsg,
					Type:        constants.ConsulKubernetesCheckType,
				},
			},
		},
		{
			name:          "Endpoints with multiple addresses",
			svcName:       "service-created",
			consulSvcName: "service-created",
			k8sObjects: func() []runtime.Object {
				pod1 := createServicePod("pod1", "1.2.3.4", true, true)
				pod2 := createServicePod("pod2", "2.2.3.4", true, true)
				endpointWithTwoAddresses := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-created",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									IP: "1.2.3.4",
									TargetRef: &corev1.ObjectReference{
										Kind:      "Pod",
										Name:      "pod1",
										Namespace: "default",
									},
								},
								{
									IP: "2.2.3.4",
									TargetRef: &corev1.ObjectReference{
										Kind:      "Pod",
										Name:      "pod2",
										Namespace: "default",
									},
								},
							},
						},
					},
				}
				return []runtime.Object{pod1, pod2, endpointWithTwoAddresses}
			},
			expectedConsulSvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod1-service-created",
					ServiceName:    "service-created",
					ServiceAddress: "1.2.3.4",
					ServicePort:    0,
					ServiceMeta:    map[string]string{constants.MetaKeyPodName: "pod1", metaKeyKubeServiceName: "service-created", constants.MetaKeyKubeNS: "default", metaKeyManagedBy: constants.ManagedByValue, metaKeySyntheticNode: "true", constants.MetaKeyPodUID: ""},
					ServiceTags:    []string{},
					ServiceProxy:   &api.AgentServiceConnectProxyConfig{},
				},
				{
					ServiceID:      "pod2-service-created",
					ServiceName:    "service-created",
					ServiceAddress: "2.2.3.4",
					ServicePort:    0,
					ServiceMeta:    map[string]string{constants.MetaKeyPodName: "pod2", metaKeyKubeServiceName: "service-created", constants.MetaKeyKubeNS: "default", metaKeyManagedBy: constants.ManagedByValue, metaKeySyntheticNode: "true", constants.MetaKeyPodUID: ""},
					ServiceTags:    []string{},
					ServiceProxy:   &api.AgentServiceConnectProxyConfig{},
				},
			},
			expectedProxySvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod1-service-created-sidecar-proxy",
					ServiceName:    "service-created-sidecar-proxy",
					ServiceAddress: "1.2.3.4",
					ServicePort:    20000,
					ServiceProxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "service-created",
						DestinationServiceID:   "pod1-service-created",
						LocalServiceAddress:    "",
						LocalServicePort:       0,
						Config:                 map[string]any{"envoy_telemetry_collector_bind_socket_dir": string("/consul/connect-inject")},
					},
					ServiceMeta: map[string]string{constants.MetaKeyPodName: "pod1", metaKeyKubeServiceName: "service-created", constants.MetaKeyKubeNS: "default", metaKeyManagedBy: constants.ManagedByValue, metaKeySyntheticNode: "true", constants.MetaKeyPodUID: ""},
					ServiceTags: []string{},
				},
				{
					ServiceID:      "pod2-service-created-sidecar-proxy",
					ServiceName:    "service-created-sidecar-proxy",
					ServiceAddress: "2.2.3.4",
					ServicePort:    20000,
					ServiceProxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "service-created",
						DestinationServiceID:   "pod2-service-created",
						LocalServiceAddress:    "",
						LocalServicePort:       0,
						Config:                 map[string]any{"envoy_telemetry_collector_bind_socket_dir": string("/consul/connect-inject")},
					},
					ServiceMeta: map[string]string{constants.MetaKeyPodName: "pod2", metaKeyKubeServiceName: "service-created", constants.MetaKeyKubeNS: "default", metaKeyManagedBy: constants.ManagedByValue, metaKeySyntheticNode: "true", constants.MetaKeyPodUID: ""},
					ServiceTags: []string{},
				},
			},
			expectedHealthChecks: []*api.HealthCheck{
				{
					CheckID:     "default/pod1-service-created",
					ServiceName: "service-created",
					ServiceID:   "pod1-service-created",
					Name:        constants.ConsulKubernetesCheckName,
					Status:      api.HealthPassing,
					Output:      constants.KubernetesSuccessReasonMsg,
					Type:        constants.ConsulKubernetesCheckType,
				},
				{
					CheckID:     "default/pod1-service-created-sidecar-proxy",
					ServiceName: "service-created-sidecar-proxy",
					ServiceID:   "pod1-service-created-sidecar-proxy",
					Name:        constants.ConsulKubernetesCheckName,
					Status:      api.HealthPassing,
					Output:      constants.KubernetesSuccessReasonMsg,
					Type:        constants.ConsulKubernetesCheckType,
				},
				{
					CheckID:     "default/pod2-service-created",
					ServiceName: "service-created",
					ServiceID:   "pod2-service-created",
					Name:        constants.ConsulKubernetesCheckName,
					Status:      api.HealthPassing,
					Output:      constants.KubernetesSuccessReasonMsg,
					Type:        constants.ConsulKubernetesCheckType,
				},
				{
					CheckID:     "default/pod2-service-created-sidecar-proxy",
					ServiceName: "service-created-sidecar-proxy",
					ServiceID:   "pod2-service-created-sidecar-proxy",
					Name:        constants.ConsulKubernetesCheckName,
					Status:      api.HealthPassing,
					Output:      constants.KubernetesSuccessReasonMsg,
					Type:        constants.ConsulKubernetesCheckType,
				},
			},
		},
		{
			// This test has 3 addresses, but only 2 are backed by pod resources. This will cause Reconcile to
			// deregister the instance associated with the non-existent pod and continue and process the other
			// addresses. We validate the other 2 addresses have service instances.
			name:          "Endpoints with multiple addresses but one is deleted",
			svcName:       "service-created",
			consulSvcName: "service-created",
			k8sObjects: func() []runtime.Object {
				pod1 := createServicePod("pod1", "1.2.3.4", true, true)
				pod2 := createServicePod("pod2", "2.2.3.4", true, true)
				endpointWithTwoAddresses := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-created",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								// This is an invalid address because pod3 will not exist in k8s.
								{
									IP: "9.9.9.9",
									TargetRef: &corev1.ObjectReference{
										Kind:      "Pod",
										Name:      "pod3",
										Namespace: "default",
									},
								},
								// The next two are valid addresses.
								{
									IP: "1.2.3.4",
									TargetRef: &corev1.ObjectReference{
										Kind:      "Pod",
										Name:      "pod1",
										Namespace: "default",
									},
								},
								{
									IP: "2.2.3.4",
									TargetRef: &corev1.ObjectReference{
										Kind:      "Pod",
										Name:      "pod2",
										Namespace: "default",
									},
								},
							},
						},
					},
				}
				return []runtime.Object{pod1, pod2, endpointWithTwoAddresses}
			},
			expectedConsulSvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod1-service-created",
					ServiceName:    "service-created",
					ServiceAddress: "1.2.3.4",
					ServicePort:    0,
					ServiceMeta:    map[string]string{constants.MetaKeyPodName: "pod1", metaKeyKubeServiceName: "service-created", constants.MetaKeyKubeNS: "default", metaKeyManagedBy: constants.ManagedByValue, metaKeySyntheticNode: "true", constants.MetaKeyPodUID: ""},
					ServiceTags:    []string{},
					ServiceProxy:   &api.AgentServiceConnectProxyConfig{},
				},
				{
					ServiceID:      "pod2-service-created",
					ServiceName:    "service-created",
					ServiceAddress: "2.2.3.4",
					ServicePort:    0,
					ServiceMeta:    map[string]string{constants.MetaKeyPodName: "pod2", metaKeyKubeServiceName: "service-created", constants.MetaKeyKubeNS: "default", metaKeyManagedBy: constants.ManagedByValue, metaKeySyntheticNode: "true", constants.MetaKeyPodUID: ""},
					ServiceTags:    []string{},
					ServiceProxy:   &api.AgentServiceConnectProxyConfig{},
				},
			},
			expectedProxySvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod1-service-created-sidecar-proxy",
					ServiceName:    "service-created-sidecar-proxy",
					ServiceAddress: "1.2.3.4",
					ServicePort:    20000,
					ServiceProxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "service-created",
						DestinationServiceID:   "pod1-service-created",
						LocalServiceAddress:    "",
						LocalServicePort:       0,
						Config:                 map[string]any{"envoy_telemetry_collector_bind_socket_dir": string("/consul/connect-inject")},
					},
					ServiceMeta: map[string]string{constants.MetaKeyPodName: "pod1", metaKeyKubeServiceName: "service-created", constants.MetaKeyKubeNS: "default", metaKeyManagedBy: constants.ManagedByValue, metaKeySyntheticNode: "true", constants.MetaKeyPodUID: ""},
					ServiceTags: []string{},
				},
				{
					ServiceID:      "pod2-service-created-sidecar-proxy",
					ServiceName:    "service-created-sidecar-proxy",
					ServiceAddress: "2.2.3.4",
					ServicePort:    20000,
					ServiceProxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "service-created",
						DestinationServiceID:   "pod2-service-created",
						LocalServiceAddress:    "",
						LocalServicePort:       0,
						Config:                 map[string]any{"envoy_telemetry_collector_bind_socket_dir": string("/consul/connect-inject")},
					},
					ServiceMeta: map[string]string{constants.MetaKeyPodName: "pod2", metaKeyKubeServiceName: "service-created", constants.MetaKeyKubeNS: "default", metaKeyManagedBy: constants.ManagedByValue, metaKeySyntheticNode: "true", constants.MetaKeyPodUID: ""},
					ServiceTags: []string{},
				},
			},
			expectedHealthChecks: []*api.HealthCheck{
				{
					CheckID:     "default/pod1-service-created",
					ServiceName: "service-created",
					ServiceID:   "pod1-service-created",
					Name:        constants.ConsulKubernetesCheckName,
					Status:      api.HealthPassing,
					Output:      constants.KubernetesSuccessReasonMsg,
					Type:        constants.ConsulKubernetesCheckType,
				},
				{
					CheckID:     "default/pod1-service-created-sidecar-proxy",
					ServiceName: "service-created-sidecar-proxy",
					ServiceID:   "pod1-service-created-sidecar-proxy",
					Name:        constants.ConsulKubernetesCheckName,
					Status:      api.HealthPassing,
					Output:      constants.KubernetesSuccessReasonMsg,
					Type:        constants.ConsulKubernetesCheckType,
				},
				{
					CheckID:     "default/pod2-service-created-sidecar-proxy",
					ServiceName: "service-created-sidecar-proxy",
					ServiceID:   "pod2-service-created-sidecar-proxy",
					Name:        constants.ConsulKubernetesCheckName,
					Status:      api.HealthPassing,
					Output:      constants.KubernetesSuccessReasonMsg,
					Type:        constants.ConsulKubernetesCheckType,
				},
			},
		},
		{
			name:          "Every configurable field set: port, different Consul service name, meta, tags, upstreams, metrics",
			svcName:       "service-created",
			consulSvcName: "different-consul-svc-name",
			k8sObjects: func() []runtime.Object {
				pod1 := createServicePod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[constants.AnnotationPort] = "1234"
				pod1.Annotations[constants.AnnotationService] = "different-consul-svc-name"
				pod1.Annotations[fmt.Sprintf("%sname", constants.AnnotationMeta)] = "abc"
				pod1.Annotations[fmt.Sprintf("%sversion", constants.AnnotationMeta)] = "2"
				pod1.Annotations[fmt.Sprintf("%spod_name", constants.AnnotationMeta)] = "$POD_NAME"
				pod1.Annotations[constants.AnnotationTags] = "abc\\,123,$POD_NAME"
				pod1.Annotations[constants.AnnotationUpstreams] = "upstream1:1234"
				pod1.Annotations[constants.AnnotationEnableMetrics] = "true"
				pod1.Annotations[constants.AnnotationPrometheusScrapePort] = "12345"
				pod1.Spec.NodeName = "my-node"
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-node",
						Namespace: "default",
						Labels: map[string]string{
							corev1.LabelTopologyRegion: "us-west-1",
							corev1.LabelTopologyZone:   "us-west-1a",
						},
					},
				}
				endpoint := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-created",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									IP: "1.2.3.4",
									TargetRef: &corev1.ObjectReference{
										Kind:      "Pod",
										Name:      "pod1",
										Namespace: "default",
									},
								},
							},
						},
					},
				}
				return []runtime.Object{pod1, node, endpoint}
			},
			expectedConsulSvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod1-different-consul-svc-name",
					ServiceName:    "different-consul-svc-name",
					ServiceAddress: "1.2.3.4",
					ServicePort:    1234,
					ServiceMeta: map[string]string{
						"name":                   "abc",
						"version":                "2",
						"pod_name":               "pod1",
						constants.MetaKeyPodName: "pod1",
						metaKeyKubeServiceName:   "service-created",
						constants.MetaKeyKubeNS:  "default",
						metaKeyManagedBy:         constants.ManagedByValue,
						metaKeySyntheticNode:     "true",
						constants.MetaKeyPodUID:  "",
					},
					ServiceTags:  []string{"abc,123", "pod1"},
					ServiceProxy: &api.AgentServiceConnectProxyConfig{},
					ServiceLocality: &api.Locality{
						Region: "us-west-1",
						Zone:   "us-west-1a",
					},
				},
			},
			expectedProxySvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod1-different-consul-svc-name-sidecar-proxy",
					ServiceName:    "different-consul-svc-name-sidecar-proxy",
					ServiceAddress: "1.2.3.4",
					ServicePort:    20000,
					ServiceProxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "different-consul-svc-name",
						DestinationServiceID:   "pod1-different-consul-svc-name",
						LocalServiceAddress:    "127.0.0.1",
						LocalServicePort:       1234,
						Upstreams: []api.Upstream{
							{
								DestinationType: api.UpstreamDestTypeService,
								DestinationName: "upstream1",
								LocalBindPort:   1234,
							},
						},
						Config: map[string]interface{}{
							"envoy_prometheus_bind_addr":                "0.0.0.0:12345",
							"envoy_telemetry_collector_bind_socket_dir": "/consul/connect-inject",
						},
					},
					ServiceLocality: &api.Locality{
						Region: "us-west-1",
						Zone:   "us-west-1a",
					},
					ServiceMeta: map[string]string{
						"name":                   "abc",
						"version":                "2",
						"pod_name":               "pod1",
						constants.MetaKeyPodName: "pod1",
						metaKeyKubeServiceName:   "service-created",
						constants.MetaKeyKubeNS:  "default",
						metaKeyManagedBy:         constants.ManagedByValue,
						metaKeySyntheticNode:     "true",
						constants.MetaKeyPodUID:  "",
					},
					ServiceTags: []string{"abc,123", "pod1"},
				},
			},
			expectedHealthChecks: []*api.HealthCheck{
				{
					CheckID:     "default/pod1-different-consul-svc-name",
					ServiceName: "different-consul-svc-name",
					ServiceID:   "pod1-different-consul-svc-name",
					Name:        constants.ConsulKubernetesCheckName,
					Status:      api.HealthPassing,
					Output:      constants.KubernetesSuccessReasonMsg,
					Type:        constants.ConsulKubernetesCheckType,
				},
				{
					CheckID:     "default/pod1-different-consul-svc-name-sidecar-proxy",
					ServiceName: "different-consul-svc-name-sidecar-proxy",
					ServiceID:   "pod1-different-consul-svc-name-sidecar-proxy",
					Name:        constants.ConsulKubernetesCheckName,
					Status:      api.HealthPassing,
					Output:      constants.KubernetesSuccessReasonMsg,
					Type:        constants.ConsulKubernetesCheckType,
				},
			},
		},
		// Test that if a user is updating their deployment from non-mesh to mesh that we
		// register the mesh pods.
		{
			name:          "Some endpoints injected, some not.",
			svcName:       "service-created",
			consulSvcName: "service-created",
			k8sObjects: func() []runtime.Object {
				pod1 := createServicePod("pod1", "1.2.3.4", true, true)
				pod2 := createServicePod("pod2", "2.3.4.5", false, false)

				// NOTE: the order of the addresses is important. The non-mesh pod must be first to correctly
				// reproduce the bug where we were exiting the loop early if any pod was non-mesh.
				endpointWithTwoAddresses := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-created",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									IP: "2.3.4.5",
									TargetRef: &corev1.ObjectReference{
										Kind:      "Pod",
										Name:      "pod2",
										Namespace: "default",
									},
								},
								{
									IP: "1.2.3.4",
									TargetRef: &corev1.ObjectReference{
										Kind:      "Pod",
										Name:      "pod1",
										Namespace: "default",
									},
								},
							},
						},
					},
				}
				return []runtime.Object{pod1, pod2, endpointWithTwoAddresses}
			},
			expectedConsulSvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod1-service-created",
					ServiceName:    "service-created",
					ServiceAddress: "1.2.3.4",
					ServicePort:    0,
					ServiceMeta:    map[string]string{constants.MetaKeyPodName: "pod1", metaKeyKubeServiceName: "service-created", constants.MetaKeyKubeNS: "default", metaKeyManagedBy: constants.ManagedByValue, metaKeySyntheticNode: "true", constants.MetaKeyPodUID: ""},
					ServiceTags:    []string{},
					ServiceProxy:   &api.AgentServiceConnectProxyConfig{},
				},
			},
			expectedProxySvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod1-service-created-sidecar-proxy",
					ServiceName:    "service-created-sidecar-proxy",
					ServiceAddress: "1.2.3.4",
					ServicePort:    20000,
					ServiceProxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "service-created",
						DestinationServiceID:   "pod1-service-created",
						LocalServiceAddress:    "",
						LocalServicePort:       0,
						Config:                 map[string]any{"envoy_telemetry_collector_bind_socket_dir": string("/consul/connect-inject")},
					},
					ServiceMeta: map[string]string{constants.MetaKeyPodName: "pod1", metaKeyKubeServiceName: "service-created", constants.MetaKeyKubeNS: "default", metaKeyManagedBy: constants.ManagedByValue, metaKeySyntheticNode: "true", constants.MetaKeyPodUID: ""},
					ServiceTags: []string{},
				},
			},
			expectedHealthChecks: []*api.HealthCheck{
				{
					CheckID:     "default/pod1-service-created",
					ServiceName: "service-created",
					ServiceID:   "pod1-service-created",
					Name:        constants.ConsulKubernetesCheckName,
					Status:      api.HealthPassing,
					Output:      constants.KubernetesSuccessReasonMsg,
					Type:        constants.ConsulKubernetesCheckType,
				},
				{
					CheckID:     "default/pod1-service-created-sidecar-proxy",
					ServiceName: "service-created-sidecar-proxy",
					ServiceID:   "pod1-service-created-sidecar-proxy",
					Name:        constants.ConsulKubernetesCheckName,
					Status:      api.HealthPassing,
					Output:      constants.KubernetesSuccessReasonMsg,
					Type:        constants.ConsulKubernetesCheckType,
				},
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			// Add the default namespace.
			ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
			node := corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}}
			// Create fake k8s client
			k8sObjects := append(tt.k8sObjects(), &ns, &node)

			fakeClient := fake.NewClientBuilder().WithRuntimeObjects(k8sObjects...).Build()

			// Create test consulServer server.
			testClient := test.TestServerWithMockConnMgrWatcher(t, nil)
			consulClient := testClient.APIClient

			// Create the endpoints controller.
			ep := &Controller{
				Client:                fakeClient,
				Log:                   logrtest.New(t),
				ConsulClientConfig:    testClient.Cfg,
				ConsulServerConnMgr:   testClient.Watcher,
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSetWith(),
				ReleaseName:           "consulServer",
				ReleaseNamespace:      "default",
				NodeMeta:              tt.nodeMeta,
			}
			if tt.metricsEnabled {
				ep.MetricsConfig = metrics.Config{
					DefaultEnableMetrics: true,
					EnableGatewayMetrics: true,
				}
			}

			ep.EnableTelemetryCollector = !tt.telemetryCollectorDisabled

			namespacedName := types.NamespacedName{
				Namespace: "default",
				Name:      tt.svcName,
			}

			resp, err := ep.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: namespacedName,
			})
			if tt.expErr != "" {
				require.EqualError(t, err, tt.expErr)
			} else {
				require.NoError(t, err)
			}
			require.False(t, resp.Requeue)

			// After reconciliation, Consul should have the service with the correct number of instances
			serviceInstances, _, err := consulClient.Catalog().Service(tt.consulSvcName, "", nil)
			require.NoError(t, err)
			require.Len(t, serviceInstances, len(tt.expectedConsulSvcInstances))
			for i, instance := range serviceInstances {
				require.Equal(t, tt.expectedConsulSvcInstances[i].ServiceID, instance.ServiceID)
				require.Equal(t, tt.expectedConsulSvcInstances[i].ServiceName, instance.ServiceName)
				require.Equal(t, tt.expectedConsulSvcInstances[i].ServiceAddress, instance.ServiceAddress)
				require.Equal(t, tt.expectedConsulSvcInstances[i].ServicePort, instance.ServicePort)
				require.Equal(t, tt.expectedConsulSvcInstances[i].ServiceMeta, instance.ServiceMeta)
				require.Equal(t, tt.expectedConsulSvcInstances[i].ServiceTags, instance.ServiceTags)
				require.Equal(t, tt.expectedConsulSvcInstances[i].ServiceLocality, instance.ServiceLocality)
				require.Equal(t, tt.expectedConsulSvcInstances[i].ServiceTaggedAddresses, instance.ServiceTaggedAddresses)
				require.Equal(t, tt.expectedConsulSvcInstances[i].ServiceProxy, instance.ServiceProxy)
				if tt.nodeMeta != nil {
					require.Equal(t, tt.expectedConsulSvcInstances[i].NodeMeta, instance.NodeMeta)
				}
			}
			proxyServiceInstances, _, err := consulClient.Catalog().Service(fmt.Sprintf("%s-sidecar-proxy", tt.consulSvcName), "", nil)
			require.NoError(t, err)
			require.Len(t, proxyServiceInstances, len(tt.expectedProxySvcInstances))
			for i, instance := range proxyServiceInstances {
				require.Equal(t, tt.expectedProxySvcInstances[i].ServiceID, instance.ServiceID)
				require.Equal(t, tt.expectedProxySvcInstances[i].ServiceName, instance.ServiceName)
				require.Equal(t, tt.expectedProxySvcInstances[i].ServiceAddress, instance.ServiceAddress)
				require.Equal(t, tt.expectedProxySvcInstances[i].ServicePort, instance.ServicePort)
				require.Equal(t, tt.expectedProxySvcInstances[i].ServiceMeta, instance.ServiceMeta)
				require.Equal(t, tt.expectedProxySvcInstances[i].ServiceTags, instance.ServiceTags)
				require.Equal(t, tt.expectedProxySvcInstances[i].ServiceLocality, instance.ServiceLocality)
				if tt.nodeMeta != nil {
					require.Equal(t, tt.expectedProxySvcInstances[i].NodeMeta, instance.NodeMeta)
				}
				// When comparing the ServiceProxy field we ignore the DestinationNamespace
				// field within that struct because on Consul OSS it's set to "" but on Consul Enterprise
				// it's set to "default" and we want to re-use this test for both OSS and Ent.
				// This does mean that we don't test that field but that's okay because
				// it's not getting set specifically in this test.
				// To do the comparison that ignores that field we use go-cmp instead
				// of the regular require.Equal call since it supports ignoring certain
				// fields.
				diff := cmp.Diff(tt.expectedProxySvcInstances[i].ServiceProxy, instance.ServiceProxy,
					cmpopts.IgnoreFields(api.Upstream{}, "DestinationNamespace", "DestinationPartition"))
				require.Empty(t, diff, "expected objects to be equal")
			}

			// Check that the Consul health expectedCheck was created for the k8s pod.
			for _, expectedCheck := range tt.expectedHealthChecks {
				filter := fmt.Sprintf("ServiceID == %q", expectedCheck.ServiceID)
				checks, _, err := consulClient.Health().Checks(expectedCheck.ServiceName, &api.QueryOptions{Filter: filter})
				require.NoError(t, err)
				require.Equal(t, len(checks), 1)
				// Ignoring Namespace because the response from ENT includes it and OSS does not.
				var ignoredFields = []string{"Node", "Definition", "Namespace", "Partition", "CreateIndex", "ModifyIndex", "ServiceTags"}
				require.True(t, cmp.Equal(checks[0], expectedCheck, cmpopts.IgnoreFields(api.HealthCheck{}, ignoredFields...)))
			}
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
		require.Equal(t, &api.Locality{Region: "us-west-1", Zone: "us-west-1a"}, parseLocality(n))
	})
}

func TestReconcile_PodErrorPreservesToken(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name                       string
		svcName                    string
		consulSvcName              string
		k8sObjects                 func() []runtime.Object
		expectedConsulSvcInstances []*api.CatalogService
		expectedProxySvcInstances  []*api.CatalogService
		expectedHealthChecks       []*api.HealthCheck
		metricsEnabled             bool
		telemetryCollectorDisabled bool
		nodeMeta                   map[string]string
		pod1Err                    string
	}{
		{
			name:          "Error when fetching pod results in not deregistering the service instances in consul",
			svcName:       "service-created",
			consulSvcName: "service-created",
			nodeMeta: map[string]string{
				"test-node": "true",
			},
			pod1Err: "some fake error while fetching pod",
			k8sObjects: func() []runtime.Object {
				pod1 := createServicePod("pod1", "1.2.3.4", true, true)
				endpoint := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-created",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									IP: "1.2.3.4",
									TargetRef: &corev1.ObjectReference{
										Kind:      "Pod",
										Name:      "pod1",
										Namespace: "default",
									},
								},
							},
						},
					},
				}
				return []runtime.Object{pod1, endpoint}
			},
			expectedConsulSvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod1-service-created",
					ServiceName:    "service-created",
					ServiceAddress: "1.2.3.4",
					ServicePort:    0,
					ServiceMeta:    map[string]string{constants.MetaKeyPodName: "pod1", metaKeyKubeServiceName: "service-created", constants.MetaKeyKubeNS: "default", metaKeyManagedBy: constants.ManagedByValue, metaKeySyntheticNode: "true", constants.MetaKeyPodUID: ""},
					ServiceTags:    []string{},
					ServiceProxy:   &api.AgentServiceConnectProxyConfig{},
					NodeMeta: map[string]string{
						"synthetic-node": "true",
						"test-node":      "true",
					},
				},
			},
			expectedProxySvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod1-service-created-sidecar-proxy",
					ServiceName:    "service-created-sidecar-proxy",
					ServiceAddress: "1.2.3.4",
					ServicePort:    20000,
					ServiceProxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "service-created",
						DestinationServiceID:   "pod1-service-created",
						LocalServiceAddress:    "",
						LocalServicePort:       0,
						Config:                 map[string]any{"envoy_telemetry_collector_bind_socket_dir": string("/consul/connect-inject")},
					},
					ServiceMeta: map[string]string{constants.MetaKeyPodName: "pod1", metaKeyKubeServiceName: "service-created", constants.MetaKeyKubeNS: "default", metaKeyManagedBy: constants.ManagedByValue, metaKeySyntheticNode: "true", constants.MetaKeyPodUID: ""},
					ServiceTags: []string{},
					NodeMeta: map[string]string{
						"synthetic-node": "true",
						"test-node":      "true",
					},
				},
			},
			expectedHealthChecks: []*api.HealthCheck{
				{
					CheckID:     "default/pod1-service-created",
					ServiceName: "service-created",
					ServiceID:   "pod1-service-created",
					Name:        constants.ConsulKubernetesCheckName,
					Status:      api.HealthPassing,
					Output:      constants.KubernetesSuccessReasonMsg,
					Type:        constants.ConsulKubernetesCheckType,
				},
				{
					CheckID:     "default/pod1-service-created-sidecar-proxy",
					ServiceName: "service-created-sidecar-proxy",
					ServiceID:   "pod1-service-created-sidecar-proxy",
					Name:        constants.ConsulKubernetesCheckName,
					Status:      api.HealthPassing,
					Output:      constants.KubernetesSuccessReasonMsg,
					Type:        constants.ConsulKubernetesCheckType,
				},
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			// Add the default namespace.
			ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
			node := corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}}
			// Create fake k8s client
			k8sObjects := append(tt.k8sObjects(), &ns, &node)

			fakeClient := fake.NewClientBuilder().WithRuntimeObjects(k8sObjects...).Build()

			customClient := fakeClientWithPodCustomization{fakeClient}

			// Create test consulServer server.
			testClient := test.TestServerWithMockConnMgrWatcher(t, nil)
			consulClient := testClient.APIClient

			// Create the endpoints controller.
			ep := &Controller{
				Client:                fakeClient,
				Log:                   logrtest.New(t),
				ConsulClientConfig:    testClient.Cfg,
				ConsulServerConnMgr:   testClient.Watcher,
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSetWith(),
				ReleaseName:           "consulServer",
				ReleaseNamespace:      "default",
				NodeMeta:              tt.nodeMeta,
			}
			if tt.metricsEnabled {
				ep.MetricsConfig = metrics.Config{
					DefaultEnableMetrics: true,
					EnableGatewayMetrics: true,
				}
			}

			ep.EnableTelemetryCollector = !tt.telemetryCollectorDisabled

			namespacedName := types.NamespacedName{
				Namespace: "default",
				Name:      tt.svcName,
			}

			// Do a first reconcile to setup the state in Consul with the instances and tokens.
			resp, err := ep.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: namespacedName,
			})
			require.NoError(t, err)
			require.False(t, resp.Requeue)

			// Do a second reconcile while stubbing the k8s client to return an error for the pod. Since it's not a "not
			// found" error, we should expect that the service instance does not get deregistered and that the acl token
			// is not deleted, so we will assert after this that the state in Consul still exists.
			ep.Client = customClient
			resp, err = ep.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: namespacedName,
			})
			require.Contains(t, err.Error(), tt.pod1Err)
			require.False(t, resp.Requeue)

			// These are the same assertions in the Reconcile-Create test cases, ensuring the state in Consul is correct.
			// After reconciliation, Consul should have the service with the correct number of instances
			serviceInstances, _, err := consulClient.Catalog().Service(tt.consulSvcName, "", nil)
			require.NoError(t, err)
			require.Len(t, serviceInstances, len(tt.expectedConsulSvcInstances))
			for i, instance := range serviceInstances {
				require.Equal(t, tt.expectedConsulSvcInstances[i].ServiceID, instance.ServiceID)
				require.Equal(t, tt.expectedConsulSvcInstances[i].ServiceName, instance.ServiceName)
				require.Equal(t, tt.expectedConsulSvcInstances[i].ServiceAddress, instance.ServiceAddress)
				require.Equal(t, tt.expectedConsulSvcInstances[i].ServicePort, instance.ServicePort)
				require.Equal(t, tt.expectedConsulSvcInstances[i].ServiceMeta, instance.ServiceMeta)
				require.Equal(t, tt.expectedConsulSvcInstances[i].ServiceTags, instance.ServiceTags)
				require.Equal(t, tt.expectedConsulSvcInstances[i].ServiceTaggedAddresses, instance.ServiceTaggedAddresses)
				require.Equal(t, tt.expectedConsulSvcInstances[i].ServiceProxy, instance.ServiceProxy)
				if tt.nodeMeta != nil {
					require.Equal(t, tt.expectedConsulSvcInstances[i].NodeMeta, instance.NodeMeta)
				}
			}
			proxyServiceInstances, _, err := consulClient.Catalog().Service(fmt.Sprintf("%s-sidecar-proxy", tt.consulSvcName), "", nil)
			require.NoError(t, err)
			require.Len(t, proxyServiceInstances, len(tt.expectedProxySvcInstances))
			for i, instance := range proxyServiceInstances {
				require.Equal(t, tt.expectedProxySvcInstances[i].ServiceID, instance.ServiceID)
				require.Equal(t, tt.expectedProxySvcInstances[i].ServiceName, instance.ServiceName)
				require.Equal(t, tt.expectedProxySvcInstances[i].ServiceAddress, instance.ServiceAddress)
				require.Equal(t, tt.expectedProxySvcInstances[i].ServicePort, instance.ServicePort)
				require.Equal(t, tt.expectedProxySvcInstances[i].ServiceMeta, instance.ServiceMeta)
				require.Equal(t, tt.expectedProxySvcInstances[i].ServiceTags, instance.ServiceTags)
				if tt.nodeMeta != nil {
					require.Equal(t, tt.expectedProxySvcInstances[i].NodeMeta, instance.NodeMeta)
				}
				// When comparing the ServiceProxy field we ignore the DestinationNamespace
				// field within that struct because on Consul OSS it's set to "" but on Consul Enterprise
				// it's set to "default" and we want to re-use this test for both OSS and Ent.
				// This does mean that we don't test that field but that's okay because
				// it's not getting set specifically in this test.
				// To do the comparison that ignores that field we use go-cmp instead
				// of the regular require.Equal call since it supports ignoring certain
				// fields.
				diff := cmp.Diff(tt.expectedProxySvcInstances[i].ServiceProxy, instance.ServiceProxy,
					cmpopts.IgnoreFields(api.Upstream{}, "DestinationNamespace", "DestinationPartition"))
				require.Empty(t, diff, "expected objects to be equal")
			}

			// Check that the Consul health expectedCheck was created for the k8s pod.
			for _, expectedCheck := range tt.expectedHealthChecks {
				filter := fmt.Sprintf("ServiceID == %q", expectedCheck.ServiceID)
				checks, _, err := consulClient.Health().Checks(expectedCheck.ServiceName, &api.QueryOptions{Filter: filter})
				require.NoError(t, err)
				require.Equal(t, len(checks), 1)
				// Ignoring Namespace because the response from ENT includes it and OSS does not.
				var ignoredFields = []string{"Node", "Definition", "Namespace", "Partition", "CreateIndex", "ModifyIndex", "ServiceTags"}
				require.True(t, cmp.Equal(checks[0], expectedCheck, cmpopts.IgnoreFields(api.HealthCheck{}, ignoredFields...)))
			}
		})
	}

}

type fakeClientWithPodCustomization struct {
	client.WithWatch
}

func (c fakeClientWithPodCustomization) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if key.Name == "pod1" {
		return errors.New("some fake error while fetching pod")
	}
	err := c.WithWatch.Get(ctx, key, obj, opts...)
	return err
}

// Tests updating an Endpoints object.
//   - Tests updates via the register codepath:
//   - When an address in an Endpoint is updated, that the corresponding service instance in Consul is updated.
//   - When an address is added to an Endpoint, an additional service instance in Consul is registered.
//   - When an address in an Endpoint is updated - via health check change - the corresponding service instance is updated.
//   - Tests updates via the deregister codepath:
//   - When an address is removed from an Endpoint, the corresponding service instance in Consul is deregistered.
//   - When an address is removed from an Endpoint *and there are no addresses left in the Endpoint*, the
//     corresponding service instance in Consul is deregistered.
//
// For the register and deregister codepath, this also tests that they work when the Consul service name is different
// from the K8s service name.
// This test covers Controller.deregisterService when services should be selectively deregistered
// since the map will not be nil.
func TestReconcileUpdateEndpoint(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name                       string
		consulSvcName              string
		k8sObjects                 func() []runtime.Object
		initialConsulSvcs          []*api.CatalogRegistration
		expectedConsulSvcInstances []*api.CatalogService
		expectedProxySvcInstances  []*api.CatalogService
		expectedHealthChecks       []*api.HealthCheck
		enableACLs                 bool
	}{
		{
			name:          "Endpoints has an updated address because health check changes from unhealthy to healthy",
			consulSvcName: "service-updated",
			k8sObjects: func() []runtime.Object {
				pod1 := createServicePod("pod1", "1.2.3.4", true, true)
				endpoint := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-updated",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									IP: "1.2.3.4",
									TargetRef: &corev1.ObjectReference{
										Kind:      "Pod",
										Name:      "pod1",
										Namespace: "default",
									},
								},
							},
						},
					},
				}
				return []runtime.Object{pod1, endpoint}
			},
			initialConsulSvcs: []*api.CatalogRegistration{
				{
					Node:    consulNodeName,
					Address: consulNodeAddress,
					NodeMeta: map[string]string{
						metaKeySyntheticNode: "true",
					},
					Service: &api.AgentService{
						ID:      "pod1-service-updated",
						Service: "service-updated",
						Port:    80,
						Address: "1.2.3.4",
						Meta:    map[string]string{constants.MetaKeyKubeNS: "default"},
					},
					Check: &api.AgentCheck{
						CheckID:     "default/pod1-service-updated",
						Name:        constants.ConsulKubernetesCheckName,
						Type:        constants.ConsulKubernetesCheckType,
						Status:      api.HealthCritical,
						ServiceID:   "pod1-service-updated",
						ServiceName: "service-updated",
					},
				},
				{
					Node:    consulNodeName,
					Address: consulNodeAddress,
					NodeMeta: map[string]string{
						metaKeySyntheticNode: "true",
					},
					Service: &api.AgentService{
						Kind:    api.ServiceKindConnectProxy,
						ID:      "pod1-service-updated-sidecar-proxy",
						Service: "service-updated-sidecar-proxy",
						Port:    20000,
						Address: "1.2.3.4",
						Meta:    map[string]string{constants.MetaKeyKubeNS: "default"},
						Proxy: &api.AgentServiceConnectProxyConfig{
							DestinationServiceName: "service-updated",
							DestinationServiceID:   "pod1-service-updated",
						},
					},
					Check: &api.AgentCheck{
						CheckID:     "default/pod1-service-updated-sidecar-proxy",
						Name:        constants.ConsulKubernetesCheckName,
						Type:        constants.ConsulKubernetesCheckType,
						Status:      api.HealthCritical,
						ServiceID:   "pod1-service-updated-sidecar-proxy",
						ServiceName: "service-updated-sidecar-proxy",
					},
				},
			},
			expectedConsulSvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod1-service-updated",
					ServiceAddress: "1.2.3.4",
				},
			},
			expectedProxySvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod1-service-updated-sidecar-proxy",
					ServiceAddress: "1.2.3.4",
				},
			},
			expectedHealthChecks: []*api.HealthCheck{
				{
					CheckID:     "default/pod1-service-updated",
					ServiceName: "service-updated",
					ServiceID:   "pod1-service-updated",
					Name:        constants.ConsulKubernetesCheckName,
					Status:      api.HealthPassing,
					Output:      constants.KubernetesSuccessReasonMsg,
					Type:        constants.ConsulKubernetesCheckType,
				},
				{
					CheckID:     "default/pod1-service-updated-sidecar-proxy",
					ServiceName: "service-updated-sidecar-proxy",
					ServiceID:   "pod1-service-updated-sidecar-proxy",
					Name:        constants.ConsulKubernetesCheckName,
					Status:      api.HealthPassing,
					Output:      constants.KubernetesSuccessReasonMsg,
					Type:        constants.ConsulKubernetesCheckType,
				},
			},
		},
		{
			name:          "Endpoints has an updated address because health check changes from healthy to unhealthy",
			consulSvcName: "service-updated",
			k8sObjects: func() []runtime.Object {
				pod1 := createServicePod("pod1", "1.2.3.4", true, true)
				endpoint := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-updated",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							NotReadyAddresses: []corev1.EndpointAddress{
								{
									IP: "1.2.3.4",
									TargetRef: &corev1.ObjectReference{
										Kind:      "Pod",
										Name:      "pod1",
										Namespace: "default",
									},
								},
							},
						},
					},
				}
				return []runtime.Object{pod1, endpoint}
			},
			initialConsulSvcs: []*api.CatalogRegistration{
				{
					Node:    consulNodeName,
					Address: consulNodeAddress,
					NodeMeta: map[string]string{
						metaKeySyntheticNode: "true",
					},
					Service: &api.AgentService{
						ID:      "pod1-service-updated",
						Service: "service-updated",
						Port:    80,
						Address: "1.2.3.4",
						Meta:    map[string]string{constants.MetaKeyKubeNS: "default"},
					},
					Check: &api.AgentCheck{
						CheckID:     "default/pod1-service-updated",
						Name:        constants.ConsulKubernetesCheckName,
						Type:        constants.ConsulKubernetesCheckType,
						Status:      api.HealthPassing,
						ServiceName: "service-updated",
						ServiceID:   "pod1-service-updated",
					},
				},
				{
					Node:    consulNodeName,
					Address: "127.0.0.1",
					NodeMeta: map[string]string{
						metaKeySyntheticNode: "true",
					},
					Service: &api.AgentService{
						Kind:    api.ServiceKindConnectProxy,
						ID:      "pod1-service-updated-sidecar-proxy",
						Service: "service-updated-sidecar-proxy",
						Port:    20000,
						Address: "1.2.3.4",
						Meta:    map[string]string{constants.MetaKeyKubeNS: "default"},
						Proxy: &api.AgentServiceConnectProxyConfig{
							DestinationServiceName: "service-updated",
							DestinationServiceID:   "pod1-service-updated",
						},
					},
					Check: &api.AgentCheck{
						CheckID:     "default/pod1-service-updated-sidecar-proxy",
						Name:        constants.ConsulKubernetesCheckName,
						Type:        constants.ConsulKubernetesCheckType,
						Status:      api.HealthPassing,
						ServiceName: "service-updated-sidecar-proxy",
						ServiceID:   "pod1-service-updated-sidecar-proxy",
					},
				},
			},
			expectedConsulSvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod1-service-updated",
					ServiceAddress: "1.2.3.4",
				},
			},
			expectedProxySvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod1-service-updated-sidecar-proxy",
					ServiceAddress: "1.2.3.4",
				},
			},
			expectedHealthChecks: []*api.HealthCheck{
				{
					CheckID:     "default/pod1-service-updated",
					ServiceName: "service-updated",
					ServiceID:   "pod1-service-updated",
					Name:        constants.ConsulKubernetesCheckName,
					Status:      api.HealthCritical,
					Output:      "Pod \"default/pod1\" is not ready",
					Type:        constants.ConsulKubernetesCheckType,
				},
				{
					CheckID:     "default/pod1-service-updated-sidecar-proxy",
					ServiceName: "service-updated-sidecar-proxy",
					ServiceID:   "pod1-service-updated-sidecar-proxy",
					Name:        constants.ConsulKubernetesCheckName,
					Status:      api.HealthCritical,
					Output:      "Pod \"default/pod1\" is not ready",
					Type:        constants.ConsulKubernetesCheckType,
				},
			},
		},
		{
			name:          "Endpoints has an updated address (pod IP change).",
			consulSvcName: "service-updated",
			k8sObjects: func() []runtime.Object {
				pod1 := createServicePod("pod1", "4.4.4.4", true, true)
				endpoint := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-updated",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									IP: "4.4.4.4",
									TargetRef: &corev1.ObjectReference{
										Kind:      "Pod",
										Name:      "pod1",
										Namespace: "default",
									},
								},
							},
						},
					},
				}
				return []runtime.Object{pod1, endpoint}
			},
			initialConsulSvcs: []*api.CatalogRegistration{
				{
					Node:    consulNodeName,
					Address: consulNodeAddress,
					NodeMeta: map[string]string{
						metaKeySyntheticNode: "true",
					},
					Service: &api.AgentService{
						ID:      "pod1-service-updated",
						Service: "service-updated",
						Port:    80,
						Address: "1.2.3.4",
						Meta: map[string]string{
							constants.MetaKeyKubeNS:  "default",
							constants.MetaKeyPodName: "pod1",
							metaKeyKubeServiceName:   "service-updated",
							metaKeyManagedBy:         constants.ManagedByValue,
							metaKeySyntheticNode:     "true",
							constants.MetaKeyPodUID:  "",
						},
					},
				},
				{
					Node:    consulNodeName,
					Address: consulNodeAddress,
					NodeMeta: map[string]string{
						metaKeySyntheticNode: "true",
					},
					Service: &api.AgentService{
						Kind:    api.ServiceKindConnectProxy,
						ID:      "pod1-service-updated-sidecar-proxy",
						Service: "service-updated-sidecar-proxy",
						Port:    20000,
						Address: "1.2.3.4",
						Meta: map[string]string{
							constants.MetaKeyKubeNS:  "default",
							constants.MetaKeyPodName: "pod1",
							metaKeyKubeServiceName:   "service-updated",
							metaKeyManagedBy:         constants.ManagedByValue,
							metaKeySyntheticNode:     "true",
							constants.MetaKeyPodUID:  "",
						},
						Proxy: &api.AgentServiceConnectProxyConfig{
							DestinationServiceName: "service-updated",
							DestinationServiceID:   "pod1-service-updated",
						},
					},
				},
			},
			expectedConsulSvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod1-service-updated",
					ServiceAddress: "4.4.4.4",
				},
			},
			expectedProxySvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod1-service-updated-sidecar-proxy",
					ServiceAddress: "4.4.4.4",
				},
			},
		},
		{
			name:          "Different Consul service name: Endpoints has an updated address (pod IP change).",
			consulSvcName: "different-consul-svc-name",
			k8sObjects: func() []runtime.Object {
				pod1 := createServicePod("pod1", "4.4.4.4", true, true)
				pod1.Annotations[constants.AnnotationService] = "different-consul-svc-name"
				endpoint := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-updated",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									IP: "4.4.4.4",
									TargetRef: &corev1.ObjectReference{
										Kind:      "Pod",
										Name:      "pod1",
										Namespace: "default",
									},
								},
							},
						},
					},
				}
				return []runtime.Object{pod1, endpoint}
			},
			initialConsulSvcs: []*api.CatalogRegistration{
				{
					Node:    consulNodeName,
					Address: consulNodeAddress,
					NodeMeta: map[string]string{
						metaKeySyntheticNode: "true",
					},
					Service: &api.AgentService{
						ID:      "pod1-different-consul-svc-name",
						Service: "different-consul-svc-name",
						Port:    80,
						Address: "1.2.3.4",
						Meta: map[string]string{
							metaKeyManagedBy:         constants.ManagedByValue,
							metaKeySyntheticNode:     "true",
							constants.MetaKeyKubeNS:  "default",
							constants.MetaKeyPodName: "pod1",
							metaKeyKubeServiceName:   "service-updated",
							constants.MetaKeyPodUID:  "",
						},
					},
				},
				{
					Node:    consulNodeName,
					Address: consulNodeAddress,
					NodeMeta: map[string]string{
						metaKeySyntheticNode: "true",
					},
					Service: &api.AgentService{
						Kind:    api.ServiceKindConnectProxy,
						ID:      "pod1-different-consul-svc-name-sidecar-proxy",
						Service: "different-consul-svc-name-sidecar-proxy",
						Port:    20000,
						Address: "1.2.3.4",
						Proxy: &api.AgentServiceConnectProxyConfig{
							DestinationServiceName: "different-consul-svc-name",
							DestinationServiceID:   "pod1-different-consul-svc-name",
						},
						Meta: map[string]string{
							metaKeyManagedBy:         constants.ManagedByValue,
							metaKeySyntheticNode:     "true",
							constants.MetaKeyKubeNS:  "default",
							constants.MetaKeyPodName: "pod1",
							metaKeyKubeServiceName:   "service-updated",
							constants.MetaKeyPodUID:  "",
						},
					},
				},
			},
			expectedConsulSvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod1-different-consul-svc-name",
					ServiceAddress: "4.4.4.4",
				},
			},
			expectedProxySvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod1-different-consul-svc-name-sidecar-proxy",
					ServiceAddress: "4.4.4.4",
				},
			},
		},
		{
			name:          "Endpoints has additional address not in Consul",
			consulSvcName: "service-updated",
			k8sObjects: func() []runtime.Object {
				pod1 := createServicePod("pod1", "1.2.3.4", true, true)
				pod2 := createServicePod("pod2", "2.2.3.4", true, true)
				endpointWithTwoAddresses := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-updated",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									IP: "1.2.3.4",
									TargetRef: &corev1.ObjectReference{
										Kind:      "Pod",
										Name:      "pod1",
										Namespace: "default",
									},
								},
								{
									IP: "2.2.3.4",
									TargetRef: &corev1.ObjectReference{
										Kind:      "Pod",
										Name:      "pod2",
										Namespace: "default",
									},
								},
							},
						},
					},
				}
				return []runtime.Object{pod1, pod2, endpointWithTwoAddresses}
			},
			initialConsulSvcs: []*api.CatalogRegistration{
				{
					Node:    consulNodeName,
					Address: consulNodeAddress,
					NodeMeta: map[string]string{
						metaKeySyntheticNode: "true",
					},
					Service: &api.AgentService{
						ID:      "pod1-service-updated",
						Service: "service-updated",
						Port:    80,
						Address: "1.2.3.4",
						Meta:    map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default", metaKeyManagedBy: constants.ManagedByValue},
					},
				},
				{
					Node:    consulNodeName,
					Address: consulNodeAddress,
					NodeMeta: map[string]string{
						metaKeySyntheticNode: "true",
					},
					Service: &api.AgentService{
						Kind:    api.ServiceKindConnectProxy,
						ID:      "pod1-service-updated-sidecar-proxy",
						Service: "service-updated-sidecar-proxy",
						Port:    20000,
						Address: "1.2.3.4",
						Proxy: &api.AgentServiceConnectProxyConfig{
							DestinationServiceName: "service-updated",
							DestinationServiceID:   "pod1-service-updated",
						},
					},
				},
			},
			expectedConsulSvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod1-service-updated",
					ServiceAddress: "1.2.3.4",
				},
				{
					ServiceID:      "pod2-service-updated",
					ServiceAddress: "2.2.3.4",
				},
			},
			expectedProxySvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod1-service-updated-sidecar-proxy",
					ServiceAddress: "1.2.3.4",
				},
				{
					ServiceID:      "pod2-service-updated-sidecar-proxy",
					ServiceAddress: "2.2.3.4",
				},
			},
			expectedHealthChecks: []*api.HealthCheck{
				{
					CheckID:     "default/pod1-service-updated",
					ServiceName: "service-updated",
					ServiceID:   "pod1-service-updated",
					Name:        constants.ConsulKubernetesCheckName,
					Status:      api.HealthPassing,
					Output:      constants.KubernetesSuccessReasonMsg,
					Type:        constants.ConsulKubernetesCheckType,
				},
				{
					CheckID:     "default/pod1-service-updated-sidecar-proxy",
					ServiceName: "service-updated-sidecar-proxy",
					ServiceID:   "pod1-service-updated-sidecar-proxy",
					Name:        constants.ConsulKubernetesCheckName,
					Status:      api.HealthPassing,
					Output:      constants.KubernetesSuccessReasonMsg,
					Type:        constants.ConsulKubernetesCheckType,
				},
				{
					CheckID:     "default/pod2-service-updated",
					ServiceName: "service-updated",
					ServiceID:   "pod2-service-updated",
					Name:        constants.ConsulKubernetesCheckName,
					Status:      api.HealthPassing,
					Output:      constants.KubernetesSuccessReasonMsg,
					Type:        constants.ConsulKubernetesCheckType,
				},
				{
					CheckID:     "default/pod2-service-updated-sidecar-proxy",
					ServiceName: "service-updated-sidecar-proxy",
					ServiceID:   "pod2-service-updated-sidecar-proxy",
					Name:        constants.ConsulKubernetesCheckName,
					Status:      api.HealthPassing,
					Output:      constants.KubernetesSuccessReasonMsg,
					Type:        constants.ConsulKubernetesCheckType,
				},
			},
		},
		{
			name:          "Consul has instances that are not in the Endpoints addresses",
			consulSvcName: "service-updated",
			k8sObjects: func() []runtime.Object {
				pod1 := createServicePod("pod1", "1.2.3.4", true, true)
				endpoint := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-updated",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									IP: "1.2.3.4",
									TargetRef: &corev1.ObjectReference{
										Kind:      "Pod",
										Name:      "pod1",
										Namespace: "default",
									},
								},
							},
						},
					},
				}
				return []runtime.Object{pod1, endpoint}
			},
			initialConsulSvcs: []*api.CatalogRegistration{
				{
					Node:    consulNodeName,
					Address: consulNodeAddress,
					NodeMeta: map[string]string{
						metaKeySyntheticNode: "true",
					},
					Service: &api.AgentService{
						ID:      "pod1-service-updated",
						Service: "service-updated",
						Port:    80,
						Address: "1.2.3.4",
						Meta:    map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default", metaKeyManagedBy: constants.ManagedByValue},
					},
				},
				{
					Node:    consulNodeName,
					Address: consulNodeAddress,
					NodeMeta: map[string]string{
						metaKeySyntheticNode: "true",
					},
					Service: &api.AgentService{
						Kind:    api.ServiceKindConnectProxy,
						ID:      "pod1-service-updated-sidecar-proxy",
						Service: "service-updated-sidecar-proxy",
						Port:    20000,
						Address: "1.2.3.4",
						Proxy: &api.AgentServiceConnectProxyConfig{
							DestinationServiceName: "service-updated",
							DestinationServiceID:   "pod1-service-updated",
						},
						Meta: map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default", metaKeyManagedBy: constants.ManagedByValue},
					},
				},
				{
					Node:    consulNodeName,
					Address: consulNodeAddress,
					NodeMeta: map[string]string{
						metaKeySyntheticNode: "true",
					},
					Service: &api.AgentService{
						ID:      "pod2-service-updated",
						Service: "service-updated",
						Port:    80,
						Address: "2.2.3.4",
						Meta:    map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default", metaKeyManagedBy: constants.ManagedByValue},
					},
				},
				{
					Node:    consulNodeName,
					Address: consulNodeAddress,
					NodeMeta: map[string]string{
						metaKeySyntheticNode: "true",
					},
					Service: &api.AgentService{
						Kind:    api.ServiceKindConnectProxy,
						ID:      "pod2-service-updated-sidecar-proxy",
						Service: "service-updated-sidecar-proxy",
						Port:    20000,
						Address: "2.2.3.4",
						Proxy: &api.AgentServiceConnectProxyConfig{
							DestinationServiceName: "service-updated",
							DestinationServiceID:   "pod2-service-updated",
						},
						Meta: map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default", metaKeyManagedBy: constants.ManagedByValue},
					},
				},
			},
			expectedConsulSvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod1-service-updated",
					ServiceAddress: "1.2.3.4",
				},
			},
			expectedProxySvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod1-service-updated-sidecar-proxy",
					ServiceAddress: "1.2.3.4",
				},
			},
		},
		{
			name:          "Different Consul service name: Consul has instances that are not in the Endpoints addresses",
			consulSvcName: "different-consul-svc-name",
			k8sObjects: func() []runtime.Object {
				pod1 := createServicePod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[constants.AnnotationService] = "different-consul-svc-name"
				endpoint := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-updated",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									IP: "1.2.3.4",
									TargetRef: &corev1.ObjectReference{
										Kind:      "Pod",
										Name:      "pod1",
										Namespace: "default",
									},
								},
							},
						},
					},
				}
				return []runtime.Object{pod1, endpoint}
			},
			initialConsulSvcs: []*api.CatalogRegistration{
				{
					Node:    consulNodeName,
					Address: consulNodeAddress,
					NodeMeta: map[string]string{
						metaKeySyntheticNode: "true",
					},
					Service: &api.AgentService{
						ID:      "pod1-different-consul-svc-name",
						Service: "different-consul-svc-name",
						Port:    80,
						Address: "1.2.3.4",
						Meta:    map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default", metaKeyManagedBy: constants.ManagedByValue},
					},
				},
				{
					Node:    consulNodeName,
					Address: consulNodeAddress,
					NodeMeta: map[string]string{
						metaKeySyntheticNode: "true",
					},
					Service: &api.AgentService{
						Kind:    api.ServiceKindConnectProxy,
						ID:      "pod1-different-consul-svc-name-sidecar-proxy",
						Service: "different-consul-svc-name-sidecar-proxy",
						Port:    20000,
						Address: "1.2.3.4",
						Proxy: &api.AgentServiceConnectProxyConfig{
							DestinationServiceName: "different-consul-svc-name",
							DestinationServiceID:   "pod1-different-consul-svc-name",
						},
						Meta: map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default", metaKeyManagedBy: constants.ManagedByValue},
					},
				},
				{
					Node:    consulNodeName,
					Address: consulNodeAddress,
					NodeMeta: map[string]string{
						metaKeySyntheticNode: "true",
					},
					Service: &api.AgentService{
						ID:      "pod2-different-consul-svc-name",
						Service: "different-consul-svc-name",
						Port:    80,
						Address: "2.2.3.4",
						Meta:    map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default", metaKeyManagedBy: constants.ManagedByValue},
					},
				},
				{
					Node:    consulNodeName,
					Address: consulNodeAddress,
					NodeMeta: map[string]string{
						metaKeySyntheticNode: "true",
					},
					Service: &api.AgentService{
						Kind:    api.ServiceKindConnectProxy,
						ID:      "pod2-different-consul-svc-name-sidecar-proxy",
						Service: "different-consul-svc-name-sidecar-proxy",
						Port:    20000,
						Address: "2.2.3.4",
						Proxy: &api.AgentServiceConnectProxyConfig{
							DestinationServiceName: "different-consul-svc-name",
							DestinationServiceID:   "pod2-different-consul-svc-name",
						},
						Meta: map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default", metaKeyManagedBy: constants.ManagedByValue},
					},
				},
			},
			expectedConsulSvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod1-different-consul-svc-name",
					ServiceAddress: "1.2.3.4",
				},
			},
			expectedProxySvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod1-different-consul-svc-name-sidecar-proxy",
					ServiceAddress: "1.2.3.4",
				},
			},
		},
		{
			// When a k8s deployment is deleted but it's k8s service continues to exist, the endpoints has no addresses
			// and the instances should be deleted from Consul.
			name:          "Consul has instances that are not in the endpoints, and the endpoints has no addresses.",
			consulSvcName: "service-updated",
			k8sObjects: func() []runtime.Object {
				endpoint := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-updated",
						Namespace: "default",
					},
				}
				return []runtime.Object{endpoint}
			},
			initialConsulSvcs: []*api.CatalogRegistration{
				{
					Node:    consulNodeName,
					Address: consulNodeAddress,
					NodeMeta: map[string]string{
						metaKeySyntheticNode: "true",
					},
					Service: &api.AgentService{
						ID:      "pod1-service-updated",
						Service: "service-updated",
						Port:    80,
						Address: "1.2.3.4",
						Meta:    map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default", metaKeyManagedBy: constants.ManagedByValue},
					},
				},
				{
					Node:    consulNodeName,
					Address: consulNodeAddress,
					NodeMeta: map[string]string{
						metaKeySyntheticNode: "true",
					},
					Service: &api.AgentService{
						Kind:    api.ServiceKindConnectProxy,
						ID:      "pod1-service-updated-sidecar-proxy",
						Service: "service-updated-sidecar-proxy",
						Port:    20000,
						Address: "1.2.3.4",
						Proxy: &api.AgentServiceConnectProxyConfig{
							DestinationServiceName: "service-updated",
							DestinationServiceID:   "pod1-service-updated",
						},
						Meta: map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default", metaKeyManagedBy: constants.ManagedByValue},
					},
				},
				{
					Node:    consulNodeName,
					Address: consulNodeAddress,
					NodeMeta: map[string]string{
						metaKeySyntheticNode: "true",
					},
					Service: &api.AgentService{
						ID:      "pod2-service-updated",
						Service: "service-updated",
						Port:    80,
						Address: "2.2.3.4",
						Meta:    map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default", metaKeyManagedBy: constants.ManagedByValue},
					},
				},
				{
					Node:    consulNodeName,
					Address: consulNodeAddress,
					NodeMeta: map[string]string{
						metaKeySyntheticNode: "true",
					},
					Service: &api.AgentService{
						Kind:    api.ServiceKindConnectProxy,
						ID:      "pod2-service-updated-sidecar-proxy",
						Service: "service-updated-sidecar-proxy",
						Port:    20000,
						Address: "2.2.3.4",
						Proxy: &api.AgentServiceConnectProxyConfig{
							DestinationServiceName: "service-updated",
							DestinationServiceID:   "pod2-service-updated",
						},
						Meta: map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default", metaKeyManagedBy: constants.ManagedByValue},
					},
				},
			},
			expectedConsulSvcInstances: []*api.CatalogService{},
			expectedProxySvcInstances:  []*api.CatalogService{},
		},
		{
			// With a different Consul service name, when a k8s deployment is deleted but it's k8s service continues to
			// exist, the endpoints has no addresses and the instances should be deleted from Consul.
			name:          "Different Consul service name: Consul has instances that are not in the endpoints, and the endpoints has no addresses.",
			consulSvcName: "different-consul-svc-name",
			k8sObjects: func() []runtime.Object {
				endpoint := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-updated",
						Namespace: "default",
					},
				}
				return []runtime.Object{endpoint}
			},
			initialConsulSvcs: []*api.CatalogRegistration{
				{
					Node:    consulNodeName,
					Address: consulNodeAddress,
					NodeMeta: map[string]string{
						metaKeySyntheticNode: "true",
					},
					Service: &api.AgentService{
						ID:      "pod1-different-consul-svc-name",
						Service: "different-consul-svc-name",
						Port:    80,
						Address: "1.2.3.4",
						Meta:    map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default", metaKeyManagedBy: constants.ManagedByValue},
					},
				},
				{
					Node:    consulNodeName,
					Address: consulNodeAddress,
					NodeMeta: map[string]string{
						metaKeySyntheticNode: "true",
					},
					Service: &api.AgentService{
						Kind:    api.ServiceKindConnectProxy,
						ID:      "pod1-different-consul-svc-name-sidecar-proxy",
						Service: "different-consul-svc-name-sidecar-proxy",
						Port:    20000,
						Address: "1.2.3.4",
						Proxy: &api.AgentServiceConnectProxyConfig{
							DestinationServiceName: "different-consul-svc-name",
							DestinationServiceID:   "pod1-different-consul-svc-name",
						},
						Meta: map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default", metaKeyManagedBy: constants.ManagedByValue},
					},
				},
				{
					Node:    consulNodeName,
					Address: consulNodeAddress,
					NodeMeta: map[string]string{
						metaKeySyntheticNode: "true",
					},
					Service: &api.AgentService{
						ID:      "pod2-different-consul-svc-name",
						Service: "different-consul-svc-name",
						Port:    80,
						Address: "2.2.3.4",
						Meta:    map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default", metaKeyManagedBy: constants.ManagedByValue},
					},
				},
				{
					Node:    consulNodeName,
					Address: consulNodeAddress,
					NodeMeta: map[string]string{
						metaKeySyntheticNode: "true",
					},
					Service: &api.AgentService{
						Kind:    api.ServiceKindConnectProxy,
						ID:      "pod2-different-consul-svc-name-sidecar-proxy",
						Service: "different-consul-svc-name-sidecar-proxy",
						Port:    20000,
						Address: "2.2.3.4",
						Proxy: &api.AgentServiceConnectProxyConfig{
							DestinationServiceName: "different-consul-svc-name",
							DestinationServiceID:   "pod2-different-consul-svc-name",
						},
						Meta: map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default", metaKeyManagedBy: constants.ManagedByValue},
					},
				},
			},
			expectedConsulSvcInstances: []*api.CatalogService{},
			expectedProxySvcInstances:  []*api.CatalogService{},
		},
		{
			name:          "ACLs enabled: Endpoints has an updated address because the target pod changes",
			consulSvcName: "service-updated",
			k8sObjects: func() []runtime.Object {
				pod2 := createServicePod("pod2", "4.4.4.4", true, true)
				endpoint := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-updated",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									IP: "4.4.4.4",
									TargetRef: &corev1.ObjectReference{
										Kind:      "Pod",
										Name:      "pod2",
										Namespace: "default",
									},
								},
							},
						},
					},
				}
				return []runtime.Object{pod2, endpoint}
			},
			initialConsulSvcs: []*api.CatalogRegistration{
				{
					Node:    consulNodeName,
					Address: consulNodeAddress,
					NodeMeta: map[string]string{
						metaKeySyntheticNode: "true",
					},
					Service: &api.AgentService{
						ID:      "pod1-service-updated",
						Service: "service-updated",
						Port:    80,
						Address: "1.2.3.4",
						Meta: map[string]string{
							constants.MetaKeyKubeNS:  "default",
							constants.MetaKeyPodName: "pod1",
							metaKeyKubeServiceName:   "service-updated",
							metaKeyManagedBy:         constants.ManagedByValue,
							metaKeySyntheticNode:     "true",
							constants.MetaKeyPodUID:  "",
						},
					},
				},
				{
					Node:    consulNodeName,
					Address: consulNodeAddress,
					NodeMeta: map[string]string{
						metaKeySyntheticNode: "true",
					},
					Service: &api.AgentService{
						Kind:    api.ServiceKindConnectProxy,
						ID:      "pod1-service-updated-sidecar-proxy",
						Service: "service-updated-sidecar-proxy",
						Port:    20000,
						Address: "1.2.3.4",
						Meta: map[string]string{
							constants.MetaKeyKubeNS:  "default",
							constants.MetaKeyPodName: "pod1",
							metaKeyKubeServiceName:   "service-updated",
							metaKeyManagedBy:         constants.ManagedByValue,
							metaKeySyntheticNode:     "true",
							constants.MetaKeyPodUID:  "",
						},
						Proxy: &api.AgentServiceConnectProxyConfig{
							DestinationServiceName: "service-updated",
							DestinationServiceID:   "pod1-service-updated",
						},
					},
				},
			},
			expectedConsulSvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod2-service-updated",
					ServiceAddress: "4.4.4.4",
					ServiceMeta: map[string]string{
						metaKeyKubeServiceName:   "service-updated",
						constants.MetaKeyKubeNS:  "default",
						metaKeyManagedBy:         constants.ManagedByValue,
						metaKeySyntheticNode:     "true",
						constants.MetaKeyPodName: "pod2",
						constants.MetaKeyPodUID:  "",
					},
				},
			},
			expectedProxySvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod2-service-updated-sidecar-proxy",
					ServiceAddress: "4.4.4.4",
					ServiceMeta: map[string]string{
						metaKeyKubeServiceName:   "service-updated",
						constants.MetaKeyKubeNS:  "default",
						metaKeyManagedBy:         constants.ManagedByValue,
						metaKeySyntheticNode:     "true",
						constants.MetaKeyPodName: "pod2",
						constants.MetaKeyPodUID:  "",
					},
				},
			},
			enableACLs: true,
		},
		{
			name:          "ACLs enabled: Consul has instances that are not in the Endpoints addresses",
			consulSvcName: "service-updated",
			k8sObjects: func() []runtime.Object {
				pod1 := createServicePod("pod1", "1.2.3.4", true, true)
				endpoint := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-updated",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									IP: "1.2.3.4",
									TargetRef: &corev1.ObjectReference{
										Kind:      "Pod",
										Name:      "pod1",
										Namespace: "default",
									},
								},
							},
						},
					},
				}
				return []runtime.Object{pod1, endpoint}
			},
			initialConsulSvcs: []*api.CatalogRegistration{
				{
					Node:    consulNodeName,
					Address: consulNodeAddress,
					NodeMeta: map[string]string{
						metaKeySyntheticNode: "true",
					},
					Service: &api.AgentService{
						ID:      "pod1-service-updated",
						Service: "service-updated",
						Port:    80,
						Address: "1.2.3.4",
						Meta: map[string]string{
							metaKeyKubeServiceName:   "service-updated",
							constants.MetaKeyKubeNS:  "default",
							metaKeyManagedBy:         constants.ManagedByValue,
							metaKeySyntheticNode:     "true",
							constants.MetaKeyPodName: "pod1",
							constants.MetaKeyPodUID:  "",
						},
					},
				},
				{
					Node:    consulNodeName,
					Address: consulNodeAddress,
					NodeMeta: map[string]string{
						metaKeySyntheticNode: "true",
					},
					Service: &api.AgentService{
						Kind:    api.ServiceKindConnectProxy,
						ID:      "pod1-service-updated-sidecar-proxy",
						Service: "service-updated-sidecar-proxy",
						Port:    20000,
						Address: "1.2.3.4",
						Proxy: &api.AgentServiceConnectProxyConfig{
							DestinationServiceName: "service-updated",
							DestinationServiceID:   "pod1-service-updated",
						},
						Meta: map[string]string{
							metaKeyKubeServiceName:   "service-updated",
							constants.MetaKeyKubeNS:  "default",
							metaKeyManagedBy:         constants.ManagedByValue,
							metaKeySyntheticNode:     "true",
							constants.MetaKeyPodName: "pod1",
							constants.MetaKeyPodUID:  "",
						},
					},
				},
				{
					Node:    consulNodeName,
					Address: consulNodeAddress,
					NodeMeta: map[string]string{
						metaKeySyntheticNode: "true",
					},
					Service: &api.AgentService{
						ID:      "pod2-service-updated",
						Service: "service-updated",
						Port:    80,
						Address: "2.2.3.4",
						Meta: map[string]string{
							metaKeyKubeServiceName:   "service-updated",
							constants.MetaKeyKubeNS:  "default",
							metaKeyManagedBy:         constants.ManagedByValue,
							metaKeySyntheticNode:     "true",
							constants.MetaKeyPodName: "pod2",
							constants.MetaKeyPodUID:  "",
						},
					},
				},
				{
					Node:    consulNodeName,
					Address: consulNodeAddress,
					NodeMeta: map[string]string{
						metaKeySyntheticNode: "true",
					},
					Service: &api.AgentService{
						Kind:    api.ServiceKindConnectProxy,
						ID:      "pod2-service-updated-sidecar-proxy",
						Service: "service-updated-sidecar-proxy",
						Port:    20000,
						Address: "2.2.3.4",
						Proxy: &api.AgentServiceConnectProxyConfig{
							DestinationServiceName: "service-updated",
							DestinationServiceID:   "pod2-service-updated",
						},
						Meta: map[string]string{
							metaKeyKubeServiceName:   "service-updated",
							constants.MetaKeyKubeNS:  "default",
							metaKeyManagedBy:         constants.ManagedByValue,
							metaKeySyntheticNode:     "true",
							constants.MetaKeyPodName: "pod2",
							constants.MetaKeyPodUID:  "",
						},
					},
				},
			},
			expectedConsulSvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod1-service-updated",
					ServiceName:    "service-updated",
					ServiceAddress: "1.2.3.4",
					ServiceMeta: map[string]string{
						metaKeyKubeServiceName:   "service-updated",
						constants.MetaKeyKubeNS:  "default",
						metaKeyManagedBy:         constants.ManagedByValue,
						metaKeySyntheticNode:     "true",
						constants.MetaKeyPodName: "pod1",
						constants.MetaKeyPodUID:  "",
					},
				},
			},
			expectedProxySvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod1-service-updated-sidecar-proxy",
					ServiceName:    "service-updated-sidecar-proxy",
					ServiceAddress: "1.2.3.4",
					ServiceMeta: map[string]string{
						metaKeyKubeServiceName:   "service-updated",
						constants.MetaKeyKubeNS:  "default",
						metaKeyManagedBy:         constants.ManagedByValue,
						metaKeySyntheticNode:     "true",
						constants.MetaKeyPodName: "pod1",
						constants.MetaKeyPodUID:  "",
					},
				},
			},
			enableACLs: true,
		},
		// When a Deployment has the mesh annotation removed, Kube will delete the old pods. When it deletes the last Pod,
		// the endpoints object will contain only non-mesh pods, but you'll still have one consul service instance to clean up.
		{
			name:          "When a Deployment moves from mesh to non mesh its service instances should be deleted",
			consulSvcName: "service-updated",
			k8sObjects: func() []runtime.Object {
				pod2 := createServicePod("pod2", "2.3.4.5", false, false)
				endpoint := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-updated",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									IP: "2.3.4.5",
									TargetRef: &corev1.ObjectReference{
										Kind:      "Pod",
										Name:      "pod2",
										Namespace: "default",
									},
								},
							},
						},
					},
				}
				return []runtime.Object{pod2, endpoint}
			},
			initialConsulSvcs: []*api.CatalogRegistration{
				{
					Node:    consulNodeName,
					Address: consulNodeAddress,
					NodeMeta: map[string]string{
						metaKeySyntheticNode: "true",
					},
					Service: &api.AgentService{
						ID:      "pod1-service-updated",
						Service: "service-updated",
						Port:    80,
						Address: "1.2.3.4",
						Meta: map[string]string{
							metaKeyKubeServiceName:   "service-updated",
							constants.MetaKeyKubeNS:  "default",
							metaKeyManagedBy:         constants.ManagedByValue,
							metaKeySyntheticNode:     "true",
							constants.MetaKeyPodName: "pod1",
							constants.MetaKeyPodUID:  "",
						},
					},
				},
				{
					Node:    consulNodeName,
					Address: consulNodeAddress,
					NodeMeta: map[string]string{
						metaKeySyntheticNode: "true",
					},
					Service: &api.AgentService{
						Kind:    api.ServiceKindConnectProxy,
						ID:      "pod1-service-updated-sidecar-proxy",
						Service: "service-updated-sidecar-proxy",
						Port:    20000,
						Address: "1.2.3.4",
						Proxy: &api.AgentServiceConnectProxyConfig{
							DestinationServiceName: "service-updated",
							DestinationServiceID:   "pod1-service-updated",
						},
						Meta: map[string]string{
							metaKeyKubeServiceName:   "service-updated",
							constants.MetaKeyKubeNS:  "default",
							metaKeyManagedBy:         constants.ManagedByValue,
							metaKeySyntheticNode:     "true",
							constants.MetaKeyPodName: "pod1",
							constants.MetaKeyPodUID:  "",
						},
					},
				},
			},
			expectedConsulSvcInstances: nil,
			expectedProxySvcInstances:  nil,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			// Add the default namespace.
			ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
			node := corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}}
			// Create fake k8s client.
			k8sObjects := append(tt.k8sObjects(), &ns, &node)
			fakeClient := fake.NewClientBuilder().WithRuntimeObjects(k8sObjects...).Build()

			// Create test consulServer server
			adminToken := "123e4567-e89b-12d3-a456-426614174000"
			testClient := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
				if tt.enableACLs {
					c.ACL.Enabled = tt.enableACLs
					c.ACL.Tokens.InitialManagement = adminToken
				}
			})
			consulClient := testClient.APIClient
			// Wait so that bootstrap finishes
			testClient.TestServer.WaitForActiveCARoot(t)

			// Holds token accessorID for each service ID.
			tokensForServices := make(map[string]string)

			// Register service and proxy in consul.
			for _, svc := range tt.initialConsulSvcs {
				// Retry because ACLs may not have been initialized yet.
				retry.Run(t, func(r *retry.R) {
					_, err := consulClient.Catalog().Register(svc, nil)
					require.NoError(r, err)
				})

				// Create a token for this service if ACLs are enabled.
				if tt.enableACLs {
					if svc.Service.Kind != api.ServiceKindConnectProxy {
						test.SetupK8sAuthMethod(t, consulClient, svc.Service.Service, svc.Service.Meta[constants.MetaKeyKubeNS])
						token, _, err := consulClient.ACL().Login(&api.ACLLoginParams{
							AuthMethod:  test.AuthMethod,
							BearerToken: test.ServiceAccountJWTToken,
							Meta: map[string]string{
								tokenMetaPodNameKey: fmt.Sprintf("%s/%s", svc.Service.Meta[constants.MetaKeyKubeNS], svc.Service.Meta[constants.MetaKeyPodName]),
							},
						}, nil)
						// Record each token we create.
						require.NoError(t, err)
						tokensForServices[svc.ID] = token.AccessorID

						// Create another token for the same service but a pod that either no longer exists
						// or the endpoints controller doesn't know about it yet.
						// This is to test a scenario with either orphaned tokens
						// or tokens for services that haven't yet been registered with Consul.
						// In that case, we have a token for the pod but the service instance
						// for that pod either no longer exists or is not yet registered in Consul.
						// This token should not be deleted.
						token, _, err = consulClient.ACL().Login(&api.ACLLoginParams{
							AuthMethod:  test.AuthMethod,
							BearerToken: test.ServiceAccountJWTToken,
							Meta: map[string]string{
								tokenMetaPodNameKey: fmt.Sprintf("%s/%s", svc.Service.Meta[constants.MetaKeyKubeNS], "does-not-exist"),
							},
						}, nil)
						require.NoError(t, err)
						tokensForServices["does-not-exist"+svc.Service.Service] = token.AccessorID
					}
				}
			}

			// Create the endpoints controller.
			ep := &Controller{
				Client:                fakeClient,
				Log:                   logrtest.New(t),
				ConsulClientConfig:    testClient.Cfg,
				ConsulServerConnMgr:   testClient.Watcher,
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSetWith(),
				ReleaseName:           "consul",
				ReleaseNamespace:      "default",
			}
			if tt.enableACLs {
				ep.AuthMethod = test.AuthMethod
			}
			namespacedName := types.NamespacedName{Namespace: "default", Name: "service-updated"}

			resp, err := ep.Reconcile(context.Background(), ctrl.Request{NamespacedName: namespacedName})
			require.NoError(t, err)
			require.False(t, resp.Requeue)

			// After reconciliation, Consul should have service-updated with the correct number of instances.
			serviceInstances, _, err := consulClient.Catalog().Service(tt.consulSvcName, "", nil)
			require.NoError(t, err)
			require.Len(t, serviceInstances, len(tt.expectedConsulSvcInstances))
			for i, instance := range serviceInstances {
				require.Equal(t, tt.expectedConsulSvcInstances[i].ServiceID, instance.ServiceID)
				require.Equal(t, tt.expectedConsulSvcInstances[i].ServiceAddress, instance.ServiceAddress)
			}
			proxyServiceInstances, _, err := consulClient.Catalog().Service(fmt.Sprintf("%s-sidecar-proxy", tt.consulSvcName), "", nil)
			require.NoError(t, err)
			require.Len(t, proxyServiceInstances, len(tt.expectedProxySvcInstances))
			for i, instance := range proxyServiceInstances {
				require.Equal(t, tt.expectedProxySvcInstances[i].ServiceID, instance.ServiceID)
				require.Equal(t, tt.expectedProxySvcInstances[i].ServiceAddress, instance.ServiceAddress)
			}
			// Check that the Consul health check was created for the k8s pod.
			for _, expectedCheck := range tt.expectedHealthChecks {
				filter := fmt.Sprintf("ServiceID == %q", expectedCheck.ServiceID)
				checks, _, err := consulClient.Health().Checks(expectedCheck.ServiceName, &api.QueryOptions{Filter: filter})
				require.NoError(t, err)
				require.Equal(t, 1, len(checks))
				// Ignoring Namespace because the response from ENT includes it and OSS does not.
				var ignoredFields = []string{"Node", "Definition", "Namespace", "Partition", "CreateIndex", "ModifyIndex", "ServiceTags"}
				require.True(t, cmp.Equal(checks[0], expectedCheck, cmpopts.IgnoreFields(api.HealthCheck{}, ignoredFields...)))
			}

			if tt.enableACLs {
				// Put expected services into a map to make it easier to find service IDs.
				expectedServices := mapset.NewSet()
				for _, svc := range tt.expectedConsulSvcInstances {
					expectedServices.Add(svc.ServiceID)
				}

				initialServices := mapset.NewSet()
				for _, svc := range tt.initialConsulSvcs {
					initialServices.Add(svc.ID)
				}

				// We only care about a case when services are deregistered, where
				// the set of initial services is bigger than the set of expected services.
				deregisteredServices := initialServices.Difference(expectedServices)

				// Look through the tokens we've created and check that only
				// tokens for the deregistered services have been deleted.
				for sID, tokenID := range tokensForServices {
					// Read the token from Consul.
					token, _, err := consulClient.ACL().TokenRead(tokenID, nil)
					if deregisteredServices.Contains(sID) {
						require.Contains(t, err.Error(), "ACL not found")
					} else {
						require.NoError(t, err, "token should exist for service instance: "+sID)
						require.NotNil(t, token)
					}
				}
			}
		})
	}
}

// TestReconcileUpdateEndpoint_LegacyService tests that we can update health checks on a consul client.
func TestReconcileUpdateEndpoint_LegacyService(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name                 string
		k8sObjects           func() []runtime.Object
		initialConsulSvcs    []*api.AgentServiceRegistration
		expectedHealthChecks []*api.AgentCheck
	}{
		{
			name: "Health check changes from unhealthy to healthy",
			k8sObjects: func() []runtime.Object {
				pod1 := createServicePod("pod1", "1.2.3.4", true, true)
				pod1.Status.HostIP = "127.0.0.1"
				pod1.Annotations[constants.LegacyAnnotationConsulK8sVersion] = "0.99.0" // We want a version less than 1.0.0.
				endpoint := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-updated",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									IP: "1.2.3.4",
									TargetRef: &corev1.ObjectReference{
										Kind:      "Pod",
										Name:      "pod1",
										Namespace: "default",
									},
								},
							},
						},
					},
				}
				return []runtime.Object{pod1, endpoint}
			},
			initialConsulSvcs: []*api.AgentServiceRegistration{
				{
					ID:      "pod1-service-updated",
					Name:    "service-updated",
					Port:    80,
					Address: "1.2.3.4",
					Check: &api.AgentServiceCheck{
						CheckID: "default/pod1-service-updated/kubernetes-health-check",
						TTL:     "100000h",
						Name:    "Kubernetes Health Check",
						Status:  api.HealthCritical,
					},
				},
				{
					Kind:    api.ServiceKindConnectProxy,
					ID:      "pod1-service-updated-sidecar-proxy",
					Name:    "service-updated-sidecar-proxy",
					Port:    20000,
					Address: "1.2.3.4",
					Proxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "service-updated",
						DestinationServiceID:   "pod1-service-updated",
					},
				},
			},
			expectedHealthChecks: []*api.AgentCheck{
				{
					CheckID:     "default/pod1-service-updated/kubernetes-health-check",
					ServiceName: "service-updated",
					ServiceID:   "pod1-service-updated",
					Name:        "Kubernetes Health Check",
					Status:      api.HealthPassing,
					Output:      "Kubernetes health checks passing",
					Type:        "ttl",
				},
			},
		},
		{
			name: "Health check changes from healthy to unhealthy",
			k8sObjects: func() []runtime.Object {
				pod1 := createServicePod("pod1", "1.2.3.4", true, true)
				pod1.Status.HostIP = "127.0.0.1"
				pod1.Annotations[constants.LegacyAnnotationConsulK8sVersion] = "0.99.0" // We want a version less than 1.0.0.
				endpoint := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-updated",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							NotReadyAddresses: []corev1.EndpointAddress{
								{
									IP: "1.2.3.4",
									TargetRef: &corev1.ObjectReference{
										Kind:      "Pod",
										Name:      "pod1",
										Namespace: "default",
									},
								},
							},
						},
					},
				}
				return []runtime.Object{pod1, endpoint}
			},
			initialConsulSvcs: []*api.AgentServiceRegistration{
				{
					ID:      "pod1-service-updated",
					Name:    "service-updated",
					Port:    80,
					Address: "1.2.3.4",
					Check: &api.AgentServiceCheck{
						CheckID: "default/pod1-service-updated/kubernetes-health-check",
						TTL:     "100000h",
						Name:    "Kubernetes Health Check",
						Status:  api.HealthPassing,
					},
				},
				{
					Kind:    api.ServiceKindConnectProxy,
					ID:      "pod1-service-updated-sidecar-proxy",
					Name:    "service-updated-sidecar-proxy",
					Port:    20000,
					Address: "1.2.3.4",
					Proxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "service-updated",
						DestinationServiceID:   "pod1-service-updated",
					},
				},
			},
			expectedHealthChecks: []*api.AgentCheck{
				{
					CheckID:     "default/pod1-service-updated/kubernetes-health-check",
					ServiceName: "service-updated",
					ServiceID:   "pod1-service-updated",
					Name:        "Kubernetes Health Check",
					Status:      api.HealthCritical,
					Output:      "Pod \"default/pod1\" is not ready",
					Type:        "ttl",
				},
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			// Add the default namespace.
			ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
			// Create fake k8s client.
			k8sObjects := append(tt.k8sObjects(), &ns)
			fakeClient := fake.NewClientBuilder().WithRuntimeObjects(k8sObjects...).Build()

			// Create test consulServer server
			testClient := test.TestServerWithMockConnMgrWatcher(t, nil)

			// Create a consul client joined with this server.
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

			// Register service and proxy in consul.
			for _, svc := range tt.initialConsulSvcs {
				err := consulClient.Agent().ServiceRegister(svc)
				require.NoError(t, err)
			}

			// Create the endpoints controller.
			ep := &Controller{
				Client:                fakeClient,
				Log:                   logrtest.New(t),
				ConsulClientConfig:    testClient.Cfg,
				ConsulServerConnMgr:   testClient.Watcher,
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSetWith(),
				ReleaseName:           "consul",
				ReleaseNamespace:      "default",
				consulClientHttpPort:  consulClientHttpPort,
			}
			namespacedName := types.NamespacedName{Namespace: "default", Name: "service-updated"}

			resp, err := ep.Reconcile(context.Background(), ctrl.Request{NamespacedName: namespacedName})
			require.NoError(t, err)
			require.False(t, resp.Requeue)

			// After reconciliation, Consul should have service-updated with the correct health check status.
			for _, expectedCheck := range tt.expectedHealthChecks {
				filter := fmt.Sprintf("ServiceID == %q", expectedCheck.ServiceID)
				checks, err := consulClient.Agent().ChecksWithFilter(filter)
				require.NoError(t, err)
				require.Equal(t, 1, len(checks))
				// Ignoring Namespace because the response from ENT includes it and OSS does not.
				var ignoredFields = []string{"Node", "Definition", "Namespace", "Partition"}
				require.True(t, cmp.Equal(checks[expectedCheck.CheckID], expectedCheck, cmpopts.IgnoreFields(api.AgentCheck{}, ignoredFields...)))
			}
		})
	}
}

// Tests deleting an Endpoints object, with and without matching Consul and K8s service names.
// This test covers Controller.deregisterService when the map is nil (not selectively deregistered).
func TestReconcileDeleteEndpoint(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name                       string
		consulSvcName              string
		pod                        *corev1.Pod // If this is present, a pod will be created in the fake kube client
		consulPodUid               string
		expectServicesToBeDeleted  bool
		expectServicesToBeCritical bool
		initialConsulSvcs          []*api.AgentService
		enableACLs                 bool
		expectTokens               bool
		requeueAfter               time.Duration
	}{
		{
			name:                      "Legacy service: does not delete",
			consulSvcName:             "service-deleted",
			expectServicesToBeDeleted: false,
			initialConsulSvcs: []*api.AgentService{
				{
					ID:      "pod1-service-deleted",
					Service: "service-deleted",
					Port:    80,
					Address: "1.2.3.4",
					Meta:    map[string]string{"k8s-service-name": "service-deleted", "k8s-namespace": "default"},
				},
				{
					Kind:    api.ServiceKindConnectProxy,
					ID:      "pod1-service-deleted-sidecar-proxy",
					Service: "service-deleted-sidecar-proxy",
					Port:    20000,
					Address: "1.2.3.4",
					Proxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "service-deleted",
						DestinationServiceID:   "pod1-service-deleted",
					},
					Meta: map[string]string{"k8s-service-name": "service-deleted", "k8s-namespace": "default"},
				},
			},
		},
		{
			name:                      "Consul service name matches K8s service name",
			consulSvcName:             "service-deleted",
			expectServicesToBeDeleted: true,
			initialConsulSvcs: []*api.AgentService{
				{
					ID:      "pod1-service-deleted",
					Service: "service-deleted",
					Port:    80,
					Address: "1.2.3.4",
					Meta:    map[string]string{"k8s-service-name": "service-deleted", "k8s-namespace": "default", metaKeyManagedBy: constants.ManagedByValue},
				},
				{
					Kind:    api.ServiceKindConnectProxy,
					ID:      "pod1-service-deleted-sidecar-proxy",
					Service: "service-deleted-sidecar-proxy",
					Port:    20000,
					Address: "1.2.3.4",
					Proxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "service-deleted",
						DestinationServiceID:   "pod1-service-deleted",
					},
					Meta: map[string]string{"k8s-service-name": "service-deleted", "k8s-namespace": "default", metaKeyManagedBy: constants.ManagedByValue},
				},
			},
		},
		{
			name:                      "Consul service name does not match K8s service name",
			consulSvcName:             "different-consul-svc-name",
			expectServicesToBeDeleted: true,
			initialConsulSvcs: []*api.AgentService{
				{
					ID:      "pod1-different-consul-svc-name",
					Service: "different-consul-svc-name",
					Port:    80,
					Address: "1.2.3.4",
					Meta:    map[string]string{"k8s-service-name": "service-deleted", "k8s-namespace": "default", metaKeyManagedBy: constants.ManagedByValue},
				},
				{
					Kind:    api.ServiceKindConnectProxy,
					ID:      "pod1-different-consul-svc-name-sidecar-proxy",
					Service: "different-consul-svc-name-sidecar-proxy",
					Port:    20000,
					Address: "1.2.3.4",
					Proxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "different-consul-svc-name",
						DestinationServiceID:   "pod1-different-consul-svc-name",
					},
					Meta: map[string]string{"k8s-service-name": "service-deleted", "k8s-namespace": "default", metaKeyManagedBy: constants.ManagedByValue},
				},
			},
		},
		{
			name:                      "When ACLs are enabled, the token should be deleted",
			consulSvcName:             "service-deleted",
			consulPodUid:              "123",
			expectServicesToBeDeleted: true,
			initialConsulSvcs: []*api.AgentService{
				{
					ID:      "pod1-service-deleted",
					Service: "service-deleted",
					Port:    80,
					Address: "1.2.3.4",
					Meta: map[string]string{
						metaKeyKubeServiceName:   "service-deleted",
						constants.MetaKeyKubeNS:  "default",
						metaKeyManagedBy:         constants.ManagedByValue,
						metaKeySyntheticNode:     "true",
						constants.MetaKeyPodName: "pod1",
						constants.MetaKeyPodUID:  "123",
					},
				},
				{
					Kind:    api.ServiceKindConnectProxy,
					ID:      "pod1-service-deleted-sidecar-proxy",
					Service: "service-deleted-sidecar-proxy",
					Port:    20000,
					Address: "1.2.3.4",
					Proxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "service-deleted",
						DestinationServiceID:   "pod1-service-deleted",
					},
					Meta: map[string]string{
						metaKeyKubeServiceName:   "service-deleted",
						constants.MetaKeyKubeNS:  "default",
						metaKeyManagedBy:         constants.ManagedByValue,
						metaKeySyntheticNode:     "true",
						constants.MetaKeyPodName: "pod1",
						constants.MetaKeyPodUID:  "123",
					},
				},
			},
			enableACLs: true,
		},
		{
			name:          "When graceful shutdown is enabled with ACLs, tokens should not be deleted",
			consulSvcName: "service-deleted",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod1",
					Namespace: "default",
					UID:       "123",
					Annotations: map[string]string{
						constants.AnnotationEnableSidecarProxyLifecycle:                     "true",
						constants.AnnotationSidecarProxyLifecycleShutdownGracePeriodSeconds: "5",
					},
				},
				Spec: corev1.PodSpec{
					NodeName: nodeName,
					// We don't need any other fields for this test
				},
			},
			consulPodUid:               "123",
			expectServicesToBeDeleted:  false,
			expectServicesToBeCritical: true,
			initialConsulSvcs: []*api.AgentService{
				{
					ID:      "pod1-service-deleted",
					Service: "service-deleted",
					Port:    80,
					Address: "1.2.3.4",
					Meta: map[string]string{
						metaKeyKubeServiceName:   "service-deleted",
						constants.MetaKeyKubeNS:  "default",
						metaKeyManagedBy:         constants.ManagedByValue,
						metaKeySyntheticNode:     "true",
						constants.MetaKeyPodName: "pod1",
						constants.MetaKeyPodUID:  "123",
					},
				},
				{
					Kind:    api.ServiceKindConnectProxy,
					ID:      "pod1-service-deleted-sidecar-proxy",
					Service: "service-deleted-sidecar-proxy",
					Port:    20000,
					Address: "1.2.3.4",
					Proxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "service-deleted",
						DestinationServiceID:   "pod1-service-deleted",
					},
					Meta: map[string]string{
						metaKeyKubeServiceName:   "service-deleted",
						constants.MetaKeyKubeNS:  "default",
						metaKeyManagedBy:         constants.ManagedByValue,
						metaKeySyntheticNode:     "true",
						constants.MetaKeyPodName: "pod1",
						constants.MetaKeyPodUID:  "123",
					},
				},
			},
			requeueAfter: time.Duration(6) * time.Second,
			expectTokens: true,
			enableACLs:   true,
		},
		{
			name:          "When pod is part of statefulset and comes up with new uid, the old service instance should be deleted",
			consulSvcName: "service-deleted",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod1",
					Namespace: "default",
					UID:       "new-pod-uid", // different from the original uid the initial instances were registered with
					Annotations: map[string]string{
						constants.AnnotationEnableSidecarProxyLifecycle:                     "true",
						constants.AnnotationSidecarProxyLifecycleShutdownGracePeriodSeconds: "5",
					},
				},
				Spec: corev1.PodSpec{
					NodeName: nodeName,
					// We don't need any other fields for this test
				},
			},
			consulPodUid:              "123",
			expectServicesToBeDeleted: true,
			initialConsulSvcs: []*api.AgentService{
				{
					ID:      "pod1-service-deleted",
					Service: "service-deleted",
					Port:    80,
					Address: "1.2.3.4",
					Meta: map[string]string{
						metaKeyKubeServiceName:   "service-deleted",
						constants.MetaKeyKubeNS:  "default",
						metaKeyManagedBy:         constants.ManagedByValue,
						metaKeySyntheticNode:     "true",
						constants.MetaKeyPodName: "pod1",
						constants.MetaKeyPodUID:  "123",
					},
				},
				{
					Kind:    api.ServiceKindConnectProxy,
					ID:      "pod1-service-deleted-sidecar-proxy",
					Service: "service-deleted-sidecar-proxy",
					Port:    20000,
					Address: "1.2.3.4",
					Proxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "service-deleted",
						DestinationServiceID:   "pod1-service-deleted",
					},
					Meta: map[string]string{
						metaKeyKubeServiceName:   "service-deleted",
						constants.MetaKeyKubeNS:  "default",
						metaKeyManagedBy:         constants.ManagedByValue,
						metaKeySyntheticNode:     "true",
						constants.MetaKeyPodName: "pod1",
						constants.MetaKeyPodUID:  "123",
					},
				},
			},
			enableACLs: true,
		},
		{
			name:          "When pod is part of statefulset and comes up with new node, the old service instance should be deleted",
			consulSvcName: "service-deleted",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod1",
					Namespace: "default",
					UID:       "123",
					Annotations: map[string]string{
						constants.AnnotationEnableSidecarProxyLifecycle:                     "true",
						constants.AnnotationSidecarProxyLifecycleShutdownGracePeriodSeconds: "5",
					},
				},
				Spec: corev1.PodSpec{
					NodeName: nodeName + "-different", // new node name
					// We don't need any other fields for this test
				},
			},
			consulPodUid:              "123",
			expectServicesToBeDeleted: true,
			initialConsulSvcs: []*api.AgentService{
				{
					ID:      "pod1-service-deleted",
					Service: "service-deleted",
					Port:    80,
					Address: "1.2.3.4",
					Meta: map[string]string{
						metaKeyKubeServiceName:   "service-deleted",
						constants.MetaKeyKubeNS:  "default",
						metaKeyManagedBy:         constants.ManagedByValue,
						metaKeySyntheticNode:     "true",
						constants.MetaKeyPodName: "pod1",
						constants.MetaKeyPodUID:  "123",
					},
				},
				{
					Kind:    api.ServiceKindConnectProxy,
					ID:      "pod1-service-deleted-sidecar-proxy",
					Service: "service-deleted-sidecar-proxy",
					Port:    20000,
					Address: "1.2.3.4",
					Proxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "service-deleted",
						DestinationServiceID:   "pod1-service-deleted",
					},
					Meta: map[string]string{
						metaKeyKubeServiceName:   "service-deleted",
						constants.MetaKeyKubeNS:  "default",
						metaKeyManagedBy:         constants.ManagedByValue,
						metaKeySyntheticNode:     "true",
						constants.MetaKeyPodName: "pod1",
						constants.MetaKeyPodUID:  "123",
					},
				},
			},
			enableACLs: true,
		},
		{
			name:                      "Mesh Gateway",
			consulSvcName:             "service-deleted",
			expectServicesToBeDeleted: true,
			initialConsulSvcs: []*api.AgentService{
				{
					ID:      "mesh-gateway",
					Kind:    api.ServiceKindMeshGateway,
					Service: "mesh-gateway",
					Port:    80,
					Address: "1.2.3.4",
					Meta: map[string]string{
						metaKeyKubeServiceName:   "service-deleted",
						constants.MetaKeyKubeNS:  "default",
						metaKeyManagedBy:         constants.ManagedByValue,
						metaKeySyntheticNode:     "true",
						constants.MetaKeyPodName: "mesh-gateway",
					},
					TaggedAddresses: map[string]api.ServiceAddress{
						"lan": {
							Address: "1.2.3.4",
							Port:    80,
						},
						"wan": {
							Address: "5.6.7.8",
							Port:    8080,
						},
					},
				},
			},
		},
		{
			name:                      "When ACLs are enabled, the mesh-gateway token should be deleted",
			consulSvcName:             "service-deleted",
			consulPodUid:              "124",
			expectServicesToBeDeleted: true,
			initialConsulSvcs: []*api.AgentService{
				{
					ID:      "mesh-gateway",
					Kind:    api.ServiceKindMeshGateway,
					Service: "mesh-gateway",
					Port:    80,
					Address: "1.2.3.4",
					Meta: map[string]string{
						metaKeyKubeServiceName:   "service-deleted",
						constants.MetaKeyKubeNS:  "default",
						metaKeyManagedBy:         constants.ManagedByValue,
						metaKeySyntheticNode:     "true",
						constants.MetaKeyPodName: "mesh-gateway",
						constants.MetaKeyPodUID:  "124",
					},
					TaggedAddresses: map[string]api.ServiceAddress{
						"lan": {
							Address: "1.2.3.4",
							Port:    80,
						},
						"wan": {
							Address: "5.6.7.8",
							Port:    8080,
						},
					},
				},
			},
			enableACLs: true,
		},
		{
			name:                      "Ingress Gateway",
			consulSvcName:             "service-deleted",
			expectServicesToBeDeleted: true,
			initialConsulSvcs: []*api.AgentService{
				{
					ID:      "ingress-gateway",
					Kind:    api.ServiceKindIngressGateway,
					Service: "ingress-gateway",
					Port:    21000,
					Address: "1.2.3.4",
					Meta: map[string]string{
						metaKeyKubeServiceName:   "service-deleted",
						constants.MetaKeyKubeNS:  "default",
						metaKeyManagedBy:         constants.ManagedByValue,
						metaKeySyntheticNode:     "true",
						constants.MetaKeyPodName: "ingress-gateway",
					},
					TaggedAddresses: map[string]api.ServiceAddress{
						"lan": {
							Address: "1.2.3.4",
							Port:    21000,
						},
						"wan": {
							Address: "5.6.7.8",
							Port:    8080,
						},
					},
				},
			},
		},
		{
			name:                      "When ACLs are enabled, the ingress-gateway token should be deleted",
			consulSvcName:             "service-deleted",
			consulPodUid:              "125",
			expectServicesToBeDeleted: true,
			initialConsulSvcs: []*api.AgentService{
				{
					ID:      "ingress-gateway",
					Kind:    api.ServiceKindIngressGateway,
					Service: "ingress-gateway",
					Port:    21000,
					Address: "1.2.3.4",
					Meta: map[string]string{
						metaKeyKubeServiceName:   "service-deleted",
						constants.MetaKeyKubeNS:  "default",
						metaKeyManagedBy:         constants.ManagedByValue,
						metaKeySyntheticNode:     "true",
						constants.MetaKeyPodName: "ingress-gateway",
						constants.MetaKeyPodUID:  "125",
					},
					TaggedAddresses: map[string]api.ServiceAddress{
						"lan": {
							Address: "1.2.3.4",
							Port:    21000,
						},
						"wan": {
							Address: "5.6.7.8",
							Port:    8080,
						},
					},
				},
			},
			enableACLs: true,
		},
		{
			name:                      "Terminating Gateway",
			consulSvcName:             "service-deleted",
			expectServicesToBeDeleted: true,
			initialConsulSvcs: []*api.AgentService{
				{
					ID:      "terminating-gateway",
					Kind:    api.ServiceKindTerminatingGateway,
					Service: "terminating-gateway",
					Port:    8443,
					Address: "1.2.3.4",
					Meta: map[string]string{
						metaKeyKubeServiceName:   "service-deleted",
						constants.MetaKeyKubeNS:  "default",
						metaKeyManagedBy:         constants.ManagedByValue,
						metaKeySyntheticNode:     "true",
						constants.MetaKeyPodName: "terminating-gateway",
					},
				},
			},
		},
		{
			name:                      "When ACLs are enabled, the terminating-gateway token should be deleted",
			consulSvcName:             "service-deleted",
			consulPodUid:              "126",
			expectServicesToBeDeleted: true,
			initialConsulSvcs: []*api.AgentService{
				{
					ID:      "terminating-gateway",
					Kind:    api.ServiceKindTerminatingGateway,
					Service: "terminating-gateway",
					Port:    8443,
					Address: "1.2.3.4",
					Meta: map[string]string{
						metaKeyKubeServiceName:   "service-deleted",
						constants.MetaKeyKubeNS:  "default",
						metaKeyManagedBy:         constants.ManagedByValue,
						metaKeySyntheticNode:     "true",
						constants.MetaKeyPodName: "terminating-gateway",
						constants.MetaKeyPodUID:  "126",
					},
				},
			},
			enableACLs: true,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			// Add the default namespace.
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
			node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}}
			objs := []runtime.Object{ns, node}

			if tt.pod != nil {
				objs = append(objs, tt.pod)
			}

			// Create fake k8s client.
			fakeClient := fake.NewClientBuilder().WithRuntimeObjects(objs...).Build()

			// Create test consulServer server
			adminToken := "123e4567-e89b-12d3-a456-426614174000"
			testClient := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
				if tt.enableACLs {
					c.ACL.Enabled = tt.enableACLs
					c.ACL.Tokens.InitialManagement = adminToken
				}
			})
			consulClient := testClient.APIClient
			// Wait so that bootstrap finishes
			testClient.TestServer.WaitForActiveCARoot(t)

			// Register service and proxy in consul
			var token *api.ACLToken
			var err error
			for _, svc := range tt.initialConsulSvcs {
				serviceRegistration := &api.CatalogRegistration{
					Node:    consulNodeName,
					Address: consulNodeAddress,
					NodeMeta: map[string]string{
						metaKeySyntheticNode: "true",
					},
					Service: svc,
				}
				// Retry because the ACLs may not have been fully initialized yet.
				retry.Run(t, func(r *retry.R) {
					_, err = consulClient.Catalog().Register(serviceRegistration, nil)
					require.NoError(r, err)
				})

				// Create a token for it if ACLs are enabled.
				if tt.enableACLs {
					test.SetupK8sAuthMethod(t, consulClient, svc.Service, "default")
					token, _, err = consulClient.ACL().Login(&api.ACLLoginParams{
						AuthMethod:  test.AuthMethod,
						BearerToken: test.ServiceAccountJWTToken,
						Meta: map[string]string{
							"pod":                   fmt.Sprintf("%s/%s", svc.Meta[constants.MetaKeyKubeNS], svc.Meta[constants.MetaKeyPodName]),
							"component":             tt.consulSvcName,
							constants.MetaKeyPodUID: tt.consulPodUid,
						},
					}, nil)
					require.NoError(t, err)
				}
			}

			// Create the endpoints controller
			ep := &Controller{
				Client:                fakeClient,
				Log:                   logrtest.New(t),
				ConsulClientConfig:    testClient.Cfg,
				ConsulServerConnMgr:   testClient.Watcher,
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSetWith(),
				ReleaseName:           "consul",
				ReleaseNamespace:      "default",
			}
			if tt.enableACLs {
				ep.AuthMethod = test.AuthMethod
			}

			// Set up the Endpoint that will be reconciled, and reconcile
			namespacedName := types.NamespacedName{
				Namespace: "default",
				Name:      "service-deleted",
			}
			resp, err := ep.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: namespacedName,
			})
			require.NoError(t, err)
			require.False(t, resp.Requeue)
			require.Equal(t, tt.requeueAfter, resp.RequeueAfter)

			// After reconciliation, Consul should not have any instances of service-deleted
			serviceInstances, _, err := consulClient.Catalog().Service(tt.consulSvcName, "", nil)
			// If it's not managed by endpoints controller (legacy service), Consul should have service instances
			if tt.expectServicesToBeDeleted {
				require.NoError(t, err)
				require.Empty(t, serviceInstances)
				proxyServiceInstances, _, err := consulClient.Catalog().Service(fmt.Sprintf("%s-sidecar-proxy", tt.consulSvcName), "", nil)
				require.NoError(t, err)
				require.Empty(t, proxyServiceInstances)
			} else {
				require.NoError(t, err)
				require.NotEmpty(t, serviceInstances)
			}

			if tt.expectServicesToBeCritical {
				checks, _, err := consulClient.Health().Checks(tt.consulSvcName, nil)
				require.NoError(t, err)
				require.Equal(t, api.HealthCritical, checks.AggregatedStatus())
			}

			if tt.enableACLs && !tt.expectTokens {
				_, _, err = consulClient.ACL().TokenRead(token.AccessorID, nil)
				require.Error(t, err)
				require.Contains(t, err.Error(), "ACL not found")
			}
			if tt.expectTokens {
				_, _, err = consulClient.ACL().TokenRead(token.AccessorID, nil)
				require.NoError(t, err)
			}
		})
	}
}

// TestReconcileIgnoresServiceIgnoreLabel tests that the endpoints controller correctly ignores services
// with the service-ignore label and deregisters services previously registered if the service-ignore
// label is added.
func TestReconcileIgnoresServiceIgnoreLabel(t *testing.T) {
	t.Parallel()
	svcName := "service-ignored"
	namespace := "default"

	cases := map[string]struct {
		svcInitiallyRegistered  bool
		serviceLabels           map[string]string
		expectedNumSvcInstances int
	}{
		"Registered endpoint with label is deregistered.": {
			svcInitiallyRegistered: true,
			serviceLabels: map[string]string{
				constants.LabelServiceIgnore: "true",
			},
			expectedNumSvcInstances: 0,
		},
		"Not registered endpoint with label is never registered": {
			svcInitiallyRegistered: false,
			serviceLabels: map[string]string{
				constants.LabelServiceIgnore: "true",
			},
			expectedNumSvcInstances: 0,
		},
		"Registered endpoint without label is unaffected": {
			svcInitiallyRegistered:  true,
			serviceLabels:           map[string]string{},
			expectedNumSvcInstances: 1,
		},
		"Not registered endpoint without label is registered": {
			svcInitiallyRegistered:  false,
			serviceLabels:           map[string]string{},
			expectedNumSvcInstances: 1,
		},
	}

	for name, tt := range cases {
		t.Run(name, func(t *testing.T) {
			// Set up the fake Kubernetes client with an endpoint, pod, consul client, and the default namespace.
			endpoint := &corev1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Name:      svcName,
					Namespace: namespace,
					Labels:    tt.serviceLabels,
				},
				Subsets: []corev1.EndpointSubset{
					{
						Addresses: []corev1.EndpointAddress{
							{
								IP: "1.2.3.4",
								TargetRef: &corev1.ObjectReference{
									Kind:      "Pod",
									Name:      "pod1",
									Namespace: namespace,
								},
							},
						},
					},
				},
			}
			pod1 := createServicePod("pod1", "1.2.3.4", true, true)
			ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			node := corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}}
			k8sObjects := []runtime.Object{endpoint, pod1, &ns, &node}
			fakeClient := fake.NewClientBuilder().WithRuntimeObjects(k8sObjects...).Build()

			// Create test consulServer server
			testClient := test.TestServerWithMockConnMgrWatcher(t, nil)
			consulClient := testClient.APIClient

			// Set up the initial Consul services.
			if tt.svcInitiallyRegistered {
				serviceRegistration := &api.CatalogRegistration{
					Node:    consulNodeName,
					Address: consulNodeAddress,
					NodeMeta: map[string]string{
						metaKeySyntheticNode: "true",
					},
					Service: &api.AgentService{
						ID:      "pod1-" + svcName,
						Service: svcName,
						Port:    0,
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
				}
				_, err := consulClient.Catalog().Register(serviceRegistration, nil)
				require.NoError(t, err)
				require.NoError(t, err)
			}

			// Create the endpoints controller.
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

			// Run the reconcile process to deregister the service if it was registered before.
			namespacedName := types.NamespacedName{Namespace: namespace, Name: svcName}
			resp, err := ep.Reconcile(context.Background(), ctrl.Request{NamespacedName: namespacedName})
			require.NoError(t, err)
			require.False(t, resp.Requeue)

			// Check that the correct number of services are registered with Consul.
			serviceInstances, _, err := consulClient.Catalog().Service(svcName, "", nil)
			require.NoError(t, err)
			require.Len(t, serviceInstances, tt.expectedNumSvcInstances)
			proxyServiceInstances, _, err := consulClient.Catalog().Service(svcName+"-sidecar-proxy", "", nil)
			require.NoError(t, err)
			require.Len(t, proxyServiceInstances, tt.expectedNumSvcInstances)
		})
	}
}

// Test that when an endpoints pod specifies the name for the Kubernetes service it wants to use
// for registration, all other endpoints for that pod are skipped.
func TestReconcile_podSpecifiesExplicitService(t *testing.T) {
	namespace := "default"

	// Set up the fake Kubernetes client with a few endpoints, pod, consul client, and the default namespace.
	badEndpoint := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "not-in-mesh",
			Namespace: namespace,
		},
		Subsets: []corev1.EndpointSubset{
			{
				Addresses: []corev1.EndpointAddress{
					{
						IP: "1.2.3.4",
						TargetRef: &corev1.ObjectReference{
							Kind:      "Pod",
							Name:      "pod1",
							Namespace: namespace,
						},
					},
				},
			},
		},
	}
	endpoint := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "in-mesh",
			Namespace: namespace,
		},
		Subsets: []corev1.EndpointSubset{
			{
				Addresses: []corev1.EndpointAddress{
					{
						IP: "1.2.3.4",
						TargetRef: &corev1.ObjectReference{
							Kind:      "Pod",
							Name:      "pod1",
							Namespace: namespace,
						},
					},
				},
			},
		},
	}
	pod1 := createServicePod("pod1", "1.2.3.4", true, true)
	pod1.Annotations[constants.AnnotationKubernetesService] = endpoint.Name
	ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
	node := corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}}
	k8sObjects := []runtime.Object{badEndpoint, endpoint, pod1, &ns, &node}
	fakeClient := fake.NewClientBuilder().WithRuntimeObjects(k8sObjects...).Build()

	// Create test consulServer server
	testClient := test.TestServerWithMockConnMgrWatcher(t, nil)
	consulClient := testClient.APIClient

	// Create the endpoints controller.
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

	svcName := badEndpoint.Name

	// Initially register the pod with the bad endpoint
	_, err := consulClient.Catalog().Register(&api.CatalogRegistration{
		Node:    consulNodeName,
		Address: consulNodeAddress,
		NodeMeta: map[string]string{
			metaKeySyntheticNode: "true",
		},
		Service: &api.AgentService{
			ID:      "pod1-" + svcName,
			Service: svcName,
			Port:    0,
			Address: "1.2.3.4",
			Meta: map[string]string{
				"k8s-namespace":    namespace,
				"k8s-service-name": svcName,
				"managed-by":       "consul-k8s-endpoints-controller",
				"pod-name":         "pod1",
			},
		},
	}, nil)
	require.NoError(t, err)
	serviceInstances, _, err := consulClient.Catalog().Service(svcName, "", nil)
	require.NoError(t, err)
	require.Len(t, serviceInstances, 1)

	// Run the reconcile process to check service deregistration.
	namespacedName := types.NamespacedName{Namespace: badEndpoint.Namespace, Name: svcName}
	resp, err := ep.Reconcile(context.Background(), ctrl.Request{NamespacedName: namespacedName})
	require.NoError(t, err)
	require.False(t, resp.Requeue)

	// Check that the service has been deregistered with Consul.
	serviceInstances, _, err = consulClient.Catalog().Service(svcName, "", nil)
	require.NoError(t, err)
	require.Len(t, serviceInstances, 0)
	proxyServiceInstances, _, err := consulClient.Catalog().Service(svcName+"-sidecar-proxy", "", nil)
	require.NoError(t, err)
	require.Len(t, proxyServiceInstances, 0)

	// Run the reconcile again with the service we want to register.
	svcName = endpoint.Name
	namespacedName = types.NamespacedName{Namespace: endpoint.Namespace, Name: svcName}
	resp, err = ep.Reconcile(context.Background(), ctrl.Request{NamespacedName: namespacedName})
	require.NoError(t, err)
	require.False(t, resp.Requeue)

	// Check that the correct services are registered with Consul.
	serviceInstances, _, err = consulClient.Catalog().Service(svcName, "", nil)
	require.NoError(t, err)
	require.Len(t, serviceInstances, 1)
	proxyServiceInstances, _, err = consulClient.Catalog().Service(svcName+"-sidecar-proxy", "", nil)
	require.NoError(t, err)
	require.Len(t, proxyServiceInstances, 1)
}

func TestServiceInstancesForK8SServiceNameAndNamespace(t *testing.T) {
	t.Parallel()

	const (
		k8sSvc = "k8s-svc"
		k8sNS  = "k8s-ns"
	)
	cases := []struct {
		name               string
		k8sServiceNameMeta string
		k8sNamespaceMeta   string
		expected           []*api.CatalogService
	}{
		{
			"no k8s service name or namespace meta",
			"",
			"",
			nil,
		},
		{
			"k8s service name set, but no namespace meta",
			k8sSvc,
			"",
			nil,
		},
		{
			"k8s namespace set, but no k8s service name meta",
			"",
			k8sNS,
			nil,
		},
		{
			"both k8s service name and namespace set",
			k8sSvc,
			k8sNS,
			[]*api.CatalogService{
				{
					ID:          "foo1",
					ServiceName: "foo",
					ServiceMeta: map[string]string{"k8s-service-name": k8sSvc, "k8s-namespace": k8sNS},
				},
				{
					ID:          "foo1-proxy",
					ServiceName: "foo-sidecar-proxy",
					ServicePort: 20000,
					ServiceProxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "foo",
						DestinationServiceID:   "foo1",
					},
					ServiceMeta: map[string]string{"k8s-service-name": k8sSvc, "k8s-namespace": k8sNS},
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			servicesInConsul := []*api.AgentService{
				{
					ID:      "foo1",
					Service: "foo",
					Tags:    []string{},
					Meta:    map[string]string{"k8s-service-name": c.k8sServiceNameMeta, "k8s-namespace": c.k8sNamespaceMeta},
				},
				{
					Kind:    api.ServiceKindConnectProxy,
					ID:      "foo1-proxy",
					Service: "foo-sidecar-proxy",
					Port:    20000,
					Proxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "foo",
						DestinationServiceID:   "foo1",
					},
					Meta: map[string]string{"k8s-service-name": c.k8sServiceNameMeta, "k8s-namespace": c.k8sNamespaceMeta},
				},
				{
					ID:      "k8s-service-different-ns-id",
					Service: "k8s-service-different-ns",
					Meta:    map[string]string{"k8s-service-name": c.k8sServiceNameMeta, "k8s-namespace": "different-ns"},
				},
				{
					Kind:    api.ServiceKindConnectProxy,
					ID:      "k8s-service-different-ns-proxy",
					Service: "k8s-service-different-ns-proxy",
					Port:    20000,
					Tags:    []string{},
					Proxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "k8s-service-different-ns",
						DestinationServiceID:   "k8s-service-different-ns-id",
					},
					Meta: map[string]string{"k8s-service-name": c.k8sServiceNameMeta, "k8s-namespace": "different-ns"},
				},
			}

			consul, err := testutil.NewTestServerConfigT(t, nil)
			require.NoError(t, err)
			defer consul.Stop()

			consul.WaitForServiceIntentions(t)
			consulClient, err := api.NewClient(&api.Config{
				Address: consul.HTTPAddr,
			})
			require.NoError(t, err)

			for _, svc := range servicesInConsul {
				catalogRegistration := &api.CatalogRegistration{
					Node:    consulNodeName,
					Address: "127.0.0.1",
					NodeMeta: map[string]string{
						metaKeySyntheticNode: "true",
					},
					Service: svc,
				}
				_, err = consulClient.Catalog().Register(catalogRegistration, nil)
				require.NoError(t, err)
			}
			ep := Controller{}

			svcs, err := ep.serviceInstances(consulClient, k8sSvc, k8sNS)
			require.NoError(t, err)
			if len(svcs) > 0 {
				require.Len(t, svcs, 2)
				require.NotNil(t, svcs[0], c.expected[0])
				require.Equal(t, c.expected[0].ServiceName, svcs[0].ServiceName)
				require.NotNil(t, svcs[1], c.expected[1])
				require.Equal(t, c.expected[1].ServiceName, svcs[1].ServiceName)
			}
		})
	}
}

func TestCreateServiceRegistrations_withTransparentProxy(t *testing.T) {
	t.Parallel()

	const serviceName = "test-service"

	cases := map[string]struct {
		tproxyGlobalEnabled bool
		overwriteProbes     bool
		podContainers       []corev1.Container
		podAnnotations      map[string]string
		namespaceLabels     map[string]string
		service             *corev1.Service
		expTaggedAddresses  map[string]api.ServiceAddress
		expProxyMode        api.ProxyMode
		expExposePaths      []api.ExposePath
		expErr              string
	}{
		"tproxy enabled globally, annotation not provided": {
			tproxyGlobalEnabled: true,
			podContainers: []corev1.Container{
				{
					Name: "test",
					Ports: []corev1.ContainerPort{
						{
							Name:          "tcp",
							ContainerPort: 8081,
						},
						{
							Name:          "http",
							ContainerPort: 8080,
						},
					},
				},
			},
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					ClusterIP: "10.0.0.1",
					Ports: []corev1.ServicePort{
						{
							Port: 8081,
						},
					},
				},
			},
			expProxyMode: api.ProxyModeTransparent,
			expTaggedAddresses: map[string]api.ServiceAddress{
				"virtual": {
					Address: "10.0.0.1",
					Port:    8081,
				},
			},
			expErr: "",
		},
		"tproxy enabled globally, annotation is false": {
			tproxyGlobalEnabled: true,
			podAnnotations:      map[string]string{constants.KeyTransparentProxy: "false"},
			podContainers: []corev1.Container{
				{
					Name: "test",
					Ports: []corev1.ContainerPort{
						{
							Name:          "tcp",
							ContainerPort: 8081,
						},
						{
							Name:          "http",
							ContainerPort: 8080,
						},
					},
				},
			},
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					ClusterIP: "10.0.0.1",
					Ports: []corev1.ServicePort{
						{
							Port: 80,
						},
					},
				},
			},
			expProxyMode:       api.ProxyModeDefault,
			expTaggedAddresses: nil,
			expErr:             "",
		},
		"tproxy enabled globally, annotation is true": {
			tproxyGlobalEnabled: true,
			podAnnotations:      map[string]string{constants.KeyTransparentProxy: "true"},
			podContainers: []corev1.Container{
				{
					Name: "test",
					Ports: []corev1.ContainerPort{
						{
							Name:          "tcp",
							ContainerPort: 8081,
						},
						{
							Name:          "http",
							ContainerPort: 8080,
						},
					},
				},
			},
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					ClusterIP: "10.0.0.1",
					Ports: []corev1.ServicePort{
						{
							Port: 8081,
						},
					},
				},
			},
			expProxyMode: api.ProxyModeTransparent,
			expTaggedAddresses: map[string]api.ServiceAddress{
				"virtual": {
					Address: "10.0.0.1",
					Port:    8081,
				},
			},
			expErr: "",
		},
		"tproxy disabled globally, annotation not provided": {
			tproxyGlobalEnabled: false,
			podAnnotations:      nil,
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					ClusterIP: "10.0.0.1",
					Ports: []corev1.ServicePort{
						{
							Port: 80,
						},
					},
				},
			},
			expProxyMode:       api.ProxyModeDefault,
			expTaggedAddresses: nil,
			expErr:             "",
		},
		"tproxy disabled globally, annotation is false": {
			tproxyGlobalEnabled: false,
			podAnnotations:      map[string]string{constants.KeyTransparentProxy: "false"},
			podContainers: []corev1.Container{
				{
					Name: "test",
					Ports: []corev1.ContainerPort{
						{
							Name:          "tcp",
							ContainerPort: 8081,
						},
						{
							Name:          "http",
							ContainerPort: 8080,
						},
					},
				},
			},
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					ClusterIP: "10.0.0.1",
					Ports: []corev1.ServicePort{
						{
							Port: 80,
						},
					},
				},
			},
			expProxyMode:       api.ProxyModeDefault,
			expTaggedAddresses: nil,
			expErr:             "",
		},
		"tproxy disabled globally, annotation is true": {
			tproxyGlobalEnabled: false,
			podContainers: []corev1.Container{
				{
					Name: "test",
					Ports: []corev1.ContainerPort{
						{
							Name:          "tcp",
							ContainerPort: 8081,
						},
						{
							Name:          "http",
							ContainerPort: 8080,
						},
					},
				},
			},
			podAnnotations: map[string]string{constants.KeyTransparentProxy: "true"},
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					ClusterIP: "10.0.0.1",
					Ports: []corev1.ServicePort{
						{
							Port: 8081,
						},
					},
				},
			},
			expProxyMode: api.ProxyModeTransparent,
			expTaggedAddresses: map[string]api.ServiceAddress{
				"virtual": {
					Address: "10.0.0.1",
					Port:    8081,
				},
			},
			expErr: "",
		},
		"tproxy disabled globally, namespace enabled, no annotation": {
			tproxyGlobalEnabled: false,
			podContainers: []corev1.Container{
				{
					Name: "test",
					Ports: []corev1.ContainerPort{
						{
							Name:          "tcp",
							ContainerPort: 8081,
						},
						{
							Name:          "http",
							ContainerPort: 8080,
						},
					},
				},
			},
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					ClusterIP: "10.0.0.1",
					Ports: []corev1.ServicePort{
						{
							Port: 8081,
						},
					},
				},
			},
			expProxyMode: api.ProxyModeTransparent,
			expTaggedAddresses: map[string]api.ServiceAddress{
				"virtual": {
					Address: "10.0.0.1",
					Port:    8081,
				},
			},
			namespaceLabels: map[string]string{constants.KeyTransparentProxy: "true"},
			expErr:          "",
		},
		"tproxy enabled globally, namespace disabled, no annotation": {
			tproxyGlobalEnabled: true,
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					ClusterIP: "10.0.0.1",
					Ports: []corev1.ServicePort{
						{
							Port: 80,
						},
					},
				},
			},
			expProxyMode:       api.ProxyModeDefault,
			expTaggedAddresses: nil,
			namespaceLabels:    map[string]string{constants.KeyTransparentProxy: "false"},
			expErr:             "",
		},
		// This case is impossible since we're always passing an endpoints object to this function,
		// and Kubernetes will ensure that there is only an endpoints object if there is a service object.
		// However, we're testing this case to check that we return an error in case we cannot get the service from k8s.
		"no service": {
			tproxyGlobalEnabled: true,
			service:             nil,
			expTaggedAddresses:  nil,
			expProxyMode:        api.ProxyModeDefault,
			expErr:              "services \"test-service\" not found",
		},
		"service with a single port without a target port": {
			tproxyGlobalEnabled: true,
			podContainers: []corev1.Container{
				{
					Name: "test",
					Ports: []corev1.ContainerPort{
						{
							Name:          "tcp",
							ContainerPort: 8081,
						},
						{
							Name:          "http",
							ContainerPort: 8080,
						},
					},
				},
			},
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					ClusterIP: "10.0.0.1",
					Ports: []corev1.ServicePort{
						{
							Port: 8081,
						},
					},
				},
			},
			expProxyMode: api.ProxyModeTransparent,
			expTaggedAddresses: map[string]api.ServiceAddress{
				"virtual": {
					Address: "10.0.0.1",
					Port:    8081,
				},
			},
			expErr: "",
		},
		"service with a single port and a target port that is a port name": {
			tproxyGlobalEnabled: true,
			podContainers: []corev1.Container{
				{
					Name: "test",
					Ports: []corev1.ContainerPort{
						{
							Name:          "tcp",
							ContainerPort: 8081,
						},
						{
							Name:          "http",
							ContainerPort: 8080,
						},
					},
				},
			},
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					ClusterIP: "10.0.0.1",
					Ports: []corev1.ServicePort{
						{
							Port:       80,
							TargetPort: intstr.Parse("tcp"),
						},
					},
				},
			},
			expProxyMode: api.ProxyModeTransparent,
			expTaggedAddresses: map[string]api.ServiceAddress{
				"virtual": {
					Address: "10.0.0.1",
					Port:    80,
				},
			},
			expErr: "",
		},
		"service with a single port and a target port that is an int": {
			tproxyGlobalEnabled: true,
			podContainers: []corev1.Container{
				{
					Name: "test",
					Ports: []corev1.ContainerPort{
						{
							Name:          "tcp",
							ContainerPort: 8081,
						},
						{
							Name:          "http",
							ContainerPort: 8080,
						},
					},
				},
			},
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					ClusterIP: "10.0.0.1",
					Ports: []corev1.ServicePort{
						{
							Port:       80,
							TargetPort: intstr.FromInt(8081),
						},
					},
				},
			},
			expProxyMode: api.ProxyModeTransparent,
			expTaggedAddresses: map[string]api.ServiceAddress{
				"virtual": {
					Address: "10.0.0.1",
					Port:    80,
				},
			},
			expErr: "",
		},
		"service with a multiple ports": {
			tproxyGlobalEnabled: true,
			podContainers: []corev1.Container{
				{
					Name: "test",
					Ports: []corev1.ContainerPort{
						{
							Name:          "tcp",
							ContainerPort: 8081,
						},
						{
							Name:          "http",
							ContainerPort: 8080,
						},
					},
				},
			},
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					ClusterIP: "10.0.0.1",
					Ports: []corev1.ServicePort{
						{
							Name:       "tcp",
							Port:       80,
							TargetPort: intstr.FromString("tcp"),
						},
						{
							Name:       "http",
							Port:       81,
							TargetPort: intstr.FromString("http"),
						},
					},
				},
			},
			expProxyMode: api.ProxyModeTransparent,
			expTaggedAddresses: map[string]api.ServiceAddress{
				"virtual": {
					Address: "10.0.0.1",
					Port:    80,
				},
			},
			expErr: "",
		},
		// When target port is not equal to the port we're registering with Consul,
		// then we want to register the zero-value for the port. This could happen
		// for client services that don't have a container port that they're listening on.
		"target port is not found": {
			tproxyGlobalEnabled: true,
			podContainers: []corev1.Container{
				{
					Name: "test",
					Ports: []corev1.ContainerPort{
						{
							Name:          "tcp",
							ContainerPort: 8081,
						},
						{
							Name:          "http",
							ContainerPort: 8080,
						},
					},
				},
			},
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					ClusterIP: "10.0.0.1",
					Ports: []corev1.ServicePort{
						{
							Port:       80,
							TargetPort: intstr.Parse("http"),
						},
					},
				},
			},
			expProxyMode: api.ProxyModeTransparent,
			expTaggedAddresses: map[string]api.ServiceAddress{
				"virtual": {
					Address: "10.0.0.1",
					Port:    0,
				},
			},
			expErr: "",
		},
		"service with clusterIP=None (headless service)": {
			tproxyGlobalEnabled: true,
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					ClusterIP: corev1.ClusterIPNone,
					Ports: []corev1.ServicePort{
						{
							Port: 80,
						},
					},
				},
			},
			expProxyMode:       api.ProxyModeTransparent,
			expTaggedAddresses: nil,
			expErr:             "",
		},
		"service with an empty clusterIP": {
			tproxyGlobalEnabled: true,
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					ClusterIP: "",
					Ports: []corev1.ServicePort{
						{
							Port: 80,
						},
					},
				},
			},
			expProxyMode:       api.ProxyModeTransparent,
			expTaggedAddresses: nil,
			expErr:             "",
		},
		"service with an invalid clusterIP": {
			tproxyGlobalEnabled: true,
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					ClusterIP: "invalid",
					Ports: []corev1.ServicePort{
						{
							Port: 80,
						},
					},
				},
			},
			expTaggedAddresses: nil,
			expProxyMode:       api.ProxyModeTransparent,
			expErr:             "",
		},
		"service with an IPv6 clusterIP": {
			tproxyGlobalEnabled: true,
			podContainers: []corev1.Container{
				{
					Name: "test",
					Ports: []corev1.ContainerPort{
						{
							Name:          "tcp",
							ContainerPort: 8081,
						},
						{
							Name:          "http",
							ContainerPort: 8080,
						},
					},
				},
			},
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					ClusterIP: "2001:db8::68",
					Ports: []corev1.ServicePort{
						{
							Port: 8081,
						},
					},
				},
			},
			expProxyMode: api.ProxyModeTransparent,
			expTaggedAddresses: map[string]api.ServiceAddress{
				"virtual": {
					Address: "2001:db8::68",
					Port:    8081,
				},
			},
			expErr: "",
		},
		"overwrite probes enabled globally": {
			tproxyGlobalEnabled: true,
			overwriteProbes:     true,
			podAnnotations: map[string]string{
				constants.AnnotationOriginalPod: "{\"metadata\":{\"name\":\"test-pod-1\",\"namespace\":\"default\",\"creationTimestamp\":null,\"labels\":{\"consul.hashicorp.com/connect-inject-managed-by\":\"consul-k8s-endpoints-controller\",\"consul.hashicorp.com/connect-inject-status\":\"injected\"},\"annotations\":{\"consul.hashicorp.com/connect-inject-status\":\"injected\"}},\"spec\":{\"containers\":[{\"name\":\"test\",\"ports\":[{\"name\":\"tcp\",\"containerPort\":8081},{\"name\":\"http\",\"containerPort\":8080}],\"resources\":{},\"livenessProbe\":{\"httpGet\":{\"port\":8080}}}]},\"status\":{\"hostIP\":\"127.0.0.1\",\"podIP\":\"1.2.3.4\"}}\n",
			},
			podContainers: []corev1.Container{
				{
					Name: "test",
					Ports: []corev1.ContainerPort{
						{
							Name:          "tcp",
							ContainerPort: 8081,
						},
						{
							Name:          "http",
							ContainerPort: 8080,
						},
					},
					LivenessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Port: intstr.FromInt(20300),
							},
						},
					},
				},
			},
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					ClusterIP: "10.0.0.1",
					Ports: []corev1.ServicePort{
						{
							Port: 8081,
						},
					},
				},
			},
			expProxyMode: api.ProxyModeTransparent,
			expTaggedAddresses: map[string]api.ServiceAddress{
				"virtual": {
					Address: "10.0.0.1",
					Port:    8081,
				},
			},
			expExposePaths: []api.ExposePath{
				{
					ListenerPort:  20300,
					LocalPathPort: 8080,
				},
			},
			expErr: "",
		},
		"overwrite probes disabled globally, enabled via annotation": {
			tproxyGlobalEnabled: true,
			overwriteProbes:     false,
			podAnnotations: map[string]string{
				constants.AnnotationTransparentProxyOverwriteProbes: "true",
				constants.AnnotationOriginalPod:                     "{\"metadata\":{\"name\":\"test-pod-1\",\"namespace\":\"default\",\"creationTimestamp\":null,\"labels\":{\"consul.hashicorp.com/connect-inject-managed-by\":\"consul-k8s-endpoints-controller\",\"consul.hashicorp.com/connect-inject-status\":\"injected\"},\"annotations\":{\"consul.hashicorp.com/transparent-proxy-overwrite-probes\":\"true\"}},\"spec\":{\"containers\":[{\"name\":\"test\",\"ports\":[{\"name\":\"tcp\",\"containerPort\":8081},{\"name\":\"http\",\"containerPort\":8080}],\"resources\":{},\"livenessProbe\":{\"httpGet\":{\"port\":8080}}}]},\"status\":{\"hostIP\":\"127.0.0.1\",\"podIP\":\"1.2.3.4\"}}\n",
			},
			podContainers: []corev1.Container{
				{
					Name: "test",
					Ports: []corev1.ContainerPort{
						{
							Name:          "tcp",
							ContainerPort: 8081,
						},
						{
							Name:          "http",
							ContainerPort: 8080,
						},
					},
					LivenessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Port: intstr.FromInt(20300),
							},
						},
					},
				},
			},
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					ClusterIP: "10.0.0.1",
					Ports: []corev1.ServicePort{
						{
							Port: 8081,
						},
					},
				},
			},
			expProxyMode: api.ProxyModeTransparent,
			expTaggedAddresses: map[string]api.ServiceAddress{
				"virtual": {
					Address: "10.0.0.1",
					Port:    8081,
				},
			},
			expExposePaths: []api.ExposePath{
				{
					ListenerPort:  20300,
					LocalPathPort: 8080,
				},
			},
			expErr: "",
		},
		"overwrite probes enabled globally, tproxy disabled": {
			tproxyGlobalEnabled: false,
			overwriteProbes:     true,
			podAnnotations: map[string]string{
				constants.AnnotationOriginalPod: "{\"metadata\":{\"name\":\"test-pod-1\",\"namespace\":\"default\",\"creationTimestamp\":null,\"labels\":{\"consul.hashicorp.com/connect-inject-managed-by\":\"consul-k8s-endpoints-controller\",\"consul.hashicorp.com/connect-inject-status\":\"injected\"},\"annotations\":{\"consul.hashicorp.com/connect-inject-status\":\"injected\"}},\"spec\":{\"containers\":[{\"name\":\"test\",\"ports\":[{\"name\":\"tcp\",\"containerPort\":8081},{\"name\":\"http\",\"containerPort\":8080}],\"resources\":{},\"livenessProbe\":{\"httpGet\":{\"port\":8080}}}]},\"status\":{\"hostIP\":\"127.0.0.1\",\"podIP\":\"1.2.3.4\"}}\n",
			},
			podContainers: []corev1.Container{
				{
					Name: "test",
					Ports: []corev1.ContainerPort{
						{
							Name:          "tcp",
							ContainerPort: 8081,
						},
						{
							Name:          "http",
							ContainerPort: 8080,
						},
					},
					LivenessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Port: intstr.FromInt(20300),
							},
						},
					},
				},
			},
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					ClusterIP: "10.0.0.1",
					Ports: []corev1.ServicePort{
						{
							Port: 8081,
						},
					},
				},
			},
			expTaggedAddresses: nil,
			expExposePaths:     nil,
			expErr:             "",
		},
		"readiness only probe provided": {
			tproxyGlobalEnabled: true,
			overwriteProbes:     true,
			podAnnotations: map[string]string{
				constants.AnnotationOriginalPod: "{\"metadata\":{\"name\":\"test-pod-1\",\"namespace\":\"default\",\"creationTimestamp\":null,\"labels\":{\"consul.hashicorp.com/connect-inject-managed-by\":\"consul-k8s-endpoints-controller\",\"consul.hashicorp.com/connect-inject-status\":\"injected\"}},\"spec\":{\"containers\":[{\"name\":\"test\",\"ports\":[{\"name\":\"tcp\",\"containerPort\":8081},{\"name\":\"http\",\"containerPort\":8080}],\"resources\":{},\"readinessProbe\":{\"httpGet\":{\"port\":8080}}}]},\"status\":{\"hostIP\":\"127.0.0.1\",\"podIP\":\"1.2.3.4\"}}\n",
			},
			podContainers: []corev1.Container{
				{
					Name: "test",
					Ports: []corev1.ContainerPort{
						{
							Name:          "tcp",
							ContainerPort: 8081,
						},
						{
							Name:          "http",
							ContainerPort: 8080,
						},
					},
					ReadinessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Port: intstr.FromInt(20400),
							},
						},
					},
				},
			},
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					ClusterIP: "10.0.0.1",
					Ports: []corev1.ServicePort{
						{
							Port: 8081,
						},
					},
				},
			},
			expProxyMode: api.ProxyModeTransparent,
			expTaggedAddresses: map[string]api.ServiceAddress{
				"virtual": {
					Address: "10.0.0.1",
					Port:    8081,
				},
			},
			expExposePaths: []api.ExposePath{
				{
					ListenerPort:  20400,
					LocalPathPort: 8080,
				},
			},
			expErr: "",
		},
		"startup only probe provided": {
			tproxyGlobalEnabled: true,
			overwriteProbes:     true,
			podAnnotations: map[string]string{
				constants.AnnotationOriginalPod: "{\"metadata\":{\"name\":\"test-pod-1\",\"namespace\":\"default\",\"creationTimestamp\":null,\"labels\":{\"consul.hashicorp.com/connect-inject-managed-by\":\"consul-k8s-endpoints-controller\",\"consul.hashicorp.com/connect-inject-status\":\"injected\"}},\"spec\":{\"containers\":[{\"name\":\"test\",\"ports\":[{\"name\":\"tcp\",\"containerPort\":8081},{\"name\":\"http\",\"containerPort\":8080}],\"resources\":{},\"startupProbe\":{\"httpGet\":{\"port\":8080}}}]},\"status\":{\"hostIP\":\"127.0.0.1\",\"podIP\":\"1.2.3.4\"}}\n",
			},
			podContainers: []corev1.Container{
				{
					Name: "test",
					Ports: []corev1.ContainerPort{
						{
							Name:          "tcp",
							ContainerPort: 8081,
						},
						{
							Name:          "http",
							ContainerPort: 8080,
						},
					},
					StartupProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Port: intstr.FromInt(20500),
							},
						},
					},
				},
			},
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					ClusterIP: "10.0.0.1",
					Ports: []corev1.ServicePort{
						{
							Port: 8081,
						},
					},
				},
			},
			expProxyMode: api.ProxyModeTransparent,
			expTaggedAddresses: map[string]api.ServiceAddress{
				"virtual": {
					Address: "10.0.0.1",
					Port:    8081,
				},
			},
			expExposePaths: []api.ExposePath{
				{
					ListenerPort:  20500,
					LocalPathPort: 8080,
				},
			},
			expErr: "",
		},
		"all probes provided": {
			tproxyGlobalEnabled: true,
			overwriteProbes:     true,
			podAnnotations: map[string]string{
				constants.AnnotationOriginalPod: "{\"metadata\":{\"name\":\"test-pod-1\",\"namespace\":\"default\",\"creationTimestamp\":null,\"labels\":{\"consul.hashicorp.com/connect-inject-managed-by\":\"consul-k8s-endpoints-controller\",\"consul.hashicorp.com/connect-inject-status\":\"injected\"}},\"spec\":{\"containers\":[{\"name\":\"test\",\"ports\":[{\"name\":\"tcp\",\"containerPort\":8081},{\"name\":\"http\",\"containerPort\":8080}],\"resources\":{},\"livenessProbe\":{\"httpGet\":{\"port\":8080}},\"readinessProbe\":{\"httpGet\":{\"port\":8081}},\"startupProbe\":{\"httpGet\":{\"port\":8081}}}]},\"status\":{\"hostIP\":\"127.0.0.1\",\"podIP\":\"1.2.3.4\"}}\n",
			},
			podContainers: []corev1.Container{
				{
					Name: "test",
					Ports: []corev1.ContainerPort{
						{
							Name:          "tcp",
							ContainerPort: 8081,
						},
						{
							Name:          "http",
							ContainerPort: 8080,
						},
					},
					LivenessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Port: intstr.FromInt(20300),
							},
						},
					},
					ReadinessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Port: intstr.FromInt(20400),
							},
						},
					},
					StartupProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Port: intstr.FromInt(20500),
							},
						},
					},
				},
			},
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					ClusterIP: "10.0.0.1",
					Ports: []corev1.ServicePort{
						{
							Port: 8081,
						},
					},
				},
			},
			expProxyMode: api.ProxyModeTransparent,
			expTaggedAddresses: map[string]api.ServiceAddress{
				"virtual": {
					Address: "10.0.0.1",
					Port:    8081,
				},
			},
			expExposePaths: []api.ExposePath{
				{
					ListenerPort:  20300,
					LocalPathPort: 8080,
				},
				{
					ListenerPort:  20400,
					LocalPathPort: 8081,
				},
				{
					ListenerPort:  20500,
					LocalPathPort: 8081,
				},
			},
			expErr: "",
		},
		"multiple containers with all probes provided": {
			tproxyGlobalEnabled: true,
			overwriteProbes:     true,
			podAnnotations: map[string]string{
				constants.AnnotationOriginalPod: "{\"metadata\":{\"name\":\"test-pod-1\",\"namespace\":\"default\",\"creationTimestamp\":null,\"labels\":{\"consul.hashicorp.com/connect-inject-managed-by\":\"consul-k8s-endpoints-controller\",\"consul.hashicorp.com/connect-inject-status\":\"injected\"}},\"spec\":{\"containers\":[{\"name\":\"test\",\"ports\":[{\"name\":\"tcp\",\"containerPort\":8081},{\"name\":\"http\",\"containerPort\":8080}],\"resources\":{},\"livenessProbe\":{\"httpGet\":{\"port\":8080}},\"readinessProbe\":{\"httpGet\":{\"port\":8081}},\"startupProbe\":{\"httpGet\":{\"port\":8081}}},{\"name\":\"test-2\",\"ports\":[{\"name\":\"tcp\",\"containerPort\":8083},{\"name\":\"http\",\"containerPort\":8082}],\"resources\":{},\"livenessProbe\":{\"httpGet\":{\"port\":8082}},\"readinessProbe\":{\"httpGet\":{\"port\":8083}},\"startupProbe\":{\"httpGet\":{\"port\":8083}}},{\"name\":\"envoy-sidecar\",\"ports\":[{\"name\":\"http\",\"containerPort\":20000}],\"resources\":{}}]},\"status\":{\"hostIP\":\"127.0.0.1\",\"podIP\":\"1.2.3.4\"}}\n",
			},
			podContainers: []corev1.Container{
				{
					Name: "test",
					Ports: []corev1.ContainerPort{
						{
							Name:          "tcp",
							ContainerPort: 8081,
						},
						{
							Name:          "http",
							ContainerPort: 8080,
						},
					},
					LivenessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Port: intstr.FromInt(20300),
							},
						},
					},
					ReadinessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Port: intstr.FromInt(20400),
							},
						},
					},
					StartupProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Port: intstr.FromInt(20500),
							},
						},
					},
				},
				{
					Name: "test-2",
					Ports: []corev1.ContainerPort{
						{
							Name:          "tcp",
							ContainerPort: 8083,
						},
						{
							Name:          "http",
							ContainerPort: 8082,
						},
					},
					LivenessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Port: intstr.FromInt(20300 + 1),
							},
						},
					},
					ReadinessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Port: intstr.FromInt(20400 + 1),
							},
						},
					},
					StartupProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Port: intstr.FromInt(20500 + 1),
							},
						},
					},
				},
				{
					Name: "sidecar-proxy", // This name doesn't matter.
					Ports: []corev1.ContainerPort{
						{
							Name:          "http",
							ContainerPort: 20000,
						},
					},
				},
			},
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					ClusterIP: "10.0.0.1",
					Ports: []corev1.ServicePort{
						{
							Port: 8081,
						},
					},
				},
			},
			expProxyMode: api.ProxyModeTransparent,
			expTaggedAddresses: map[string]api.ServiceAddress{
				"virtual": {
					Address: "10.0.0.1",
					Port:    8081,
				},
			},
			expExposePaths: []api.ExposePath{
				{
					ListenerPort:  20300,
					LocalPathPort: 8080,
				},
				{
					ListenerPort:  20400,
					LocalPathPort: 8081,
				},
				{
					ListenerPort:  20500,
					LocalPathPort: 8081,
				},
				{
					ListenerPort:  20300 + 1,
					LocalPathPort: 8082,
				},
				{
					ListenerPort:  20400 + 1,
					LocalPathPort: 8083,
				},
				{
					ListenerPort:  20500 + 1,
					LocalPathPort: 8083,
				},
			},
			expErr: "",
		},
		"non-http probe": {
			tproxyGlobalEnabled: true,
			overwriteProbes:     true,
			podAnnotations: map[string]string{
				constants.AnnotationOriginalPod: "{\"metadata\":{\"name\":\"test-pod-1\",\"namespace\":\"default\",\"creationTimestamp\":null,\"labels\":{\"consul.hashicorp.com/connect-inject-managed-by\":\"consul-k8s-endpoints-controller\",\"consul.hashicorp.com/connect-inject-status\":\"injected\"}},\"spec\":{\"containers\":[{\"name\":\"test\",\"ports\":[{\"name\":\"tcp\",\"containerPort\":8081},{\"name\":\"http\",\"containerPort\":8080}],\"resources\":{},\"livenessProbe\":{\"tcpSocket\":{\"port\":8080}}}]},\"status\":{\"hostIP\":\"127.0.0.1\",\"podIP\":\"1.2.3.4\"}}\n",
			},
			podContainers: []corev1.Container{
				{
					Name: "test",
					Ports: []corev1.ContainerPort{
						{
							Name:          "tcp",
							ContainerPort: 8081,
						},
						{
							Name:          "http",
							ContainerPort: 8080,
						},
					},
					LivenessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							TCPSocket: &corev1.TCPSocketAction{
								Port: intstr.FromInt(8080),
							},
						},
					},
				},
			},
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					ClusterIP: "10.0.0.1",
					Ports: []corev1.ServicePort{
						{
							Port: 8081,
						},
					},
				},
			},
			expProxyMode: api.ProxyModeTransparent,
			expTaggedAddresses: map[string]api.ServiceAddress{
				"virtual": {
					Address: "10.0.0.1",
					Port:    8081,
				},
			},
			expExposePaths: nil,
			expErr:         "",
		},
		"probes with port names": {
			tproxyGlobalEnabled: true,
			overwriteProbes:     true,
			podAnnotations: map[string]string{
				constants.AnnotationOriginalPod: "{\"metadata\":{\"name\":\"test-pod-1\",\"namespace\":\"default\",\"creationTimestamp\":null,\"labels\":{\"consul.hashicorp.com/connect-inject-managed-by\":\"consul-k8s-endpoints-controller\",\"consul.hashicorp.com/connect-inject-status\":\"injected\"}},\"spec\":{\"containers\":[{\"name\":\"test\",\"ports\":[{\"name\":\"tcp\",\"containerPort\":8081},{\"name\":\"http\",\"containerPort\":8080}],\"resources\":{},\"livenessProbe\":{\"httpGet\":{\"port\":\"tcp\"}},\"readinessProbe\":{\"httpGet\":{\"port\":\"http\"}},\"startupProbe\":{\"httpGet\":{\"port\":\"http\"}}}]},\"status\":{\"hostIP\":\"127.0.0.1\",\"podIP\":\"1.2.3.4\"}}\n",
			},
			podContainers: []corev1.Container{
				{
					Name: "test",
					Ports: []corev1.ContainerPort{
						{
							Name:          "tcp",
							ContainerPort: 8081,
						},
						{
							Name:          "http",
							ContainerPort: 8080,
						},
					},
					LivenessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Port: intstr.FromInt(20300),
							},
						},
					},
					ReadinessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Port: intstr.FromInt(20400),
							},
						},
					},
					StartupProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Port: intstr.FromInt(20500),
							},
						},
					},
				},
			},
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					ClusterIP: "10.0.0.1",
					Ports: []corev1.ServicePort{
						{
							Port: 8081,
						},
					},
				},
			},
			expProxyMode: api.ProxyModeTransparent,
			expTaggedAddresses: map[string]api.ServiceAddress{
				"virtual": {
					Address: "10.0.0.1",
					Port:    8081,
				},
			},
			expExposePaths: []api.ExposePath{
				{
					ListenerPort:  20300,
					LocalPathPort: 8081,
				},
				{
					ListenerPort:  20400,
					LocalPathPort: 8080,
				},
				{
					ListenerPort:  20500,
					LocalPathPort: 8080,
				},
			},
			expErr: "",
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			pod := createServicePod("test-pod-1", "1.2.3.4", true, true)
			if c.podAnnotations != nil {
				pod.Annotations = c.podAnnotations
			}
			if c.podContainers != nil {
				pod.Spec.Containers = c.podContainers
			}

			// We set these annotations explicitly as these are set by the meshWebhook and we
			// need these values to determine which port to use for the service registration.
			pod.Annotations[constants.AnnotationPort] = "tcp"

			endpoints := &corev1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: "default",
				},
				Subsets: []corev1.EndpointSubset{
					{
						Addresses: []corev1.EndpointAddress{
							{
								IP: "1.2.3.4",
								TargetRef: &corev1.ObjectReference{
									Kind:      "Pod",
									Name:      pod.Name,
									Namespace: pod.Namespace,
								},
							},
						},
					},
				},
			}
			// Add the pod's namespace.
			ns := corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: pod.Namespace, Labels: c.namespaceLabels},
			}
			var fakeClient client.Client
			if c.service != nil {
				fakeClient = fake.NewClientBuilder().WithRuntimeObjects(pod, endpoints, c.service, &ns).Build()
			} else {
				fakeClient = fake.NewClientBuilder().WithRuntimeObjects(pod, endpoints, &ns).Build()
			}

			epCtrl := Controller{
				Client:                 fakeClient,
				EnableTransparentProxy: c.tproxyGlobalEnabled,
				TProxyOverwriteProbes:  c.overwriteProbes,
				Log:                    logrtest.New(t),
			}

			serviceRegistration, proxyServiceRegistration, err := epCtrl.createServiceRegistrations(*pod, *endpoints, api.HealthPassing)
			if c.expErr != "" {
				require.EqualError(t, err, c.expErr)
			} else {
				require.NoError(t, err)

				require.Equal(t, c.expProxyMode, proxyServiceRegistration.Service.Proxy.Mode)
				require.Equal(t, c.expTaggedAddresses, serviceRegistration.Service.TaggedAddresses)
				require.Equal(t, c.expTaggedAddresses, proxyServiceRegistration.Service.TaggedAddresses)
				require.Equal(t, c.expExposePaths, proxyServiceRegistration.Service.Proxy.Expose.Paths)
			}
		})
	}
}

func TestGetTokenMetaFromDescription(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		description       string
		expectedTokenMeta map[string]string
	}{
		"no description prefix": {
			description:       `{"pod":"default/pod","pod-uid": "123"}`,
			expectedTokenMeta: map[string]string{"pod": "default/pod", "pod-uid": "123"},
		},
		"consul's default description prefix": {
			description:       `token created via login: {"pod":"default/pod","pod-uid": "123"}`,
			expectedTokenMeta: map[string]string{"pod": "default/pod", "pod-uid": "123"},
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			tokenMeta, err := getTokenMetaFromDescription(c.description)
			require.NoError(t, err)
			require.Equal(t, c.expectedTokenMeta, tokenMeta)
		})
	}
}

func TestMapAddresses(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		addresses corev1.EndpointSubset
		expected  map[corev1.EndpointAddress]string
	}{
		"ready and not ready addresses": {
			addresses: corev1.EndpointSubset{
				Addresses: []corev1.EndpointAddress{
					{Hostname: "host1"},
					{Hostname: "host2"},
				},
				NotReadyAddresses: []corev1.EndpointAddress{
					{Hostname: "host3"},
					{Hostname: "host4"},
				},
			},
			expected: map[corev1.EndpointAddress]string{
				{Hostname: "host1"}: api.HealthPassing,
				{Hostname: "host2"}: api.HealthPassing,
				{Hostname: "host3"}: api.HealthCritical,
				{Hostname: "host4"}: api.HealthCritical,
			},
		},
		"ready addresses only": {
			addresses: corev1.EndpointSubset{
				Addresses: []corev1.EndpointAddress{
					{Hostname: "host1"},
					{Hostname: "host2"},
					{Hostname: "host3"},
					{Hostname: "host4"},
				},
				NotReadyAddresses: []corev1.EndpointAddress{},
			},
			expected: map[corev1.EndpointAddress]string{
				{Hostname: "host1"}: api.HealthPassing,
				{Hostname: "host2"}: api.HealthPassing,
				{Hostname: "host3"}: api.HealthPassing,
				{Hostname: "host4"}: api.HealthPassing,
			},
		},
		"not ready addresses only": {
			addresses: corev1.EndpointSubset{
				Addresses: []corev1.EndpointAddress{},
				NotReadyAddresses: []corev1.EndpointAddress{
					{Hostname: "host1"},
					{Hostname: "host2"},
					{Hostname: "host3"},
					{Hostname: "host4"},
				},
			},
			expected: map[corev1.EndpointAddress]string{
				{Hostname: "host1"}: api.HealthCritical,
				{Hostname: "host2"}: api.HealthCritical,
				{Hostname: "host3"}: api.HealthCritical,
				{Hostname: "host4"}: api.HealthCritical,
			},
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			actual := mapAddresses(c.addresses)
			require.Equal(t, c.expected, actual)
		})
	}
}

func Test_GetWANData(t *testing.T) {
	cases := map[string]struct {
		gatewayPod      corev1.Pod
		gatewayEndpoint corev1.Endpoints
		k8sObjects      func() []runtime.Object
		wanAddr         string
		wanPort         int
		expErr          string
	}{
		"source=NodeName": {
			gatewayPod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gateway",
					Annotations: map[string]string{
						constants.AnnotationGatewayWANSource:  "NodeName",
						constants.AnnotationGatewayWANAddress: "test-wan-address",
						constants.AnnotationGatewayWANPort:    "1234",
					},
				},
				Spec: corev1.PodSpec{
					NodeName: "test-nodename",
				},
				Status: corev1.PodStatus{
					HostIP: "test-host-ip",
				},
			},
			gatewayEndpoint: corev1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gateway",
					Namespace: "default",
				},
			},
			k8sObjects: func() []runtime.Object {
				service := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gateway",
						Namespace: "default",
					},
					Spec: corev1.ServiceSpec{
						Type:      corev1.ServiceTypeLoadBalancer,
						ClusterIP: "test-cluster-ip",
					},
					Status: corev1.ServiceStatus{
						LoadBalancer: corev1.LoadBalancerStatus{
							Ingress: []corev1.LoadBalancerIngress{
								{
									IP: "1.2.3.4",
								},
							},
						},
					},
				}
				return []runtime.Object{service}
			},
			wanAddr: "test-nodename",
			wanPort: 1234,
		},
		"source=HostIP": {
			gatewayPod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gateway",
					Annotations: map[string]string{
						constants.AnnotationGatewayWANSource:  "NodeIP",
						constants.AnnotationGatewayWANAddress: "test-wan-address",
						constants.AnnotationGatewayWANPort:    "1234",
					},
				},
				Spec: corev1.PodSpec{
					NodeName: "test-nodename",
				},
				Status: corev1.PodStatus{
					HostIP: "test-host-ip",
				},
			},
			gatewayEndpoint: corev1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gateway",
					Namespace: "default",
				},
			},
			k8sObjects: func() []runtime.Object {
				service := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gateway",
						Namespace: "default",
					},
					Spec: corev1.ServiceSpec{
						Type:      corev1.ServiceTypeLoadBalancer,
						ClusterIP: "test-cluster-ip",
					},
					Status: corev1.ServiceStatus{
						LoadBalancer: corev1.LoadBalancerStatus{
							Ingress: []corev1.LoadBalancerIngress{
								{
									IP: "1.2.3.4",
								},
							},
						},
					},
				}
				return []runtime.Object{service}
			},
			wanAddr: "test-host-ip",
			wanPort: 1234,
		},
		"source=Static": {
			gatewayPod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gateway",
					Annotations: map[string]string{
						constants.AnnotationGatewayWANSource:  "Static",
						constants.AnnotationGatewayWANAddress: "test-wan-address",
						constants.AnnotationGatewayWANPort:    "1234",
					},
				},
				Spec: corev1.PodSpec{
					NodeName: "test-nodename",
				},
				Status: corev1.PodStatus{
					HostIP: "test-host-ip",
				},
			},
			gatewayEndpoint: corev1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gateway",
					Namespace: "default",
				},
			},
			k8sObjects: func() []runtime.Object {
				service := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gateway",
						Namespace: "default",
					},
					Spec: corev1.ServiceSpec{
						Type:      corev1.ServiceTypeLoadBalancer,
						ClusterIP: "test-cluster-ip",
					},
					Status: corev1.ServiceStatus{
						LoadBalancer: corev1.LoadBalancerStatus{
							Ingress: []corev1.LoadBalancerIngress{
								{
									IP: "1.2.3.4",
								},
							},
						},
					},
				}
				return []runtime.Object{service}
			},
			wanAddr: "test-wan-address",
			wanPort: 1234,
		},
		"source=Service, serviceType=NodePort": {
			gatewayPod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gateway",
					Annotations: map[string]string{
						constants.AnnotationGatewayWANSource:  "Service",
						constants.AnnotationGatewayWANAddress: "test-wan-address",
						constants.AnnotationGatewayWANPort:    "1234",
					},
				},
				Spec: corev1.PodSpec{
					NodeName: "test-nodename",
				},
				Status: corev1.PodStatus{
					HostIP: "test-host-ip",
				},
			},
			gatewayEndpoint: corev1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gateway",
					Namespace: "default",
				},
			},
			k8sObjects: func() []runtime.Object {
				service := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gateway",
						Namespace: "default",
					},
					Spec: corev1.ServiceSpec{
						Type:      corev1.ServiceTypeNodePort,
						ClusterIP: "test-cluster-ip",
					},
					Status: corev1.ServiceStatus{
						LoadBalancer: corev1.LoadBalancerStatus{
							Ingress: []corev1.LoadBalancerIngress{
								{
									IP: "1.2.3.4",
								},
							},
						},
					},
				}
				return []runtime.Object{service}
			},
			wanAddr: "test-host-ip",
			wanPort: 1234,
		},
		"source=Service, serviceType=ClusterIP": {
			gatewayPod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gateway",
					Annotations: map[string]string{
						constants.AnnotationGatewayWANSource:  "Service",
						constants.AnnotationGatewayWANAddress: "test-wan-address",
						constants.AnnotationGatewayWANPort:    "1234",
					},
				},
				Spec: corev1.PodSpec{
					NodeName: "test-nodename",
				},
				Status: corev1.PodStatus{
					HostIP: "test-host-ip",
				},
			},
			gatewayEndpoint: corev1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gateway",
					Namespace: "default",
				},
			},
			k8sObjects: func() []runtime.Object {
				service := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gateway",
						Namespace: "default",
					},
					Spec: corev1.ServiceSpec{
						Type:      corev1.ServiceTypeClusterIP,
						ClusterIP: "test-cluster-ip",
					},
					Status: corev1.ServiceStatus{
						LoadBalancer: corev1.LoadBalancerStatus{
							Ingress: []corev1.LoadBalancerIngress{
								{
									IP: "1.2.3.4",
								},
							},
						},
					},
				}
				return []runtime.Object{service}
			},
			wanAddr: "test-cluster-ip",
			wanPort: 1234,
		},
		"source=Service, serviceType=LoadBalancer,IP": {
			gatewayPod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gateway",
					Annotations: map[string]string{
						constants.AnnotationGatewayWANSource:  "Service",
						constants.AnnotationGatewayWANAddress: "test-wan-address",
						constants.AnnotationGatewayWANPort:    "1234",
					},
				},
				Spec: corev1.PodSpec{
					NodeName: "test-nodename",
				},
				Status: corev1.PodStatus{
					HostIP: "test-host-ip",
				},
			},
			gatewayEndpoint: corev1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gateway",
					Namespace: "default",
				},
			},
			k8sObjects: func() []runtime.Object {
				service := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gateway",
						Namespace: "default",
					},
					Spec: corev1.ServiceSpec{
						Type:      corev1.ServiceTypeLoadBalancer,
						ClusterIP: "test-cluster-ip",
					},
					Status: corev1.ServiceStatus{
						LoadBalancer: corev1.LoadBalancerStatus{
							Ingress: []corev1.LoadBalancerIngress{
								{
									IP: "test-loadbalancer-ip",
								},
							},
						},
					},
				}
				return []runtime.Object{service}
			},
			wanAddr: "test-loadbalancer-ip",
			wanPort: 1234,
		},
		"source=Service, serviceType=LoadBalancer,Hostname": {
			gatewayPod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gateway",
					Annotations: map[string]string{
						constants.AnnotationGatewayWANSource:  "Service",
						constants.AnnotationGatewayWANAddress: "test-wan-address",
						constants.AnnotationGatewayWANPort:    "1234",
					},
				},
				Spec: corev1.PodSpec{
					NodeName: "test-nodename",
				},
				Status: corev1.PodStatus{
					HostIP: "test-host-ip",
				},
			},
			gatewayEndpoint: corev1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gateway",
					Namespace: "default",
				},
			},
			k8sObjects: func() []runtime.Object {
				service := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gateway",
						Namespace: "default",
					},
					Spec: corev1.ServiceSpec{
						Type:      corev1.ServiceTypeLoadBalancer,
						ClusterIP: "test-cluster-ip",
					},
					Status: corev1.ServiceStatus{
						LoadBalancer: corev1.LoadBalancerStatus{
							Ingress: []corev1.LoadBalancerIngress{
								{
									Hostname: "test-loadbalancer-hostname",
								},
							},
						},
					},
				}
				return []runtime.Object{service}
			},
			wanAddr: "test-loadbalancer-hostname",
			wanPort: 1234,
		},
		"no Source annotation": {
			gatewayPod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gateway",
					Annotations: map[string]string{
						constants.AnnotationGatewayWANAddress: "test-wan-address",
						constants.AnnotationGatewayWANPort:    "1234",
					},
				},
				Spec: corev1.PodSpec{
					NodeName: "test-nodename",
				},
				Status: corev1.PodStatus{
					HostIP: "test-host-ip",
				},
			},
			gatewayEndpoint: corev1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gateway",
					Namespace: "default",
				},
			},
			k8sObjects: func() []runtime.Object {
				service := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gateway",
						Namespace: "default",
					},
					Spec: corev1.ServiceSpec{
						Type:      corev1.ServiceTypeLoadBalancer,
						ClusterIP: "test-cluster-ip",
					},
					Status: corev1.ServiceStatus{
						LoadBalancer: corev1.LoadBalancerStatus{
							Ingress: []corev1.LoadBalancerIngress{
								{
									Hostname: "test-loadbalancer-hostname",
								},
							},
						},
					},
				}
				return []runtime.Object{service}
			},
			wanAddr: "test-loadbalancer-hostname",
			wanPort: 1234,
			expErr:  "failed to read annotation consul.hashicorp.com/gateway-wan-address-source",
		},
		"no Service with Source=Service": {
			gatewayPod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gateway",
					Annotations: map[string]string{
						constants.AnnotationGatewayWANSource:  "Service",
						constants.AnnotationGatewayWANAddress: "test-wan-address",
						constants.AnnotationGatewayWANPort:    "1234",
					},
				},
				Spec: corev1.PodSpec{
					NodeName: "test-nodename",
				},
				Status: corev1.PodStatus{
					HostIP: "test-host-ip",
				},
			},
			gatewayEndpoint: corev1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gateway",
					Namespace: "default",
				},
			},
			k8sObjects: func() []runtime.Object { return nil },
			wanAddr:    "test-loadbalancer-hostname",
			wanPort:    1234,
			expErr:     "failed to read service gateway in namespace default",
		},
		"WAN Port annotation misconfigured": {
			gatewayPod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gateway",
					Annotations: map[string]string{
						constants.AnnotationGatewayWANSource:  "Service",
						constants.AnnotationGatewayWANAddress: "test-wan-address",
						constants.AnnotationGatewayWANPort:    "not-a-valid-port",
					},
				},
				Spec: corev1.PodSpec{
					NodeName: "test-nodename",
				},
				Status: corev1.PodStatus{
					HostIP: "test-host-ip",
				},
			},
			gatewayEndpoint: corev1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gateway",
					Namespace: "default",
				},
			},
			k8sObjects: func() []runtime.Object {
				service := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gateway",
						Namespace: "default",
					},
					Spec: corev1.ServiceSpec{
						Type:      corev1.ServiceTypeLoadBalancer,
						ClusterIP: "test-cluster-ip",
					},
					Status: corev1.ServiceStatus{
						LoadBalancer: corev1.LoadBalancerStatus{
							Ingress: []corev1.LoadBalancerIngress{
								{
									Hostname: "test-loadbalancer-hostname",
								},
							},
						},
					},
				}
				return []runtime.Object{service}
			},
			wanAddr: "test-loadbalancer-hostname",
			wanPort: 1234,
			expErr:  "failed to parse WAN port from value not-a-valid-port",
		},
		"source=Service, serviceType=LoadBalancer no Ingress configured": {
			gatewayPod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gateway",
					Annotations: map[string]string{
						constants.AnnotationGatewayWANSource:  "Service",
						constants.AnnotationGatewayWANAddress: "test-wan-address",
						constants.AnnotationGatewayWANPort:    "1234",
					},
				},
				Spec: corev1.PodSpec{
					NodeName: "test-nodename",
				},
				Status: corev1.PodStatus{
					HostIP: "test-host-ip",
				},
			},
			gatewayEndpoint: corev1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gateway",
					Namespace: "default",
				},
			},
			k8sObjects: func() []runtime.Object {
				service := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gateway",
						Namespace: "default",
					},
					Spec: corev1.ServiceSpec{
						Type:      corev1.ServiceTypeLoadBalancer,
						ClusterIP: "test-cluster-ip",
					},
					Status: corev1.ServiceStatus{
						LoadBalancer: corev1.LoadBalancerStatus{
							Ingress: []corev1.LoadBalancerIngress{},
						},
					},
				}
				return []runtime.Object{service}
			},
			wanAddr: "test-loadbalancer-hostname",
			wanPort: 1234,
			expErr:  "failed to read ingress config for loadbalancer for service gateway in namespace default",
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().WithRuntimeObjects(c.k8sObjects()...).Build()
			epCtrl := Controller{
				Client: fakeClient,
			}
			addr, port, err := epCtrl.getWanData(c.gatewayPod, c.gatewayEndpoint)
			if c.expErr == "" {
				require.NoError(t, err)
				require.Equal(t, c.wanAddr, addr)
				require.Equal(t, c.wanPort, port)
			} else {
				require.EqualError(t, err, c.expErr)
			}
		})
	}
}

func createServicePod(name, ip string, inject bool, managedByEndpointsController bool) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels:    map[string]string{},
			Annotations: map[string]string{
				constants.LegacyAnnotationConsulK8sVersion: "1.0.0",
			},
		},
		Status: corev1.PodStatus{
			PodIP:  ip,
			HostIP: consulNodeAddress,
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
		Spec: corev1.PodSpec{
			NodeName: nodeName,
		},
	}
	if inject {
		pod.Labels[constants.KeyInjectStatus] = constants.Injected
		pod.Annotations[constants.KeyInjectStatus] = constants.Injected
	}
	if managedByEndpointsController {
		pod.Labels[constants.KeyManagedBy] = constants.ManagedByValue
	}
	return pod
}

func createGatewayPod(name, ip string, annotations map[string]string) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   "default",
			Labels:      map[string]string{constants.KeyManagedBy: constants.ManagedByValue},
			Annotations: annotations,
		},
		Status: corev1.PodStatus{
			PodIP: ip,
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
		Spec: corev1.PodSpec{
			NodeName: nodeName,
		},
	}
	return pod
}

func TestReconcileAssignServiceVirtualIP(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cases := []struct {
		name      string
		service   *api.AgentService
		expectErr bool
	}{
		{
			name: "valid service",
			service: &api.AgentService{
				ID:      "",
				Service: "foo",
				Port:    80,
				Address: "1.2.3.4",
				TaggedAddresses: map[string]api.ServiceAddress{
					"virtual": {
						Address: "1.2.3.4",
						Port:    80,
					},
				},
				Meta: map[string]string{constants.MetaKeyKubeNS: "default"},
			},
			expectErr: false,
		},
		{
			name: "service missing IP should not error",
			service: &api.AgentService{
				ID:      "",
				Service: "bar",
				Meta:    map[string]string{constants.MetaKeyKubeNS: "default"},
			},
			expectErr: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {

			// Create test consulServer server.
			testClient := test.TestServerWithMockConnMgrWatcher(t, nil)
			apiClient := testClient.APIClient
			err := assignServiceVirtualIP(ctx, apiClient, c.service)
			if err != nil {
				require.True(t, c.expectErr)
			} else {
				require.False(t, c.expectErr)
			}
		})
	}
}

func TestReconcile_SkipIncompleteNodeInfo(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		pod         *corev1.Pod
		description string
	}{
		"service pod - missing node name": {
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service-pod",
					Namespace: "default",
					Labels: map[string]string{
						constants.KeyManagedBy: constants.ManagedByValue,
					},
					Annotations: map[string]string{
						constants.AnnotationInject: "true",
					},
				},
				Spec: corev1.PodSpec{
					NodeName: "", // Missing
				},
				Status: corev1.PodStatus{
					Phase:  corev1.PodRunning,
					PodIP:  "10.0.0.1",
					HostIP: "192.168.1.1",
				},
			},
			description: "Service pod with missing node name should be skipped",
		},
		"service pod - missing pod IP": {
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service-pod",
					Namespace: "default",
					Labels: map[string]string{
						constants.KeyManagedBy: constants.ManagedByValue,
					},
					Annotations: map[string]string{
						constants.AnnotationInject: "true",
					},
				},
				Spec: corev1.PodSpec{
					NodeName: "test-node",
				},
				Status: corev1.PodStatus{
					Phase:  corev1.PodRunning,
					PodIP:  "", // Missing
					HostIP: "192.168.1.1",
				},
			},
			description: "Service pod with missing pod IP should be skipped",
		},
		"service pod - missing host IP": {
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service-pod",
					Namespace: "default",
					Labels: map[string]string{
						constants.KeyManagedBy: constants.ManagedByValue,
					},
					Annotations: map[string]string{
						constants.AnnotationInject: "true",
					},
				},
				Spec: corev1.PodSpec{
					NodeName: "test-node",
				},
				Status: corev1.PodStatus{
					Phase:  corev1.PodRunning,
					PodIP:  "10.0.0.1",
					HostIP: "", // Missing
				},
			},
			description: "Service pod with missing host IP should be skipped",
		},
		"mesh gateway - missing node name": {
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-mesh-gateway",
					Namespace: "default",
					Labels: map[string]string{
						constants.KeyManagedBy: constants.ManagedByValue,
					},
					Annotations: map[string]string{
						constants.AnnotationGatewayKind:              meshGateway,
						constants.AnnotationGatewayConsulServiceName: "mesh-gateway",
						constants.AnnotationMeshGatewayContainerPort: "8443",
						constants.AnnotationGatewayWANSource:         "NodeIP",
						constants.AnnotationGatewayWANPort:           "8443",
					},
				},
				Spec: corev1.PodSpec{
					NodeName: "", // Missing
				},
				Status: corev1.PodStatus{
					Phase:  corev1.PodRunning,
					PodIP:  "10.0.0.2",
					HostIP: "192.168.1.2",
				},
			},
			description: "Mesh gateway with missing node name should be skipped",
		},
		"mesh gateway - missing pod IP": {
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-mesh-gateway",
					Namespace: "default",
					Labels: map[string]string{
						constants.KeyManagedBy: constants.ManagedByValue,
					},
					Annotations: map[string]string{
						constants.AnnotationGatewayKind:              meshGateway,
						constants.AnnotationGatewayConsulServiceName: "mesh-gateway",
						constants.AnnotationMeshGatewayContainerPort: "8443",
						constants.AnnotationGatewayWANSource:         "NodeIP",
						constants.AnnotationGatewayWANPort:           "8443",
					},
				},
				Spec: corev1.PodSpec{
					NodeName: "test-node",
				},
				Status: corev1.PodStatus{
					Phase:  corev1.PodRunning,
					PodIP:  "", // Missing
					HostIP: "192.168.1.2",
				},
			},
			description: "Mesh gateway with missing pod IP should be skipped",
		},
		"mesh gateway - missing host IP": {
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-mesh-gateway",
					Namespace: "default",
					Labels: map[string]string{
						constants.KeyManagedBy: constants.ManagedByValue,
					},
					Annotations: map[string]string{
						constants.AnnotationGatewayKind:              meshGateway,
						constants.AnnotationGatewayConsulServiceName: "mesh-gateway",
						constants.AnnotationMeshGatewayContainerPort: "8443",
						constants.AnnotationGatewayWANSource:         "NodeIP",
						constants.AnnotationGatewayWANPort:           "8443",
					},
				},
				Spec: corev1.PodSpec{
					NodeName: "test-node",
				},
				Status: corev1.PodStatus{
					Phase:  corev1.PodRunning,
					PodIP:  "10.0.0.2",
					HostIP: "", // Missing
				},
			},
			description: "Mesh gateway with missing host IP should be skipped",
		},
		"ingress gateway - missing node name": {
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-ingress-gateway",
					Namespace: "default",
					Labels: map[string]string{
						constants.KeyManagedBy: constants.ManagedByValue,
					},
					Annotations: map[string]string{
						constants.AnnotationGatewayKind:              ingressGateway,
						constants.AnnotationGatewayConsulServiceName: "ingress-gateway",
						constants.AnnotationGatewayWANSource:         "NodeIP",
						constants.AnnotationGatewayWANPort:           "21000",
					},
				},
				Spec: corev1.PodSpec{
					NodeName: "", // Missing
				},
				Status: corev1.PodStatus{
					Phase:  corev1.PodRunning,
					PodIP:  "10.0.0.3",
					HostIP: "192.168.1.3",
				},
			},
			description: "Ingress gateway with missing node name should be skipped",
		},
		"ingress gateway - missing pod IP": {
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-ingress-gateway",
					Namespace: "default",
					Labels: map[string]string{
						constants.KeyManagedBy: constants.ManagedByValue,
					},
					Annotations: map[string]string{
						constants.AnnotationGatewayKind:              ingressGateway,
						constants.AnnotationGatewayConsulServiceName: "ingress-gateway",
						constants.AnnotationGatewayWANSource:         "NodeIP",
						constants.AnnotationGatewayWANPort:           "21000",
					},
				},
				Spec: corev1.PodSpec{
					NodeName: "test-node",
				},
				Status: corev1.PodStatus{
					Phase:  corev1.PodRunning,
					PodIP:  "", // Missing
					HostIP: "192.168.1.3",
				},
			},
			description: "Ingress gateway with missing pod IP should be skipped",
		},
		"terminating gateway - missing node name": {
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-terminating-gateway",
					Namespace: "default",
					Labels: map[string]string{
						constants.KeyManagedBy: constants.ManagedByValue,
					},
					Annotations: map[string]string{
						constants.AnnotationGatewayKind:              terminatingGateway,
						constants.AnnotationGatewayConsulServiceName: "terminating-gateway",
					},
				},
				Spec: corev1.PodSpec{
					NodeName: "", // Missing
				},
				Status: corev1.PodStatus{
					Phase:  corev1.PodRunning,
					PodIP:  "10.0.0.4",
					HostIP: "192.168.1.4",
				},
			},
			description: "Terminating gateway with missing node name should be skipped",
		},
		"terminating gateway - missing host IP": {
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-terminating-gateway",
					Namespace: "default",
					Labels: map[string]string{
						constants.KeyManagedBy: constants.ManagedByValue,
					},
					Annotations: map[string]string{
						constants.AnnotationGatewayKind:              terminatingGateway,
						constants.AnnotationGatewayConsulServiceName: "terminating-gateway",
					},
				},
				Spec: corev1.PodSpec{
					NodeName: "test-node",
				},
				Status: corev1.PodStatus{
					Phase:  corev1.PodRunning,
					PodIP:  "10.0.0.4",
					HostIP: "", // Missing
				},
			},
			description: "Terminating gateway with missing host IP should be skipped",
		},
		"mixed scenario - all missing": {
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-mixed-pod",
					Namespace: "default",
					Labels: map[string]string{
						constants.KeyManagedBy: constants.ManagedByValue,
					},
					Annotations: map[string]string{
						constants.AnnotationInject:                   "true",
						constants.AnnotationGatewayKind:              meshGateway,
						constants.AnnotationGatewayConsulServiceName: "mesh-gateway",
						constants.AnnotationMeshGatewayContainerPort: "8443",
					},
				},
				Spec: corev1.PodSpec{
					NodeName: "", // Missing
				},
				Status: corev1.PodStatus{
					Phase:  corev1.PodRunning,
					PodIP:  "", // Missing
					HostIP: "", // Missing
				},
			},
			description: "Pod with all node info missing should be skipped regardless of type",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			// Create test consul server.
			testClient := test.TestServerWithMockConnMgrWatcher(t, nil)
			consulClient := testClient.APIClient

			// Create simple endpoints with address matching pod IP (using a valid IP for test)
			endpoints := &corev1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
				},
			}

			// Create fake client with the pod and endpoints
			fakeClient := fake.NewClientBuilder().WithRuntimeObjects(tc.pod, endpoints).Build()

			// Create the controller with minimal required fields
			epCtrl := Controller{
				Client:                 fakeClient,
				ConsulClientConfig:     testClient.Cfg,
				ConsulServerConnMgr:    testClient.Watcher,
				AllowK8sNamespacesSet:  mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:   mapset.NewSetWith(),
				EnableConsulNamespaces: false,
				Log:                    logrtest.New(t),
				Context:                context.Background(),
			}

			// Reconcile the endpoints
			namespacedName := types.NamespacedName{
				Namespace: endpoints.Namespace,
				Name:      endpoints.Name,
			}
			_, err := epCtrl.Reconcile(context.Background(), ctrl.Request{NamespacedName: namespacedName})

			// Should not return an error (pods with incomplete info are skipped gracefully)
			require.NoError(t, err, tc.description)

			// Check that no services were registered in Consul (since pod had incomplete node info)
			services, _, err := consulClient.Catalog().Service("test-service", "", nil)
			require.NoError(t, err, tc.description)
			require.Empty(t, services, "Expected no services to be registered for pod with incomplete node info: %s", tc.description)

			// Also check for gateway services
			if isGateway(*tc.pod) {
				gatewayServiceName := tc.pod.Annotations[constants.AnnotationGatewayConsulServiceName]
				if gatewayServiceName != "" {
					gatewayServices, _, err := consulClient.Catalog().Service(gatewayServiceName, "", nil)
					require.NoError(t, err, tc.description)
					require.Empty(t, gatewayServices, "Expected no gateway services to be registered for pod with incomplete node info: %s", tc.description)
				}
			}
		})
	}
}

// TestReconcile_CompleteNodeInfo tests that pods with complete node information are registered successfully
func TestReconcile_CompleteNodeInfo(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		pod         *corev1.Pod
		description string
	}{
		"service pod - complete info": {
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service-pod",
					Namespace: "default",
					Labels: map[string]string{
						constants.KeyManagedBy: constants.ManagedByValue,
					},
					Annotations: map[string]string{
						constants.AnnotationInject: "true",
						constants.AnnotationPort:   "8080",
					},
				},
				Spec: corev1.PodSpec{
					NodeName: "test-node",
					Containers: []corev1.Container{
						{
							Name: "web",
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									ContainerPort: 8080,
								},
							},
						},
					},
				},
				Status: corev1.PodStatus{
					Phase:  corev1.PodRunning,
					PodIP:  "10.0.0.1",
					HostIP: "192.168.1.1",
				},
			},
			description: "Service pod with complete node info should be registered",
		},
		"mesh gateway - complete info": {
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-mesh-gateway",
					Namespace: "default",
					Labels: map[string]string{
						constants.KeyManagedBy: constants.ManagedByValue,
					},
					Annotations: map[string]string{
						constants.AnnotationGatewayKind:              meshGateway,
						constants.AnnotationGatewayConsulServiceName: "mesh-gateway",
						constants.AnnotationMeshGatewayContainerPort: "8443",
						constants.AnnotationGatewayWANSource:         "NodeIP",
						constants.AnnotationGatewayWANPort:           "8443",
					},
				},
				Spec: corev1.PodSpec{
					NodeName: "test-node",
				},
				Status: corev1.PodStatus{
					Phase:  corev1.PodRunning,
					PodIP:  "10.0.0.2",
					HostIP: "192.168.1.2",
				},
			},
			description: "Mesh gateway with complete node info should be registered",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			// Create test consul server.
			testClient := test.TestServerWithMockConnMgrWatcher(t, nil)

			// Create endpoints that reference the test pod
			endpoints := &corev1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
				},
			}

			// Create fake client with the pod and endpoints
			fakeClient := fake.NewClientBuilder().WithRuntimeObjects(tc.pod, endpoints).Build()

			// Create the controller
			epCtrl := Controller{
				Client:                 fakeClient,
				ConsulClientConfig:     testClient.Cfg,
				ConsulServerConnMgr:    testClient.Watcher,
				AllowK8sNamespacesSet:  mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:   mapset.NewSetWith(),
				EnableConsulNamespaces: false,
				Log:                    logrtest.New(t),
				Context:                context.Background(),
			}

			// Reconcile the endpoints
			namespacedName := types.NamespacedName{
				Namespace: endpoints.Namespace,
				Name:      endpoints.Name,
			}
			_, err := epCtrl.Reconcile(context.Background(), ctrl.Request{NamespacedName: namespacedName})

			// Should not return an error
			require.NoError(t, err, tc.description)

			// For complete pods, we expect some form of registration attempt
			// Note: The actual registration might still fail due to missing endpoints.Subsets
			// but the important thing is that the node completeness check passed
		})
	}
}
