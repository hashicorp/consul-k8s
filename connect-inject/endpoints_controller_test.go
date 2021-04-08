package connectinject

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/deckarep/golang-set"
	logrtest "github.com/go-logr/logr/testing"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/hashicorp/consul-k8s/subcommand/common"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	testFailureMessage = "Kubernetes pod readiness probe failed"
	ttl                = "ttl"
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
				pod1 := createPod("pod1", "1.2.3.4", true)
				return *pod1
			},
			expected: true,
		},
		{
			name: "Pod without injected annotation",
			pod: func() corev1.Pod {
				pod1 := createPod("pod1", "1.2.3.4", false)
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
	caFile, certFile, keyFile := common.GenerateServerCerts(t)
	// Create test consul server with ACLs and TLS
	consul, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
		c.ACL.Enabled = true
		c.ACL.DefaultPolicy = "deny"
		c.ACL.Tokens.Master = masterToken
		c.CAFile = caFile
		c.CertFile = certFile
		c.KeyFile = keyFile
		c.NodeName = nodeName
	})
	require.NoError(t, err)
	defer consul.Stop()

	consul.WaitForSerfCheck(t)
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

	pod := createPod("pod1", "1.2.3.4", true)
	pod.Annotations[annotationUpstreams] = "upstream1:1234:dc1"

	upstreams, err := ep.processUpstreams(*pod)
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
		name              string
		pod               func() *corev1.Pod
		expected          []api.Upstream
		expErr            string
		configEntry       func() api.ConfigEntry
		consulUnavailable bool
	}{
		{
			name: "upstream with datacenter without ProxyDefaults",
			pod: func() *corev1.Pod {
				pod1 := createPod("pod1", "1.2.3.4", true)
				pod1.Annotations[annotationUpstreams] = "upstream1:1234:dc1"
				return pod1
			},
			expErr: "upstream \"upstream1:1234:dc1\" is invalid: there is no ProxyDefaults config to set mesh gateway mode",
		},
		{
			name: "upstream with datacenter with ProxyDefaults whose mesh gateway mode is not local or remote",
			pod: func() *corev1.Pod {
				pod1 := createPod("pod1", "1.2.3.4", true)
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
		},
		{
			name: "upstream with datacenter with ProxyDefaults and mesh gateway is in local mode",
			pod: func() *corev1.Pod {
				pod1 := createPod("pod1", "1.2.3.4", true)
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
		},
		{
			name: "upstream with datacenter with ProxyDefaults and mesh gateway in remote mode",
			pod: func() *corev1.Pod {
				pod1 := createPod("pod1", "1.2.3.4", true)
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
		},
		{
			name:              "when consul is unavailable, we don't return an error",
			consulUnavailable: true,
			pod: func() *corev1.Pod {
				pod1 := createPod("pod1", "1.2.3.4", true)
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
		},
		{
			name: "single upstream",
			pod: func() *corev1.Pod {
				pod1 := createPod("pod1", "1.2.3.4", true)
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
		},
		{
			name: "multiple upstreams",
			pod: func() *corev1.Pod {
				pod1 := createPod("pod1", "1.2.3.4", true)
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
		},
		{
			name: "prepared query upstream",
			pod: func() *corev1.Pod {
				pod1 := createPod("pod1", "1.2.3.4", true)
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
		},
		{
			name: "prepared query and non-query upstreams",
			pod: func() *corev1.Pod {
				pod1 := createPod("pod1", "1.2.3.4", true)
				pod1.Annotations[annotationUpstreams] = "prepared_query:queryname:1234, upstream1:2234, prepared_query:6687bd19-5654-76be-d764:8202"
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
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			// Create test consul server
			consul, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
				c.NodeName = nodeName
			})
			require.NoError(t, err)
			defer consul.Stop()

			consul.WaitForSerfCheck(t)
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
				Log:                   logrtest.TestLogger{T: t},
				ConsulClient:          consulClient,
				ConsulPort:            consulPort,
				ConsulScheme:          "http",
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSetWith(),
			}

			upstreams, err := ep.processUpstreams(*tt.pod())
			if tt.expErr != "" {
				require.EqualError(t, err, tt.expErr)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expected, upstreams)
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
						corev1.EndpointSubset{
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
				pod1 := createPod("pod1", "1.2.3.4", true)
				endpoint := &corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-created",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						corev1.EndpointSubset{
							Addresses: []corev1.EndpointAddress{
								corev1.EndpointAddress{
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
					ServiceMeta:    map[string]string{MetaKeyPodName: "pod1", MetaKeyKubeServiceName: "service-created", MetaKeyKubeNS: "default"},
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
					ServiceMeta: map[string]string{MetaKeyPodName: "pod1", MetaKeyKubeServiceName: "service-created", MetaKeyKubeNS: "default"},
					ServiceTags: []string{},
				},
			},
			expectedAgentHealthChecks: []*api.AgentCheck{
				{
					CheckID:     "default/pod1-service-created/kubernetes-health-check",
					ServiceName: "service-created",
					ServiceID:   "pod1-service-created",
					Name:        "Kubernetes Health Check",
					Status:      api.HealthCritical,
					Output:      testFailureMessage,
					Type:        ttl,
				},
			},
		},
		{
			name:          "Endpoints with multiple addresses",
			consulSvcName: "service-created",
			k8sObjects: func() []runtime.Object {
				pod1 := createPod("pod1", "1.2.3.4", true)
				pod2 := createPod("pod2", "2.2.3.4", true)
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
					ServiceMeta:    map[string]string{MetaKeyPodName: "pod1", MetaKeyKubeServiceName: "service-created", MetaKeyKubeNS: "default"},
					ServiceTags:    []string{},
				},
				{
					ServiceID:      "pod2-service-created",
					ServiceName:    "service-created",
					ServiceAddress: "2.2.3.4",
					ServicePort:    0,
					ServiceMeta:    map[string]string{MetaKeyPodName: "pod2", MetaKeyKubeServiceName: "service-created", MetaKeyKubeNS: "default"},
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
					ServiceMeta: map[string]string{MetaKeyPodName: "pod1", MetaKeyKubeServiceName: "service-created", MetaKeyKubeNS: "default"},
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
					ServiceMeta: map[string]string{MetaKeyPodName: "pod2", MetaKeyKubeServiceName: "service-created", MetaKeyKubeNS: "default"},
					ServiceTags: []string{},
				},
			},
			expectedAgentHealthChecks: []*api.AgentCheck{
				{
					CheckID:     "default/pod1-service-created/kubernetes-health-check",
					ServiceName: "service-created",
					ServiceID:   "pod1-service-created",
					Name:        "Kubernetes Health Check",
					Status:      api.HealthCritical,
					Output:      testFailureMessage,
					Type:        ttl,
				},
				{
					CheckID:     "default/pod2-service-created/kubernetes-health-check",
					ServiceName: "service-created",
					ServiceID:   "pod2-service-created",
					Name:        "Kubernetes Health Check",
					Status:      api.HealthCritical,
					Output:      testFailureMessage,
					Type:        ttl,
				},
			},
		},
		{
			name:          "Every configurable field set: port, different Consul service name, meta, tags, upstreams, metrics",
			consulSvcName: "different-consul-svc-name",
			k8sObjects: func() []runtime.Object {
				pod1 := createPod("pod1", "1.2.3.4", true)
				pod1.Annotations[annotationPort] = "1234"
				pod1.Annotations[annotationService] = "different-consul-svc-name"
				pod1.Annotations[fmt.Sprintf("%sname", annotationMeta)] = "abc"
				pod1.Annotations[fmt.Sprintf("%sversion", annotationMeta)] = "2"
				pod1.Annotations[annotationTags] = "abc,123"
				pod1.Annotations[annotationConnectTags] = "def,456"
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
						MetaKeyPodName:         "pod1",
						MetaKeyKubeServiceName: "service-created",
						MetaKeyKubeNS:          "default",
					},
					ServiceTags: []string{"abc", "123", "def", "456"},
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
						MetaKeyPodName:         "pod1",
						MetaKeyKubeServiceName: "service-created",
						MetaKeyKubeNS:          "default",
					},
					ServiceTags: []string{"abc", "123", "def", "456"},
				},
			},
			expectedAgentHealthChecks: []*api.AgentCheck{
				{
					CheckID:     "default/pod1-different-consul-svc-name/kubernetes-health-check",
					ServiceName: "different-consul-svc-name",
					ServiceID:   "pod1-different-consul-svc-name",
					Name:        "Kubernetes Health Check",
					Status:      api.HealthCritical,
					Output:      testFailureMessage,
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
			fakeClientPod := createPod("fake-consul-client", "127.0.0.1", false)
			fakeClientPod.Labels = map[string]string{"component": "client", "app": "consul", "release": "consul"}

			// Create fake k8s client
			k8sObjects := append(tt.k8sObjects(), fakeClientPod)
			fakeClient := fake.NewClientBuilder().WithRuntimeObjects(k8sObjects...).Build()

			// Create test consul server
			consul, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
				c.NodeName = nodeName
			})
			require.NoError(t, err)
			defer consul.Stop()
			consul.WaitForSerfCheck(t)

			cfg := &api.Config{
				Address: consul.HTTPAddr,
			}
			consulClient, err := api.NewClient(cfg)
			require.NoError(t, err)
			addr := strings.Split(consul.HTTPAddr, ":")
			consulPort := addr[1]

			// Register service and proxy in consul
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
			require.NoError(t, err)
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
				require.Equal(t, tt.expectedProxySvcInstances[i].ServiceProxy, instance.ServiceProxy)
				require.Equal(t, tt.expectedProxySvcInstances[i].ServiceMeta, instance.ServiceMeta)
				require.Equal(t, tt.expectedProxySvcInstances[i].ServiceTags, instance.ServiceTags)
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
				for i, _ := range tt.expectedConsulSvcInstances {
					filter := fmt.Sprintf("CheckID == `%s`", tt.expectedAgentHealthChecks[i].CheckID)
					check, err := consulClient.Agent().ChecksWithFilter(filter)
					require.NoError(t, err)
					require.EqualValues(t, len(check), 1)
					// Ignoring Namespace because the response from ENT includes it and OSS does not.
					var ignoredFields = []string{"Node", "Definition", "Namespace"}
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
// since the map will not be nil. This test also runs each test with ACLs+TLS enabled and disabled, since it covers all the cases where a Consul client is created.
func TestReconcileUpdateEndpoint(t *testing.T) {
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
	}{
		{
			name:          "Endpoints has an updated address because health check changes from unhealthy to healthy",
			consulSvcName: "service-updated",
			k8sObjects: func() []runtime.Object {
				pod1 := createPod("pod1", "1.2.3.4", true)
				pod1.Status.Conditions = []corev1.PodCondition{{
					Type:   corev1.PodReady,
					Status: corev1.ConditionTrue,
				}}
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
						Status:                 "passing",
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
			expectedNumSvcInstances: 1,
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
				pod1 := createPod("pod1", "1.2.3.4", true)
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
						Status:                 "passing",
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
			expectedNumSvcInstances: 1,
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
					Output:      testFailureMessage,
					Type:        ttl,
				},
			},
		},
		{
			name:          "Endpoints has an updated address (pod IP change).",
			consulSvcName: "service-updated",
			k8sObjects: func() []runtime.Object {
				pod1 := createPod("pod1", "4.4.4.4", true)
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
			expectedNumSvcInstances: 1,
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
				pod1 := createPod("pod1", "4.4.4.4", true)
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
				},
			},
			expectedNumSvcInstances: 1,
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
			name:          "Endpoints has additional address not in Consul.",
			consulSvcName: "service-updated",
			k8sObjects: func() []runtime.Object {
				pod1 := createPod("pod1", "1.2.3.4", true)
				pod2 := createPod("pod2", "2.2.3.4", true)
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
			expectedNumSvcInstances: 2,
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
					Status:      api.HealthCritical,
					Output:      testFailureMessage,
					Type:        ttl,
				},
				{
					CheckID:     "default/pod2-service-updated/kubernetes-health-check",
					ServiceName: "service-updated",
					ServiceID:   "pod2-service-updated",
					Name:        "Kubernetes Health Check",
					Status:      api.HealthCritical,
					Output:      testFailureMessage,
					Type:        ttl,
				},
			},
		},
		{
			name:          "Consul has instances that are not in the Endpoints addresses.",
			consulSvcName: "service-updated",
			k8sObjects: func() []runtime.Object {
				pod1 := createPod("pod1", "1.2.3.4", true)
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
					Meta:    map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default"},
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
					Meta: map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default"},
				},
				{
					ID:      "pod2-service-updated",
					Name:    "service-updated",
					Port:    80,
					Address: "2.2.3.4",
					Meta:    map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default"},
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
					Meta: map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default"},
				},
			},
			expectedNumSvcInstances: 1,
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
			name:          "Different Consul service name: Consul has instances that are not in the Endpoints addresses.",
			consulSvcName: "different-consul-svc-name",
			k8sObjects: func() []runtime.Object {
				pod1 := createPod("pod1", "1.2.3.4", true)
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
					Meta:    map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default"},
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
					Meta: map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default"},
				},
				{
					ID:      "pod2-different-consul-svc-name",
					Name:    "different-consul-svc-name",
					Port:    80,
					Address: "2.2.3.4",
					Meta:    map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default"},
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
					Meta: map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default"},
				},
			},
			expectedNumSvcInstances: 1,
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
					Meta:    map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default"},
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
					Meta: map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default"},
				},
				{
					ID:      "pod2-service-updated",
					Name:    "service-updated",
					Port:    80,
					Address: "2.2.3.4",
					Meta:    map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default"},
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
					Meta: map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default"},
				},
			},
			expectedNumSvcInstances:    0,
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
					Meta:    map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default"},
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
					Meta: map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default"},
				},
				{
					ID:      "pod2-different-consul-svc-name",
					Name:    "different-consul-svc-name",
					Port:    80,
					Address: "2.2.3.4",
					Meta:    map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default"},
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
					Meta: map[string]string{"k8s-service-name": "service-updated", "k8s-namespace": "default"},
				},
			},
			expectedNumSvcInstances:    0,
			expectedConsulSvcInstances: []*api.CatalogService{},
			expectedProxySvcInstances:  []*api.CatalogService{},
		},
	}
	// Each test is run with ACLs+TLS (secure) enabled and disabled.
	for _, secure := range []bool{true, false} {
		for _, tt := range cases {
			t.Run(fmt.Sprintf("%s - secure: %v", tt.name, secure), func(t *testing.T) {
				// The agent pod needs to have the address 127.0.0.1 so when the
				// code gets the agent pods via the label component=client, and
				// makes requests against the agent API, it will actually hit the
				// test server we have on localhost.
				fakeClientPod := createPod("fake-consul-client", "127.0.0.1", false)
				fakeClientPod.Labels = map[string]string{"component": "client", "app": "consul", "release": "consul"}

				// Create fake k8s client
				k8sObjects := append(tt.k8sObjects(), fakeClientPod)
				fakeClient := fake.NewClientBuilder().WithRuntimeObjects(k8sObjects...).Build()

				masterToken := "b78d37c7-0ca7-5f4d-99ee-6d9975ce4586"
				caFile, certFile, keyFile := common.GenerateServerCerts(t)
				// Create test consul server, with ACLs+TLS if necessary
				consul, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
					if secure {
						c.ACL.Enabled = true
						c.ACL.DefaultPolicy = "deny"
						c.ACL.Tokens.Master = masterToken
						c.CAFile = caFile
						c.CertFile = certFile
						c.KeyFile = keyFile
					}
					c.NodeName = nodeName
				})
				require.NoError(t, err)
				defer consul.Stop()
				consul.WaitForSerfCheck(t)
				addr := strings.Split(consul.HTTPAddr, ":")
				consulPort := addr[1]

				cfg := &api.Config{
					Scheme:  "http",
					Address: consul.HTTPAddr,
				}
				if secure {
					consulPort = strings.Split(consul.HTTPSAddr, ":")[1]
					cfg.Address = consul.HTTPSAddr
					cfg.Scheme = "https"
					cfg.TLSConfig = api.TLSConfig{
						CAFile: caFile,
					}
					cfg.Token = masterToken
				}
				consulClient, err := api.NewClient(cfg)
				require.NoError(t, err)

				// Register service and proxy in consul
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
					ConsulScheme:          cfg.Scheme,
					AllowK8sNamespacesSet: mapset.NewSetWith("*"),
					DenyK8sNamespacesSet:  mapset.NewSetWith(),
					ReleaseName:           "consul",
					ReleaseNamespace:      "default",
					ConsulClientCfg:       cfg,
				}
				namespacedName := types.NamespacedName{
					Namespace: "default",
					Name:      "service-updated",
				}

				resp, err := ep.Reconcile(context.Background(), ctrl.Request{
					NamespacedName: namespacedName,
				})
				require.NoError(t, err)
				require.False(t, resp.Requeue)

				// After reconciliation, Consul should have service-updated with the correct number of instances
				serviceInstances, _, err := consulClient.Catalog().Service(tt.consulSvcName, "", nil)
				require.NoError(t, err)
				require.Len(t, serviceInstances, tt.expectedNumSvcInstances)
				for i, instance := range serviceInstances {
					require.Equal(t, tt.expectedConsulSvcInstances[i].ServiceID, instance.ServiceID)
					require.Equal(t, tt.expectedConsulSvcInstances[i].ServiceAddress, instance.ServiceAddress)
				}
				proxyServiceInstances, _, err := consulClient.Catalog().Service(fmt.Sprintf("%s-sidecar-proxy", tt.consulSvcName), "", nil)
				require.NoError(t, err)
				require.Len(t, proxyServiceInstances, tt.expectedNumSvcInstances)
				for i, instance := range proxyServiceInstances {
					require.Equal(t, tt.expectedProxySvcInstances[i].ServiceID, instance.ServiceID)
					require.Equal(t, tt.expectedProxySvcInstances[i].ServiceAddress, instance.ServiceAddress)
				}
				// Check that the Consul health check was created for the k8s pod.
				if tt.expectedAgentHealthChecks != nil {
					for i, _ := range tt.expectedConsulSvcInstances {
						filter := fmt.Sprintf("CheckID == `%s`", tt.expectedAgentHealthChecks[i].CheckID)
						check, err := consulClient.Agent().ChecksWithFilter(filter)
						require.NoError(t, err)
						require.EqualValues(t, len(check), 1)
						// Ignoring Namespace because the response from ENT includes it and OSS does not.
						var ignoredFields = []string{"Node", "Definition", "Namespace"}
						require.True(t, cmp.Equal(check[tt.expectedAgentHealthChecks[i].CheckID], tt.expectedAgentHealthChecks[i], cmpopts.IgnoreFields(api.AgentCheck{}, ignoredFields...)))
					}
				}
			})
		}
	}
}

