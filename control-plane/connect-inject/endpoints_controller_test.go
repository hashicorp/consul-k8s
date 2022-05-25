package connectinject

import (
	"context"
	"fmt"
	"strings"
	"testing"

	mapset "github.com/deckarep/golang-set"
	logrtest "github.com/go-logr/logr/testing"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	ttl = "ttl"
)

func TestShouldIgnore(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		namespace string
		denySet   mapset.Set
		allowSet  mapset.Set
		expected  bool
	}{
		{
			name:      "system namespace",
			namespace: "kube-system",
			denySet:   mapset.NewSetWith(),
			allowSet:  mapset.NewSetWith("*"),
			expected:  true,
		},
		{
			name:      "other system namespace",
			namespace: "local-path-storage",
			denySet:   mapset.NewSetWith(),
			allowSet:  mapset.NewSetWith("*"),
			expected:  true,
		},
		{
			name:      "any namespace allowed",
			namespace: "foo",
			denySet:   mapset.NewSetWith(),
			allowSet:  mapset.NewSetWith("*"),
			expected:  false,
		},
		{
			name:      "in deny list",
			namespace: "foo",
			denySet:   mapset.NewSetWith("foo"),
			allowSet:  mapset.NewSetWith("*"),
			expected:  true,
		},
		{
			name:      "not in allow list",
			namespace: "foo",
			denySet:   mapset.NewSetWith(),
			allowSet:  mapset.NewSetWith("bar"),
			expected:  true,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			actual := shouldIgnore(tt.namespace, tt.denySet, tt.allowSet)
			require.Equal(t, tt.expected, actual)
		})
	}
}

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
				pod1 := createPod("pod1", "1.2.3.4", true, true)
				return *pod1
			},
			expected: true,
		},
		{
			name: "Pod without injected annotation",
			pod: func() corev1.Pod {
				pod1 := createPod("pod1", "1.2.3.4", false, true)
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

// TestProcessUpstreamsTLSandACLs enables TLS and ACLS and tests processUpstreams through
// the only path which sets up and uses a consul client: when proxy defaults need to be read.
// This test was plucked from the table test TestProcessUpstreams as the rest do not use the client.
func TestProcessUpstreamsTLSandACLs(t *testing.T) {
	t.Parallel()
	nodeName := "test-node"

	masterToken := "b78d37c7-0ca7-5f4d-99ee-6d9975ce4586"
	caFile, certFile, keyFile := test.GenerateServerCerts(t)
	// Create test consul server with ACLs and TLS
	consul, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
		c.ACL.Enabled = true
		c.ACL.DefaultPolicy = "deny"
		c.ACL.Tokens.InitialManagement = masterToken
		c.CAFile = caFile
		c.CertFile = certFile
		c.KeyFile = keyFile
		c.NodeName = nodeName
	})
	require.NoError(t, err)
	defer consul.Stop()

	consul.WaitForServiceIntentions(t)
	cfg := &api.Config{
		Address: consul.HTTPSAddr,
		Scheme:  "https",
		TLSConfig: api.TLSConfig{
			CAFile: caFile,
		},
		Token: masterToken,
	}
	consulClient, err := api.NewClient(cfg)
	require.NoError(t, err)
	addr := strings.Split(consul.HTTPSAddr, ":")
	consulPort := addr[1]

	ce, _ := api.MakeConfigEntry(api.ProxyDefaults, "global")
	pd := ce.(*api.ProxyConfigEntry)
	pd.MeshGateway.Mode = api.MeshGatewayModeRemote
	_, _, err = consulClient.ConfigEntries().Set(pd, &api.WriteOptions{})
	require.NoError(t, err)

	ep := &EndpointsController{
		Log:                   logrtest.TestLogger{T: t},
		ConsulClient:          consulClient,
		ConsulPort:            consulPort,
		ConsulScheme:          "https",
		AllowK8sNamespacesSet: mapset.NewSetWith("*"),
		DenyK8sNamespacesSet:  mapset.NewSetWith(),
	}

	pod := createPod("pod1", "1.2.3.4", true, true)
	pod.Annotations[annotationUpstreams] = "upstream1:1234:dc1"

	upstreams, err := ep.processUpstreams(*pod, corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "svcname",
			Namespace:   "default",
			Labels:      map[string]string{},
			Annotations: map[string]string{},
		},
	})
	require.NoError(t, err)

	expected := []api.Upstream{
		{
			DestinationType: api.UpstreamDestTypeService,
			DestinationName: "upstream1",
			Datacenter:      "dc1",
			LocalBindPort:   1234,
		},
	}
	require.Equal(t, expected, upstreams)
}

func TestProcessUpstreams(t *testing.T) {
	t.Parallel()
	nodeName := "test-node"
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
				pod1 := createPod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[annotationUpstreams] = "upstream1.svc:1234"
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
				pod1 := createPod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[annotationUpstreams] = "upstream1.svc.dc1.dc:1234"
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
				pod1 := createPod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[annotationUpstreams] = "upstream1.svc.peer1.peer:1234"
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
				pod1 := createPod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[annotationUpstreams] = "upstream1.svc.peer1.peer:1234"
				return pod1
			},
			expErr:                  "upstream structured incorrectly: upstream1.svc.peer1.peer:1234",
			consulNamespacesEnabled: true,
			consulPartitionsEnabled: false,
		},
		{
			name: "annotated upstream with svc, ns, and peer",
			pod: func() *corev1.Pod {
				pod1 := createPod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[annotationUpstreams] = "upstream1.svc.ns1.ns.peer1.peer:1234"
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
				pod1 := createPod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[annotationUpstreams] = "upstream1.svc.ns1.ns.part1.ap:1234"
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
				pod1 := createPod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[annotationUpstreams] = "upstream1.svc.ns1.ns.dc1.dc:1234"
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
				pod1 := createPod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[annotationUpstreams] = "upstream1.svc.ns1.ns.dc1.dc:1234, upstream2.svc:2234, upstream3.svc.ns1.ns:3234, upstream4.svc.ns1.ns.peer1.peer:4234"
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
				pod1 := createPod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[annotationUpstreams] = "upstream1.svc.ns1.ns.part1.err:1234"
				return pod1
			},
			expErr:                  "upstream structured incorrectly: upstream1.svc.ns1.ns.part1.err:1234",
			consulNamespacesEnabled: true,
			consulPartitionsEnabled: false,
		},
		{
			name: "annotated upstream error: invalid namespace",
			pod: func() *corev1.Pod {
				pod1 := createPod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[annotationUpstreams] = "upstream1.svc.ns1.err:1234"
				return pod1
			},
			expErr:                  "upstream structured incorrectly: upstream1.svc.ns1.err:1234",
			consulNamespacesEnabled: true,
			consulPartitionsEnabled: false,
		},
		{
			name: "annotated upstream error: invalid number of pieces in the address",
			pod: func() *corev1.Pod {
				pod1 := createPod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[annotationUpstreams] = "upstream1.svc.err:1234"
				return pod1
			},
			expErr:                  "upstream structured incorrectly: upstream1.svc.err:1234",
			consulNamespacesEnabled: true,
			consulPartitionsEnabled: false,
		},
		{
			name: "annotated upstream error: invalid peer",
			pod: func() *corev1.Pod {
				pod1 := createPod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[annotationUpstreams] = "upstream1.svc.peer1.err:1234"
				return pod1
			},
			expErr:                  "upstream structured incorrectly: upstream1.svc.peer1.err:1234",
			consulNamespacesEnabled: false,
			consulPartitionsEnabled: false,
		},
		{
			name: "annotated upstream error: invalid number of pieces in the address without namespaces and partitions",
			pod: func() *corev1.Pod {
				pod1 := createPod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[annotationUpstreams] = "upstream1.svc.err:1234"
				return pod1
			},
			expErr:                  "upstream structured incorrectly: upstream1.svc.err:1234",
			consulNamespacesEnabled: false,
			consulPartitionsEnabled: false,
		},
		{
			name: "upstream with datacenter without ProxyDefaults",
			pod: func() *corev1.Pod {
				pod1 := createPod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[annotationUpstreams] = "upstream1:1234:dc1"
				return pod1
			},
			expErr:                  "upstream \"upstream1:1234:dc1\" is invalid: there is no ProxyDefaults config to set mesh gateway mode",
			consulNamespacesEnabled: false,
			consulPartitionsEnabled: false,
		},
		{
			name: "upstream with datacenter with ProxyDefaults whose mesh gateway mode is not local or remote",
			pod: func() *corev1.Pod {
				pod1 := createPod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[annotationUpstreams] = "upstream1:1234:dc1"
				return pod1
			},
			expErr: "upstream \"upstream1:1234:dc1\" is invalid: ProxyDefaults mesh gateway mode is neither \"local\" nor \"remote\"",
			configEntry: func() api.ConfigEntry {
				ce, _ := api.MakeConfigEntry(api.ProxyDefaults, "pd")
				pd := ce.(*api.ProxyConfigEntry)
				pd.MeshGateway.Mode = "bad-mode"
				return pd
			},
			consulNamespacesEnabled: false,
			consulPartitionsEnabled: false,
		},
		{
			name: "upstream with datacenter with ProxyDefaults and mesh gateway is in local mode",
			pod: func() *corev1.Pod {
				pod1 := createPod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[annotationUpstreams] = "upstream1:1234:dc1"
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
			configEntry: func() api.ConfigEntry {
				ce, _ := api.MakeConfigEntry(api.ProxyDefaults, "pd")
				pd := ce.(*api.ProxyConfigEntry)
				pd.MeshGateway.Mode = api.MeshGatewayModeLocal
				return pd
			},
			consulNamespacesEnabled: false,
			consulPartitionsEnabled: false,
		},
		{
			name: "upstream with datacenter with ProxyDefaults and mesh gateway in remote mode",
			pod: func() *corev1.Pod {
				pod1 := createPod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[annotationUpstreams] = "upstream1:1234:dc1"
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
			configEntry: func() api.ConfigEntry {
				ce, _ := api.MakeConfigEntry(api.ProxyDefaults, "pd")
				pd := ce.(*api.ProxyConfigEntry)
				pd.MeshGateway.Mode = api.MeshGatewayModeRemote
				return pd
			},
			consulNamespacesEnabled: false,
			consulPartitionsEnabled: false,
		},
		{
			name: "when consul is unavailable, we don't return an error",
			pod: func() *corev1.Pod {
				pod1 := createPod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[annotationUpstreams] = "upstream1:1234:dc1"
				return pod1
			},
			expErr: "",
			configEntry: func() api.ConfigEntry {
				ce, _ := api.MakeConfigEntry(api.ProxyDefaults, "pd")
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
				pod1 := createPod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[annotationUpstreams] = "upstream:1234"
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
				pod1 := createPod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[annotationUpstreams] = "upstream.foo:1234"
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
				pod1 := createPod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[annotationUpstreams] = "upstream.foo.bar:1234"
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
				pod1 := createPod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[annotationUpstreams] = "upstream1:1234, upstream2:2234"
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
				pod1 := createPod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[annotationUpstreams] = "upstream1:1234, upstream2.bar:2234, upstream3.foo.baz:3234:dc2"
				return pod1
			},
			configEntry: func() api.ConfigEntry {
				ce, _ := api.MakeConfigEntry(api.ProxyDefaults, "pd")
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
				pod1 := createPod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[annotationUpstreams] = "upstream1:1234, upstream2.bar:2234, upstream3.foo:3234:dc2"
				return pod1
			},
			configEntry: func() api.ConfigEntry {
				ce, _ := api.MakeConfigEntry(api.ProxyDefaults, "pd")
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
				pod1 := createPod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[annotationUpstreams] = "prepared_query:queryname:1234"
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
				pod1 := createPod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[annotationUpstreams] = "prepared_query:queryname:1234, upstream1:2234, prepared_query:6687bd19-5654-76be-d764:8202, upstream2.svc:3234"
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
			// Create test consul server.
			consul, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
				c.NodeName = nodeName
			})
			require.NoError(t, err)
			defer consul.Stop()

			consul.WaitForServiceIntentions(t)
			httpAddr := consul.HTTPAddr
			if tt.consulUnavailable {
				httpAddr = "hostname.does.not.exist:8500"
			}
			consulClient, err := api.NewClient(&api.Config{
				Address: httpAddr,
			})
			require.NoError(t, err)
			addr := strings.Split(httpAddr, ":")
			consulPort := addr[1]

			if tt.configEntry != nil {
				consulClient.ConfigEntries().Set(tt.configEntry(), &api.WriteOptions{})
			}

			ep := &EndpointsController{
				Log:                    logrtest.TestLogger{T: t},
				ConsulClient:           consulClient,
				ConsulPort:             consulPort,
				ConsulScheme:           "http",
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
				pod1 := createPod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[annotationService] = "web"
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
				pod1 := createPod("pod1", "1.2.3.4", true, true)
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
				pod1 := createPod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[annotationService] = "web,web-admin"
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

			svcName := getServiceName(*tt.pod(), *tt.endpoint)
			require.Equal(t, tt.expSvcName, svcName)

		})
	}
}

func TestReconcileCreateEndpoint_MultiportService(t *testing.T) {
	t.Parallel()
	nodeName := "test-node"
	cases := []struct {
		name                          string
		consulSvcName                 string
		k8sObjects                    func() []runtime.Object
		initialConsulSvcs             []*api.AgentServiceRegistration
		expectedNumSvcInstances       int
		expectedConsulSvcInstancesMap map[string][]*api.CatalogService
		expectedProxySvcInstancesMap  map[string][]*api.CatalogService
		expectedAgentHealthChecks     []*api.AgentCheck
	}{
		{
			name:          "Multiport service",
			consulSvcName: "web,web-admin",
			k8sObjects: func() []runtime.Object {
				pod1 := createPod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[annotationPort] = "8080,9090"
				pod1.Annotations[annotationService] = "web,web-admin"
				pod1.Annotations[annotationUpstreams] = "upstream1:1234"
				endpoint1 := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "web",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									IP:       "1.2.3.4",
									NodeName: &nodeName,
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
									IP:       "1.2.3.4",
									NodeName: &nodeName,
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
			initialConsulSvcs:       []*api.AgentServiceRegistration{},
			expectedNumSvcInstances: 1,
			expectedConsulSvcInstancesMap: map[string][]*api.CatalogService{
				"web": {
					{
						ServiceID:      "pod1-web",
						ServiceName:    "web",
						ServiceAddress: "1.2.3.4",
						ServicePort:    8080,
						ServiceMeta: map[string]string{
							MetaKeyPodName:         "pod1",
							MetaKeyKubeServiceName: "web",
							MetaKeyKubeNS:          "default",
							MetaKeyManagedBy:       managedByValue,
						},
						ServiceTags: []string{},
					},
				},
				"web-admin": {
					{
						ServiceID:      "pod1-web-admin",
						ServiceName:    "web-admin",
						ServiceAddress: "1.2.3.4",
						ServicePort:    9090,
						ServiceMeta: map[string]string{
							MetaKeyPodName:         "pod1",
							MetaKeyKubeServiceName: "web-admin",
							MetaKeyKubeNS:          "default",
							MetaKeyManagedBy:       managedByValue,
						},
						ServiceTags: []string{},
					},
				},
			},
			expectedProxySvcInstancesMap: map[string][]*api.CatalogService{
				"web": {
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
							MetaKeyPodName:         "pod1",
							MetaKeyKubeServiceName: "web",
							MetaKeyKubeNS:          "default",
							MetaKeyManagedBy:       managedByValue,
						},
						ServiceTags: []string{},
					},
				},
				"web-admin": {
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
							MetaKeyPodName:         "pod1",
							MetaKeyKubeServiceName: "web-admin",
							MetaKeyKubeNS:          "default",
							MetaKeyManagedBy:       managedByValue,
						},
						ServiceTags: []string{},
					},
				},
			},
			expectedAgentHealthChecks: []*api.AgentCheck{
				{
					CheckID:     "default/pod1-web/kubernetes-health-check",
					ServiceName: "web",
					ServiceID:   "pod1-web",
					Name:        "Kubernetes Health Check",
					Status:      api.HealthPassing,
					Output:      kubernetesSuccessReasonMsg,
					Type:        ttl,
				},
				{
					CheckID:     "default/pod1-web-admin/kubernetes-health-check",
					ServiceName: "web-admin",
					ServiceID:   "pod1-web-admin",
					Name:        "Kubernetes Health Check",
					Status:      api.HealthPassing,
					Output:      kubernetesSuccessReasonMsg,
					Type:        ttl,
				},
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			// The agent pod needs to have the address 127.0.0.1 so when the
			// code gets the agent pods via the label component=client, and
			// makes requests against the agent API, it will actually hit the
			// test server we have on localhost.
			fakeClientPod := createPod("fake-consul-client", "127.0.0.1", false, true)
			fakeClientPod.Labels = map[string]string{"component": "client", "app": "consul", "release": "consul"}

			// Add the default namespace.
			ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
			// Create fake k8s client
			k8sObjects := append(tt.k8sObjects(), fakeClientPod, &ns)

			fakeClient := fake.NewClientBuilder().WithRuntimeObjects(k8sObjects...).Build()

			// Create test consul server
			consul, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
				c.NodeName = nodeName
			})
			require.NoError(t, err)
			defer consul.Stop()
			consul.WaitForServiceIntentions(t)

			cfg := &api.Config{
				Address: consul.HTTPAddr,
			}
			consulClient, err := api.NewClient(cfg)
			require.NoError(t, err)
			addr := strings.Split(consul.HTTPAddr, ":")
			consulPort := addr[1]

			// Register service and proxy in consul.
			for _, svc := range tt.initialConsulSvcs {
				err = consulClient.Agent().ServiceRegister(svc)
				require.NoError(t, err)
			}

			// Create the endpoints controller
			ep := &EndpointsController{
				Client:                fakeClient,
				Log:                   logrtest.TestLogger{T: t},
				ConsulClient:          consulClient,
				ConsulPort:            consulPort,
				ConsulScheme:          "http",
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSetWith(),
				ReleaseName:           "consul",
				ReleaseNamespace:      "default",
				ConsulClientCfg:       cfg,
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
			for _, service := range svcs {
				serviceInstances, _, err := consulClient.Catalog().Service(service, "", nil)
				require.NoError(t, err)
				require.Len(t, serviceInstances, tt.expectedNumSvcInstances)
				for i, instance := range serviceInstances {
					require.Equal(t, tt.expectedConsulSvcInstancesMap[service][i].ServiceID, instance.ServiceID)
					require.Equal(t, tt.expectedConsulSvcInstancesMap[service][i].ServiceName, instance.ServiceName)
					require.Equal(t, tt.expectedConsulSvcInstancesMap[service][i].ServiceAddress, instance.ServiceAddress)
					require.Equal(t, tt.expectedConsulSvcInstancesMap[service][i].ServicePort, instance.ServicePort)
					require.Equal(t, tt.expectedConsulSvcInstancesMap[service][i].ServiceMeta, instance.ServiceMeta)
					require.Equal(t, tt.expectedConsulSvcInstancesMap[service][i].ServiceTags, instance.ServiceTags)
				}
				proxyServiceInstances, _, err := consulClient.Catalog().Service(fmt.Sprintf("%s-sidecar-proxy", service), "", nil)
				require.NoError(t, err)
				require.Len(t, proxyServiceInstances, tt.expectedNumSvcInstances)
				for i, instance := range proxyServiceInstances {
					require.Equal(t, tt.expectedProxySvcInstancesMap[service][i].ServiceID, instance.ServiceID)
					require.Equal(t, tt.expectedProxySvcInstancesMap[service][i].ServiceName, instance.ServiceName)
					require.Equal(t, tt.expectedProxySvcInstancesMap[service][i].ServiceAddress, instance.ServiceAddress)
					require.Equal(t, tt.expectedProxySvcInstancesMap[service][i].ServicePort, instance.ServicePort)
					require.Equal(t, tt.expectedProxySvcInstancesMap[service][i].ServiceMeta, instance.ServiceMeta)
					require.Equal(t, tt.expectedProxySvcInstancesMap[service][i].ServiceTags, instance.ServiceTags)

					// When comparing the ServiceProxy field we ignore the DestinationNamespace
					// field within that struct because on Consul OSS it's set to "" but on Consul Enterprise
					// it's set to "default" and we want to re-use this test for both OSS and Ent.
					// This does mean that we don't test that field but that's okay because
					// it's not getting set specifically in this test.
					// To do the comparison that ignores that field we use go-cmp instead
					// of the regular require.Equal call since it supports ignoring certain
					// fields.
					diff := cmp.Diff(tt.expectedProxySvcInstancesMap[service][i].ServiceProxy, instance.ServiceProxy,
						cmpopts.IgnoreFields(api.Upstream{}, "DestinationNamespace", "DestinationPartition"))
					require.Empty(t, diff, "expected objects to be equal")
				}
				_, checkInfos, err := consulClient.Agent().AgentHealthServiceByName(fmt.Sprintf("%s-sidecar-proxy", service))
				expectedChecks := []string{"Proxy Public Listener", "Destination Alias"}
				require.NoError(t, err)
				require.Len(t, checkInfos, tt.expectedNumSvcInstances)
				for _, checkInfo := range checkInfos {
					checks := checkInfo.Checks
					require.Contains(t, expectedChecks, checks[0].Name)
					require.Contains(t, expectedChecks, checks[1].Name)
				}
			}

			// Check that the Consul health check was created for the k8s pod.
			if tt.expectedAgentHealthChecks != nil {
				for i := range tt.expectedAgentHealthChecks {
					filter := fmt.Sprintf("CheckID == `%s`", tt.expectedAgentHealthChecks[i].CheckID)
					check, err := consulClient.Agent().ChecksWithFilter(filter)
					require.NoError(t, err)
					require.EqualValues(t, len(check), 1)
					// Ignoring Namespace because the response from ENT includes it and OSS does not.
					var ignoredFields = []string{"Node", "Definition", "Namespace", "Partition"}
					require.True(t, cmp.Equal(check[tt.expectedAgentHealthChecks[i].CheckID], tt.expectedAgentHealthChecks[i], cmpopts.IgnoreFields(api.AgentCheck{}, ignoredFields...)))
				}
			}
		})
	}
}