// Tests deleting an Endpoints object, with and without matching Consul and K8s service names.
// This test covers EndpointsController.deregisterServiceOnAllAgents when the map is nil (not selectively deregistered).
func TestReconcileDeleteEndpoint(t *testing.T) {
	t.Parallel()
	nodeName := "test-node"
	cases := []struct {
		name              string
		consulSvcName     string
		initialConsulSvcs []*api.AgentServiceRegistration
	}{
		{
			name:          "Consul service name matches K8s service name",
			consulSvcName: "service-deleted",
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
		},
		{
			name:          "Consul service name does not match K8s service name",
			consulSvcName: "different-consul-svc-name",
			initialConsulSvcs: []*api.AgentServiceRegistration{
				{
					ID:      "pod1-different-consul-svc-name",
					Name:    "different-consul-svc-name",
					Port:    80,
					Address: "1.2.3.4",
					Meta:    map[string]string{"k8s-service-name": "service-deleted", "k8s-namespace": "default"},
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
					Meta: map[string]string{"k8s-service-name": "service-deleted", "k8s-namespace": "default"},
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
			fakeClientPod := createPod("fake-consul-client", "127.0.0.1", false)
			fakeClientPod.Labels = map[string]string{"component": "client", "app": "consul", "release": "consul"}

			// Create fake k8s client
			fakeClient := fake.NewClientBuilder().WithRuntimeObjects(fakeClientPod).Build()

			// Create test consul server
			consul, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
				c.NodeName = nodeName
			})
			require.NoError(t, err)
			defer consul.Stop()

			consul.WaitForSerfCheck(t)
			cfg := &api.Config{
				Address: consul.HTTPAddr,
			}
			consulClient, err := api.NewClient(cfg)
			require.NoError(t, err)
			addr := strings.Split(consul.HTTPAddr, ":")
			consulPort := addr[1]

			// Register service and proxy in consul
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
			require.NoError(t, err)
			require.Empty(t, serviceInstances)
			proxyServiceInstances, _, err := consulClient.Catalog().Service(fmt.Sprintf("%s-sidecar-proxy", tt.consulSvcName), "", nil)
			require.NoError(t, err)
			require.Empty(t, proxyServiceInstances)

		})
	}
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

			consul.WaitForSerfCheck(t)
			consulClient, err := api.NewClient(&api.Config{
				Address: consul.HTTPAddr,
			})

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

func createPod(name, ip string, inject bool) *corev1.Pod {
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
			Phase:  corev1.PodRunning,
			Conditions: []corev1.PodCondition{{
				Type:    corev1.PodReady,
				Status:  corev1.ConditionFalse,
				Message: testFailureMessage,
			}},
		},
	}
	if inject {
		pod.Labels[annotationStatus] = injected
		pod.Annotations[annotationStatus] = injected
	}
	return pod
}

func toStringPtr(input string) *string {
	return &input
}