// TestReconcileCreateEndpoint tests the logic to create service instances in Consul from the addresses in the Endpoints
// object. The cases test an empty endpoints object, a basic endpoints object with one address, a basic endpoints object
// with two addresses, and an endpoints object with every possible customization.
// This test covers EndpointsController.createServiceRegistrations.
func TestReconcileCreateEndpoint(t *testing.T) {
	t.Parallel()
	nodeName := "test-node"
	cases := []struct {
		name                       string
		consulSvcName              string
		k8sObjects                 func() []runtime.Object
		initialConsulSvcs          []*api.AgentServiceRegistration
		expectedNumSvcInstances    int
		expectedConsulSvcInstances []*api.CatalogService
		expectedProxySvcInstances  []*api.CatalogService
		expectedAgentHealthChecks  []*api.AgentCheck
		expErr                     string
	}{
		{
			name:          "Empty endpoints",
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
			initialConsulSvcs:          []*api.AgentServiceRegistration{},
			expectedNumSvcInstances:    0,
			expectedConsulSvcInstances: []*api.CatalogService{},
			expectedProxySvcInstances:  []*api.CatalogService{},
			expectedAgentHealthChecks:  nil,
		},
		{
			name:          "Basic endpoints",
			consulSvcName: "service-created",
			k8sObjects: func() []runtime.Object {
				pod1 := createPod("pod1", "1.2.3.4", true, true)
				endpoint := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-created",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									IP:       "1.2.3.4",
									NodeName: &nodeName,
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
			initialConsulSvcs:       []*api.AgentServiceRegistration{},
			expectedNumSvcInstances: 1,
			expectedConsulSvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod1-service-created",
					ServiceName:    "service-created",
					ServiceAddress: "1.2.3.4",
					ServicePort:    0,
					ServiceMeta:    map[string]string{MetaKeyPodName: "pod1", MetaKeyKubeServiceName: "service-created", MetaKeyKubeNS: "default", MetaKeyManagedBy: managedByValue},
					ServiceTags:    []string{},
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
					},
					ServiceMeta: map[string]string{MetaKeyPodName: "pod1", MetaKeyKubeServiceName: "service-created", MetaKeyKubeNS: "default", MetaKeyManagedBy: managedByValue},
					ServiceTags: []string{},
				},
			},
			expectedAgentHealthChecks: []*api.AgentCheck{
				{
					CheckID:     "default/pod1-service-created/kubernetes-health-check",
					ServiceName: "service-created",
					ServiceID:   "pod1-service-created",
					Name:        "Kubernetes Health Check",
					Status:      api.HealthPassing,
					Output:      kubernetesSuccessReasonMsg,
					Type:        ttl,
				},
			},
		},
		{
			name:          "Endpoints with multiple addresses",
			consulSvcName: "service-created",
			k8sObjects: func() []runtime.Object {
				pod1 := createPod("pod1", "1.2.3.4", true, true)
				pod2 := createPod("pod2", "2.2.3.4", true, true)
				endpointWithTwoAddresses := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-created",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									IP:       "1.2.3.4",
									NodeName: &nodeName,
									TargetRef: &corev1.ObjectReference{
										Kind:      "Pod",
										Name:      "pod1",
										Namespace: "default",
									},
								},
								{
									IP:       "2.2.3.4",
									NodeName: &nodeName,
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
			initialConsulSvcs:       []*api.AgentServiceRegistration{},
			expectedNumSvcInstances: 2,
			expectedConsulSvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod1-service-created",
					ServiceName:    "service-created",
					ServiceAddress: "1.2.3.4",
					ServicePort:    0,
					ServiceMeta:    map[string]string{MetaKeyPodName: "pod1", MetaKeyKubeServiceName: "service-created", MetaKeyKubeNS: "default", MetaKeyManagedBy: managedByValue},
					ServiceTags:    []string{},
				},
				{
					ServiceID:      "pod2-service-created",
					ServiceName:    "service-created",
					ServiceAddress: "2.2.3.4",
					ServicePort:    0,
					ServiceMeta:    map[string]string{MetaKeyPodName: "pod2", MetaKeyKubeServiceName: "service-created", MetaKeyKubeNS: "default", MetaKeyManagedBy: managedByValue},
					ServiceTags:    []string{},
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
					},
					ServiceMeta: map[string]string{MetaKeyPodName: "pod1", MetaKeyKubeServiceName: "service-created", MetaKeyKubeNS: "default", MetaKeyManagedBy: managedByValue},
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
					},
					ServiceMeta: map[string]string{MetaKeyPodName: "pod2", MetaKeyKubeServiceName: "service-created", MetaKeyKubeNS: "default", MetaKeyManagedBy: managedByValue},
					ServiceTags: []string{},
				},
			},
			expectedAgentHealthChecks: []*api.AgentCheck{
				{
					CheckID:     "default/pod1-service-created/kubernetes-health-check",
					ServiceName: "service-created",
					ServiceID:   "pod1-service-created",
					Name:        "Kubernetes Health Check",
					Status:      api.HealthPassing,
					Output:      kubernetesSuccessReasonMsg,
					Type:        ttl,
				},
				{
					CheckID:     "default/pod2-service-created/kubernetes-health-check",
					ServiceName: "service-created",
					ServiceID:   "pod2-service-created",
					Name:        "Kubernetes Health Check",
					Status:      api.HealthPassing,
					Output:      kubernetesSuccessReasonMsg,
					Type:        ttl,
				},
			},
		},
		{
			// This test has 3 addresses, but only 2 are backed by pod resources. This will cause Reconcile to error
			// on the invalid address but continue and process the other addresses. We check for error specific to
			// pod3 being non-existant at the end, and validate the other 2 addresses have service instances.
			name:          "Endpoints with multiple addresses but one is invalid",
			consulSvcName: "service-created",
			k8sObjects: func() []runtime.Object {
				pod1 := createPod("pod1", "1.2.3.4", true, true)
				pod2 := createPod("pod2", "2.2.3.4", true, true)
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
									IP:       "9.9.9.9",
									NodeName: &nodeName,
									TargetRef: &corev1.ObjectReference{
										Kind:      "Pod",
										Name:      "pod3",
										Namespace: "default",
									},
								},
								// The next two are valid addresses.
								{
									IP:       "1.2.3.4",
									NodeName: &nodeName,
									TargetRef: &corev1.ObjectReference{
										Kind:      "Pod",
										Name:      "pod1",
										Namespace: "default",
									},
								},
								{
									IP:       "2.2.3.4",
									NodeName: &nodeName,
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
			initialConsulSvcs:       []*api.AgentServiceRegistration{},
			expectedNumSvcInstances: 2,
			expectedConsulSvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod1-service-created",
					ServiceName:    "service-created",
					ServiceAddress: "1.2.3.4",
					ServicePort:    0,
					ServiceMeta:    map[string]string{MetaKeyPodName: "pod1", MetaKeyKubeServiceName: "service-created", MetaKeyKubeNS: "default", MetaKeyManagedBy: managedByValue},
					ServiceTags:    []string{},
				},
				{
					ServiceID:      "pod2-service-created",
					ServiceName:    "service-created",
					ServiceAddress: "2.2.3.4",
					ServicePort:    0,
					ServiceMeta:    map[string]string{MetaKeyPodName: "pod2", MetaKeyKubeServiceName: "service-created", MetaKeyKubeNS: "default", MetaKeyManagedBy: managedByValue},
					ServiceTags:    []string{},
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
					},
					ServiceMeta: map[string]string{MetaKeyPodName: "pod1", MetaKeyKubeServiceName: "service-created", MetaKeyKubeNS: "default", MetaKeyManagedBy: managedByValue},
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
					},
					ServiceMeta: map[string]string{MetaKeyPodName: "pod2", MetaKeyKubeServiceName: "service-created", MetaKeyKubeNS: "default", MetaKeyManagedBy: managedByValue},
					ServiceTags: []string{},
				},
			},
			expectedAgentHealthChecks: []*api.AgentCheck{
				{
					CheckID:     "default/pod1-service-created/kubernetes-health-check",
					ServiceName: "service-created",
					ServiceID:   "pod1-service-created",
					Name:        "Kubernetes Health Check",
					Status:      api.HealthPassing,
					Output:      kubernetesSuccessReasonMsg,
					Type:        ttl,
				},
				{
					CheckID:     "default/pod2-service-created/kubernetes-health-check",
					ServiceName: "service-created",
					ServiceID:   "pod2-service-created",
					Name:        "Kubernetes Health Check",
					Status:      api.HealthPassing,
					Output:      kubernetesSuccessReasonMsg,
					Type:        ttl,
				},
			},
			expErr: "1 error occurred:\n\t* pods \"pod3\" not found\n\n",
		},
		{
			name:          "Every configurable field set: port, different Consul service name, meta, tags, upstreams, metrics",
			consulSvcName: "different-consul-svc-name",
			k8sObjects: func() []runtime.Object {
				pod1 := createPod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[annotationPort] = "1234"
				pod1.Annotations[annotationService] = "different-consul-svc-name"
				pod1.Annotations[fmt.Sprintf("%sname", annotationMeta)] = "abc"
				pod1.Annotations[fmt.Sprintf("%sversion", annotationMeta)] = "2"
				pod1.Annotations[fmt.Sprintf("%spod_name", annotationMeta)] = "$POD_NAME"
				pod1.Annotations[annotationTags] = "abc,123,$POD_NAME"
				pod1.Annotations[annotationConnectTags] = "def,456,$POD_NAME"
				pod1.Annotations[annotationUpstreams] = "upstream1:1234"
				pod1.Annotations[annotationEnableMetrics] = "true"
				pod1.Annotations[annotationPrometheusScrapePort] = "12345"
				endpoint := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-created",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									IP:       "1.2.3.4",
									NodeName: &nodeName,
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
			initialConsulSvcs:       []*api.AgentServiceRegistration{},
			expectedNumSvcInstances: 1,
			expectedConsulSvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod1-different-consul-svc-name",
					ServiceName:    "different-consul-svc-name",
					ServiceAddress: "1.2.3.4",
					ServicePort:    1234,
					ServiceMeta: map[string]string{
						"name":                 "abc",
						"version":              "2",
						"pod_name":             "pod1",
						MetaKeyPodName:         "pod1",
						MetaKeyKubeServiceName: "service-created",
						MetaKeyKubeNS:          "default",
						MetaKeyManagedBy:       managedByValue,
					},
					ServiceTags: []string{"abc", "123", "pod1", "def", "456", "pod1"},
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
							"envoy_prometheus_bind_addr": "0.0.0.0:12345",
						},
					},
					ServiceMeta: map[string]string{
						"name":                 "abc",
						"version":              "2",
						"pod_name":             "pod1",
						MetaKeyPodName:         "pod1",
						MetaKeyKubeServiceName: "service-created",
						MetaKeyKubeNS:          "default",
						MetaKeyManagedBy:       managedByValue,
					},
					ServiceTags: []string{"abc", "123", "pod1", "def", "456", "pod1"},
				},
			},
			expectedAgentHealthChecks: []*api.AgentCheck{
				{
					CheckID:     "default/pod1-different-consul-svc-name/kubernetes-health-check",
					ServiceName: "different-consul-svc-name",
					ServiceID:   "pod1-different-consul-svc-name",
					Name:        "Kubernetes Health Check",
					Status:      api.HealthPassing,
					Output:      kubernetesSuccessReasonMsg,
					Type:        ttl,
				},
			},
		},
		// Test that if a user is updating their deployment from non-mesh to mesh that we
		// register the mesh pods.
		{
			name:          "Some endpoints injected, some not.",
			consulSvcName: "service-created",
			k8sObjects: func() []runtime.Object {
				pod1 := createPod("pod1", "1.2.3.4", true, true)
				pod2 := createPod("pod2", "2.3.4.5", false, false)

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
									IP:       "2.3.4.5",
									NodeName: &nodeName,
									TargetRef: &corev1.ObjectReference{
										Kind:      "Pod",
										Name:      "pod2",
										Namespace: "default",
									},
								},
								{
									IP:       "1.2.3.4",
									NodeName: &nodeName,
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
			initialConsulSvcs:       []*api.AgentServiceRegistration{},
			expectedNumSvcInstances: 1,
			expectedConsulSvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod1-service-created",
					ServiceName:    "service-created",
					ServiceAddress: "1.2.3.4",
					ServicePort:    0,
					ServiceMeta:    map[string]string{MetaKeyPodName: "pod1", MetaKeyKubeServiceName: "service-created", MetaKeyKubeNS: "default", MetaKeyManagedBy: managedByValue},
					ServiceTags:    []string{},
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
					},
					ServiceMeta: map[string]string{MetaKeyPodName: "pod1", MetaKeyKubeServiceName: "service-created", MetaKeyKubeNS: "default", MetaKeyManagedBy: managedByValue},
					ServiceTags: []string{},
				},
			},
			expectedAgentHealthChecks: []*api.AgentCheck{
				{
					CheckID:     "default/pod1-service-created/kubernetes-health-check",
					ServiceName: "service-created",
					ServiceID:   "pod1-service-created",
					Name:        "Kubernetes Health Check",
					Status:      api.HealthPassing,
					Output:      kubernetesSuccessReasonMsg,
					Type:        ttl,
				},
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			// The agent pod needs to have the address 127.0.0.1 so when the
			// code gets the agent pods via the label component=client, and
			// makes requests against the agent API, it will actually hit the
			// test server we have on localhost.
			fakeClientPod := createPod("fake-consul-client", "127.0.0.1", false, true)
			fakeClientPod.Labels = map[string]string{"component": "client", "app": "consul", "release": "consul"}

			// Add the default namespace.
			ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
			// Create fake k8s client
			k8sObjects := append(tt.k8sObjects(), fakeClientPod, &ns)

			fakeClient := fake.NewClientBuilder().WithRuntimeObjects(k8sObjects...).Build()

			// Create test consul server
			consul, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
				c.NodeName = nodeName
			})
			require.NoError(t, err)
			defer consul.Stop()
			consul.WaitForServiceIntentions(t)

			cfg := &api.Config{
				Address: consul.HTTPAddr,
			}
			consulClient, err := api.NewClient(cfg)
			require.NoError(t, err)
			addr := strings.Split(consul.HTTPAddr, ":")
			consulPort := addr[1]

			// Register service and proxy in consul.
			for _, svc := range tt.initialConsulSvcs {
				err = consulClient.Agent().ServiceRegister(svc)
				require.NoError(t, err)
			}

			// Create the endpoints controller
			ep := &EndpointsController{
				Client:                fakeClient,
				Log:                   logrtest.TestLogger{T: t},
				ConsulClient:          consulClient,
				ConsulPort:            consulPort,
				ConsulScheme:          "http",
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSetWith(),
				ReleaseName:           "consul",
				ReleaseNamespace:      "default",
				ConsulClientCfg:       cfg,
			}
			namespacedName := types.NamespacedName{
				Namespace: "default",
				Name:      "service-created",
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
			require.Len(t, serviceInstances, tt.expectedNumSvcInstances)
			for i, instance := range serviceInstances {
				require.Equal(t, tt.expectedConsulSvcInstances[i].ServiceID, instance.ServiceID)
				require.Equal(t, tt.expectedConsulSvcInstances[i].ServiceName, instance.ServiceName)
				require.Equal(t, tt.expectedConsulSvcInstances[i].ServiceAddress, instance.ServiceAddress)
				require.Equal(t, tt.expectedConsulSvcInstances[i].ServicePort, instance.ServicePort)
				require.Equal(t, tt.expectedConsulSvcInstances[i].ServiceMeta, instance.ServiceMeta)
				require.Equal(t, tt.expectedConsulSvcInstances[i].ServiceTags, instance.ServiceTags)
			}
			proxyServiceInstances, _, err := consulClient.Catalog().Service(fmt.Sprintf("%s-sidecar-proxy", tt.consulSvcName), "", nil)
			require.NoError(t, err)
			require.Len(t, proxyServiceInstances, tt.expectedNumSvcInstances)
			for i, instance := range proxyServiceInstances {
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

			_, checkInfos, err := consulClient.Agent().AgentHealthServiceByName(fmt.Sprintf("%s-sidecar-proxy", tt.consulSvcName))
			expectedChecks := []string{"Proxy Public Listener", "Destination Alias"}
			require.NoError(t, err)
			require.Len(t, checkInfos, tt.expectedNumSvcInstances)
			for _, checkInfo := range checkInfos {
				checks := checkInfo.Checks
				require.Contains(t, expectedChecks, checks[0].Name)
				require.Contains(t, expectedChecks, checks[1].Name)
			}

			// Check that the Consul health check was created for the k8s pod.
			if tt.expectedAgentHealthChecks != nil {
				for i := range tt.expectedConsulSvcInstances {
					filter := fmt.Sprintf("CheckID == `%s`", tt.expectedAgentHealthChecks[i].CheckID)
					check, err := consulClient.Agent().ChecksWithFilter(filter)
					require.NoError(t, err)
					require.EqualValues(t, len(check), 1)
					// Ignoring Namespace because the response from ENT includes it and OSS does not.
					var ignoredFields = []string{"Node", "Definition", "Namespace", "Partition"}
					require.True(t, cmp.Equal(check[tt.expectedAgentHealthChecks[i].CheckID], tt.expectedAgentHealthChecks[i], cmpopts.IgnoreFields(api.AgentCheck{}, ignoredFields...)))
				}
			}
		})
	}
}

// Tests updating an Endpoints object.
//   - Tests updates via the register codepath:
//     - When an address in an Endpoint is updated, that the corresponding service instance in Consul is updated.
//     - When an address is added to an Endpoint, an additional service instance in Consul is registered.
//     - When an address in an Endpoint is updated - via health check change - the corresponding service instance is updated.
//   - Tests updates via the deregister codepath:
//     - When an address is removed from an Endpoint, the corresponding service instance in Consul is deregistered.
//     - When an address is removed from an Endpoint *and there are no addresses left in the Endpoint*, the
//     corresponding service instance in Consul is deregistered.
// For the register and deregister codepath, this also tests that they work when the Consul service name is different
// from the K8s service name.
// This test covers EndpointsController.deregisterServiceOnAllAgents when services should be selectively deregistered
// since the map will not be nil.
func TestReconcileUpdateEndpoint(t *testing.T) {
	t.Parallel()
	nodeName := "test-node"
	cases := []struct {
		name                       string
		consulSvcName              string
		k8sObjects                 func() []runtime.Object
		initialConsulSvcs          []*api.AgentServiceRegistration
		expectedConsulSvcInstances []*api.CatalogService
		expectedProxySvcInstances  []*api.CatalogService
		expectedAgentHealthChecks  []*api.AgentCheck
		enableACLs                 bool
	}{
		// Legacy services are not managed by endpoints controller, but endpoints controller
		// will still add/update the legacy service's health checks.
		{
			name:          "Legacy service: Health check is added when the pod is healthy",
			consulSvcName: "service-updated",
			k8sObjects: func() []runtime.Object {
				pod1 := createPod("pod1", "1.2.3.4", true, false)
				endpoint := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-updated",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									IP:       "1.2.3.4",
									NodeName: &nodeName,
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
			expectedAgentHealthChecks: []*api.AgentCheck{
				{
					CheckID:     "default/pod1-service-updated/kubernetes-health-check",
					ServiceName: "service-updated",
					ServiceID:   "pod1-service-updated",
					Name:        "Kubernetes Health Check",
					Status:      api.HealthPassing,
					Output:      kubernetesSuccessReasonMsg,
					Type:        ttl,
				},
			},
		},
		{
			name:          "Legacy service: Health check is added when the pod is unhealthy",
			consulSvcName: "service-updated",
			k8sObjects: func() []runtime.Object {
				pod1 := createPod("pod1", "1.2.3.4", true, false)
				endpoint := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-updated",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							NotReadyAddresses: []corev1.EndpointAddress{
								{
									IP:       "1.2.3.4",
									NodeName: &nodeName,
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
			expectedAgentHealthChecks: []*api.AgentCheck{
				{
					CheckID:     "default/pod1-service-updated/kubernetes-health-check",
					ServiceName: "service-updated",
					ServiceID:   "pod1-service-updated",
					Name:        "Kubernetes Health Check",
					Status:      api.HealthCritical,
					Output:      "Pod \"default/pod1\" is not ready",
					Type:        ttl,
				},
			},
		},
		{
			name:          "Legacy service: Service health check is updated when the pod goes from healthy --> unhealthy",
			consulSvcName: "service-updated",
			k8sObjects: func() []runtime.Object {
				pod1 := createPod("pod1", "1.2.3.4", true, false)
				endpoint := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-updated",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							NotReadyAddresses: []corev1.EndpointAddress{
								{
									IP:       "1.2.3.4",
									NodeName: &nodeName,
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
						CheckID:                "default/pod1-service-updated/kubernetes-health-check",
						Name:                   "Kubernetes Health Check",
						TTL:                    "100000h",
						Status:                 api.HealthPassing,
						SuccessBeforePassing:   1,
						FailuresBeforeCritical: 1,
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
			expectedAgentHealthChecks: []*api.AgentCheck{
				{
					CheckID:     "default/pod1-service-updated/kubernetes-health-check",
					ServiceName: "service-updated",
					ServiceID:   "pod1-service-updated",
					Name:        "Kubernetes Health Check",
					Status:      api.HealthCritical,
					Output:      "Pod \"default/pod1\" is not ready",
					Type:        ttl,
				},
			},
		},
		{
			name:          "Legacy service: Service health check is updated when the pod goes from unhealthy --> healthy",
			consulSvcName: "service-updated",
			k8sObjects: func() []runtime.Object {
				pod1 := createPod("pod1", "1.2.3.4", true, false)
				endpoint := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-updated",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									IP:       "1.2.3.4",
									NodeName: &nodeName,
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
						CheckID:                "default/pod1-service-updated/kubernetes-health-check",
						Name:                   "Kubernetes Health Check",
						TTL:                    "100000h",
						Status:                 api.HealthCritical,
						SuccessBeforePassing:   1,
						FailuresBeforeCritical: 1,
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
			expectedAgentHealthChecks: []*api.AgentCheck{
				{
					CheckID:     "default/pod1-service-updated/kubernetes-health-check",
					ServiceName: "service-updated",
					ServiceID:   "pod1-service-updated",
					Name:        "Kubernetes Health Check",
					Status:      api.HealthPassing,
					Output:      kubernetesSuccessReasonMsg,
					Type:        ttl,
				},
			},
		},
		{
			name:          "Endpoints has an updated address because health check changes from unhealthy to healthy",
			consulSvcName: "service-updated",
			k8sObjects: func() []runtime.Object {
				pod1 := createPod("pod1", "1.2.3.4", true, true)
				endpoint := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-updated",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									IP:       "1.2.3.4",
									NodeName: &nodeName,
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
					Meta:    map[string]string{MetaKeyKubeNS: "default"},
					Check: &api.AgentServiceCheck{
						CheckID:                "default/pod1-service-updated/kubernetes-health-check",
						Name:                   "Kubernetes Health Check",
						TTL:                    "100000h",
						Status:                 api.HealthCritical,
						SuccessBeforePassing:   1,
						FailuresBeforeCritical: 1,
					},
				},
				{
					Kind:    api.ServiceKindConnectProxy,
					ID:      "pod1-service-updated-sidecar-proxy",
					Name:    "service-updated-sidecar-proxy",
					Port:    20000,
					Address: "1.2.3.4",
					Meta:    map[string]string{MetaKeyKubeNS: "default"},
					Proxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "service-updated",
						DestinationServiceID:   "pod1-service-updated",
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
			expectedAgentHealthChecks: []*api.AgentCheck{
				{
					CheckID:     "default/pod1-service-updated/kubernetes-health-check",
					ServiceName: "service-updated",
					ServiceID:   "pod1-service-updated",
					Name:        "Kubernetes Health Check",
					Status:      api.HealthPassing,
					Output:      kubernetesSuccessReasonMsg,
					Type:        ttl,
				},
			},
		},
		{
			name:          "Endpoints has an updated address because health check changes from healthy to unhealthy",
			consulSvcName: "service-updated",
			k8sObjects: func() []runtime.Object {
				pod1 := createPod("pod1", "1.2.3.4", true, true)
				endpoint := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-updated",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							NotReadyAddresses: []corev1.EndpointAddress{
								{
									IP:       "1.2.3.4",
									NodeName: &nodeName,
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
					Meta:    map[string]string{MetaKeyKubeNS: "default"},
					Check: &api.AgentServiceCheck{
						CheckID:                "default/pod1-service-updated/kubernetes-health-check",
						Name:                   "Kubernetes Health Check",
						TTL:                    "100000h",
						Status:                 api.HealthPassing,
						SuccessBeforePassing:   1,
						FailuresBeforeCritical: 1,
					},
				},
				{
					Kind:    api.ServiceKindConnectProxy,
					ID:      "pod1-service-updated-sidecar-proxy",
					Name:    "service-updated-sidecar-proxy",
					Port:    20000,
					Address: "1.2.3.4",
					Meta:    map[string]string{MetaKeyKubeNS: "default"},
					Proxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "service-updated",
						DestinationServiceID:   "pod1-service-updated",
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
			expectedAgentHealthChecks: []*api.AgentCheck{
				{
					CheckID:     "default/pod1-service-updated/kubernetes-health-check",
					ServiceName: "service-updated",
					ServiceID:   "pod1-service-updated",
					Name:        "Kubernetes Health Check",
					Status:      api.HealthCritical,
					Output:      "Pod \"default/pod1\" is not ready",
					Type:        ttl,
				},
			},
		},
		{
			name:          "Endpoints has an updated address (pod IP change).",
			consulSvcName: "service-updated",
			k8sObjects: func() []runtime.Object {
				pod1 := createPod("pod1", "4.4.4.4", true, true)
				endpoint := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-updated",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									IP:       "4.4.4.4",
									NodeName: &nodeName,
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
					Meta: map[string]string{
						MetaKeyKubeNS:          "default",
						MetaKeyPodName:         "pod1",
						MetaKeyKubeServiceName: "service-updated",
						MetaKeyManagedBy:       managedByValue,
					},
				},
				{
					Kind:    api.ServiceKindConnectProxy,
					ID:      "pod1-service-updated-sidecar-proxy",
					Name:    "service-updated-sidecar-proxy",
					Port:    20000,
					Address: "1.2.3.4",
					Meta: map[string]string{
						MetaKeyKubeNS:          "default",
						MetaKeyPodName:         "pod1",
						MetaKeyKubeServiceName: "service-updated",
						MetaKeyManagedBy:       managedByValue,
					},
					Proxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "service-updated",
						DestinationServiceID:   "pod1-service-updated",
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
				pod1 := createPod("pod1", "4.4.4.4", true, true)
				pod1.Annotations[annotationService] = "different-consul-svc-name"
				endpoint := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-updated",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									IP:       "4.4.4.4",
									NodeName: &nodeName,
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
					ID:      "pod1-different-consul-svc-name",
					Name:    "different-consul-svc-name",
					Port:    80,
					Address: "1.2.3.4",
					Meta: map[string]string{
						MetaKeyManagedBy:       managedByValue,
						MetaKeyKubeNS:          "default",
						MetaKeyPodName:         "pod1",
						MetaKeyKubeServiceName: "service-updated",
					},
				},
				{
					Kind:    api.ServiceKindConnectProxy,
					ID:      "pod1-different-consul-svc-name-sidecar-proxy",
					Name:    "different-consul-svc-name-sidecar-proxy",
					Port:    20000,
					Address: "1.2.3.4",
					Proxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "different-consul-svc-name",
						DestinationServiceID:   "pod1-different-consul-svc-name",
					},
					Meta: map[string]string{
						MetaKeyManagedBy:       managedByValue,
						MetaKeyKubeNS:          "default",
						MetaKeyPodName:         "pod1",
						MetaKeyKubeServiceName: "service-updated",
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
				pod1 := createPod("pod1", "1.2.3.4", true, true)
				pod2 := createPod("pod2", "2.2.3.4", true, true)
				endpointWithTwoAddresses := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-updated",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									IP:       "1.2.3.4",
									NodeName: &nodeName,
									TargetRef: &corev1.ObjectReference{
										Kind:      "Pod",
										Name:      "pod1",
										Namespace: "default",
									},
								},
								{
									IP:       "2.2.3.4",
									NodeName: &nodeName,
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
			initialConsulSvcs: []*api.AgentServiceRegistration{
				{
					ID:      "pod1-service-updated",
					Name:    "service-updated",
					Port:    80,
					Address: "1.2.3.4",
					Meta:    map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default", MetaKeyManagedBy: managedByValue},
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
			expectedAgentHealthChecks: []*api.AgentCheck{
				{
					CheckID:     "default/pod1-service-updated/kubernetes-health-check",
					ServiceName: "service-updated",
					ServiceID:   "pod1-service-updated",
					Name:        "Kubernetes Health Check",
					Status:      api.HealthPassing,
					Output:      kubernetesSuccessReasonMsg,
					Type:        ttl,
				},
				{
					CheckID:     "default/pod2-service-updated/kubernetes-health-check",
					ServiceName: "service-updated",
					ServiceID:   "pod2-service-updated",
					Name:        "Kubernetes Health Check",
					Status:      api.HealthPassing,
					Output:      kubernetesSuccessReasonMsg,
					Type:        ttl,
				},
			},
		},
		{
			name:          "Consul has instances that are not in the Endpoints addresses",
			consulSvcName: "service-updated",
			k8sObjects: func() []runtime.Object {
				pod1 := createPod("pod1", "1.2.3.4", true, true)
				endpoint := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-updated",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									IP:       "1.2.3.4",
									NodeName: &nodeName,
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
					Meta:    map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default", MetaKeyManagedBy: managedByValue},
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
					Meta: map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default", MetaKeyManagedBy: managedByValue},
				},
				{
					ID:      "pod2-service-updated",
					Name:    "service-updated",
					Port:    80,
					Address: "2.2.3.4",
					Meta:    map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default", MetaKeyManagedBy: managedByValue},
				},
				{
					Kind:    api.ServiceKindConnectProxy,
					ID:      "pod2-service-updated-sidecar-proxy",
					Name:    "service-updated-sidecar-proxy",
					Port:    20000,
					Address: "2.2.3.4",
					Proxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "service-updated",
						DestinationServiceID:   "pod2-service-updated",
					},
					Meta: map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default", MetaKeyManagedBy: managedByValue},
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
				pod1 := createPod("pod1", "1.2.3.4", true, true)
				pod1.Annotations[annotationService] = "different-consul-svc-name"
				endpoint := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-updated",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									IP:       "1.2.3.4",
									NodeName: &nodeName,
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
					ID:      "pod1-different-consul-svc-name",
					Name:    "different-consul-svc-name",
					Port:    80,
					Address: "1.2.3.4",
					Meta:    map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default", MetaKeyManagedBy: managedByValue},
				},
				{
					Kind:    api.ServiceKindConnectProxy,
					ID:      "pod1-different-consul-svc-name-sidecar-proxy",
					Name:    "different-consul-svc-name-sidecar-proxy",
					Port:    20000,
					Address: "1.2.3.4",
					Proxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "different-consul-svc-name",
						DestinationServiceID:   "pod1-different-consul-svc-name",
					},
					Meta: map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default", MetaKeyManagedBy: managedByValue},
				},
				{
					ID:      "pod2-different-consul-svc-name",
					Name:    "different-consul-svc-name",
					Port:    80,
					Address: "2.2.3.4",
					Meta:    map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default", MetaKeyManagedBy: managedByValue},
				},
				{
					Kind:    api.ServiceKindConnectProxy,
					ID:      "pod2-different-consul-svc-name-sidecar-proxy",
					Name:    "different-consul-svc-name-sidecar-proxy",
					Port:    20000,
					Address: "2.2.3.4",
					Proxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "different-consul-svc-name",
						DestinationServiceID:   "pod2-different-consul-svc-name",
					},
					Meta: map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default", MetaKeyManagedBy: managedByValue},
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
			initialConsulSvcs: []*api.AgentServiceRegistration{
				{
					ID:      "pod1-service-updated",
					Name:    "service-updated",
					Port:    80,
					Address: "1.2.3.4",
					Meta:    map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default", MetaKeyManagedBy: managedByValue},
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
					Meta: map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default", MetaKeyManagedBy: managedByValue},
				},
				{
					ID:      "pod2-service-updated",
					Name:    "service-updated",
					Port:    80,
					Address: "2.2.3.4",
					Meta:    map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default", MetaKeyManagedBy: managedByValue},
				},
				{
					Kind:    api.ServiceKindConnectProxy,
					ID:      "pod2-service-updated-sidecar-proxy",
					Name:    "service-updated-sidecar-proxy",
					Port:    20000,
					Address: "2.2.3.4",
					Proxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "service-updated",
						DestinationServiceID:   "pod2-service-updated",
					},
					Meta: map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default", MetaKeyManagedBy: managedByValue},
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
			initialConsulSvcs: []*api.AgentServiceRegistration{
				{
					ID:      "pod1-different-consul-svc-name",
					Name:    "different-consul-svc-name",
					Port:    80,
					Address: "1.2.3.4",
					Meta:    map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default", MetaKeyManagedBy: managedByValue},
				},
				{
					Kind:    api.ServiceKindConnectProxy,
					ID:      "pod1-different-consul-svc-name-sidecar-proxy",
					Name:    "different-consul-svc-name-sidecar-proxy",
					Port:    20000,
					Address: "1.2.3.4",
					Proxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "different-consul-svc-name",
						DestinationServiceID:   "pod1-different-consul-svc-name",
					},
					Meta: map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default", MetaKeyManagedBy: managedByValue},
				},
				{
					ID:      "pod2-different-consul-svc-name",
					Name:    "different-consul-svc-name",
					Port:    80,
					Address: "2.2.3.4",
					Meta:    map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default", MetaKeyManagedBy: managedByValue},
				},
				{
					Kind:    api.ServiceKindConnectProxy,
					ID:      "pod2-different-consul-svc-name-sidecar-proxy",
					Name:    "different-consul-svc-name-sidecar-proxy",
					Port:    20000,
					Address: "2.2.3.4",
					Proxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "different-consul-svc-name",
						DestinationServiceID:   "pod2-different-consul-svc-name",
					},
					Meta: map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default", MetaKeyManagedBy: managedByValue},
				},
			},
			expectedConsulSvcInstances: []*api.CatalogService{},
			expectedProxySvcInstances:  []*api.CatalogService{},
		},
		{
			name:          "ACLs enabled: Endpoints has an updated address because the target pod changes",
			consulSvcName: "service-updated",
			k8sObjects: func() []runtime.Object {
				pod2 := createPod("pod2", "4.4.4.4", true, true)
				endpoint := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-updated",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									IP:       "4.4.4.4",
									NodeName: &nodeName,
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
			initialConsulSvcs: []*api.AgentServiceRegistration{
				{
					ID:      "pod1-service-updated",
					Name:    "service-updated",
					Port:    80,
					Address: "1.2.3.4",
					Meta: map[string]string{
						MetaKeyKubeNS:          "default",
						MetaKeyPodName:         "pod1",
						MetaKeyKubeServiceName: "service-updated",
						MetaKeyManagedBy:       managedByValue,
					},
				},
				{
					Kind:    api.ServiceKindConnectProxy,
					ID:      "pod1-service-updated-sidecar-proxy",
					Name:    "service-updated-sidecar-proxy",
					Port:    20000,
					Address: "1.2.3.4",
					Meta: map[string]string{
						MetaKeyKubeNS:          "default",
						MetaKeyPodName:         "pod1",
						MetaKeyKubeServiceName: "service-updated",
						MetaKeyManagedBy:       managedByValue,
					},
					Proxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "service-updated",
						DestinationServiceID:   "pod1-service-updated",
					},
				},
			},
			expectedConsulSvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod2-service-updated",
					ServiceAddress: "4.4.4.4",
					ServiceMeta: map[string]string{
						MetaKeyKubeServiceName: "service-updated",
						MetaKeyKubeNS:          "default",
						MetaKeyManagedBy:       managedByValue,
						MetaKeyPodName:         "pod2",
					},
				},
			},
			expectedProxySvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod2-service-updated-sidecar-proxy",
					ServiceAddress: "4.4.4.4",
					ServiceMeta: map[string]string{
						MetaKeyKubeServiceName: "service-updated",
						MetaKeyKubeNS:          "default",
						MetaKeyManagedBy:       managedByValue,
						MetaKeyPodName:         "pod2",
					},
				},
			},
			enableACLs: true,
		},
		{
			name:          "ACLs enabled: Consul has instances that are not in the Endpoints addresses",
			consulSvcName: "service-updated",
			k8sObjects: func() []runtime.Object {
				pod1 := createPod("pod1", "1.2.3.4", true, true)
				endpoint := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-updated",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									IP:       "1.2.3.4",
									NodeName: &nodeName,
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
					Meta: map[string]string{
						MetaKeyKubeServiceName: "service-updated",
						MetaKeyKubeNS:          "default",
						MetaKeyManagedBy:       managedByValue,
						MetaKeyPodName:         "pod1",
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
					Meta: map[string]string{
						MetaKeyKubeServiceName: "service-updated",
						MetaKeyKubeNS:          "default",
						MetaKeyManagedBy:       managedByValue,
						MetaKeyPodName:         "pod1",
					},
				},
				{
					ID:      "pod2-service-updated",
					Name:    "service-updated",
					Port:    80,
					Address: "2.2.3.4",
					Meta: map[string]string{
						MetaKeyKubeServiceName: "service-updated",
						MetaKeyKubeNS:          "default",
						MetaKeyManagedBy:       managedByValue,
						MetaKeyPodName:         "pod2",
					},
				},
				{
					Kind:    api.ServiceKindConnectProxy,
					ID:      "pod2-service-updated-sidecar-proxy",
					Name:    "service-updated-sidecar-proxy",
					Port:    20000,
					Address: "2.2.3.4",
					Proxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "service-updated",
						DestinationServiceID:   "pod2-service-updated",
					},
					Meta: map[string]string{
						MetaKeyKubeServiceName: "service-updated",
						MetaKeyKubeNS:          "default",
						MetaKeyManagedBy:       managedByValue,
						MetaKeyPodName:         "pod2",
					},
				},
			},
			expectedConsulSvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod1-service-updated",
					ServiceName:    "service-updated",
					ServiceAddress: "1.2.3.4",
					ServiceMeta: map[string]string{
						MetaKeyKubeServiceName: "service-updated",
						MetaKeyKubeNS:          "default",
						MetaKeyManagedBy:       managedByValue,
						MetaKeyPodName:         "pod1",
					},
				},
			},
			expectedProxySvcInstances: []*api.CatalogService{
				{
					ServiceID:      "pod1-service-updated-sidecar-proxy",
					ServiceName:    "service-updated-sidecar-proxy",
					ServiceAddress: "1.2.3.4",
					ServiceMeta: map[string]string{
						MetaKeyKubeServiceName: "service-updated",
						MetaKeyKubeNS:          "default",
						MetaKeyManagedBy:       managedByValue,
						MetaKeyPodName:         "pod1",
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
				pod2 := createPod("pod2", "2.3.4.5", false, false)
				endpoint := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-updated",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									IP:       "2.3.4.5",
									NodeName: &nodeName,
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
			initialConsulSvcs: []*api.AgentServiceRegistration{
				{
					ID:      "pod1-service-updated",
					Name:    "service-updated",
					Port:    80,
					Address: "1.2.3.4",
					Meta: map[string]string{
						MetaKeyKubeServiceName: "service-updated",
						MetaKeyKubeNS:          "default",
						MetaKeyManagedBy:       managedByValue,
						MetaKeyPodName:         "pod1",
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
					Meta: map[string]string{
						MetaKeyKubeServiceName: "service-updated",
						MetaKeyKubeNS:          "default",
						MetaKeyManagedBy:       managedByValue,
						MetaKeyPodName:         "pod1",
					},
				},
			},
			expectedConsulSvcInstances: nil,
			expectedProxySvcInstances:  nil,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			// The agent pod needs to have the address 127.0.0.1 so when the
			// code gets the agent pods via the label component=client, and
			// makes requests against the agent API, it will actually hit the
			// test server we have on localhost.
			fakeClientPod := createPod("fake-consul-client", "127.0.0.1", false, true)
			fakeClientPod.Labels = map[string]string{"component": "client", "app": "consul", "release": "consul"}

			// Add the default namespace.
			ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
			// Create fake k8s client.
			k8sObjects := append(tt.k8sObjects(), fakeClientPod, &ns)
			fakeClient := fake.NewClientBuilder().WithRuntimeObjects(k8sObjects...).Build()

			// Create test consul server.
			adminToken := "123e4567-e89b-12d3-a456-426614174000"
			consul, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
				if tt.enableACLs {
					c.ACL.Enabled = tt.enableACLs
					c.ACL.Tokens.InitialManagement = adminToken
				}
				c.NodeName = nodeName
			})
			require.NoError(t, err)
			defer consul.Stop()
			consul.WaitForServiceIntentions(t)
			addr := strings.Split(consul.HTTPAddr, ":")
			consulPort := addr[1]

			cfg := &api.Config{Scheme: "http", Address: consul.HTTPAddr}
			if tt.enableACLs {
				cfg.Token = adminToken
			}
			consulClient, err := api.NewClient(cfg)
			require.NoError(t, err)

			// Holds token accessorID for each service ID.
			tokensForServices := make(map[string]string)

			// Register service and proxy in consul.
			for _, svc := range tt.initialConsulSvcs {
				err = consulClient.Agent().ServiceRegister(svc)
				require.NoError(t, err)

				// Create a token for this service if ACLs are enabled.
				if tt.enableACLs {
					if svc.Kind != api.ServiceKindConnectProxy {
						test.SetupK8sAuthMethod(t, consulClient, svc.Name, svc.Meta[MetaKeyKubeNS])
						token, _, err := consulClient.ACL().Login(&api.ACLLoginParams{
							AuthMethod:  test.AuthMethod,
							BearerToken: test.ServiceAccountJWTToken,
							Meta: map[string]string{
								TokenMetaPodNameKey: fmt.Sprintf("%s/%s", svc.Meta[MetaKeyKubeNS], svc.Meta[MetaKeyPodName]),
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
								TokenMetaPodNameKey: fmt.Sprintf("%s/%s", svc.Meta[MetaKeyKubeNS], "does-not-exist"),
							},
						}, nil)
						require.NoError(t, err)
						tokensForServices["does-not-exist"+svc.Name] = token.AccessorID
					}
				}
			}

			// Create the endpoints controller.
			ep := &EndpointsController{
				Client:                fakeClient,
				Log:                   logrtest.TestLogger{T: t},
				ConsulClient:          consulClient,
				ConsulPort:            consulPort,
				ConsulScheme:          cfg.Scheme,
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSetWith(),
				ReleaseName:           "consul",
				ReleaseNamespace:      "default",
				ConsulClientCfg:       cfg,
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
			if tt.expectedAgentHealthChecks != nil {
				for i := range tt.expectedConsulSvcInstances {
					filter := fmt.Sprintf("CheckID == `%s`", tt.expectedAgentHealthChecks[i].CheckID)
					check, err := consulClient.Agent().ChecksWithFilter(filter)
					require.NoError(t, err)
					require.EqualValues(t, len(check), 1)
					// Ignoring Namespace because the response from ENT includes it and OSS does not.
					var ignoredFields = []string{"Node", "Definition", "Namespace", "Partition"}
					require.True(t, cmp.Equal(check[tt.expectedAgentHealthChecks[i].CheckID], tt.expectedAgentHealthChecks[i], cmpopts.IgnoreFields(api.AgentCheck{}, ignoredFields...)))
				}
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
				for serviceID, tokenID := range tokensForServices {
					// Read the token from Consul.
					token, _, err := consulClient.ACL().TokenRead(tokenID, nil)
					if deregisteredServices.Contains(serviceID) {
						require.EqualError(t, err, "Unexpected response code: 403 (ACL not found)")
					} else {
						require.NoError(t, err, "token should exist for service instance: "+serviceID)
						require.NotNil(t, token)
					}
				}
			}
		})
	}
}

// Tests deleting an Endpoints object, with and without matching Consul and K8s service names.
// This test covers EndpointsController.deregisterServiceOnAllAgents when the map is nil (not selectively deregistered).
func TestReconcileDeleteEndpoint(t *testing.T) {
	t.Parallel()
	nodeName := "test-node"
	cases := []struct {
		name                      string
		consulSvcName             string
		expectServicesToBeDeleted bool
		initialConsulSvcs         []*api.AgentServiceRegistration
		enableACLs                bool
		consulClientReady         bool
	}{
		{
			name:                      "Legacy service: does not delete",
			consulSvcName:             "service-deleted",
			expectServicesToBeDeleted: false,
			initialConsulSvcs: []*api.AgentServiceRegistration{
				{
					ID:      "pod1-service-deleted",
					Name:    "service-deleted",
					Port:    80,
					Address: "1.2.3.4",
					Meta:    map[string]string{"k8s-service-name": "service-deleted", "k8s-namespace": "default"},
				},
				{
					Kind:    api.ServiceKindConnectProxy,
					ID:      "pod1-service-deleted-sidecar-proxy",
					Name:    "service-deleted-sidecar-proxy",
					Port:    20000,
					Address: "1.2.3.4",
					Proxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "service-deleted",
						DestinationServiceID:   "pod1-service-deleted",
					},
					Meta: map[string]string{"k8s-service-name": "service-deleted", "k8s-namespace": "default"},
				},
			},
			consulClientReady: true,
		},
		{
			name:                      "Consul service name matches K8s service name",
			consulSvcName:             "service-deleted",
			expectServicesToBeDeleted: true,
			initialConsulSvcs: []*api.AgentServiceRegistration{
				{
					ID:      "pod1-service-deleted",
					Name:    "service-deleted",
					Port:    80,
					Address: "1.2.3.4",
					Meta:    map[string]string{"k8s-service-name": "service-deleted", "k8s-namespace": "default", MetaKeyManagedBy: managedByValue},
				},
				{
					Kind:    api.ServiceKindConnectProxy,
					ID:      "pod1-service-deleted-sidecar-proxy",
					Name:    "service-deleted-sidecar-proxy",
					Port:    20000,
					Address: "1.2.3.4",
					Proxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "service-deleted",
						DestinationServiceID:   "pod1-service-deleted",
					},
					Meta: map[string]string{"k8s-service-name": "service-deleted", "k8s-namespace": "default", MetaKeyManagedBy: managedByValue},
				},
			},
			consulClientReady: true,
		},
		{
			name:                      "Consul service name does not match K8s service name",
			consulSvcName:             "different-consul-svc-name",
			expectServicesToBeDeleted: true,
			initialConsulSvcs: []*api.AgentServiceRegistration{
				{
					ID:      "pod1-different-consul-svc-name",
					Name:    "different-consul-svc-name",
					Port:    80,
					Address: "1.2.3.4",
					Meta:    map[string]string{"k8s-service-name": "service-deleted", "k8s-namespace": "default", MetaKeyManagedBy: managedByValue},
				},
				{
					Kind:    api.ServiceKindConnectProxy,
					ID:      "pod1-different-consul-svc-name-sidecar-proxy",
					Name:    "different-consul-svc-name-sidecar-proxy",
					Port:    20000,
					Address: "1.2.3.4",
					Proxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "different-consul-svc-name",
						DestinationServiceID:   "pod1-different-consul-svc-name",
					},
					Meta: map[string]string{"k8s-service-name": "service-deleted", "k8s-namespace": "default", MetaKeyManagedBy: managedByValue},
				},
			},
			consulClientReady: true,
		},
		{
			name:                      "When ACLs are enabled, the token should be deleted",
			consulSvcName:             "service-deleted",
			expectServicesToBeDeleted: true,
			initialConsulSvcs: []*api.AgentServiceRegistration{
				{
					ID:      "pod1-service-deleted",
					Name:    "service-deleted",
					Port:    80,
					Address: "1.2.3.4",
					Meta: map[string]string{
						MetaKeyKubeServiceName: "service-deleted",
						MetaKeyKubeNS:          "default",
						MetaKeyManagedBy:       managedByValue,
						MetaKeyPodName:         "pod1",
					},
				},
				{
					Kind:    api.ServiceKindConnectProxy,
					ID:      "pod1-service-deleted-sidecar-proxy",
					Name:    "service-deleted-sidecar-proxy",
					Port:    20000,
					Address: "1.2.3.4",
					Proxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "service-deleted",
						DestinationServiceID:   "pod1-service-deleted",
					},
					Meta: map[string]string{
						MetaKeyKubeServiceName: "service-deleted",
						MetaKeyKubeNS:          "default",
						MetaKeyManagedBy:       managedByValue,
						MetaKeyPodName:         "pod1",
					},
				},
			},
			enableACLs:        true,
			consulClientReady: true,
		},
		{
			name:                      "When Consul client pod is not ready, services are not deleted",
			consulSvcName:             "service-deleted",
			expectServicesToBeDeleted: false,
			initialConsulSvcs: []*api.AgentServiceRegistration{
				{
					ID:      "pod1-service-deleted",
					Name:    "service-deleted",
					Port:    80,
					Address: "1.2.3.4",
					Meta: map[string]string{
						MetaKeyKubeServiceName: "service-deleted",
						MetaKeyKubeNS:          "default",
						MetaKeyManagedBy:       managedByValue,
						MetaKeyPodName:         "pod1",
					},
				},
				{
					Kind:    api.ServiceKindConnectProxy,
					ID:      "pod1-service-deleted-sidecar-proxy",
					Name:    "service-deleted-sidecar-proxy",
					Port:    20000,
					Address: "1.2.3.4",
					Proxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "service-deleted",
						DestinationServiceID:   "pod1-service-deleted",
					},
					Meta: map[string]string{
						MetaKeyKubeServiceName: "service-deleted",
						MetaKeyKubeNS:          "default",
						MetaKeyManagedBy:       managedByValue,
						MetaKeyPodName:         "pod1",
					},
				},
			},
			consulClientReady: false,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			// The agent pod needs to have the address 127.0.0.1 so when the
			// code gets the agent pods via the label component=client, and
			// makes requests against the agent API, it will actually hit the
			// test server we have on localhost.
			fakeClientPod := createPod("fake-consul-client", "127.0.0.1", false, true)
			fakeClientPod.Labels = map[string]string{"component": "client", "app": "consul", "release": "consul"}
			if !tt.consulClientReady {
				fakeClientPod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionFalse}}
			}

			// Add the default namespace.
			ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
			// Create fake k8s client.
			fakeClient := fake.NewClientBuilder().WithRuntimeObjects(fakeClientPod, &ns).Build()

			// Create test consul server.
			adminToken := "123e4567-e89b-12d3-a456-426614174000"
			consul, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
				if tt.enableACLs {
					c.ACL.Enabled = true
					c.ACL.Tokens.InitialManagement = adminToken
				}
				c.NodeName = nodeName
			})
			require.NoError(t, err)
			defer consul.Stop()

			consul.WaitForServiceIntentions(t)
			cfg := &api.Config{Address: consul.HTTPAddr}
			if tt.enableACLs {
				cfg.Token = adminToken
			}
			consulClient, err := api.NewClient(cfg)
			require.NoError(t, err)
			addr := strings.Split(consul.HTTPAddr, ":")
			consulPort := addr[1]

			// Register service and proxy in consul
			var token *api.ACLToken
			for _, svc := range tt.initialConsulSvcs {
				err = consulClient.Agent().ServiceRegister(svc)
				require.NoError(t, err)

				// Create a token for it if ACLs are enabled.
				if tt.enableACLs {
					test.SetupK8sAuthMethod(t, consulClient, svc.Name, "default")
					if svc.Kind != api.ServiceKindConnectProxy {
						token, _, err = consulClient.ACL().Login(&api.ACLLoginParams{
							AuthMethod:  test.AuthMethod,
							BearerToken: test.ServiceAccountJWTToken,
							Meta: map[string]string{
								"pod": fmt.Sprintf("%s/%s", svc.Meta[MetaKeyKubeNS], svc.Meta[MetaKeyPodName]),
							},
						}, nil)

						require.NoError(t, err)
					}
				}
			}

			// Create the endpoints controller
			ep := &EndpointsController{
				Client:                fakeClient,
				Log:                   logrtest.TestLogger{T: t},
				ConsulClient:          consulClient,
				ConsulPort:            consulPort,
				ConsulScheme:          "http",
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSetWith(),
				ReleaseName:           "consul",
				ReleaseNamespace:      "default",
				ConsulClientCfg:       cfg,
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

			if tt.enableACLs {
				_, _, err = consulClient.ACL().TokenRead(token.AccessorID, nil)
				require.EqualError(t, err, "Unexpected response code: 403 (ACL not found)")
			}
		})
	}
}

// TestReconcileIgnoresServiceIgnoreLabel tests that the endpoints controller correctly ignores services
// with the service-ignore label and deregisters services previously registered if the service-ignore
// label is added.
func TestReconcileIgnoresServiceIgnoreLabel(t *testing.T) {
	t.Parallel()
	nodeName := "test-node"
	serviceName := "service-ignored"
	namespace := "default"

	cases := map[string]struct {
		svcInitiallyRegistered  bool
		serviceLabels           map[string]string
		expectedNumSvcInstances int
	}{
		"Registered endpoint with label is deregistered.": {
			svcInitiallyRegistered: true,
			serviceLabels: map[string]string{
				labelServiceIgnore: "true",
			},
			expectedNumSvcInstances: 0,
		},
		"Not registered endpoint with label is never registered": {
			svcInitiallyRegistered: false,
			serviceLabels: map[string]string{
				labelServiceIgnore: "true",
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
					Name:      serviceName,
					Namespace: namespace,
					Labels:    tt.serviceLabels,
				},
				Subsets: []corev1.EndpointSubset{
					{
						Addresses: []corev1.EndpointAddress{
							{
								IP:       "1.2.3.4",
								NodeName: &nodeName,
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
			pod1 := createPod("pod1", "1.2.3.4", true, true)
			fakeClientPod := createPod("fake-consul-client", "127.0.0.1", false, true)
			fakeClientPod.Labels = map[string]string{"component": "client", "app": "consul", "release": "consul"}
			ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			k8sObjects := []runtime.Object{endpoint, pod1, fakeClientPod, &ns}
			fakeClient := fake.NewClientBuilder().WithRuntimeObjects(k8sObjects...).Build()

			// Create test Consul server.
			consul, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) { c.NodeName = nodeName })
			require.NoError(t, err)
			defer consul.Stop()
			consul.WaitForServiceIntentions(t)
			cfg := &api.Config{Address: consul.HTTPAddr}
			consulClient, err := api.NewClient(cfg)
			require.NoError(t, err)
			addr := strings.Split(consul.HTTPAddr, ":")
			consulPort := addr[1]

			// Set up the initial Consul services.
			if tt.svcInitiallyRegistered {
				err = consulClient.Agent().ServiceRegister(&api.AgentServiceRegistration{
					ID:      "pod1-" + serviceName,
					Name:    serviceName,
					Port:    0,
					Address: "1.2.3.4",
					Meta: map[string]string{
						"k8s-namespace":    namespace,
						"k8s-service-name": serviceName,
						"managed-by":       "consul-k8s-endpoints-controller",
						"pod-name":         "pod1",
					},
				})
				require.NoError(t, err)
				err = consulClient.Agent().ServiceRegister(&api.AgentServiceRegistration{
					ID:   "pod1-sidecar-proxy-" + serviceName,
					Name: serviceName + "-sidecar-proxy",
					Port: 0,
					Meta: map[string]string{
						"k8s-namespace":    namespace,
						"k8s-service-name": serviceName,
						"managed-by":       "consul-k8s-endpoints-controller",
						"pod-name":         "pod1",
					},
				})
				require.NoError(t, err)
			}

			// Create the endpoints controller.
			ep := &EndpointsController{
				Client:                fakeClient,
				Log:                   logrtest.TestLogger{T: t},
				ConsulClient:          consulClient,
				ConsulPort:            consulPort,
				ConsulScheme:          "http",
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSetWith(),
				ReleaseName:           "consul",
				ReleaseNamespace:      namespace,
				ConsulClientCfg:       cfg,
			}

			// Run the reconcile process to deregister the service if it was registered before.
			namespacedName := types.NamespacedName{Namespace: namespace, Name: serviceName}
			resp, err := ep.Reconcile(context.Background(), ctrl.Request{NamespacedName: namespacedName})
			require.NoError(t, err)
			require.False(t, resp.Requeue)

			// Check that the correct number of services are registered with Consul.
			serviceInstances, _, err := consulClient.Catalog().Service(serviceName, "", nil)
			require.NoError(t, err)
			require.Len(t, serviceInstances, tt.expectedNumSvcInstances)
			proxyServiceInstances, _, err := consulClient.Catalog().Service(serviceName+"-sidecar-proxy", "", nil)
			require.NoError(t, err)
			require.Len(t, proxyServiceInstances, tt.expectedNumSvcInstances)
		})
	}
}

// Test that when an endpoints pod specifies the name for the Kubernetes service it wants to use
// for registration, all other endpoints for that pod are skipped.
func TestReconcile_podSpecifiesExplicitService(t *testing.T) {
	nodeName := "test-node"
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
						IP:       "1.2.3.4",
						NodeName: &nodeName,
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
						IP:       "1.2.3.4",
						NodeName: &nodeName,
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
	pod1 := createPod("pod1", "1.2.3.4", true, true)
	pod1.Annotations[annotationKubernetesService] = endpoint.Name
	fakeClientPod := createPod("fake-consul-client", "127.0.0.1", false, true)
	fakeClientPod.Labels = map[string]string{"component": "client", "app": "consul", "release": "consul"}
	ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
	k8sObjects := []runtime.Object{badEndpoint, endpoint, pod1, fakeClientPod, &ns}
	fakeClient := fake.NewClientBuilder().WithRuntimeObjects(k8sObjects...).Build()

	// Create test Consul server.
	consul, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) { c.NodeName = nodeName })
	require.NoError(t, err)
	defer consul.Stop()
	consul.WaitForServiceIntentions(t)
	cfg := &api.Config{Address: consul.HTTPAddr}
	consulClient, err := api.NewClient(cfg)
	require.NoError(t, err)
	addr := strings.Split(consul.HTTPAddr, ":")
	consulPort := addr[1]

	// Create the endpoints controller.
	ep := &EndpointsController{
		Client:                fakeClient,
		Log:                   logrtest.TestLogger{T: t},
		ConsulClient:          consulClient,
		ConsulPort:            consulPort,
		ConsulScheme:          "http",
		AllowK8sNamespacesSet: mapset.NewSetWith("*"),
		DenyK8sNamespacesSet:  mapset.NewSetWith(),
		ReleaseName:           "consul",
		ReleaseNamespace:      namespace,
		ConsulClientCfg:       cfg,
	}

	serviceName := badEndpoint.Name

	// Initially register the pod with the bad endpoint
	err = consulClient.Agent().ServiceRegister(&api.AgentServiceRegistration{
		ID:      "pod1-" + serviceName,
		Name:    serviceName,
		Port:    0,
		Address: "1.2.3.4",
		Meta: map[string]string{
			"k8s-namespace":    namespace,
			"k8s-service-name": serviceName,
			"managed-by":       "consul-k8s-endpoints-controller",
			"pod-name":         "pod1",
		},
	})
	require.NoError(t, err)
	serviceInstances, _, err := consulClient.Catalog().Service(serviceName, "", nil)
	require.NoError(t, err)
	require.Len(t, serviceInstances, 1)

	// Run the reconcile process to check service deregistration.
	namespacedName := types.NamespacedName{Namespace: badEndpoint.Namespace, Name: serviceName}
	resp, err := ep.Reconcile(context.Background(), ctrl.Request{NamespacedName: namespacedName})
	require.NoError(t, err)
	require.False(t, resp.Requeue)

	// Check that the service has been deregistered with Consul.
	serviceInstances, _, err = consulClient.Catalog().Service(serviceName, "", nil)
	require.NoError(t, err)
	require.Len(t, serviceInstances, 0)
	proxyServiceInstances, _, err := consulClient.Catalog().Service(serviceName+"-sidecar-proxy", "", nil)
	require.NoError(t, err)
	require.Len(t, proxyServiceInstances, 0)

	// Run the reconcile again with the service we want to register.
	serviceName = endpoint.Name
	namespacedName = types.NamespacedName{Namespace: endpoint.Namespace, Name: serviceName}
	resp, err = ep.Reconcile(context.Background(), ctrl.Request{NamespacedName: namespacedName})
	require.NoError(t, err)
	require.False(t, resp.Requeue)

	// Check that the correct services are registered with Consul.
	serviceInstances, _, err = consulClient.Catalog().Service(serviceName, "", nil)
	require.NoError(t, err)
	require.Len(t, serviceInstances, 1)
	proxyServiceInstances, _, err = consulClient.Catalog().Service(serviceName+"-sidecar-proxy", "", nil)
	require.NoError(t, err)
	require.Len(t, proxyServiceInstances, 1)
}

func TestFilterAgentPods(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		object   client.Object
		expected bool
	}{
		"label[app]=consul label[component]=client label[release] consul": {
			object: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":       "consul",
						"component": "client",
						"release":   "consul",
					},
				},
			},
			expected: true,
		},
		"no labels": {
			object:   &corev1.Pod{},
			expected: false,
		},
		"label[app] empty": {
			object: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"component": "client",
						"release":   "consul",
					},
				},
			},
			expected: false,
		},
		"label[component] empty": {
			object: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":     "consul",
						"release": "consul",
					},
				},
			},
			expected: false,
		},
		"label[release] empty": {
			object: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":       "consul",
						"component": "client",
					},
				},
			},
			expected: false,
		},
		"label[app]!=consul label[component]=client label[release]=consul": {
			object: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":       "not-consul",
						"component": "client",
						"release":   "consul",
					},
				},
			},
			expected: false,
		},
		"label[component]!=client label[app]=consul label[release]=consul": {
			object: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":       "consul",
						"component": "not-client",
						"release":   "consul",
					},
				},
			},
			expected: false,
		},
		"label[release]!=consul label[app]=consul label[component]=client": {
			object: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":       "consul",
						"component": "client",
						"release":   "not-consul",
					},
				},
			},
			expected: false,
		},
		"label[app]!=consul label[component]!=client label[release]!=consul": {
			object: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":       "not-consul",
						"component": "not-client",
						"release":   "not-consul",
					},
				},
			},
			expected: false,
		},
	}

	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			controller := EndpointsController{
				ReleaseName: "consul",
			}

			result := controller.filterAgentPods(test.object)
			require.Equal(t, test.expected, result)
		})
	}
}

func TestRequestsForRunningAgentPods(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		agentPod          *corev1.Pod
		existingEndpoints []*corev1.Endpoints
		expectedRequests  []ctrl.Request
	}{
		"pod=running, all endpoints need to be reconciled": {
			agentPod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "consul-agent",
				},
				Spec: corev1.PodSpec{
					NodeName: "node-foo",
				},
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodReady,
							Status: corev1.ConditionTrue,
						},
					},
					Phase: corev1.PodRunning,
				},
			},
			existingEndpoints: []*corev1.Endpoints{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "endpoint-1",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									NodeName: toStringPtr("node-foo"),
								},
							},
							NotReadyAddresses: []corev1.EndpointAddress{
								{
									NodeName: toStringPtr("node-bar"),
								},
							},
						},
					},
				},
			},
			expectedRequests: []ctrl.Request{
				{
					NamespacedName: types.NamespacedName{
						Name: "endpoint-1",
					},
				},
			},
		},
		"pod=running, endpoints with ready address need to be reconciled": {
			agentPod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "consul-agent",
				},
				Spec: corev1.PodSpec{
					NodeName: "node-foo",
				},
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodReady,
							Status: corev1.ConditionTrue,
						},
					},
					Phase: corev1.PodRunning,
				},
			},
			existingEndpoints: []*corev1.Endpoints{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "endpoint-1",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									NodeName: toStringPtr("node-foo"),
								},
							},
						},
					},
				},
			},
			expectedRequests: []ctrl.Request{
				{
					NamespacedName: types.NamespacedName{
						Name: "endpoint-1",
					},
				},
			},
		},
		"pod=running, endpoints with not-ready address need to be reconciled": {
			agentPod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "consul-agent",
				},
				Spec: corev1.PodSpec{
					NodeName: "node-foo",
				},
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodReady,
							Status: corev1.ConditionTrue,
						},
					},
					Phase: corev1.PodRunning,
				},
			},
			existingEndpoints: []*corev1.Endpoints{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "endpoint-1",
					},
					Subsets: []corev1.EndpointSubset{
						{
							NotReadyAddresses: []corev1.EndpointAddress{
								{
									NodeName: toStringPtr("node-foo"),
								},
							},
						},
					},
				},
			},
			expectedRequests: []ctrl.Request{
				{
					NamespacedName: types.NamespacedName{
						Name: "endpoint-1",
					},
				},
			},
		},
		"pod=running, some endpoints need to be reconciled": {
			agentPod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "consul-agent",
				},
				Spec: corev1.PodSpec{
					NodeName: "node-foo",
				},
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodReady,
							Status: corev1.ConditionTrue,
						},
					},
					Phase: corev1.PodRunning,
				},
			},
			existingEndpoints: []*corev1.Endpoints{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "endpoint-1",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									NodeName: toStringPtr("node-foo"),
								},
							},
							NotReadyAddresses: []corev1.EndpointAddress{
								{
									NodeName: toStringPtr("node-bar"),
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "endpoint-2",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									NodeName: toStringPtr("node-other"),
								},
							},
							NotReadyAddresses: []corev1.EndpointAddress{
								{
									NodeName: toStringPtr("node-baz"),
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "endpoint-3",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									NodeName: toStringPtr("node-foo"),
								},
							},
							NotReadyAddresses: []corev1.EndpointAddress{
								{
									NodeName: toStringPtr("node-baz"),
								},
							},
						},
					},
				},
			},
			expectedRequests: []ctrl.Request{
				{
					NamespacedName: types.NamespacedName{
						Name: "endpoint-1",
					},
				},
				{
					NamespacedName: types.NamespacedName{
						Name: "endpoint-3",
					},
				},
			},
		},
		"pod=running, no endpoints need to be reconciled": {
			agentPod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "consul-agent",
				},
				Spec: corev1.PodSpec{
					NodeName: "node-foo",
				},
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodReady,
							Status: corev1.ConditionTrue,
						},
					},
					Phase: corev1.PodRunning,
				},
			},
			existingEndpoints: []*corev1.Endpoints{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "endpoint-1",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									NodeName: toStringPtr("node-baz"),
								},
							},
							NotReadyAddresses: []corev1.EndpointAddress{
								{
									NodeName: toStringPtr("node-bar"),
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "endpoint-2",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									NodeName: toStringPtr("node-bar"),
								},
							},
							NotReadyAddresses: []corev1.EndpointAddress{
								{
									NodeName: toStringPtr("node-baz"),
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "endpoint-3",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									NodeName: toStringPtr("node-bar"),
								},
							},
							NotReadyAddresses: []corev1.EndpointAddress{
								{
									NodeName: toStringPtr("node-baz"),
								},
							},
						},
					},
				},
			},
			expectedRequests: []ctrl.Request{},
		},
		"pod not ready, no endpoints need to be reconciled": {
			agentPod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "consul-agent",
				},
				Spec: corev1.PodSpec{
					NodeName: "node-foo",
				},
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodReady,
							Status: corev1.ConditionFalse,
						},
					},
					Phase: corev1.PodRunning,
				},
			},
			existingEndpoints: []*corev1.Endpoints{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "endpoint-1",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									NodeName: toStringPtr("node-foo"),
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "endpoint-3",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									NodeName: toStringPtr("node-foo"),
								},
							},
						},
					},
				},
			},
			expectedRequests: []ctrl.Request{},
		},
		"pod not running, no endpoints need to be reconciled": {
			agentPod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "consul-agent",
				},
				Spec: corev1.PodSpec{
					NodeName: "node-foo",
				},
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodReady,
							Status: corev1.ConditionTrue,
						},
					},
					Phase: corev1.PodUnknown,
				},
			},
			existingEndpoints: []*corev1.Endpoints{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "endpoint-1",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									NodeName: toStringPtr("node-foo"),
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "endpoint-3",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									NodeName: toStringPtr("node-foo"),
								},
							},
						},
					},
				},
			},
			expectedRequests: []ctrl.Request{},
		},
		"pod is deleted, no endpoints need to be reconciled": {
			agentPod: nil,
			existingEndpoints: []*corev1.Endpoints{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "endpoint-1",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									NodeName: toStringPtr("node-foo"),
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "endpoint-3",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									NodeName: toStringPtr("node-foo"),
								},
							},
						},
					},
				},
			},
			expectedRequests: []ctrl.Request{},
		},
	}

	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			logger := logrtest.TestLogger{T: t}
			s := runtime.NewScheme()
			s.AddKnownTypes(corev1.SchemeGroupVersion, &corev1.Pod{}, &corev1.Endpoints{}, &corev1.EndpointsList{})
			var objects []runtime.Object
			if test.agentPod != nil {
				objects = append(objects, test.agentPod)
			}
			for _, endpoint := range test.existingEndpoints {
				objects = append(objects, endpoint)
			}

			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(objects...).Build()

			controller := &EndpointsController{
				Client: fakeClient,
				Scheme: s,
				Log:    logger,
			}
			var requests []ctrl.Request
			if test.agentPod != nil {
				requests = controller.requestsForRunningAgentPods(test.agentPod)
			} else {
				requests = controller.requestsForRunningAgentPods(minimal())
			}
			require.ElementsMatch(t, requests, test.expectedRequests)
		})
	}
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
		expected           map[string]*api.AgentService
	}{
		{
			"no k8s service name or namespace meta",
			"",
			"",
			map[string]*api.AgentService{},
		},
		{
			"k8s service name set, but no namespace meta",
			k8sSvc,
			"",
			map[string]*api.AgentService{},
		},
		{
			"k8s namespace set, but no k8s service name meta",
			"",
			k8sNS,
			map[string]*api.AgentService{},
		},
		{
			"both k8s service name and namespace set",
			k8sSvc,
			k8sNS,
			map[string]*api.AgentService{
				"foo1": {
					ID:      "foo1",
					Service: "foo",
					Meta:    map[string]string{"k8s-service-name": k8sSvc, "k8s-namespace": k8sNS},
				},
				"foo1-proxy": {
					Kind:    api.ServiceKindConnectProxy,
					ID:      "foo1-proxy",
					Service: "foo-sidecar-proxy",
					Port:    20000,
					Proxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "foo",
						DestinationServiceID:   "foo1",
					},
					Meta: map[string]string{"k8s-service-name": k8sSvc, "k8s-namespace": k8sNS},
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			servicesInConsul := []*api.AgentServiceRegistration{
				{
					ID:   "foo1",
					Name: "foo",
					Tags: []string{},
					Meta: map[string]string{"k8s-service-name": c.k8sServiceNameMeta, "k8s-namespace": c.k8sNamespaceMeta},
				},
				{
					Kind: api.ServiceKindConnectProxy,
					ID:   "foo1-proxy",
					Name: "foo-sidecar-proxy",
					Port: 20000,
					Proxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "foo",
						DestinationServiceID:   "foo1",
					},
					Meta: map[string]string{"k8s-service-name": c.k8sServiceNameMeta, "k8s-namespace": c.k8sNamespaceMeta},
				},
				{
					ID:   "k8s-service-different-ns-id",
					Name: "k8s-service-different-ns",
					Meta: map[string]string{"k8s-service-name": c.k8sServiceNameMeta, "k8s-namespace": "different-ns"},
				},
				{
					Kind: api.ServiceKindConnectProxy,
					ID:   "k8s-service-different-ns-proxy",
					Name: "k8s-service-different-ns-proxy",
					Port: 20000,
					Tags: []string{},
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
				err := consulClient.Agent().ServiceRegister(svc)
				require.NoError(t, err)
			}

			svcs, err := serviceInstancesForK8SServiceNameAndNamespace(k8sSvc, k8sNS, consulClient)
			require.NoError(t, err)
			if len(svcs) > 0 {
				require.Len(t, svcs, 2)
				require.NotNil(t, c.expected["foo1"], svcs["foo1"])
				require.Equal(t, c.expected["foo1"].Service, svcs["foo1"].Service)
				require.NotNil(t, c.expected["foo1-proxy"], svcs["foo1-proxy"])
				require.Equal(t, c.expected["foo1-proxy"].Service, svcs["foo1-proxy"].Service)
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
			podAnnotations:      map[string]string{keyTransparentProxy: "false"},
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
			podAnnotations:      map[string]string{keyTransparentProxy: "true"},
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
			podAnnotations:      map[string]string{keyTransparentProxy: "false"},
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
			podAnnotations: map[string]string{keyTransparentProxy: "true"},
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
			namespaceLabels: map[string]string{keyTransparentProxy: "true"},
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
			namespaceLabels:    map[string]string{keyTransparentProxy: "false"},
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
			expProxyMode:       api.ProxyModeDefault,
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
			expProxyMode:       api.ProxyModeDefault,
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
			expProxyMode:       api.ProxyModeDefault,
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
				annotationOriginalPod: "{\"metadata\":{\"name\":\"test-pod-1\",\"namespace\":\"default\",\"creationTimestamp\":null,\"labels\":{\"consul.hashicorp.com/connect-inject-managed-by\":\"consul-k8s-endpoints-controller\",\"consul.hashicorp.com/connect-inject-status\":\"injected\"},\"annotations\":{\"consul.hashicorp.com/connect-inject-status\":\"injected\"}},\"spec\":{\"containers\":[{\"name\":\"test\",\"ports\":[{\"name\":\"tcp\",\"containerPort\":8081},{\"name\":\"http\",\"containerPort\":8080}],\"resources\":{},\"livenessProbe\":{\"httpGet\":{\"port\":8080}}}]},\"status\":{\"hostIP\":\"127.0.0.1\",\"podIP\":\"1.2.3.4\"}}\n",
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
						Handler: corev1.Handler{
							HTTPGet: &corev1.HTTPGetAction{
								Port: intstr.FromInt(exposedPathsLivenessPortsRangeStart),
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
					ListenerPort:  exposedPathsLivenessPortsRangeStart,
					LocalPathPort: 8080,
				},
			},
			expErr: "",
		},
		"overwrite probes disabled globally, enabled via annotation": {
			tproxyGlobalEnabled: true,
			overwriteProbes:     false,
			podAnnotations: map[string]string{
				annotationTransparentProxyOverwriteProbes: "true",
				annotationOriginalPod:                     "{\"metadata\":{\"name\":\"test-pod-1\",\"namespace\":\"default\",\"creationTimestamp\":null,\"labels\":{\"consul.hashicorp.com/connect-inject-managed-by\":\"consul-k8s-endpoints-controller\",\"consul.hashicorp.com/connect-inject-status\":\"injected\"},\"annotations\":{\"consul.hashicorp.com/transparent-proxy-overwrite-probes\":\"true\"}},\"spec\":{\"containers\":[{\"name\":\"test\",\"ports\":[{\"name\":\"tcp\",\"containerPort\":8081},{\"name\":\"http\",\"containerPort\":8080}],\"resources\":{},\"livenessProbe\":{\"httpGet\":{\"port\":8080}}}]},\"status\":{\"hostIP\":\"127.0.0.1\",\"podIP\":\"1.2.3.4\"}}\n",
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
						Handler: corev1.Handler{
							HTTPGet: &corev1.HTTPGetAction{
								Port: intstr.FromInt(exposedPathsLivenessPortsRangeStart),
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
					ListenerPort:  exposedPathsLivenessPortsRangeStart,
					LocalPathPort: 8080,
				},
			},
			expErr: "",
		},
		"overwrite probes enabled globally, tproxy disabled": {
			tproxyGlobalEnabled: false,
			overwriteProbes:     true,
			podAnnotations: map[string]string{
				annotationOriginalPod: "{\"metadata\":{\"name\":\"test-pod-1\",\"namespace\":\"default\",\"creationTimestamp\":null,\"labels\":{\"consul.hashicorp.com/connect-inject-managed-by\":\"consul-k8s-endpoints-controller\",\"consul.hashicorp.com/connect-inject-status\":\"injected\"},\"annotations\":{\"consul.hashicorp.com/connect-inject-status\":\"injected\"}},\"spec\":{\"containers\":[{\"name\":\"test\",\"ports\":[{\"name\":\"tcp\",\"containerPort\":8081},{\"name\":\"http\",\"containerPort\":8080}],\"resources\":{},\"livenessProbe\":{\"httpGet\":{\"port\":8080}}}]},\"status\":{\"hostIP\":\"127.0.0.1\",\"podIP\":\"1.2.3.4\"}}\n",
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
						Handler: corev1.Handler{
							HTTPGet: &corev1.HTTPGetAction{
								Port: intstr.FromInt(exposedPathsLivenessPortsRangeStart),
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
				annotationOriginalPod: "{\"metadata\":{\"name\":\"test-pod-1\",\"namespace\":\"default\",\"creationTimestamp\":null,\"labels\":{\"consul.hashicorp.com/connect-inject-managed-by\":\"consul-k8s-endpoints-controller\",\"consul.hashicorp.com/connect-inject-status\":\"injected\"}},\"spec\":{\"containers\":[{\"name\":\"test\",\"ports\":[{\"name\":\"tcp\",\"containerPort\":8081},{\"name\":\"http\",\"containerPort\":8080}],\"resources\":{},\"readinessProbe\":{\"httpGet\":{\"port\":8080}}}]},\"status\":{\"hostIP\":\"127.0.0.1\",\"podIP\":\"1.2.3.4\"}}\n",
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
						Handler: corev1.Handler{
							HTTPGet: &corev1.HTTPGetAction{
								Port: intstr.FromInt(exposedPathsReadinessPortsRangeStart),
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
					ListenerPort:  exposedPathsReadinessPortsRangeStart,
					LocalPathPort: 8080,
				},
			},
			expErr: "",
		},
		"startup only probe provided": {
			tproxyGlobalEnabled: true,
			overwriteProbes:     true,
			podAnnotations: map[string]string{
				annotationOriginalPod: "{\"metadata\":{\"name\":\"test-pod-1\",\"namespace\":\"default\",\"creationTimestamp\":null,\"labels\":{\"consul.hashicorp.com/connect-inject-managed-by\":\"consul-k8s-endpoints-controller\",\"consul.hashicorp.com/connect-inject-status\":\"injected\"}},\"spec\":{\"containers\":[{\"name\":\"test\",\"ports\":[{\"name\":\"tcp\",\"containerPort\":8081},{\"name\":\"http\",\"containerPort\":8080}],\"resources\":{},\"startupProbe\":{\"httpGet\":{\"port\":8080}}}]},\"status\":{\"hostIP\":\"127.0.0.1\",\"podIP\":\"1.2.3.4\"}}\n",
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
						Handler: corev1.Handler{
							HTTPGet: &corev1.HTTPGetAction{
								Port: intstr.FromInt(exposedPathsStartupPortsRangeStart),
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
					ListenerPort:  exposedPathsStartupPortsRangeStart,
					LocalPathPort: 8080,
				},
			},
			expErr: "",
		},
		"all probes provided": {
			tproxyGlobalEnabled: true,
			overwriteProbes:     true,
			podAnnotations: map[string]string{
				annotationOriginalPod: "{\"metadata\":{\"name\":\"test-pod-1\",\"namespace\":\"default\",\"creationTimestamp\":null,\"labels\":{\"consul.hashicorp.com/connect-inject-managed-by\":\"consul-k8s-endpoints-controller\",\"consul.hashicorp.com/connect-inject-status\":\"injected\"}},\"spec\":{\"containers\":[{\"name\":\"test\",\"ports\":[{\"name\":\"tcp\",\"containerPort\":8081},{\"name\":\"http\",\"containerPort\":8080}],\"resources\":{},\"livenessProbe\":{\"httpGet\":{\"port\":8080}},\"readinessProbe\":{\"httpGet\":{\"port\":8081}},\"startupProbe\":{\"httpGet\":{\"port\":8081}}}]},\"status\":{\"hostIP\":\"127.0.0.1\",\"podIP\":\"1.2.3.4\"}}\n",
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
						Handler: corev1.Handler{
							HTTPGet: &corev1.HTTPGetAction{
								Port: intstr.FromInt(exposedPathsLivenessPortsRangeStart),
							},
						},
					},
					ReadinessProbe: &corev1.Probe{
						Handler: corev1.Handler{
							HTTPGet: &corev1.HTTPGetAction{
								Port: intstr.FromInt(exposedPathsReadinessPortsRangeStart),
							},
						},
					},
					StartupProbe: &corev1.Probe{
						Handler: corev1.Handler{
							HTTPGet: &corev1.HTTPGetAction{
								Port: intstr.FromInt(exposedPathsStartupPortsRangeStart),
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
					ListenerPort:  exposedPathsLivenessPortsRangeStart,
					LocalPathPort: 8080,
				},
				{
					ListenerPort:  exposedPathsReadinessPortsRangeStart,
					LocalPathPort: 8081,
				},
				{
					ListenerPort:  exposedPathsStartupPortsRangeStart,
					LocalPathPort: 8081,
				},
			},
			expErr: "",
		},
		"multiple containers with all probes provided": {
			tproxyGlobalEnabled: true,
			overwriteProbes:     true,
			podAnnotations: map[string]string{
				annotationOriginalPod: "{\"metadata\":{\"name\":\"test-pod-1\",\"namespace\":\"default\",\"creationTimestamp\":null,\"labels\":{\"consul.hashicorp.com/connect-inject-managed-by\":\"consul-k8s-endpoints-controller\",\"consul.hashicorp.com/connect-inject-status\":\"injected\"}},\"spec\":{\"containers\":[{\"name\":\"test\",\"ports\":[{\"name\":\"tcp\",\"containerPort\":8081},{\"name\":\"http\",\"containerPort\":8080}],\"resources\":{},\"livenessProbe\":{\"httpGet\":{\"port\":8080}},\"readinessProbe\":{\"httpGet\":{\"port\":8081}},\"startupProbe\":{\"httpGet\":{\"port\":8081}}},{\"name\":\"test-2\",\"ports\":[{\"name\":\"tcp\",\"containerPort\":8083},{\"name\":\"http\",\"containerPort\":8082}],\"resources\":{},\"livenessProbe\":{\"httpGet\":{\"port\":8082}},\"readinessProbe\":{\"httpGet\":{\"port\":8083}},\"startupProbe\":{\"httpGet\":{\"port\":8083}}},{\"name\":\"envoy-sidecar\",\"ports\":[{\"name\":\"http\",\"containerPort\":20000}],\"resources\":{}}]},\"status\":{\"hostIP\":\"127.0.0.1\",\"podIP\":\"1.2.3.4\"}}\n",
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
						Handler: corev1.Handler{
							HTTPGet: &corev1.HTTPGetAction{
								Port: intstr.FromInt(exposedPathsLivenessPortsRangeStart),
							},
						},
					},
					ReadinessProbe: &corev1.Probe{
						Handler: corev1.Handler{
							HTTPGet: &corev1.HTTPGetAction{
								Port: intstr.FromInt(exposedPathsReadinessPortsRangeStart),
							},
						},
					},
					StartupProbe: &corev1.Probe{
						Handler: corev1.Handler{
							HTTPGet: &corev1.HTTPGetAction{
								Port: intstr.FromInt(exposedPathsStartupPortsRangeStart),
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
						Handler: corev1.Handler{
							HTTPGet: &corev1.HTTPGetAction{
								Port: intstr.FromInt(exposedPathsLivenessPortsRangeStart + 1),
							},
						},
					},
					ReadinessProbe: &corev1.Probe{
						Handler: corev1.Handler{
							HTTPGet: &corev1.HTTPGetAction{
								Port: intstr.FromInt(exposedPathsReadinessPortsRangeStart + 1),
							},
						},
					},
					StartupProbe: &corev1.Probe{
						Handler: corev1.Handler{
							HTTPGet: &corev1.HTTPGetAction{
								Port: intstr.FromInt(exposedPathsStartupPortsRangeStart + 1),
							},
						},
					},
				},
				{
					Name: envoySidecarContainer,
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
					ListenerPort:  exposedPathsLivenessPortsRangeStart,
					LocalPathPort: 8080,
				},
				{
					ListenerPort:  exposedPathsReadinessPortsRangeStart,
					LocalPathPort: 8081,
				},
				{
					ListenerPort:  exposedPathsStartupPortsRangeStart,
					LocalPathPort: 8081,
				},
				{
					ListenerPort:  exposedPathsLivenessPortsRangeStart + 1,
					LocalPathPort: 8082,
				},
				{
					ListenerPort:  exposedPathsReadinessPortsRangeStart + 1,
					LocalPathPort: 8083,
				},
				{
					ListenerPort:  exposedPathsStartupPortsRangeStart + 1,
					LocalPathPort: 8083,
				},
			},
			expErr: "",
		},
		"non-http probe": {
			tproxyGlobalEnabled: true,
			overwriteProbes:     true,
			podAnnotations: map[string]string{
				annotationOriginalPod: "{\"metadata\":{\"name\":\"test-pod-1\",\"namespace\":\"default\",\"creationTimestamp\":null,\"labels\":{\"consul.hashicorp.com/connect-inject-managed-by\":\"consul-k8s-endpoints-controller\",\"consul.hashicorp.com/connect-inject-status\":\"injected\"}},\"spec\":{\"containers\":[{\"name\":\"test\",\"ports\":[{\"name\":\"tcp\",\"containerPort\":8081},{\"name\":\"http\",\"containerPort\":8080}],\"resources\":{},\"livenessProbe\":{\"tcpSocket\":{\"port\":8080}}}]},\"status\":{\"hostIP\":\"127.0.0.1\",\"podIP\":\"1.2.3.4\"}}\n",
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
						Handler: corev1.Handler{
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
				annotationOriginalPod: "{\"metadata\":{\"name\":\"test-pod-1\",\"namespace\":\"default\",\"creationTimestamp\":null,\"labels\":{\"consul.hashicorp.com/connect-inject-managed-by\":\"consul-k8s-endpoints-controller\",\"consul.hashicorp.com/connect-inject-status\":\"injected\"}},\"spec\":{\"containers\":[{\"name\":\"test\",\"ports\":[{\"name\":\"tcp\",\"containerPort\":8081},{\"name\":\"http\",\"containerPort\":8080}],\"resources\":{},\"livenessProbe\":{\"httpGet\":{\"port\":\"tcp\"}},\"readinessProbe\":{\"httpGet\":{\"port\":\"http\"}},\"startupProbe\":{\"httpGet\":{\"port\":\"http\"}}}]},\"status\":{\"hostIP\":\"127.0.0.1\",\"podIP\":\"1.2.3.4\"}}\n",
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
						Handler: corev1.Handler{
							HTTPGet: &corev1.HTTPGetAction{
								Port: intstr.FromInt(exposedPathsLivenessPortsRangeStart),
							},
						},
					},
					ReadinessProbe: &corev1.Probe{
						Handler: corev1.Handler{
							HTTPGet: &corev1.HTTPGetAction{
								Port: intstr.FromInt(exposedPathsReadinessPortsRangeStart),
							},
						},
					},
					StartupProbe: &corev1.Probe{
						Handler: corev1.Handler{
							HTTPGet: &corev1.HTTPGetAction{
								Port: intstr.FromInt(exposedPathsStartupPortsRangeStart),
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
					ListenerPort:  exposedPathsLivenessPortsRangeStart,
					LocalPathPort: 8081,
				},
				{
					ListenerPort:  exposedPathsReadinessPortsRangeStart,
					LocalPathPort: 8080,
				},
				{
					ListenerPort:  exposedPathsStartupPortsRangeStart,
					LocalPathPort: 8080,
				},
			},
			expErr: "",
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			pod := createPod("test-pod-1", "1.2.3.4", true, true)
			if c.podAnnotations != nil {
				pod.Annotations = c.podAnnotations
			}
			if c.podContainers != nil {
				pod.Spec.Containers = c.podContainers
			}

			// We set these annotations explicitly as these are set by the handler and we
			// need these values to determine which port to use for the service registration.
			pod.Annotations[annotationPort] = "tcp"

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

			epCtrl := EndpointsController{
				Client:                 fakeClient,
				EnableTransparentProxy: c.tproxyGlobalEnabled,
				TProxyOverwriteProbes:  c.overwriteProbes,
				Log:                    logrtest.TestLogger{T: t},
			}

			serviceRegistration, proxyServiceRegistration, err := epCtrl.createServiceRegistrations(*pod, *endpoints)
			if c.expErr != "" {
				require.EqualError(t, err, c.expErr)
			} else {
				require.NoError(t, err)

				require.Equal(t, c.expProxyMode, proxyServiceRegistration.Proxy.Mode)
				require.Equal(t, c.expTaggedAddresses, serviceRegistration.TaggedAddresses)
				require.Equal(t, c.expTaggedAddresses, proxyServiceRegistration.TaggedAddresses)
				require.Equal(t, c.expExposePaths, proxyServiceRegistration.Proxy.Expose.Paths)
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
			description:       `{"pod":"default/pod"}`,
			expectedTokenMeta: map[string]string{"pod": "default/pod"},
		},
		"consul's default description prefix": {
			description:       `token created via login: {"pod":"default/pod"}`,
			expectedTokenMeta: map[string]string{"pod": "default/pod"},
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

func createPod(name, ip string, inject bool, managedByEndpointsController bool) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   "default",
			Labels:      map[string]string{},
			Annotations: map[string]string{},
		},
		Status: corev1.PodStatus{
			PodIP:  ip,
			HostIP: "127.0.0.1",
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}
	if inject {
		pod.Labels[keyInjectStatus] = injected
		pod.Annotations[keyInjectStatus] = injected
	}
	if managedByEndpointsController {
		pod.Labels[keyManagedBy] = managedByValue
	}
	return pod
}

func toStringPtr(input string) *string {
	return &input
}
